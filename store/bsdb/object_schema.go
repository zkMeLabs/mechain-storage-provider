package bsdb

import (
	"encoding/json"

	"github.com/forbole/juno/v4/common"
	"github.com/lib/pq"
	"gorm.io/datatypes"

	storagetypes "github.com/evmos/evmos/v12/x/storage/types"
	"github.com/zkMeLabs/mechain-storage-provider/pkg/log"
)

// Object is the structure for user object
type Object struct {
	// ID defines db auto_increment id of object
	ID uint64 `gorm:"id"`
	// Creator defines the account address of object creator
	Creator common.Address `gorm:"creator"`
	// Operator defines the operator address of object
	Operator common.Address `gorm:"operator"`
	// Owner defines the account address of object owner
	Owner common.Address `gorm:"owner"`
	// LocalVirtualGroupId defines the local virtual group id of object
	LocalVirtualGroupId uint32 `gorm:"local_virtual_group_id"`
	// BucketName is the name of the bucket
	BucketName string `gorm:"bucket_name"`
	// ObjectName is the name of object
	ObjectName string `gorm:"object_name"`
	// ObjectID is the unique identifier of object
	ObjectID common.Hash `gorm:"object_id"`
	// BucketID is the unique identifier of bucket
	BucketID common.Hash `gorm:"bucket_id"`
	// PayloadSize is the total size of the object payload
	PayloadSize uint64 `gorm:"payload_size"`
	// Visibility defines the highest permissions for bucket. When a bucket is public, everyone can get storage obj
	Visibility string `gorm:"visibility"`
	// ContentType defines the format of the object which should be a standard MIME type
	ContentType string `gorm:"content_type"`
	// CreateAt defines the block number when the object created
	CreateAt int64 `gorm:"create_at"`
	// CreateTime defines the timestamp when the object created
	CreateTime int64 `gorm:"create_time"`
	// ObjectStatus defines the upload status of the object.
	ObjectStatus string `gorm:"column:status"`
	// RedundancyType defines the type of the redundancy which can be multi-replication or EC
	RedundancyType string `gorm:"redundancy_type"`
	// SourceType defines the source of the object.
	SourceType string `gorm:"source_type"`
	// CheckSums defines the root hash of the pieces which stored in a SP
	Checksums pq.ByteaArray `gorm:"check_sums;type:text"`
	// LockedBalance defines locked balance of object
	LockedBalance common.Hash `gorm:"locked_balance"`
	// Removed defines the object is deleted or not
	Removed bool `gorm:"removed"`
	// UpdateTime defines the time when the object updated
	UpdateTime int64 `gorm:"update_time"`
	// UpdateAt defines the block number when the object updated
	UpdateAt int64 `gorm:"update_at;index:idx_update_at"`
	// DeleteAt defines the block number when the object deleted
	DeleteAt int64 `gorm:"delete_at"`
	// DeleteReason defines the deleted reason of object
	DeleteReason string `gorm:"delete_reason"`
	// CreateTxHash defines the creation transaction hash of object
	CreateTxHash common.Hash `gorm:"create_tx_hash"`
	// UpdateTxHash defines the update transaction hash of object
	UpdateTxHash common.Hash `gorm:"update_tx_hash"`
	// SealTxHash defines the sealed transaction hash of object
	SealTxHash common.Hash `gorm:"column:sealed_tx_hash"`
	// Tags
	Tags datatypes.JSON `gorm:"column:tags;TYPE:json"` // tags
	// Updating
	IsUpdating bool `gorm:"is_updating"`
	// ContentUpdatedTime defines the content updated time, it is related to updated_at in ObjectInfo
	ContentUpdatedTime int64 `gorm:"content_updated_time"`
	// Updater defines the account that updates the object content recently.
	Updater common.Address `gorm:"updater"`
	// Version
	Version int64 `gorm:"version"`
}

// TableName is used to set Object table name in database
func (o *Object) TableName() string {
	return ObjectTableName
}

// GetResourceTags is used to convert the db tags string to *storage_types.ResourceTags type
func (o *Object) GetResourceTags() *storagetypes.ResourceTags {
	tags := &storagetypes.ResourceTags{}
	if o.Tags != nil {
		tagUnmarshalErr := json.Unmarshal([]byte(o.Tags), tags)
		if tagUnmarshalErr != nil {
			log.Warnw("failed to Unmarshal object tags", "error", tagUnmarshalErr)
		}
	}
	return tags
}

type DataStat struct {
	OneRowId         bool   `gorm:"one_row_id;not null;default:true;primaryKey"`
	BlockHeight      int64  `gorm:"column:block_height;type:bigint(64)"`
	ObjectTotalCount string `gorm:"column:object_total_count;type:VARCHAR(2048)"`
	ObjectSealCount  string `gorm:"column:object_seal_count;type:VARCHAR(2048)"`
	BucketCount      int64  `gorm:"column:bucket_count;type:int"`
	UpdateTime       int64  `gorm:"update_time;type:bigint(64)"`
}

func (*DataStat) TableName() string {
	return "data_stat"
}
