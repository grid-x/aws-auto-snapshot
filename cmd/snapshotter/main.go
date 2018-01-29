package main

import (
	"context"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	awsdynamodb "github.com/aws/aws-sdk-go/service/dynamodb"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/lightsail"
	log "github.com/sirupsen/logrus"
	"gopkg.in/alecthomas/kingpin.v2"

	"github.com/grid-x/aws-auto-snapshot/pkg/datastore/dynamodb"
	snapec2 "github.com/grid-x/aws-auto-snapshot/pkg/snapshot/ec2"
	snaplightsail "github.com/grid-x/aws-auto-snapshot/pkg/snapshot/lightsail"
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
		logger = log.New().WithFields(log.Fields{"component": "main"})

		region          = kingpin.Flag("region", "AWS region to use").Default("eu-central-1").String()
		disablePrune    = kingpin.Flag("disable-prune", "Disable pruning of old snapshots").Default("false").Bool()
		disableSnapshot = kingpin.Flag("disable-snapshot", "Disable snapshot").Default("false").Bool()

		awsAccessKeyID     = kingpin.Flag("aws-access-key-id", "AWS Access Key ID to use").Required().String()
		awsSecretAccessKey = kingpin.Flag("aws-secret-access-key", "AWS Secret Access Key to use").Required().String()

		lightsailCmd = kingpin.Command("lightsail", "Run snapshotter for lightsail")
		retention    = lightsailCmd.Flag("retention", "Retention duration").Default("240h").Duration()

		ebsCmd           = kingpin.Command("ebs", "Run snapshotter for EBS")
		ebsBackupTag     = ebsCmd.Flag("ebs-backup-tag", "EBS tag that needs to be set for this EBS volume to be backed up").Default("backup").String()
		ebsRetentionTag  = ebsCmd.Flag("ebs-retention-tag", "EBS tag that indicates the number of retention days").Default("retention").String()
		ebsDynamodbTable = ebsCmd.Flag("dynamodb-table", "DynamoDB table to use for metadata storage").Required().String()
	)
	cmd := kingpin.Parse()

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
	ec2Client := ec2.New(sess)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var snaps []Snapshotter
	var err error
	switch cmd {
	case "lightsail":
		snaps, err = lightsailSnapshotter(ctx, logger, lightsailClient, *retention)
		if err != nil {
			logger.Fatal(err)
		}
	case "ebs":
		dydb := awsdynamodb.New(sess)
		dynamodbDs, err := dynamodb.New(dydb, *ebsDynamodbTable)
		if err != nil {
			logger.Fatalf("dynamodb.New: %+v", err)
		}

		snaps = []Snapshotter{
			snapec2.NewSnapshotManager(
				ec2Client,
				dynamodbDs,
				snapec2.WithRetentionTag(*ebsRetentionTag),
				snapec2.WithBackupTag(*ebsBackupTag),
			),
		}
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

}
