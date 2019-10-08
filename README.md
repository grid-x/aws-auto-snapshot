# aws-auto-snapshot

NOTE: As of October 2019 [AWS Lightsail supports automatic snapshots](https://aws.amazon.com/about-aws/whats-new/2019/10/amazon-lightsail-now-provides-automatic-snapshots/) with a seven 
day retention period. It is recommended to use the lightsail version instead of 
this tool if possible. Details can be found 
[here](https://lightsail.aws.amazon.com/ls/docs/en_us/articles/amazon-lightsail-configuring-automatic-snapshots).

aws-auto-snapshot is a set of tools that help to create snapshots for

* EBS volumes
* Lightsail instances

The so-called snapshotter lets you create those snapshots. By default it will
snapshot all running lightsail instances in the account and all EBS volumes that
have a special `backup` tag.

It can be configured how long snapshots are stored, i.e. when the tool will prune
them.

Generally, you will want to run the tool on a regular basis, e.g. once a day, via,
for example, a cron job. At gridX we run it as a cronjob in our Kubernetes cluster.

Metadata about each snapshot can be stored in a datastore. Currently, only DynamoDB
is supported as datastore.

If metadata was written to a datastore, this can be used to automatically restore
the latest snapshot of a resource. This is currently only supported for the EBS
volumes, though.

## Develop

```
# To build
# This will create a file called bin/snapshotter which is the above mentioned
# snapshotting tool
make

# To lint
make lint

# To test
make test
```
