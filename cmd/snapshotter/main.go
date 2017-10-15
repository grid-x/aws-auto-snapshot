package main

import (
	"context"
	"flag"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/lightsail"
	log "github.com/sirupsen/logrus"

	"github.com/grid-x/lightsail-auto-snapshot/pkg/snapshot"
)

func main() {

	var (
		logger = log.New().WithFields(log.Fields{"component": "main"})

		retention       = flag.Duration("retention", 10*24*time.Hour, "Retention duration")
		region          = flag.String("region", "eu-central-1", "AWS region to use")
		disablePrune    = flag.Bool("disable-prune", false, "Disable pruning of old snapshots")
		disableSnapshot = flag.Bool("disable-snapshot", false, "Disable snapshot")
	)
	flag.Parse()

	sess := session.New()
	client := lightsail.New(sess, aws.NewConfig().WithRegion(*region))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var instances []*lightsail.Instance
	var token *string
	for {
		in := &lightsail.GetInstancesInput{}
		if token != nil {
			in.PageToken = token
		}

		resp, err := client.GetInstancesWithContext(ctx, in)
		if err != nil {
			logger.Fatal(err)
		}
		instances = append(instances, resp.Instances...)

		if resp.NextPageToken == nil {
			break
		}
		token = resp.NextPageToken
	}

	for _, instance := range instances {
		if instance.Name == nil {
			//skip
			continue
		}
		logger.WithFields(log.Fields{"instance": *instance.Name}).Infof("Starting snapshot manager")
		smgr := snapshot.NewSnapshotManager(client, *instance.Name, snapshot.WithRetention(*retention))
		if !*disableSnapshot {
			if err := smgr.Snapshot(ctx); err != nil {
				logger.Error(err)
			} else {
				logger.WithFields(log.Fields{"instance": *instance.Name}).Infof("Snapshot successfull")
			}
		}
		if !*disablePrune {
			if err := smgr.Prune(ctx); err != nil {
				logger.Error(err)
			} else {
				logger.WithFields(log.Fields{"instance": *instance.Name}).Infof("Prune done")
			}
		}
	}
}
