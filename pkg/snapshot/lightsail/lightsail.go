package lightsail

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/lightsail"
	"github.com/prometheus/client_golang/prometheus"
	log "github.com/sirupsen/logrus"
)

const (
	defaultSnapshotSuffix = "auto-snapshot"
)

var (
	defaultRetention = 10 * 24 * time.Hour

	createInstanceSnapshotRequest = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "lightsail_create_instance_snapshot_requests_total",
		Help: "Total number of create instance snapshot requests",
	})
	getInstanceSnapshotRequest = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "lightsail_get_instance_snapshot_requests_total",
		Help: "Total number of get instance snapshot requests",
	})
	deleteInstanceSnapshotRequest = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "lightsail_delete_instance_snapshot_requests_total",
		Help: "Total number of delete instance snapshot requests",
	})
)

func init() {
	prometheus.MustRegister(createInstanceSnapshotRequest)
	prometheus.MustRegister(getInstanceSnapshotRequest)
	prometheus.MustRegister(deleteInstanceSnapshotRequest)
}

// SnapshotManager manages the snapshots of a single lightsail instance
type SnapshotManager struct {
	client   *lightsail.Lightsail
	instance string // instance name

	retention time.Duration // retention time
	suffix    string        // snapshot suffix

	logger log.FieldLogger
}

// Opt represents Options that can be passed to the SnapshotManager
type Opt func(*SnapshotManager)

// WithRetention set the retention duration
func WithRetention(r time.Duration) Opt {
	return func(m *SnapshotManager) {
		m.retention = r
	}
}

// WithSnapshotSuffix sets the suffix of the automated snapshots
func WithSnapshotSuffix(suf string) Opt {
	return func(m *SnapshotManager) {
		m.suffix = suf
	}
}

// NewSnapshotManager creates a new SnapshotManager for an instance  given an
// lightsail client and a set of Opts
func NewSnapshotManager(client *lightsail.Lightsail, instance string, opts ...Opt) *SnapshotManager {
	smgr := &SnapshotManager{
		client:   client,
		instance: instance,

		retention: defaultRetention,
		suffix:    defaultSnapshotSuffix,

		logger: log.New().WithFields(
			log.Fields{
				"component": "snapshot-manager",
				"instance":  instance,
			}),
	}

	for _, o := range opts {
		o(smgr)
	}

	return smgr
}

// Snapshot creates a snapshots for the Lightsail instance this SnapshotManager
// belongs to
func (smgr *SnapshotManager) Snapshot(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	snapshotName := fmt.Sprintf("%s-%d-%s",
		smgr.instance,
		time.Now().UnixNano(),
		smgr.suffix,
	)
	smgr.logger.Infof("Creating snapshot with name %s", snapshotName)
	// TODO: Check for errors in response
	_, err := smgr.client.CreateInstanceSnapshotWithContext(
		ctx,
		&lightsail.CreateInstanceSnapshotInput{
			InstanceName:         aws.String(smgr.instance),
			InstanceSnapshotName: aws.String(snapshotName),
		},
	)
	createInstanceSnapshotRequest.Inc()
	return err
}

// Prune deletes old snapshots of the lightsail instance belonging to the
// SnapshotManager
func (smgr *SnapshotManager) Prune(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	var snapshots []*lightsail.InstanceSnapshot
	var token *string

	for {
		in := &lightsail.GetInstanceSnapshotsInput{}
		if token != nil {
			in.PageToken = token
		}
		resp, err := smgr.client.GetInstanceSnapshotsWithContext(ctx, in)
		if err != nil {
			return err
		}
		getInstanceSnapshotRequest.Inc()

		for _, snapshot := range resp.InstanceSnapshots {

			// Only use snapshots from the current instance
			if snapshot.FromInstanceName == nil ||
				*snapshot.FromInstanceName != smgr.instance {
				continue
			}
			// Filter out snapshots not created by this tool
			if !strings.HasSuffix(*snapshot.Name, smgr.suffix) {
				continue
			}

			snapshots = append(snapshots, snapshot)
		}
		if resp.NextPageToken == nil {
			break
		}
		token = resp.NextPageToken
	}

	for _, snapshot := range snapshots {
		if snapshot.CreatedAt == nil {
			//skip
			continue
		}

		if snapshot.CreatedAt.After(time.Now().Add(-smgr.retention)) {
			// Snapshot is not yet old enough
			smgr.logger.Debugf("Snapshot %s not old enough", *snapshot.Name)
			continue
		}
		smgr.logger.Infof("Deleting snapshot %s", *snapshot.Name)
		_, err := smgr.client.DeleteInstanceSnapshotWithContext(
			ctx,
			&lightsail.DeleteInstanceSnapshotInput{
				InstanceSnapshotName: snapshot.Name,
			})
		if err != nil {
			smgr.logger.Error(err)
		}
		deleteInstanceSnapshotRequest.Inc()
	}

	return nil
}
