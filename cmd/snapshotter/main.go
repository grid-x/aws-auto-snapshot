package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/credentials/stscreds"
	"github.com/aws/aws-sdk-go/aws/session"
	awsdynamodb "github.com/aws/aws-sdk-go/service/dynamodb"
	awsec2 "github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/lightsail"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/push"
	log "github.com/sirupsen/logrus"
	"gopkg.in/alecthomas/kingpin.v2"

	"github.com/grid-x/aws-auto-snapshot/pkg/datastore"
	"github.com/grid-x/aws-auto-snapshot/pkg/datastore/dynamodb"
	"github.com/grid-x/aws-auto-snapshot/pkg/snapshot/ec2"
	snaplightsail "github.com/grid-x/aws-auto-snapshot/pkg/snapshot/lightsail"
)

var (
	completionTime = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "aws_auto_snapshot_last_completion_timestamp_seconds",
		Help: "The timestamp of the last successful completion of a aws auto snapshotter run",
	})
)

// Snapshotter is the interface for snapshotable (is this even a word?!) and
// pruneable resources, i.e. a resource we can create a snapshot for and can
// prune the snapshot at
type Snapshotter interface {
	Snapshot(context.Context) error
	Prune(context.Context) error
}

func lightsailSnapshotter(ctx context.Context, logger log.FieldLogger,
	client *lightsail.Lightsail,
	retention time.Duration) ([]Snapshotter, error) {
	var result []Snapshotter
	var token *string
	for {
		in := &lightsail.GetInstancesInput{}
		if token != nil {
			in.PageToken = token
		}

		resp, err := client.GetInstancesWithContext(ctx, in)
		if err != nil {
			return nil, err
		}
		for _, instance := range resp.Instances {
			if instance.Name == nil {
				//skip
				continue
			}
			result = append(result, snaplightsail.NewSnapshotManager(client, *instance.Name, snaplightsail.WithRetention(retention)))
		}

		if resp.NextPageToken == nil {
			break
		}
		token = resp.NextPageToken
	}

	return result, nil
}

func main() {

	var (
		logger             = log.New()
		output             = kingpin.Flag("output", "Output format").Short('o').Default("").String()
		region             = kingpin.Flag("region", "AWS region to use").Default("eu-central-1").String()
		pushgatewayURL     = kingpin.Flag("pushgateway-url", "URL of Prometheus' pushgateway").String()
		awsAccessKeyID     = kingpin.Flag("aws-access-key-id", "AWS Access Key ID to use").Required().String()
		awsSecretAccessKey = kingpin.Flag("aws-secret-access-key", "AWS Secret Access Key to use").Required().String()

		snapshotCmd     = kingpin.Command("snapshot", "Snapshot a resource")
		disablePrune    = snapshotCmd.Flag("disable-prune", "Disable pruning of old snapshots").Default("false").Bool()
		disableSnapshot = snapshotCmd.Flag("disable-snapshot", "Disable snapshot").Default("false").Bool()

		lightsailCmd = snapshotCmd.Command("lightsail", "Run snapshotter for lightsail")
		retention    = lightsailCmd.Flag("retention", "Retention duration").Default("240h").Duration()

		ebsCmd           = snapshotCmd.Command("ebs", "Run snapshotter for EBS")
		ebsBackupTag     = ebsCmd.Flag("ebs-backup-tag", "EBS tag that needs to be set for this EBS volume to be backed up").Default("backup").String()
		ebsRetentionTag  = ebsCmd.Flag("ebs-retention-tag", "EBS tag that indicates the number of retention days").Default("retention").String()
		ebsDynamodbTable = ebsCmd.Flag("dynamodb-table", "DynamoDB table to use for metadata storage").Required().String()

		restoreCmd    = kingpin.Command("restore", "Restore a resource")
		restoreEBSCmd = restoreCmd.Command("ebs", "Restore from an EBS snapshot")

		restoreEBSSnapshotID         = restoreEBSCmd.Flag("from-snapshot", "Snapshot from to restore from").String()
		restoreEBSResource           = restoreEBSCmd.Flag("from-resource", "Resource to restore from").String()
		restoreEBSDynamoDBTable      = restoreEBSCmd.Flag("dynamodb-table", "DynamoDB Table used for storing snapshot infos").String()
		restoreEBSDynamoDBAssumeRole = restoreEBSCmd.Flag("dynamodb-assume-role", "ARN of the role to assume for accessing DynamoDB table").String()

		restoreEBSAZ        = restoreEBSCmd.Flag("availability-zone", "AZ to create volume in ").Required().String()
		restoreEBSSize      = restoreEBSCmd.Flag("size", "The size of the volume (default: the snapshot's size)").Int64()
		restoreEBSIOPS      = restoreEBSCmd.Flag("iops", "Only valid for Provisioned IOPS SSD volumes. The number of I/O operations per second (IOPS) to provision for the volume, with a maximum ratio of 50 IOPS/GiB. Constraint: Range is 100 to 20000 for Provisioned IOPS SSD volumes").Int64()
		restoreEBSType      = restoreEBSCmd.Flag("type", "The type of the volume. This can be gp2 for General Purpose SSD, io1 for Provisioned IOPS SSD, st1 for Throughput Optimized HDD, sc1 for Cold HDD, or standard for Magnetic volumes.").Default("").String()
		restoreEBSEncrypted = restoreEBSCmd.Flag("encrypted", "Encrypt volume").Default("false").Bool()
		restoreEBSKMSKeyID  = restoreEBSCmd.Flag("kms-key-id", "ARN of the KMS Key to use when encrypting (requires encrypt flag)").Default("").String()
	)
	cmd := kingpin.Parse()

	*output = strings.ToLower(*output)
	switch *output {
	case "json":
		logger.Level = log.ErrorLevel
		logger.Out = os.Stderr
	}

	creds := credentials.NewCredentials(&credentials.StaticProvider{
		Value: credentials.Value{
			AccessKeyID:     *awsAccessKeyID,
			SecretAccessKey: *awsSecretAccessKey,
		},
	})

	sess := session.New(aws.NewConfig().
		WithCredentials(creds).
		WithRegion(*region),
	)
	lightsailClient := lightsail.New(sess)
	ec2Client := awsec2.New(sess)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var snaps []Snapshotter
	var err error
	switch cmd {
	case "snapshot lightsail":
		snaps, err = lightsailSnapshotter(ctx, logger, lightsailClient, *retention)
		if err != nil {
			logger.Fatal(err)
		}
	case "snapshot ebs":
		dydb := awsdynamodb.New(sess)
		dynamodbDs, err := dynamodb.New(dydb, *ebsDynamodbTable)
		if err != nil {
			logger.Fatalf("dynamodb.New: %+v", err)
		}

		snaps = []Snapshotter{
			ec2.NewSnapshotManager(
				ec2Client,
				dynamodbDs,
				ec2.WithRetentionTag(*ebsRetentionTag),
				ec2.WithBackupTag(*ebsBackupTag),
			),
		}
	case "restore ebs":
		var snapshot string
		if *restoreEBSResource == "" && *restoreEBSSnapshotID == "" {
			logger.Fatal("need either snapshotID or resource")
		}
		if *restoreEBSResource != "" {
			conf := &aws.Config{}
			if *restoreEBSDynamoDBAssumeRole != "" {
				conf.Credentials = stscreds.NewCredentials(sess, *restoreEBSDynamoDBAssumeRole)
			}
			dydb := awsdynamodb.New(sess, conf)
			if *restoreEBSDynamoDBTable == "" {
				logger.Fatal("need to dynamodb table to retrieve snapshot infos from")
			}
			dynamodbDs, err := dynamodb.New(dydb, *restoreEBSDynamoDBTable)
			info, err := dynamodbDs.GetLatestSnapshotInfo(datastore.SnapshotResource(*restoreEBSResource))
			if err != nil {
				logger.Fatalf("getLatestSnapshotInfo: %+v", err)
			}
			snapshot = string(info.ID)
		} else {
			snapshot = *restoreEBSSnapshotID
		}

		var opts []ec2.RestoreOption
		if *restoreEBSSize > 0 {
			logger.Infof("setting size to: %d", *restoreEBSSize)
			opts = append(opts, ec2.RestoreWithSize(*restoreEBSSize))
		}
		if *restoreEBSIOPS > 0 {
			logger.Infof("setting iops to: %d", *restoreEBSIOPS)
			opts = append(opts, ec2.RestoreWithIOPS(*restoreEBSIOPS))
		}
		if *restoreEBSType != "" {
			logger.Infof("setting volume type to: %s", *restoreEBSType)
			opts = append(opts, ec2.RestoreWithType(*restoreEBSType))
		}
		logger.Infof("setting encryption to: %t", *restoreEBSEncrypted)
		opts = append(opts, ec2.RestoreWithEncrypted(*restoreEBSEncrypted))
		if *restoreEBSKMSKeyID != "" {
			logger.Infof("setting encryption to true with KMS key: %s", *restoreEBSKMSKeyID)
			opts = append(opts, ec2.RestoreWithEncrypted(true), ec2.RestoreWithKMSKeyID(*restoreEBSKMSKeyID))
		}

		logger.Infof("running restore manager for snapshot %s in AZ %s", snapshot, *restoreEBSAZ)
		if volumeID, err := ec2.NewRestoreManager(ec2Client, snapshot, *restoreEBSAZ, opts...).Run(ctx); err != nil {
			logger.Errorf("restoreManager: %+v", err)
		} else {
			switch *output {
			case "json":
				fmt.Printf("{ \"volumeID\": \"%s\"}", volumeID)
			default:
				fmt.Printf("created volume with ID: %s\n", volumeID)
			}
		}
		return
	default:
		logger.Fatalf("Invalid command %q", cmd)
	}

	for _, s := range snaps {
		if !*disableSnapshot {
			logger.Infof("Trying to snapshot")
			if err := s.Snapshot(ctx); err != nil {
				logger.Error(err)
			}
		}
		if !*disablePrune {
			logger.Infof("Trying to Prune")
			if err := s.Prune(ctx); err != nil {
				logger.Error(err)
			}
		}
	}

	if *pushgatewayURL != "" {
		completionTime.SetToCurrentTime()
		if err := push.AddFromGatherer(
			"aws_auto_snapshot",
			nil,
			*pushgatewayURL,
			prometheus.DefaultGatherer,
		); err != nil {
			logger.Errorf("cannot push metrics to pushgateway at %s: %+v", *pushgatewayURL, err)
		}
	}

}
