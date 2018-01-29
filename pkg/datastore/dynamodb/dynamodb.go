package dynamodb

import (
	"fmt"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	awsdynamodb "github.com/aws/aws-sdk-go/service/dynamodb"
	"github.com/aws/aws-sdk-go/service/dynamodb/dynamodbattribute"
	log "github.com/sirupsen/logrus"

	"github.com/grid-x/aws-auto-snapshot/pkg/datastore"
)

const (
	primaryKey = "snapshot_resource"
	rangeKey   = "created_at"
)

// DynamoDB represents a datastore that uses dynamodb under the hood
type DynamoDB struct {
	table  string
	client *awsdynamodb.DynamoDB

	logger log.FieldLogger
}

type item struct {
	Resource  string            `dynamodbav:"snapshot_resource"`
	CreatedAt int64             `dynamodbav:"created_at"`
	ID        string            `dynamodbav:"snap_id"`
	Labels    map[string]string `dynamodbav:"labels"`
}

// New creates a new DynamoDB-based datastore
func New(client *awsdynamodb.DynamoDB, table string) *DynamoDB {
	return &DynamoDB{
		table:  table,
		client: client,
		logger: log.New().WithFields(log.Fields{
			"component": "datastore",
			"datastore": "dynamodb",
		}),
	}
}

// StoreSnapshotInfo stores the given snapshot info in the datastore
func (d *DynamoDB) StoreSnapshotInfo(info *datastore.SnapshotInfo) error {

	record := &item{
		Resource:  string(info.Resource),
		ID:        string(info.ID),
		CreatedAt: info.CreatedAt.Unix(),
		Labels:    (map[string]string)(info.Labels),
	}

	logger := d.logger.WithFields(log.Fields{
		"resource":    string(info.Resource),
		"snapshot-id": string(info.ID),
	})

	av, err := dynamodbattribute.MarshalMap(record)
	if err != nil {
		return err
	}

	logger.Info("trying to put item into dynamodb table...")
	_, err = d.client.PutItem(&awsdynamodb.PutItemInput{
		TableName: aws.String(d.table),
		Item:      av,
	})
	if err != nil {
		return err
	}
	logger.Info("successfully added item to table")
	return nil
}

// GetLatestSnapshotInfo returns the latest snapshot info found in the datastore
func (d *DynamoDB) GetLatestSnapshotInfo(resource datastore.SnapshotResource) (*datastore.SnapshotInfo, error) {
	logger := d.logger.WithFields(log.Fields{
		"resource": string(resource),
	})
	logger.Info("Trying to get latest snapshot info...")
	out, err := d.client.Query(&awsdynamodb.QueryInput{
		TableName: aws.String(d.table),
		KeyConditionExpression: aws.String(
			primaryKey + " = :snapshot_resource and " + rangeKey + " >= :created_at",
		),
		ExpressionAttributeValues: map[string]*awsdynamodb.AttributeValue{
			":snapshot_resource": {
				S: aws.String(string(resource)),
			},
			":created_at": {
				N: aws.String(strconv.FormatInt(time.Now().Add(-2*24*time.Hour).Unix(), 10)),
			},
		},
	})
	if err != nil {
		return nil, err
	}

	var items []*item
	if err := dynamodbattribute.UnmarshalListOfMaps(out.Items, &items); err != nil {
		return nil, err
	}

	if len(items) <= 0 {
		return nil, fmt.Errorf("No items found")
	}

	logger.Info("found latest snapshot info...")

	last := items[len(items)-1]
	return &datastore.SnapshotInfo{
		Resource:  datastore.SnapshotResource(last.Resource),
		ID:        datastore.SnapshotID(last.ID),
		CreatedAt: time.Unix(last.CreatedAt, 0),
		Labels:    datastore.SnapshotLabels(last.Labels),
	}, nil
}
