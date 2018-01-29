package datastore

import (
	"time"
)

// SnapshotResource describes which resource this snapshot belongs to, e.g. if
// we consider a EBS volume the name or ID of the volume would be the snapshot
// resource
type SnapshotResource string

// SnapshotID is the actual ID of the snapshot created
type SnapshotID string

// SnapshotLabels represent arbitrary labels added to a snapshot
type SnapshotLabels map[string]string

// SnapshotInfo describes meta infos of a snapshot
type SnapshotInfo struct {
	Resource  SnapshotResource
	ID        SnapshotID
	CreatedAt time.Time
	Labels    SnapshotLabels
}

// Datastore describes the interface needed by a storage for snapshot info
type Datastore interface {
	StoreSnapshotInfo(*SnapshotInfo) error
	GetLatestSnapshotInfo(SnapshotResource) (*SnapshotInfo, error)
}
