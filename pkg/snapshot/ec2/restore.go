package ec2

import (
	"context"

	"github.com/aws/aws-sdk-go/aws"
	awsec2 "github.com/aws/aws-sdk-go/service/ec2"
)

// RestoreManager manages a restore operation from an EBS snapshot
type RestoreManager struct {
	client *awsec2.EC2

	snapshotID string
	az         string
	iops, size *int64
	encrypted  bool
	kmsKeyID   *string
	volumeType *string
}

// RestoreOption is an option passed to the RestoreManager
type RestoreOption func(*RestoreManager)

// RestoreWithSize sets the size of the volume being restored
func RestoreWithSize(v int64) RestoreOption {
	return func(mgr *RestoreManager) {
		mgr.size = new(int64)
		*mgr.size = v
	}
}

// RestoreWithIOPS sets the IOPS to be provisioned for the EBS volume
func RestoreWithIOPS(v int64) RestoreOption {
	return func(mgr *RestoreManager) {
		mgr.iops = new(int64)
		*mgr.iops = v
	}
}

// RestoreWithType sets the type of the volume
func RestoreWithType(t string) RestoreOption {
	return func(mgr *RestoreManager) {
		mgr.volumeType = new(string)
		*mgr.volumeType = t
	}
}

// RestoreWithEncrypted sets whether the volume should be encrypted
func RestoreWithEncrypted(enc bool) RestoreOption {
	return func(mgr *RestoreManager) {
		mgr.encrypted = enc
	}
}

// RestoreWithKMSKeyID sets the id of KMS key to be used
func RestoreWithKMSKeyID(id string) RestoreOption {
	return func(mgr *RestoreManager) {
		mgr.kmsKeyID = new(string)
		*mgr.kmsKeyID = id
	}
}

// NewRestoreManager creates a new RestoreManager with the given settings
func NewRestoreManager(client *awsec2.EC2, snapshotID, az string, opts ...RestoreOption) *RestoreManager {
	mgr := &RestoreManager{
		client:     client,
		snapshotID: snapshotID,
		az:         az,
		encrypted:  false,
	}

	for _, opt := range opts {
		opt(mgr)
	}

	return mgr
}

// Run will perform the actual request and restore the volume
func (mgr *RestoreManager) Run(ctx context.Context) (string, error) {
	input := &awsec2.CreateVolumeInput{
		AvailabilityZone: aws.String(mgr.az),
		SnapshotId:       aws.String(mgr.snapshotID),
	}

	if mgr.size != nil && *mgr.size > 0 {
		input.Size = aws.Int64(*mgr.size)
	}

	if mgr.iops != nil && *mgr.iops > 0 {
		input.Iops = aws.Int64(*mgr.iops)
	}

	if mgr.volumeType != nil && *mgr.volumeType != "" {
		input.VolumeType = aws.String(*mgr.volumeType)
	}

	if mgr.encrypted {
		input.Encrypted = aws.Bool(mgr.encrypted)
		if mgr.kmsKeyID != nil && *mgr.kmsKeyID != "" {
			input.KmsKeyId = aws.String(*mgr.kmsKeyID)
		}
	}
	out, err := mgr.client.CreateVolumeWithContext(ctx, input)
	if err != nil {
		return "", err
	}
	return *out.VolumeId, nil
}
