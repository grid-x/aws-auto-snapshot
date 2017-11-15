package ec2

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ec2"
	log "github.com/sirupsen/logrus"
)

const (
	defaultBackupTag    = "backup"
	defaultRetentionTag = "retention"

	defaultSnapshotSuffix = "auto-snapshot"
	defaultDeleteAfterTag = "_DELETE_AFTER"

	defaultRetentionDays = 7 // Default are 7 days retention
	defaultDescription   = "auto snapshot created by grid-x/aws-auto-snapshot"
)

// SnapshotManager manages the snapshot creation and pruning of EC2 EBS-based
// snapshots
type SnapshotManager struct {
	client   *ec2.EC2
	volumeID string

	suffix         string // snapshot suffix
	backupTag      string
	retentionTag   string
	deleteAfterTag string

	logger log.FieldLogger
}

// Opt is the type for Options of the SnapshotManager
type Opt func(*SnapshotManager)

// WithRetentionTag sets the retention tag key
func WithRetentionTag(t string) Opt {
	return func(m *SnapshotManager) {
		m.retentionTag = t
	}
}

// WithBackupTag sets the backup tag key
func WithBackupTag(t string) Opt {
	return func(m *SnapshotManager) {
		m.backupTag = t
	}
}

// WithSnapshotSuffix sets the automated snapshot suffix
func WithSnapshotSuffix(suf string) Opt {
	return func(m *SnapshotManager) {
		m.suffix = suf
	}
}

// WithDeleteAfterTag sets the tag key to be used for indication the deletion
// date
func WithDeleteAfterTag(tag string) Opt {
	return func(m *SnapshotManager) {
		m.deleteAfterTag = tag
	}
}

// NewSnapshotManager creates a new SnapshotManager given an EC2 client and a
// set of Opts
func NewSnapshotManager(client *ec2.EC2, opts ...Opt) *SnapshotManager {
	smgr := &SnapshotManager{
		client: client,

		suffix:         defaultSnapshotSuffix,
		retentionTag:   defaultRetentionTag,
		backupTag:      defaultBackupTag,
		deleteAfterTag: defaultDeleteAfterTag,

		logger: log.New().WithFields(
			log.Fields{
				"component": "ec2-snapshot-manager",
			}),
	}

	for _, o := range opts {
		o(smgr)
	}

	return smgr
}

func (smgr *SnapshotManager) fetchVolumes(ctx context.Context) ([]*ec2.Volume, error) {
	var result []*ec2.Volume
	var token *string
	for {
		in := &ec2.DescribeVolumesInput{}
		if token != nil {
			in.NextToken = token
		}

		// Filter so we get only volumes that have the Backup tag set
		in.SetFilters([]*ec2.Filter{
			{
				Name: aws.String("tag-key"),
				Values: []*string{
					aws.String(smgr.backupTag),
					aws.String(strings.ToLower(smgr.backupTag)), // we are not case sensitive
				},
			},
		})

		resp, err := smgr.client.DescribeVolumesWithContext(ctx, in)
		if err != nil {
			return nil, err
		}
		for _, volume := range resp.Volumes {
			if volume.VolumeId == nil {
				//skip
				continue
			}
			result = append(result, volume)
		}

		if resp.NextToken == nil {
			break
		}
		token = resp.NextToken
	}

	return result, nil
}

func (smgr *SnapshotManager) fetchSnapshots(ctx context.Context) ([]*ec2.Snapshot, error) {
	var result []*ec2.Snapshot
	var token *string
	for {
		in := &ec2.DescribeSnapshotsInput{}
		if token != nil {
			in.NextToken = token
		}

		// Filter so we get only volumes that have the Backup tag set
		in.SetFilters([]*ec2.Filter{
			{
				Name: aws.String("tag-key"),
				Values: []*string{
					aws.String(smgr.deleteAfterTag),
				},
			},
		})

		resp, err := smgr.client.DescribeSnapshotsWithContext(ctx, in)
		if err != nil {
			return nil, err
		}
		for _, snap := range resp.Snapshots {
			if snap.SnapshotId == nil {
				//skip
				continue
			}
			result = append(result, snap)
		}

		if resp.NextToken == nil {
			break
		}
		token = resp.NextToken
	}

	return result, nil
}

// Snapshot creates EBS snapshots for all matching EBS volumes, i.e. all EBS
// volumes having a Backup tag and optionally a retention tag set
func (smgr *SnapshotManager) Snapshot(ctx context.Context) error {

	volumes, err := smgr.fetchVolumes(ctx)
	if err != nil {
		return err
	}

	for _, volume := range volumes {
		// For each volume it should at most take 5 minutes
		ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
		defer cancel()

		snapshotName := fmt.Sprintf("%s-%d-%s",
			volume.VolumeId,
			time.Now().UnixNano(),
			smgr.suffix,
		)

		logger := smgr.logger.WithFields(
			log.Fields{
				"volume-id":     volume.VolumeId,
				"snapshot-name": snapshotName,
			},
		)

		var days int64
		for _, tag := range volume.Tags {
			if tag.Key == nil {
				continue
			}
			if strings.ToLower(*tag.Key) == strings.ToLower(smgr.retentionTag) {
				if tag.Value == nil {
					logger.Warnf("Retention tag value is nil")
					continue
				}
				days, err = strconv.ParseInt(*tag.Value, 10, 64)
				if err != nil {
					logger.Warnf("Couldn't parse retention days: %+v. Falling back to default value", err)
					days = defaultRetentionDays // if error occurs fall back to default retention time
				}
				break
			}
		}

		deleteAfter := time.Now().Add(time.Duration(days) * 24 * time.Hour)

		logger.Infof("Creating snapshot with name %s", snapshotName)
		snapshot, err := smgr.client.CreateSnapshotWithContext(
			ctx,
			&ec2.CreateSnapshotInput{
				Description: aws.String(defaultDescription),
			},
		)
		if err != nil {
			logger.Error(err)
			continue
		}

		if snapshot.SnapshotId == nil {
			logger.Errorf("Snapshot ID is nil.")
			continue
		}

		if _, err := smgr.client.CreateTagsWithContext(
			ctx,
			&ec2.CreateTagsInput{
				Resources: []*string{
					snapshot.SnapshotId,
				},
				Tags: []*ec2.Tag{
					{
						Key:   aws.String("name"),
						Value: aws.String(snapshotName),
					},
					{
						Key:   aws.String(smgr.deleteAfterTag),
						Value: aws.String(deleteAfter.Format(time.RFC3339)),
					},
				},
			},
		); err != nil {
			logger.Error(err)
			continue
		}
	}
	return nil
}

// Prune deletes all matching EBS snapshots, i.e. snapshots with a delete after
// tag that is set to a date in the past
func (smgr *SnapshotManager) Prune(ctx context.Context) error {

	snaps, err := smgr.fetchSnapshots(ctx)
	if err != nil {
		return err
	}
	for _, snap := range snaps {
		for _, tag := range snap.Tags {
			if tag.Key == nil {
				continue
			}
			if *tag.Key == smgr.deleteAfterTag {
				// add context to the logger
				logger := smgr.logger.WithFields(log.Fields{
					"snapshotID": snap.SnapshotId,
				})
				if tag.Value == nil {
					logger.Errorf("Delete after tag value is nil")
					continue
				}

				deleteAfter, err := time.Parse(time.RFC3339, *tag.Value)
				if err != nil {
					logger.Error("Couldn't parse tag value for : %+v", err)
					break
				}
				if deleteAfter.Before(time.Now()) {
					// snapshot not yet scheduled for deletion
					break
				}
				if _, err := smgr.client.DeleteSnapshotWithContext(ctx, &ec2.DeleteSnapshotInput{
					SnapshotId: snap.SnapshotId,
				}); err != nil {
					logger.Error("Couldn't delete snapshot: %+v", err)
				}
			}
		}
	}

	return nil
}
