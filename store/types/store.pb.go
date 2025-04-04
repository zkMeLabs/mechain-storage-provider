// Code generated by protoc-gen-gogo. DO NOT EDIT.
// source: store/types/store.proto

package types

import (
	fmt "fmt"
	proto "github.com/cosmos/gogoproto/proto"
	math "math"
)

// Reference imports to suppress errors if they are not otherwise used.
var _ = proto.Marshal
var _ = fmt.Errorf
var _ = math.Inf

// This is a compile-time assertion to ensure that this generated file
// is compatible with the proto package it is being compiled against.
// A compilation error at this line likely means your copy of the
// proto package needs to be updated.
const _ = proto.GoGoProtoPackageIsVersion3 // please upgrade the proto package

type TaskState int32

const (
	TaskState_TASK_STATE_INIT_UNSPECIFIED       TaskState = 0
	TaskState_TASK_STATE_UPLOAD_OBJECT_DOING    TaskState = 1
	TaskState_TASK_STATE_UPLOAD_OBJECT_DONE     TaskState = 2
	TaskState_TASK_STATE_UPLOAD_OBJECT_ERROR    TaskState = 3
	TaskState_TASK_STATE_ALLOC_SECONDARY_DOING  TaskState = 4
	TaskState_TASK_STATE_ALLOC_SECONDARY_DONE   TaskState = 5
	TaskState_TASK_STATE_ALLOC_SECONDARY_ERROR  TaskState = 6
	TaskState_TASK_STATE_REPLICATE_OBJECT_DOING TaskState = 7
	TaskState_TASK_STATE_REPLICATE_OBJECT_DONE  TaskState = 8
	TaskState_TASK_STATE_REPLICATE_OBJECT_ERROR TaskState = 9
	TaskState_TASK_STATE_SIGN_OBJECT_DOING      TaskState = 10
	TaskState_TASK_STATE_SIGN_OBJECT_DONE       TaskState = 11
	TaskState_TASK_STATE_SIGN_OBJECT_ERROR      TaskState = 12
	TaskState_TASK_STATE_SEAL_OBJECT_DOING      TaskState = 13
	TaskState_TASK_STATE_SEAL_OBJECT_DONE       TaskState = 14
	TaskState_TASK_STATE_SEAL_OBJECT_ERROR      TaskState = 15
	TaskState_TASK_STATE_OBJECT_DISCONTINUED    TaskState = 16
)

var TaskState_name = map[int32]string{
	0:  "TASK_STATE_INIT_UNSPECIFIED",
	1:  "TASK_STATE_UPLOAD_OBJECT_DOING",
	2:  "TASK_STATE_UPLOAD_OBJECT_DONE",
	3:  "TASK_STATE_UPLOAD_OBJECT_ERROR",
	4:  "TASK_STATE_ALLOC_SECONDARY_DOING",
	5:  "TASK_STATE_ALLOC_SECONDARY_DONE",
	6:  "TASK_STATE_ALLOC_SECONDARY_ERROR",
	7:  "TASK_STATE_REPLICATE_OBJECT_DOING",
	8:  "TASK_STATE_REPLICATE_OBJECT_DONE",
	9:  "TASK_STATE_REPLICATE_OBJECT_ERROR",
	10: "TASK_STATE_SIGN_OBJECT_DOING",
	11: "TASK_STATE_SIGN_OBJECT_DONE",
	12: "TASK_STATE_SIGN_OBJECT_ERROR",
	13: "TASK_STATE_SEAL_OBJECT_DOING",
	14: "TASK_STATE_SEAL_OBJECT_DONE",
	15: "TASK_STATE_SEAL_OBJECT_ERROR",
	16: "TASK_STATE_OBJECT_DISCONTINUED",
}

var TaskState_value = map[string]int32{
	"TASK_STATE_INIT_UNSPECIFIED":       0,
	"TASK_STATE_UPLOAD_OBJECT_DOING":    1,
	"TASK_STATE_UPLOAD_OBJECT_DONE":     2,
	"TASK_STATE_UPLOAD_OBJECT_ERROR":    3,
	"TASK_STATE_ALLOC_SECONDARY_DOING":  4,
	"TASK_STATE_ALLOC_SECONDARY_DONE":   5,
	"TASK_STATE_ALLOC_SECONDARY_ERROR":  6,
	"TASK_STATE_REPLICATE_OBJECT_DOING": 7,
	"TASK_STATE_REPLICATE_OBJECT_DONE":  8,
	"TASK_STATE_REPLICATE_OBJECT_ERROR": 9,
	"TASK_STATE_SIGN_OBJECT_DOING":      10,
	"TASK_STATE_SIGN_OBJECT_DONE":       11,
	"TASK_STATE_SIGN_OBJECT_ERROR":      12,
	"TASK_STATE_SEAL_OBJECT_DOING":      13,
	"TASK_STATE_SEAL_OBJECT_DONE":       14,
	"TASK_STATE_SEAL_OBJECT_ERROR":      15,
	"TASK_STATE_OBJECT_DISCONTINUED":    16,
}

func (x TaskState) String() string {
	return proto.EnumName(TaskState_name, int32(x))
}

func (TaskState) EnumDescriptor() ([]byte, []int) {
	return fileDescriptor_adc82498be3fc479, []int{0}
}

type BucketMigrationState int32

const (
	BucketMigrationState_BUCKET_MIGRATION_STATE_INIT_UNSPECIFIED BucketMigrationState = 0
	// produced execute plan and pre deduct quota
	BucketMigrationState_BUCKET_MIGRATION_STATE_SRC_SP_PRE_DEDUCT_QUOTA_DONE  BucketMigrationState = 1
	BucketMigrationState_BUCKET_MIGRATION_STATE_DEST_SP_PRE_DEDUCT_QUOTA_DONE BucketMigrationState = 2
	// migrating gvg task
	BucketMigrationState_BUCKET_MIGRATION_STATE_MIGRATE_GVG_DOING       BucketMigrationState = 5
	BucketMigrationState_BUCKET_MIGRATION_STATE_MIGRATE_GVG_DONE        BucketMigrationState = 6
	BucketMigrationState_BUCKET_MIGRATION_STATE_MIGRATE_QUOTA_INFO_DONE BucketMigrationState = 7
	// confirm tx
	BucketMigrationState_BUCKET_MIGRATION_STATE_SEND_COMPLETE_TX_DONE       BucketMigrationState = 10
	BucketMigrationState_BUCKET_MIGRATION_STATE_WAIT_COMPLETE_TX_EVENT_DONE BucketMigrationState = 11
	BucketMigrationState_BUCKET_MIGRATION_STATE_SEND_REJECT_TX_DONE         BucketMigrationState = 12
	BucketMigrationState_BUCKET_MIGRATION_STATE_WAIT_REJECT_TX_EVENT_DONE   BucketMigrationState = 13
	BucketMigrationState_BUCKET_MIGRATION_STATE_WAIT_CANCEL_TX_EVENT_DONE   BucketMigrationState = 14
	// gc
	BucketMigrationState_BUCKET_MIGRATION_STATE_SRC_SP_GC_DOING    BucketMigrationState = 20
	BucketMigrationState_BUCKET_MIGRATION_STATE_DEST_SP_GC_DOING   BucketMigrationState = 21
	BucketMigrationState_BUCKET_MIGRATION_STATE_POST_SRC_SP_DONE   BucketMigrationState = 30
	BucketMigrationState_BUCKET_MIGRATION_STATE_MIGRATION_FINISHED BucketMigrationState = 31
)

var BucketMigrationState_name = map[int32]string{
	0:  "BUCKET_MIGRATION_STATE_INIT_UNSPECIFIED",
	1:  "BUCKET_MIGRATION_STATE_SRC_SP_PRE_DEDUCT_QUOTA_DONE",
	2:  "BUCKET_MIGRATION_STATE_DEST_SP_PRE_DEDUCT_QUOTA_DONE",
	5:  "BUCKET_MIGRATION_STATE_MIGRATE_GVG_DOING",
	6:  "BUCKET_MIGRATION_STATE_MIGRATE_GVG_DONE",
	7:  "BUCKET_MIGRATION_STATE_MIGRATE_QUOTA_INFO_DONE",
	10: "BUCKET_MIGRATION_STATE_SEND_COMPLETE_TX_DONE",
	11: "BUCKET_MIGRATION_STATE_WAIT_COMPLETE_TX_EVENT_DONE",
	12: "BUCKET_MIGRATION_STATE_SEND_REJECT_TX_DONE",
	13: "BUCKET_MIGRATION_STATE_WAIT_REJECT_TX_EVENT_DONE",
	14: "BUCKET_MIGRATION_STATE_WAIT_CANCEL_TX_EVENT_DONE",
	20: "BUCKET_MIGRATION_STATE_SRC_SP_GC_DOING",
	21: "BUCKET_MIGRATION_STATE_DEST_SP_GC_DOING",
	30: "BUCKET_MIGRATION_STATE_POST_SRC_SP_DONE",
	31: "BUCKET_MIGRATION_STATE_MIGRATION_FINISHED",
}

var BucketMigrationState_value = map[string]int32{
	"BUCKET_MIGRATION_STATE_INIT_UNSPECIFIED":              0,
	"BUCKET_MIGRATION_STATE_SRC_SP_PRE_DEDUCT_QUOTA_DONE":  1,
	"BUCKET_MIGRATION_STATE_DEST_SP_PRE_DEDUCT_QUOTA_DONE": 2,
	"BUCKET_MIGRATION_STATE_MIGRATE_GVG_DOING":             5,
	"BUCKET_MIGRATION_STATE_MIGRATE_GVG_DONE":              6,
	"BUCKET_MIGRATION_STATE_MIGRATE_QUOTA_INFO_DONE":       7,
	"BUCKET_MIGRATION_STATE_SEND_COMPLETE_TX_DONE":         10,
	"BUCKET_MIGRATION_STATE_WAIT_COMPLETE_TX_EVENT_DONE":   11,
	"BUCKET_MIGRATION_STATE_SEND_REJECT_TX_DONE":           12,
	"BUCKET_MIGRATION_STATE_WAIT_REJECT_TX_EVENT_DONE":     13,
	"BUCKET_MIGRATION_STATE_WAIT_CANCEL_TX_EVENT_DONE":     14,
	"BUCKET_MIGRATION_STATE_SRC_SP_GC_DOING":               20,
	"BUCKET_MIGRATION_STATE_DEST_SP_GC_DOING":              21,
	"BUCKET_MIGRATION_STATE_POST_SRC_SP_DONE":              30,
	"BUCKET_MIGRATION_STATE_MIGRATION_FINISHED":            31,
}

func (x BucketMigrationState) String() string {
	return proto.EnumName(BucketMigrationState_name, int32(x))
}

func (BucketMigrationState) EnumDescriptor() ([]byte, []int) {
	return fileDescriptor_adc82498be3fc479, []int{1}
}

func init() {
	proto.RegisterEnum("store.types.TaskState", TaskState_name, TaskState_value)
	proto.RegisterEnum("store.types.BucketMigrationState", BucketMigrationState_name, BucketMigrationState_value)
}

func init() { proto.RegisterFile("store/types/store.proto", fileDescriptor_adc82498be3fc479) }

var fileDescriptor_adc82498be3fc479 = []byte{
	// 582 bytes of a gzipped FileDescriptorProto
	0x1f, 0x8b, 0x08, 0x00, 0x00, 0x00, 0x00, 0x00, 0x02, 0xff, 0x84, 0x94, 0x4f, 0x4f, 0xdb, 0x3e,
	0x1c, 0xc6, 0x5b, 0x7e, 0x50, 0x7e, 0x98, 0x3f, 0xb3, 0x2c, 0xa6, 0x1d, 0xb6, 0x05, 0xd8, 0xff,
	0x75, 0xd0, 0x22, 0x40, 0x1b, 0xd7, 0xd4, 0xf9, 0xd2, 0x79, 0xa4, 0x76, 0x96, 0x38, 0xec, 0xcf,
	0xc5, 0x0a, 0x2c, 0x82, 0x0a, 0x41, 0x50, 0x1b, 0x26, 0x6d, 0xa7, 0xbd, 0x84, 0xbd, 0xac, 0x1d,
	0x39, 0xee, 0x38, 0xc1, 0x6d, 0xaf, 0x62, 0x6a, 0x4d, 0x59, 0x12, 0x2d, 0xee, 0x2d, 0x56, 0x3e,
	0xcf, 0xf3, 0xd8, 0x7e, 0xbe, 0x32, 0xba, 0xd3, 0x4f, 0x93, 0x5e, 0xdc, 0x4c, 0xbf, 0x9c, 0xc5,
	0xfd, 0xe6, 0xf0, 0xbb, 0x71, 0xd6, 0x4b, 0xd2, 0x84, 0xcc, 0xea, 0xc5, 0xf0, 0x47, 0xfd, 0xf7,
	0x24, 0x9a, 0x91, 0x51, 0xff, 0x38, 0x48, 0xa3, 0x34, 0x26, 0x4b, 0xe8, 0xae, 0xb4, 0x83, 0x5d,
	0x15, 0x48, 0x5b, 0x82, 0x62, 0x9c, 0x49, 0x15, 0xf2, 0xc0, 0x03, 0xca, 0x76, 0x18, 0x38, 0xb8,
	0x42, 0x1e, 0x20, 0x2b, 0x03, 0x84, 0x9e, 0x2b, 0x6c, 0x47, 0x89, 0xd6, 0x1b, 0xa0, 0x52, 0x39,
	0x82, 0xf1, 0x36, 0xae, 0x92, 0x15, 0x74, 0xdf, 0xc0, 0x70, 0xc0, 0x13, 0x46, 0x1b, 0xf0, 0x7d,
	0xe1, 0xe3, 0xff, 0xc8, 0x23, 0xb4, 0x9c, 0x61, 0x6c, 0xd7, 0x15, 0x54, 0x05, 0x40, 0x05, 0x77,
	0x6c, 0xff, 0xc3, 0x75, 0xd8, 0x24, 0x79, 0x88, 0x96, 0x8c, 0x14, 0x07, 0x3c, 0x35, 0xc6, 0x4a,
	0x07, 0xd6, 0xc8, 0x63, 0xb4, 0x92, 0xa1, 0x7c, 0xf0, 0x5c, 0x46, 0x07, 0x5f, 0xb9, 0xe3, 0x4d,
	0x17, 0xcc, 0xfe, 0x81, 0x71, 0xc0, 0xff, 0x8f, 0x33, 0xd3, 0x99, 0x33, 0x64, 0x19, 0xdd, 0xcb,
	0x60, 0x01, 0x6b, 0xf3, 0x7c, 0x1c, 0x2a, 0x54, 0x92, 0x27, 0x38, 0xe0, 0x59, 0x83, 0x85, 0x0e,
	0x99, 0x2b, 0x12, 0x60, 0xbb, 0xf9, 0x90, 0xf9, 0x62, 0x48, 0x8e, 0xe0, 0x80, 0x17, 0x0c, 0x16,
	0x3a, 0xe4, 0x56, 0xa1, 0xd2, 0x91, 0x9a, 0x05, 0x54, 0x70, 0xc9, 0x78, 0x08, 0x0e, 0xc6, 0xf5,
	0x6f, 0x35, 0xb4, 0xd8, 0x3a, 0x3f, 0x38, 0x8e, 0xd3, 0x4e, 0xf7, 0xb0, 0x17, 0xa5, 0xdd, 0xe4,
	0x54, 0xcf, 0xdd, 0x0b, 0xf4, 0xb4, 0x15, 0xd2, 0x5d, 0x90, 0xaa, 0xc3, 0xda, 0xbe, 0x2d, 0x99,
	0xe0, 0xe5, 0x33, 0xf8, 0x0a, 0x6d, 0x96, 0xc0, 0x81, 0x4f, 0x55, 0xe0, 0x29, 0xcf, 0x07, 0xe5,
	0x80, 0x13, 0x52, 0xa9, 0xde, 0x86, 0x42, 0xda, 0xfa, 0x10, 0x55, 0xb2, 0x8d, 0xb6, 0x4a, 0x84,
	0x0e, 0x04, 0xb2, 0x5c, 0x39, 0x41, 0x56, 0xd1, 0xb3, 0x12, 0xa5, 0x5e, 0x83, 0x6a, 0xef, 0xb5,
	0xaf, 0x6f, 0x73, 0xca, 0x70, 0x9a, 0x3c, 0xcd, 0x01, 0xd7, 0xc8, 0x06, 0x6a, 0x8c, 0x81, 0xf5,
	0x4e, 0x18, 0xdf, 0x11, 0x5a, 0x33, 0x4d, 0xd6, 0xd1, 0x6a, 0xd9, 0x0d, 0x00, 0x77, 0x14, 0x15,
	0x1d, 0xcf, 0x05, 0x09, 0x4a, 0xbe, 0xd7, 0x0a, 0x44, 0x5e, 0xa2, 0x8d, 0x12, 0xc5, 0x3b, 0x9b,
	0xc9, 0x9c, 0x02, 0xf6, 0x80, 0xdf, 0x0c, 0x57, 0x03, 0xd5, 0x4d, 0x49, 0x3e, 0x0c, 0x6b, 0x1e,
	0xe5, 0xcc, 0x91, 0x2d, 0xb4, 0x6e, 0xca, 0xf9, 0xcb, 0x67, 0x52, 0xe6, 0xc7, 0xa9, 0xa8, 0xcd,
	0x29, 0xb8, 0x05, 0xd5, 0x02, 0xa9, 0xa3, 0x27, 0xe6, 0x39, 0x68, 0xd3, 0xeb, 0x4a, 0x16, 0x0d,
	0x95, 0x8c, 0xaa, 0xbf, 0x81, 0x6f, 0x1b, 0x60, 0x4f, 0x0c, 0x60, 0xed, 0x3e, 0xdc, 0x85, 0x45,
	0xd6, 0xd0, 0x73, 0x63, 0x7f, 0x83, 0xf5, 0x0e, 0xe3, 0x2c, 0x78, 0x0d, 0x0e, 0x5e, 0x6a, 0xf9,
	0x3f, 0x2e, 0xad, 0xea, 0xc5, 0xa5, 0x55, 0xfd, 0x75, 0x69, 0x55, 0xbf, 0x5f, 0x59, 0x95, 0x8b,
	0x2b, 0xab, 0xf2, 0xf3, 0xca, 0xaa, 0x7c, 0xdc, 0x3e, 0xec, 0xa6, 0x47, 0xe7, 0xfb, 0x8d, 0x83,
	0xe4, 0xa4, 0xf9, 0xf5, 0xb8, 0x13, 0xbb, 0xd1, 0x7e, 0xbf, 0x79, 0x12, 0x1f, 0x1c, 0x45, 0xdd,
	0xd3, 0xb5, 0xc1, 0x93, 0x1d, 0x1d, 0xc6, 0x6b, 0x67, 0xbd, 0xe4, 0x73, 0xf7, 0x53, 0xdc, 0x6b,
	0x66, 0x1e, 0xf7, 0xfd, 0xda, 0xf0, 0x5d, 0xdf, 0xfc, 0x13, 0x00, 0x00, 0xff, 0xff, 0x00, 0x36,
	0x1b, 0x2e, 0xf2, 0x05, 0x00, 0x00,
}
