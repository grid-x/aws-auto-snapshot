package dynamodb_test

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/session"
	awsdynamodb "github.com/aws/aws-sdk-go/service/dynamodb"
	"github.com/google/go-cmp/cmp"

	"github.com/grid-x/aws-auto-snapshot/pkg/datastore"
	"github.com/grid-x/aws-auto-snapshot/pkg/datastore/dynamodb"
)

const (
	testTablePrefix = "Snapshots_test"
	region          = "eu-central-1"
)

func createTestTable(client *awsdynamodb.DynamoDB, prefix string) (string, error) {
	tableName := fmt.Sprintf("%s_%d", prefix, time.Now().Unix())
	if _, err := client.CreateTable(&awsdynamodb.CreateTableInput{
		TableName: aws.String(tableName),
		ProvisionedThroughput: &awsdynamodb.ProvisionedThroughput{
			ReadCapacityUnits:  aws.Int64(1),
			WriteCapacityUnits: aws.Int64(1),
		},
		AttributeDefinitions: []*awsdynamodb.AttributeDefinition{
			{
				AttributeName: aws.String("snapshot_resource"),
				AttributeType: aws.String("S"),
			},
			{
				AttributeName: aws.String("created_at"),
				AttributeType: aws.String("N"),
			},
		},
		KeySchema: []*awsdynamodb.KeySchemaElement{
			{
				AttributeName: aws.String("snapshot_resource"),
				KeyType:       aws.String("HASH"),
			},
			{
				AttributeName: aws.String("created_at"),
				KeyType:       aws.String("RANGE"),
			},
		},
	}); err != nil {
		return "", err
	}
	return tableName, nil
}

func deleteTestTable(client *awsdynamodb.DynamoDB, tableName string) error {
	_, err := client.DeleteTable(&awsdynamodb.DeleteTableInput{
		TableName: aws.String(tableName),
	})
	return err
}

func waitForTable(client *awsdynamodb.DynamoDB, tableName string) error {
	for {
		out, err := client.DescribeTable(&awsdynamodb.DescribeTableInput{
			TableName: aws.String(tableName),
		})
		if err != nil {
			if aerr, ok := err.(awserr.Error); ok {
				switch aerr.Code() {
				case awsdynamodb.ErrCodeResourceNotFoundException:
					time.Sleep(1 * time.Second)
					continue
				default:
					return err
				}
			} else {
				return err
			}
		}
		if *out.Table.TableStatus == "ACTIVE" {
			return nil
		}
		time.Sleep(1 * time.Second)
	}
}

func Test_StoreSnapshotAndGetLatest(t *testing.T) {

	if ci := os.Getenv("CI"); ci != "" {
		t.Skip()
	}

	now := time.Now()

	testcases := []struct {
		snapshots []*datastore.SnapshotInfo
		resource  string
		want      *datastore.SnapshotInfo
	}{
		{
			snapshots: []*datastore.SnapshotInfo{
				{
					Resource:  "vol-123abcdefghi",
					ID:        "snap-abc00000000",
					CreatedAt: now.Add(-1 * time.Hour).Truncate(time.Second),
					Labels: datastore.SnapshotLabels{
						"origin": "influxdb-data",
					},
				},
				{
					Resource:  "vol-123abcdefghi",
					ID:        "snap-abc00000001",
					CreatedAt: now.Add(-30 * time.Minute).Truncate(time.Second),
					Labels: datastore.SnapshotLabels{
						"origin": "influxdb-data",
					},
				},
				{
					Resource:  "vol-456abdefghi",
					ID:        "snap-abc00000001",
					CreatedAt: now.Add(-1 * time.Hour).Truncate(time.Second),
				},
				{
					Resource:  "vol-456abdefghi",
					ID:        "snap-abc00000001",
					CreatedAt: now.Add(-10 * time.Hour).Truncate(time.Second),
				},
				{
					Resource:  "vol-456abdefghi",
					ID:        "snap-abc00000011",
					CreatedAt: now.Add(-15 * time.Hour).Truncate(time.Second),
				},
			},
			resource: "vol-456abdefghi",
			want: &datastore.SnapshotInfo{
				Resource:  "vol-456abdefghi",
				ID:        "snap-abc00000001",
				CreatedAt: now.Add(-1 * time.Hour).Truncate(time.Second),
			},
		},
		{
			snapshots: []*datastore.SnapshotInfo{
				{
					Resource:  "vol-123abcdefghi",
					ID:        "snap-abc00000000",
					CreatedAt: now.Add(-1 * time.Hour).Truncate(time.Second),
					Labels: datastore.SnapshotLabels{
						"origin": "influxdb-data",
					},
				},
				{
					Resource:  "vol-123abcdefghi",
					ID:        "snap-abc00000001",
					CreatedAt: now.Add(-30 * time.Minute).Truncate(time.Second),
					Labels: datastore.SnapshotLabels{
						"origin": "influxdb-data",
					},
				},
				{
					Resource:  "vol-456abdefghi",
					ID:        "snap-abc00000001",
					CreatedAt: now.Add(-1 * time.Hour).Truncate(time.Second),
				},
				{
					Resource:  "vol-456abdefghi",
					ID:        "snap-abc00000001",
					CreatedAt: now.Add(-10 * time.Hour).Truncate(time.Second),
				},
				{
					Resource:  "vol-456abdefghi",
					ID:        "snap-abc00000011",
					CreatedAt: now.Add(-15 * time.Hour).Truncate(time.Second),
				},
			},
			resource: "vol-123abcdefghi",
			want: &datastore.SnapshotInfo{
				Resource:  "vol-123abcdefghi",
				ID:        "snap-abc00000001",
				CreatedAt: now.Add(-30 * time.Minute).Truncate(time.Second),
				Labels: datastore.SnapshotLabels{
					"origin": "influxdb-data",
				},
			},
		},
	}

	client := awsdynamodb.New(session.New(aws.NewConfig().WithRegion(region)))
	t.Log("creating test table")
	testTable, err := createTestTable(client, testTablePrefix+"_latest")
	if err != nil {
		t.Fatalf("createTestTable: %+v", err)
	}
	if err := waitForTable(client, testTable); err != nil {
		t.Fatalf("waitForTable: %+v", err)
	}
	t.Log("created test table")
	defer func() {
		t.Log("deleting test table again")
		deleteTestTable(client, testTable)
	}()
	ddb, err := dynamodb.New(client, testTable)
	if err != nil {
		t.Fatalf("new: %+v", err)
	}

	t.Log("Starting to run testcases")
	for i, tc := range testcases {
		t.Run(fmt.Sprintf("%d", i), func(t *testing.T) {
			for _, snap := range tc.snapshots {
				if err := ddb.StoreSnapshotInfo(snap); err != nil {
					t.Fatalf("storeSnapshotInfo: %+v", err)
				}
				time.Sleep(1 * time.Second)
			}

			got, err := ddb.GetLatestSnapshotInfo(datastore.SnapshotResource(tc.resource))
			if err != nil {
				t.Fatalf("getLatestSnapshotInfo: %+v", err)
			}
			if !cmp.Equal(tc.want, got) {
				t.Errorf("getLatestSnapshotInfo unexpected output: %s", cmp.Diff(tc.want, got))
			}
		})
	}
}

func Test_DeleteSnapshotInfo(t *testing.T) {

	if ci := os.Getenv("CI"); ci != "" {
		t.Skip()
	}

	now := time.Now()

	testcases := []struct {
		snapshots []*datastore.SnapshotInfo
		toDelete  *datastore.SnapshotInfo
	}{
		{
			snapshots: []*datastore.SnapshotInfo{
				{
					Resource:  "vol-123abcdefghi",
					ID:        "snap-abc00000000",
					CreatedAt: now.Add(-1 * time.Hour).Truncate(time.Second),
					Labels: datastore.SnapshotLabels{
						"origin": "influxdb-data",
					},
				},
				{
					Resource:  "vol-123abcdefghi",
					ID:        "snap-abc00000001",
					CreatedAt: now.Add(-30 * time.Minute).Truncate(time.Second),
					Labels: datastore.SnapshotLabels{
						"origin": "influxdb-data",
					},
				},
				{
					Resource:  "vol-456abdefghi",
					ID:        "snap-abc00000001",
					CreatedAt: now.Add(-1 * time.Hour).Truncate(time.Second),
				},
				{
					Resource:  "vol-456abdefghi",
					ID:        "snap-abc00000001",
					CreatedAt: now.Add(-10 * time.Hour).Truncate(time.Second),
				},
				{
					Resource:  "vol-456abdefghi",
					ID:        "snap-abc00000011",
					CreatedAt: now.Add(-15 * time.Hour).Truncate(time.Second),
				},
			},
			toDelete: &datastore.SnapshotInfo{
				Resource: "vol-456abdefghi",
				ID:       "snap-abc00000001",
			},
		},
		{
			snapshots: []*datastore.SnapshotInfo{
				{
					Resource:  "vol-123abcdefghi",
					ID:        "snap-abc00000000",
					CreatedAt: now.Add(-1 * time.Hour).Truncate(time.Second),
					Labels: datastore.SnapshotLabels{
						"origin": "influxdb-data",
					},
				},
				{
					Resource:  "vol-123abcdefghi",
					ID:        "snap-abc00000001",
					CreatedAt: now.Add(-30 * time.Minute).Truncate(time.Second),
					Labels: datastore.SnapshotLabels{
						"origin": "influxdb-data",
					},
				},
				{
					Resource:  "vol-456abdefghi",
					ID:        "snap-abc00000001",
					CreatedAt: now.Add(-1 * time.Hour).Truncate(time.Second),
				},
				{
					Resource:  "vol-456abdefghi",
					ID:        "snap-abc00000001",
					CreatedAt: now.Add(-10 * time.Hour).Truncate(time.Second),
				},
				{
					Resource:  "vol-456abdefghi",
					ID:        "snap-abc00000011",
					CreatedAt: now.Add(-15 * time.Hour).Truncate(time.Second),
				},
			},
			toDelete: &datastore.SnapshotInfo{
				Resource:  "vol-123abcdefghi",
				ID:        "snap-abc00000001",
				CreatedAt: now.Add(-30 * time.Minute).Truncate(time.Second),
			},
		},
	}

	client := awsdynamodb.New(session.New(aws.NewConfig().WithRegion(region)))
	t.Log("creating test table")
	testTable, err := createTestTable(client, testTablePrefix+"_delete")
	if err != nil {
		t.Fatalf("createTestTable: %+v", err)
	}
	if err := waitForTable(client, testTable); err != nil {
		t.Fatalf("waitForTable: %+v", err)
	}
	t.Log("created test table")
	defer func() {
		t.Log("deleting test table again")
		//deleteTestTable(client, testTable)
	}()
	ddb, err := dynamodb.New(client, testTable)
	if err != nil {
		t.Fatalf("new: %+v", err)
	}

	t.Log("Starting to run testcases")
	for i, tc := range testcases {
		t.Run(fmt.Sprintf("%d", i), func(t *testing.T) {
			for _, snap := range tc.snapshots {
				if err := ddb.StoreSnapshotInfo(snap); err != nil {
					t.Fatalf("storeSnapshotInfo: %+v", err)
				}
				time.Sleep(1 * time.Second)
			}

			err := ddb.DeleteSnapshotInfo(tc.toDelete)
			if err != nil {
				t.Fatalf("deleteSnapshotInfo: %+v", err)
			}
		})
	}
}
