package gnfd

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	sdkmath "cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client/grpc/tmservice"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/query"
	"github.com/cosmos/cosmos-sdk/types/tx"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	paymenttypes "github.com/evmos/evmos/v12/x/payment/types"
	permissiontypes "github.com/evmos/evmos/v12/x/permission/types"
	sptypes "github.com/evmos/evmos/v12/x/sp/types"
	storagetypes "github.com/evmos/evmos/v12/x/storage/types"
	virtualgrouptypes "github.com/evmos/evmos/v12/x/virtualgroup/types"
	"github.com/zkMeLabs/mechain-storage-provider/pkg/log"
	"github.com/zkMeLabs/mechain-storage-provider/pkg/metrics"
)

const (
	// ChainSuccessTotal defines the metrics label of successfully total chain rpc
	ChainSuccessTotal = "chain_total_success"
	// ChainFailureTotal defines the metrics label of unsuccessfully total chain rpc
	ChainFailureTotal = "chain_total_failure"
	// ChainSuccessCurrentHeight defines the metrics label of successfully get current block height
	ChainSuccessCurrentHeight = "get_current_height_success"
	// ChainFailureCurrentHeight defines the metrics label of unsuccessfully get current block height
	ChainFailureCurrentHeight = "get_current_height_failure"
	// ChainSuccessHasAccount defines the metrics label of successfully check has account
	ChainSuccessHasAccount = "has_account_success"
	// ChainFailureHasAccount defines the metrics label of unsuccessfully check has account
	ChainFailureHasAccount = "has_account_failure"
	// ChainSuccessListSPs defines the metrics label of successfully list all sp infos
	ChainSuccessListSPs = "list_sp_success"
	// ChainFailureListSPs defines the metrics label of unsuccessfully list all sp infos
	ChainFailureListSPs = "list_sp_failure"
	// ChainSuccessListBondedValidators defines the metrics label of successfully list bonded validators
	ChainSuccessListBondedValidators = "list_bonded_validators_success"
	// ChainFailureListBondedValidators defines the metrics label of unsuccessfully list bonded validators
	ChainFailureListBondedValidators = "list_bonded_validators_failure"
	// ChainSuccessQueryStorageParams defines the metrics label of successfully query storage params
	ChainSuccessQueryStorageParams = "query_storage_param_success"
	// ChainFailureQueryStorageParams defines the metrics label of unsuccessfully query storage params
	ChainFailureQueryStorageParams = "query_storage_param_failure"
	// ChainSuccessQueryStorageParamsByTimestamp defines the metrics label of successfully query storage params by time
	ChainSuccessQueryStorageParamsByTimestamp = "query_storage_param_by_timestamp_success"
	// ChainFailureQueryStorageParamsByTimestamp defines the metrics label of unsuccessfully query storage params by time
	ChainFailureQueryStorageParamsByTimestamp = "query_storage_param_by_timestamp_failure"
	// ChainSuccessQueryBucketInfo defines the metrics label of successfully query bucket info
	ChainSuccessQueryBucketInfo = "query_bucket_info_success"
	// ChainFailureQueryBucketInfo defines the metrics label of successfully query object info
	ChainFailureQueryBucketInfo = "query_bucket_info_failure"
	// ChainSuccessQueryObjectInfo defines the metrics label of successfully query object info
	ChainSuccessQueryObjectInfo = "query_object_info_success"
	// ChainFailureQueryObjectInfo defines the metrics label of unsuccessfully query object info
	ChainFailureQueryObjectInfo = "query_object_info_failure"
	// ChainSuccessQueryObjectInfoByID defines the metrics label of successfully query object info by id
	ChainSuccessQueryObjectInfoByID = "query_object_info_by_id_success"
	// ChainFailureQueryObjectInfoByID defines the metrics label of unsuccessfully query object info by id
	ChainFailureQueryObjectInfoByID = "query_object_info_by_id_failure"
	// ChainSuccessQueryBucketInfoAndObjectInfo defines the metrics label of successfully query bucket and object info
	ChainSuccessQueryBucketInfoAndObjectInfo = "query_bucket_and_object_info_success"
	// ChainFailureQueryBucketInfoAndObjectInfo defines the metrics label of unsuccessfully query bucket and object info
	ChainFailureQueryBucketInfoAndObjectInfo = "query_bucket_and_object_info_failure"
	// ChainSuccessListenObjectSeal defines the metrics label of successfully listen object seal
	ChainSuccessListenObjectSeal = "listen_object_seal_success"
	// ChainFailureListenObjectSeal defines the metrics label of unsuccessfully listen object seal
	ChainFailureListenObjectSeal = "listen_object_seal_failure"
	// ChainSuccessListenRejectUnSealObject defines the metrics label of successfully listen object reject unseal
	ChainSuccessListenRejectUnSealObject = "listen_reject_unseal_object_success"
	// ChainFailureListenRejectUnSealObject defines the metrics label of unsuccessfully listen object reject unseal
	ChainFailureListenRejectUnSealObject = "listen_reject_unseal_object_failure"
	// ChainSuccessQueryPaymentStreamRecord defines the metrics label of successfully query payment stream
	ChainSuccessQueryPaymentStreamRecord = "query_payment_stream_record_success"
	// ChainFailureQueryPaymentStreamRecord defines the metrics label of unsuccessfully query payment stream
	ChainFailureQueryPaymentStreamRecord = "query_payment_stream_record_failure"
	// ChainSuccessVerifyGetObjectPermission defines the metrics label of successfully verify get object permission
	ChainSuccessVerifyGetObjectPermission = "verify_get_object_permission_success"
	// ChainFailureVerifyGetObjectPermission defines the metrics label of unsuccessfully verify get object permission
	ChainFailureVerifyGetObjectPermission = "verify_get_object_permission_failure"
	// ChainSuccessVerifyPutObjectPermission defines the metrics label of successfully verify put object permission
	ChainSuccessVerifyPutObjectPermission = "verify_put_object_permission_success"
	// ChainFailureVerifyPutObjectPermission defines the metrics label of unsuccessfully verify put object permission
	ChainFailureVerifyPutObjectPermission = "verify_put_object_permission_failure"
	// ChainSuccessQuerySwapInInfo defines the metrics label of successfully query swap in info
	ChainSuccessQuerySwapInInfo = "query_swap_in_info_success"
	// ChainFailureQuerySwapInInfo defines the metrics label of unsuccessfully query swap in info
	ChainFailureQuerySwapInInfo = "query_swap_in_info_failure"

	// ConfirmBlockNumber defines wait block number.
	ConfirmBlockNumber = 5
	// WaitForNextBlockTimeout define wait block timeout.
	WaitForNextBlockTimeout = 5 * time.Second
)

// CurrentHeight the block height sub one as the stable height.
func (g *Gnfd) CurrentHeight(ctx context.Context) (height uint64, err error) {
	startTime := time.Now()
	defer func() {
		if err != nil {
			metrics.GnfdChainCounter.WithLabelValues(ChainFailureCurrentHeight).Inc()
			metrics.GnfdChainTime.WithLabelValues(ChainFailureCurrentHeight).Observe(
				time.Since(startTime).Seconds())
			metrics.GnfdChainCounter.WithLabelValues(ChainFailureTotal).Inc()
			metrics.GnfdChainTime.WithLabelValues(ChainFailureTotal).Observe(
				time.Since(startTime).Seconds())
			return
		}
		metrics.GnfdChainCounter.WithLabelValues(ChainSuccessCurrentHeight).Inc()
		metrics.GnfdChainTime.WithLabelValues(ChainSuccessCurrentHeight).Observe(
			time.Since(startTime).Seconds())
		metrics.GnfdChainCounter.WithLabelValues(ChainSuccessTotal).Inc()
		metrics.GnfdChainTime.WithLabelValues(ChainSuccessTotal).Observe(
			time.Since(startTime).Seconds())
	}()

	resp, err := g.getCurrentWsClient().ABCIInfo(ctx)
	if err != nil {
		log.CtxErrorw(ctx, "get latest block height failed", "node_addr",
			g.getCurrentWsClient().Remote(), "error", err)
		return 0, err
	}
	return (uint64)(resp.Response.LastBlockHeight), nil
}

// HasAccount returns an indication of the existence of address.
func (g *Gnfd) HasAccount(ctx context.Context, address string) (has bool, err error) {
	startTime := time.Now()
	defer func() {
		if err != nil {
			metrics.GnfdChainCounter.WithLabelValues(ChainFailureHasAccount).Inc()
			metrics.GnfdChainTime.WithLabelValues(ChainFailureHasAccount).Observe(
				time.Since(startTime).Seconds())
			metrics.GnfdChainCounter.WithLabelValues(ChainFailureTotal).Inc()
			metrics.GnfdChainTime.WithLabelValues(ChainFailureTotal).Observe(
				time.Since(startTime).Seconds())
			return
		}
		metrics.GnfdChainCounter.WithLabelValues(ChainSuccessHasAccount).Inc()
		metrics.GnfdChainTime.WithLabelValues(ChainSuccessHasAccount).Observe(
			time.Since(startTime).Seconds())
		metrics.GnfdChainCounter.WithLabelValues(ChainSuccessTotal).Inc()
		metrics.GnfdChainTime.WithLabelValues(ChainSuccessTotal).Observe(
			time.Since(startTime).Seconds())
	}()

	client := g.getCurrentClient().GnfdClient()
	resp, err := client.Account(ctx, &authtypes.QueryAccountRequest{Address: address})
	if err != nil {
		log.CtxErrorw(ctx, "failed to query account", "address", address, "error", err)
		return false, err
	}
	return resp.GetAccount() != nil, nil
}

// ListSPs returns the list of storage provider info.
func (g *Gnfd) ListSPs(ctx context.Context) (spInfos []*sptypes.StorageProvider, err error) {
	startTime := time.Now()
	defer func() {
		if err != nil {
			metrics.GnfdChainCounter.WithLabelValues(ChainFailureListSPs).Inc()
			metrics.GnfdChainTime.WithLabelValues(ChainFailureListSPs).Observe(
				time.Since(startTime).Seconds())
			metrics.GnfdChainCounter.WithLabelValues(ChainFailureTotal).Inc()
			metrics.GnfdChainTime.WithLabelValues(ChainFailureTotal).Observe(
				time.Since(startTime).Seconds())
			return
		}
		metrics.GnfdChainCounter.WithLabelValues(ChainSuccessListSPs).Inc()
		metrics.GnfdChainTime.WithLabelValues(ChainSuccessListSPs).Observe(
			time.Since(startTime).Seconds())
		metrics.GnfdChainCounter.WithLabelValues(ChainSuccessTotal).Inc()
		metrics.GnfdChainTime.WithLabelValues(ChainSuccessTotal).Observe(
			time.Since(startTime).Seconds())
	}()

	client := g.getCurrentClient().GnfdClient()
	resp, err := client.StorageProviders(ctx, &sptypes.QueryStorageProvidersRequest{
		Pagination: &query.PageRequest{
			Offset: 0,
			Limit:  math.MaxUint64,
		},
	})
	if err != nil {
		log.Errorw("failed to list storage providers", "error", err)
		return spInfos, err
	}
	for i := 0; i < len(resp.GetSps()); i++ {
		spInfos = append(spInfos, resp.GetSps()[i])
	}
	return spInfos, nil
}

// QuerySP returns the sp info.
func (g *Gnfd) QuerySP(ctx context.Context, operatorAddress string) (*sptypes.StorageProvider, error) {
	startTime := time.Now()
	defer func() {
		metrics.GnfdChainTime.WithLabelValues("query_sp").Observe(time.Since(startTime).Seconds())
	}()
	client := g.getCurrentClient().GnfdClient()
	resp, err := client.StorageProviderByOperatorAddress(ctx, &sptypes.QueryStorageProviderByOperatorAddressRequest{
		OperatorAddress: operatorAddress,
	})
	if err != nil {
		log.Errorw("failed to query storage provider", "error", err)
		return nil, err
	}
	return resp.GetStorageProvider(), nil
}

// QuerySPFreeQuota returns the sp free quota
func (g *Gnfd) QuerySPFreeQuota(ctx context.Context, operatorAddress string) (uint64, error) {
	startTime := time.Now()
	defer func() {
		metrics.GnfdChainTime.WithLabelValues("query_sp_quota").Observe(time.Since(startTime).Seconds())
	}()
	client := g.getCurrentClient().GnfdClient()
	resp, err := client.QuerySpStoragePrice(ctx, &sptypes.QuerySpStoragePriceRequest{
		SpAddr: operatorAddress,
	})
	if err != nil {
		log.Errorw("failed to query storage provider", "error", err)
		return 0, err
	}
	return resp.GetSpStoragePrice().FreeReadQuota, nil
}

// QuerySPPrice returns the sp price info
func (g *Gnfd) QuerySPPrice(ctx context.Context, operatorAddress string) (sptypes.SpStoragePrice, error) {
	startTime := time.Now()
	defer func() {
		metrics.GnfdChainTime.WithLabelValues("query_sp_price").Observe(time.Since(startTime).Seconds())
	}()
	client := g.getCurrentClient().GnfdClient()
	resp, err := client.QuerySpStoragePrice(ctx, &sptypes.QuerySpStoragePriceRequest{
		SpAddr: operatorAddress,
	})
	if err != nil {
		log.Errorw("failed to query storage provider", "error", err)
		return sptypes.SpStoragePrice{}, err
	}
	return resp.GetSpStoragePrice(), nil
}

// QuerySPByID returns the sp info.
func (g *Gnfd) QuerySPByID(ctx context.Context, spID uint32) (*sptypes.StorageProvider, error) {
	startTime := time.Now()
	defer func() {
		metrics.GnfdChainTime.WithLabelValues("query_sp_by_id").Observe(time.Since(startTime).Seconds())
	}()
	client := g.getCurrentClient().GnfdClient()
	resp, err := client.StorageProvider(ctx, &sptypes.QueryStorageProviderRequest{
		Id: spID,
	})
	if err != nil {
		log.Errorw("failed to query storage provider", "error", err)
		return nil, err
	}
	return resp.GetStorageProvider(), nil
}

// ListBondedValidators returns the list of bonded validators.
func (g *Gnfd) ListBondedValidators(ctx context.Context) (validators []stakingtypes.Validator, err error) {
	startTime := time.Now()
	defer func() {
		if err != nil {
			metrics.GnfdChainCounter.WithLabelValues(ChainFailureListBondedValidators).Inc()
			metrics.GnfdChainTime.WithLabelValues(ChainFailureListBondedValidators).Observe(
				time.Since(startTime).Seconds())
			metrics.GnfdChainCounter.WithLabelValues(ChainFailureTotal).Inc()
			metrics.GnfdChainTime.WithLabelValues(ChainFailureTotal).Observe(
				time.Since(startTime).Seconds())
			return
		}
		metrics.GnfdChainCounter.WithLabelValues(ChainSuccessListBondedValidators).Inc()
		metrics.GnfdChainTime.WithLabelValues(ChainSuccessListBondedValidators).Observe(
			time.Since(startTime).Seconds())
		metrics.GnfdChainCounter.WithLabelValues(ChainSuccessTotal).Inc()
		metrics.GnfdChainTime.WithLabelValues(ChainSuccessTotal).Observe(
			time.Since(startTime).Seconds())
	}()

	client := g.getCurrentClient().GnfdClient()
	resp, err := client.Validators(ctx, &stakingtypes.QueryValidatorsRequest{Status: "BOND_STATUS_BONDED"})
	if err != nil {
		log.Errorw("failed to list validators", "error", err)
		return validators, err
	}
	for i := 0; i < len(resp.GetValidators()); i++ {
		validators = append(validators, resp.GetValidators()[i])
	}
	return validators, nil
}

// ListVirtualGroupFamilies return the list of virtual group family.
// TODO: improve it by metadata indexer.
func (g *Gnfd) ListVirtualGroupFamilies(ctx context.Context, spID uint32) ([]*virtualgrouptypes.GlobalVirtualGroupFamily, error) {
	startTime := time.Now()
	defer func() {
		metrics.GnfdChainTime.WithLabelValues("list_virtual_group_family").Observe(time.Since(startTime).Seconds())
	}()
	client := g.getCurrentClient().GnfdClient()
	var vgfs []*virtualgrouptypes.GlobalVirtualGroupFamily
	var nextKey []byte
	for {
		resp, err := client.VirtualGroupQueryClient.GlobalVirtualGroupFamilies(ctx, &virtualgrouptypes.QueryGlobalVirtualGroupFamiliesRequest{
			Pagination: &query.PageRequest{
				Key:   nextKey,
				Limit: 1000,
			},
		})
		if err != nil {
			log.Errorw("failed to list virtual group families", "error", err)
			return vgfs, err
		}
		log.Infow("list families", "response", resp)
		for i := 0; i < len(resp.GetGvgFamilies()); i++ {
			f := resp.GetGvgFamilies()[i]
			if f.PrimarySpId == spID {
				vgfs = append(vgfs, resp.GetGvgFamilies()[i])
			}
		}
		nextKey = resp.GetPagination().GetNextKey()
		if nextKey == nil { // finish
			break
		}
	}
	return vgfs, nil
}

// QueryVirtualGroupFamily returns the virtual group family.
func (g *Gnfd) QueryVirtualGroupFamily(ctx context.Context, vgfID uint32) (*virtualgrouptypes.GlobalVirtualGroupFamily, error) {
	startTime := time.Now()
	defer func() {
		metrics.GnfdChainTime.WithLabelValues("query_virtual_group_family").Observe(time.Since(startTime).Seconds())
	}()
	client := g.getCurrentClient().GnfdClient()
	resp, err := client.VirtualGroupQueryClient.GlobalVirtualGroupFamily(ctx, &virtualgrouptypes.QueryGlobalVirtualGroupFamilyRequest{
		FamilyId: vgfID,
	})
	if err != nil {
		log.Errorw("failed to query virtual group family", "error", err)
		return nil, err
	}
	return resp.GetGlobalVirtualGroupFamily(), nil
}

// ListGlobalVirtualGroupsByFamilyID returns gvg list by family.
func (g *Gnfd) ListGlobalVirtualGroupsByFamilyID(ctx context.Context, vgfID uint32) ([]*virtualgrouptypes.GlobalVirtualGroup, error) {
	startTime := time.Now()
	defer func() {
		metrics.GnfdChainTime.WithLabelValues("list_virtual_group_by_family_id").Observe(time.Since(startTime).Seconds())
	}()
	client := g.getCurrentClient().GnfdClient()
	resp, err := client.VirtualGroupQueryClient.GlobalVirtualGroupByFamilyID(ctx, &virtualgrouptypes.QueryGlobalVirtualGroupByFamilyIDRequest{
		GlobalVirtualGroupFamilyId: vgfID,
	})
	if err != nil {
		log.Errorw("failed to query virtual group family", "error", err)
		return nil, err
	}
	return resp.GetGlobalVirtualGroups(), nil
}

// QueryGlobalVirtualGroup returns the global virtual group info.
func (g *Gnfd) QueryGlobalVirtualGroup(ctx context.Context, gvgID uint32) (*virtualgrouptypes.GlobalVirtualGroup, error) {
	startTime := time.Now()
	defer func() {
		metrics.GnfdChainTime.WithLabelValues("query_global_virtual_group").Observe(time.Since(startTime).Seconds())
	}()
	client := g.getCurrentClient().GnfdClient()
	resp, err := client.VirtualGroupQueryClient.GlobalVirtualGroup(ctx, &virtualgrouptypes.QueryGlobalVirtualGroupRequest{
		GlobalVirtualGroupId: gvgID,
	})
	if err != nil {
		log.Errorw("failed to query global virtual group", "error", err)
		return nil, err
	}
	return resp.GetGlobalVirtualGroup(), nil
}

// AvailableGlobalVirtualGroupFamilies submits a list global virtual group families Id to chain and return the filtered list of families which are able to server create bucket request.
func (g *Gnfd) AvailableGlobalVirtualGroupFamilies(ctx context.Context, globalVirtualGroupFamiliesIDs []uint32) ([]uint32, error) {
	startTime := time.Now()
	defer func() {
		metrics.GnfdChainTime.WithLabelValues("available_global_virtual_families").Observe(time.Since(startTime).Seconds())
	}()
	client := g.getCurrentClient().GnfdClient()
	resp, err := client.VirtualGroupQueryClient.AvailableGlobalVirtualGroupFamilies(ctx, &virtualgrouptypes.AvailableGlobalVirtualGroupFamiliesRequest{
		GlobalVirtualGroupFamilyIds: globalVirtualGroupFamiliesIDs,
	})
	if err != nil {
		log.Errorw("failed to query available global virtual group families", "error", err)
		return nil, err
	}
	return resp.GlobalVirtualGroupFamilyIds, nil
}

// QueryVirtualGroupParams return virtual group params.
func (g *Gnfd) QueryVirtualGroupParams(ctx context.Context) (*virtualgrouptypes.Params, error) {
	startTime := time.Now()
	defer func() {
		metrics.GnfdChainTime.WithLabelValues("query_virtual_group_params").Observe(time.Since(startTime).Seconds())
	}()
	client := g.getCurrentClient().GnfdClient()
	resp, err := client.VirtualGroupQueryClient.Params(ctx, &virtualgrouptypes.QueryParamsRequest{})
	if err != nil {
		log.CtxErrorw(ctx, "failed to query virtual group params", "error", err)
		return nil, err
	}
	return &resp.Params, nil
}

// QueryStorageParams returns storage params
func (g *Gnfd) QueryStorageParams(ctx context.Context) (params *storagetypes.Params, err error) {
	startTime := time.Now()
	defer func() {
		if err != nil {
			metrics.GnfdChainCounter.WithLabelValues(ChainFailureQueryStorageParams).Inc()
			metrics.GnfdChainTime.WithLabelValues(ChainFailureQueryStorageParams).Observe(
				time.Since(startTime).Seconds())
			metrics.GnfdChainCounter.WithLabelValues(ChainFailureTotal).Inc()
			metrics.GnfdChainTime.WithLabelValues(ChainFailureTotal).Observe(
				time.Since(startTime).Seconds())
			return
		}
		metrics.GnfdChainCounter.WithLabelValues(ChainSuccessQueryStorageParams).Inc()
		metrics.GnfdChainTime.WithLabelValues(ChainSuccessQueryStorageParams).Observe(
			time.Since(startTime).Seconds())
		metrics.GnfdChainCounter.WithLabelValues(ChainSuccessTotal).Inc()
		metrics.GnfdChainTime.WithLabelValues(ChainSuccessTotal).Observe(
			time.Since(startTime).Seconds())
	}()

	client := g.getCurrentClient().GnfdClient()
	resp, err := client.StorageQueryClient.Params(ctx, &storagetypes.QueryParamsRequest{})
	if err != nil {
		log.CtxErrorw(ctx, "failed to query storage params", "error", err)
		return nil, err
	}
	return &resp.Params, nil
}

// QueryStorageParamsByTimestamp returns storage params by block create time.
func (g *Gnfd) QueryStorageParamsByTimestamp(ctx context.Context, timestamp int64) (params *storagetypes.Params, err error) {
	startTime := time.Now()
	defer func() {
		if err != nil {
			metrics.GnfdChainCounter.WithLabelValues(ChainFailureQueryStorageParamsByTimestamp).Inc()
			metrics.GnfdChainTime.WithLabelValues(ChainFailureQueryStorageParamsByTimestamp).Observe(
				time.Since(startTime).Seconds())
			metrics.GnfdChainCounter.WithLabelValues(ChainFailureTotal).Inc()
			metrics.GnfdChainTime.WithLabelValues(ChainFailureTotal).Observe(
				time.Since(startTime).Seconds())
			return
		}
		metrics.GnfdChainCounter.WithLabelValues(ChainSuccessQueryStorageParamsByTimestamp).Inc()
		metrics.GnfdChainTime.WithLabelValues(ChainSuccessQueryStorageParamsByTimestamp).Observe(
			time.Since(startTime).Seconds())
		metrics.GnfdChainCounter.WithLabelValues(ChainSuccessTotal).Inc()
		metrics.GnfdChainTime.WithLabelValues(ChainSuccessTotal).Observe(
			time.Since(startTime).Seconds())
	}()

	client := g.getCurrentClient().GnfdClient()
	resp, err := client.StorageQueryClient.QueryParamsByTimestamp(ctx,
		&storagetypes.QueryParamsByTimestampRequest{Timestamp: timestamp})
	if err != nil {
		log.CtxErrorw(ctx, "failed to query storage params", "error", err)
		return nil, err
	}
	return &resp.Params, nil
}

// QueryBucketInfo returns the bucket info by name.
func (g *Gnfd) QueryBucketInfo(ctx context.Context, bucket string) (bucketInfo *storagetypes.BucketInfo, err error) {
	startTime := time.Now()
	defer func() {
		if err != nil {
			metrics.GnfdChainCounter.WithLabelValues(ChainFailureQueryBucketInfo).Inc()
			metrics.GnfdChainTime.WithLabelValues(ChainFailureQueryBucketInfo).Observe(
				time.Since(startTime).Seconds())
			metrics.GnfdChainCounter.WithLabelValues(ChainFailureTotal).Inc()
			metrics.GnfdChainTime.WithLabelValues(ChainFailureTotal).Observe(
				time.Since(startTime).Seconds())
			return
		}
		metrics.GnfdChainCounter.WithLabelValues(ChainSuccessQueryBucketInfo).Inc()
		metrics.GnfdChainTime.WithLabelValues(ChainSuccessQueryBucketInfo).Observe(
			time.Since(startTime).Seconds())
		metrics.GnfdChainCounter.WithLabelValues(ChainSuccessTotal).Inc()
		metrics.GnfdChainTime.WithLabelValues(ChainSuccessTotal).Observe(
			time.Since(startTime).Seconds())
	}()

	client := g.getCurrentClient().GnfdClient()
	resp, err := client.HeadBucket(ctx, &storagetypes.QueryHeadBucketRequest{BucketName: bucket})
	if err != nil {
		log.CtxErrorw(ctx, "failed to query bucket", "bucket_name", bucket, "error", err)
		return nil, err
	}
	return resp.GetBucketInfo(), nil
}

// QueryBucketExtraInfo returns the bucket extra info by name.
func (g *Gnfd) QueryBucketExtraInfo(ctx context.Context, bucket string) (bucketInfo *storagetypes.BucketExtraInfo, err error) {
	startTime := time.Now()
	defer func() {
		if err != nil {
			metrics.GnfdChainCounter.WithLabelValues(ChainFailureQueryBucketInfo).Inc()
			metrics.GnfdChainTime.WithLabelValues(ChainFailureQueryBucketInfo).Observe(
				time.Since(startTime).Seconds())
			metrics.GnfdChainCounter.WithLabelValues(ChainFailureTotal).Inc()
			metrics.GnfdChainTime.WithLabelValues(ChainFailureTotal).Observe(
				time.Since(startTime).Seconds())
			return
		}
		metrics.GnfdChainCounter.WithLabelValues(ChainSuccessQueryBucketInfo).Inc()
		metrics.GnfdChainTime.WithLabelValues(ChainSuccessQueryBucketInfo).Observe(
			time.Since(startTime).Seconds())
		metrics.GnfdChainCounter.WithLabelValues(ChainSuccessTotal).Inc()
		metrics.GnfdChainTime.WithLabelValues(ChainSuccessTotal).Observe(
			time.Since(startTime).Seconds())
	}()

	client := g.getCurrentClient().GnfdClient()
	resp, err := client.HeadBucket(ctx, &storagetypes.QueryHeadBucketRequest{BucketName: bucket})
	if err != nil {
		log.CtxErrorw(ctx, "failed to query bucket", "bucket_name", bucket, "error", err)
		return nil, err
	}
	return resp.GetExtraInfo(), nil
}

// QueryBucketInfoById returns the bucket info by name.
func (g *Gnfd) QueryBucketInfoById(ctx context.Context, bucketId uint64) (bucketInfo *storagetypes.BucketInfo, err error) {
	startTime := time.Now()
	defer func() {
		if err != nil {
			metrics.GnfdChainCounter.WithLabelValues(ChainFailureQueryBucketInfo).Inc()
			metrics.GnfdChainTime.WithLabelValues(ChainFailureQueryBucketInfo).Observe(
				time.Since(startTime).Seconds())
			metrics.GnfdChainCounter.WithLabelValues(ChainFailureTotal).Inc()
			metrics.GnfdChainTime.WithLabelValues(ChainFailureTotal).Observe(
				time.Since(startTime).Seconds())
			return
		}
		metrics.GnfdChainCounter.WithLabelValues(ChainSuccessQueryBucketInfo).Inc()
		metrics.GnfdChainTime.WithLabelValues(ChainSuccessQueryBucketInfo).Observe(
			time.Since(startTime).Seconds())
		metrics.GnfdChainCounter.WithLabelValues(ChainSuccessTotal).Inc()
		metrics.GnfdChainTime.WithLabelValues(ChainSuccessTotal).Observe(
			time.Since(startTime).Seconds())
	}()

	client := g.getCurrentClient().GnfdClient()
	id := sdkmath.NewUint(bucketId)
	resp, err := client.HeadBucketById(ctx, &storagetypes.QueryHeadBucketByIdRequest{BucketId: id.String()})
	if err != nil {
		log.CtxErrorw(ctx, "failed to query bucket", "bucket_id", bucketId, "error", err)
		return nil, err
	}
	return resp.GetBucketInfo(), nil
}

// QueryObjectInfo returns the object info by name.
func (g *Gnfd) QueryObjectInfo(ctx context.Context, bucket, object string) (objectInfo *storagetypes.ObjectInfo, err error) {
	startTime := time.Now()
	defer func() {
		if err != nil {
			metrics.GnfdChainCounter.WithLabelValues(ChainFailureQueryObjectInfo).Inc()
			metrics.GnfdChainTime.WithLabelValues(ChainFailureQueryObjectInfo).Observe(
				time.Since(startTime).Seconds())
			metrics.GnfdChainCounter.WithLabelValues(ChainFailureTotal).Inc()
			metrics.GnfdChainTime.WithLabelValues(ChainFailureTotal).Observe(
				time.Since(startTime).Seconds())
			return
		}
		metrics.GnfdChainCounter.WithLabelValues(ChainSuccessQueryObjectInfo).Inc()
		metrics.GnfdChainTime.WithLabelValues(ChainSuccessQueryObjectInfo).Observe(
			time.Since(startTime).Seconds())
		metrics.GnfdChainCounter.WithLabelValues(ChainSuccessTotal).Inc()
		metrics.GnfdChainTime.WithLabelValues(ChainSuccessTotal).Observe(
			time.Since(startTime).Seconds())
	}()

	client := g.getCurrentClient().GnfdClient()
	resp, err := client.HeadObject(ctx, &storagetypes.QueryHeadObjectRequest{
		BucketName: bucket,
		ObjectName: object,
	})
	if err != nil {
		log.CtxErrorw(ctx, "failed to query object", "bucket_name", bucket, "object_name", object, "error", err)
		return nil, err
	}
	return resp.GetObjectInfo(), nil
}

// QueryObjectInfoByID returns the object info by name.
func (g *Gnfd) QueryObjectInfoByID(ctx context.Context, objectID string) (objectInfo *storagetypes.ObjectInfo, err error) {
	startTime := time.Now()
	defer func() {
		if err != nil {
			metrics.GnfdChainCounter.WithLabelValues(ChainFailureQueryObjectInfoByID).Inc()
			metrics.GnfdChainTime.WithLabelValues(ChainFailureQueryObjectInfoByID).Observe(
				time.Since(startTime).Seconds())
			metrics.GnfdChainCounter.WithLabelValues(ChainFailureTotal).Inc()
			metrics.GnfdChainTime.WithLabelValues(ChainFailureTotal).Observe(
				time.Since(startTime).Seconds())
			return
		}
		metrics.GnfdChainCounter.WithLabelValues(ChainSuccessQueryObjectInfoByID).Inc()
		metrics.GnfdChainTime.WithLabelValues(ChainSuccessQueryObjectInfoByID).Observe(
			time.Since(startTime).Seconds())
		metrics.GnfdChainCounter.WithLabelValues(ChainSuccessTotal).Inc()
		metrics.GnfdChainTime.WithLabelValues(ChainSuccessTotal).Observe(
			time.Since(startTime).Seconds())
	}()

	client := g.getCurrentClient().GnfdClient()
	resp, err := client.HeadObjectById(ctx, &storagetypes.QueryHeadObjectByIdRequest{
		ObjectId: objectID,
	})
	if err != nil {
		log.CtxErrorw(ctx, "failed to query object", "object_id", objectID, "error", err)
		return nil, err
	}
	return resp.GetObjectInfo(), nil
}

// QueryBucketInfoAndObjectInfo returns bucket info and object info, if not found, return the corresponding error code
func (g *Gnfd) QueryBucketInfoAndObjectInfo(ctx context.Context, bucket, object string) (bucketInfo *storagetypes.BucketInfo,
	objectInfo *storagetypes.ObjectInfo, err error,
) {
	startTime := time.Now()
	defer func() {
		if err != nil {
			metrics.GnfdChainCounter.WithLabelValues(ChainFailureQueryBucketInfoAndObjectInfo).Inc()
			metrics.GnfdChainTime.WithLabelValues(ChainFailureQueryBucketInfoAndObjectInfo).Observe(
				time.Since(startTime).Seconds())
			metrics.GnfdChainCounter.WithLabelValues(ChainFailureTotal).Inc()
			metrics.GnfdChainTime.WithLabelValues(ChainFailureTotal).Observe(
				time.Since(startTime).Seconds())
			return
		}
		metrics.GnfdChainCounter.WithLabelValues(ChainSuccessQueryBucketInfoAndObjectInfo).Inc()
		metrics.GnfdChainTime.WithLabelValues(ChainSuccessQueryBucketInfoAndObjectInfo).Observe(
			time.Since(startTime).Seconds())
		metrics.GnfdChainCounter.WithLabelValues(ChainSuccessTotal).Inc()
		metrics.GnfdChainTime.WithLabelValues(ChainSuccessTotal).Observe(
			time.Since(startTime).Seconds())
	}()

	bucketInfo, err = g.QueryBucketInfo(ctx, bucket)
	if err != nil {
		return nil, nil, err
	}
	objectInfo, err = g.QueryObjectInfo(ctx, bucket, object)
	if err != nil {
		return bucketInfo, nil, err
	}
	return bucketInfo, objectInfo, nil
}

// ListenObjectSeal returns an indication of the object is sealed.
// TODO:: retrieve service support seal event subscription
func (g *Gnfd) ListenObjectSeal(ctx context.Context, objectID uint64, timeoutHeight int) (seal bool, err error) {
	startTime := time.Now()
	defer func() {
		if err != nil {
			metrics.GnfdChainCounter.WithLabelValues(ChainFailureListenObjectSeal).Inc()
			metrics.GnfdChainTime.WithLabelValues(ChainFailureListenObjectSeal).Observe(
				time.Since(startTime).Seconds())
			metrics.GnfdChainCounter.WithLabelValues(ChainFailureTotal).Inc()
			metrics.GnfdChainTime.WithLabelValues(ChainFailureTotal).Observe(
				time.Since(startTime).Seconds())
			return
		}
		metrics.GnfdChainCounter.WithLabelValues(ChainSuccessListenObjectSeal).Inc()
		metrics.GnfdChainTime.WithLabelValues(ChainSuccessListenObjectSeal).Observe(
			time.Since(startTime).Seconds())
		metrics.GnfdChainCounter.WithLabelValues(ChainSuccessTotal).Inc()
		metrics.GnfdChainTime.WithLabelValues(ChainSuccessTotal).Observe(
			time.Since(startTime).Seconds())
	}()

	var objectInfo *storagetypes.ObjectInfo
	for i := 0; i < timeoutHeight; i++ {
		objectInfo, err = g.QueryObjectInfoByID(ctx, strconv.FormatUint(objectID, 10))
		if err != nil {
			time.Sleep(ExpectedOutputBlockInternal * time.Second)
			continue
		}
		if objectInfo.GetObjectStatus() == storagetypes.OBJECT_STATUS_SEALED && !objectInfo.GetIsUpdating() {
			log.CtxDebugw(ctx, "succeed to listen object stat")
			return true, nil
		}
		time.Sleep(ExpectedOutputBlockInternal * time.Second)
	}
	if err == nil {
		log.CtxErrorw(ctx, "seal object timeout", "object_id", objectID)
		return false, ErrSealTimeout
	}
	log.CtxErrorw(ctx, "failed to listen seal object", "object_id", objectID, "error", err)
	return false, err
}

// ListenRejectUnSealObject returns an indication of the object is rejected.
// TODO:: retrieve service support reject unseal event subscription
func (g *Gnfd) ListenRejectUnSealObject(ctx context.Context, objectID uint64, timeoutHeight int) (rejected bool, err error) {
	startTime := time.Now()
	defer func() {
		if err != nil {
			metrics.GnfdChainCounter.WithLabelValues(ChainFailureListenRejectUnSealObject).Inc()
			metrics.GnfdChainTime.WithLabelValues(ChainFailureListenRejectUnSealObject).Observe(
				time.Since(startTime).Seconds())
			metrics.GnfdChainCounter.WithLabelValues(ChainFailureTotal).Inc()
			metrics.GnfdChainTime.WithLabelValues(ChainFailureTotal).Observe(
				time.Since(startTime).Seconds())
			return
		}
		metrics.GnfdChainCounter.WithLabelValues(ChainSuccessListenRejectUnSealObject).Inc()
		metrics.GnfdChainTime.WithLabelValues(ChainSuccessListenRejectUnSealObject).Observe(
			time.Since(startTime).Seconds())
		metrics.GnfdChainCounter.WithLabelValues(ChainSuccessTotal).Inc()
		metrics.GnfdChainTime.WithLabelValues(ChainSuccessTotal).Observe(
			time.Since(startTime).Seconds())
	}()

	for i := 0; i < timeoutHeight; i++ {
		_, err = g.QueryObjectInfoByID(ctx, strconv.FormatUint(objectID, 10))
		if err != nil {
			if strings.Contains(err.Error(), "No such object") {
				return true, nil
			}
		}
		time.Sleep(ExpectedOutputBlockInternal * time.Second)
	}
	if err == nil {
		log.CtxErrorw(ctx, "reject unseal object timeout", "object_id", objectID)
		return false, ErrRejectUnSealTimeout
	}
	log.CtxErrorw(ctx, "failed to listen reject unseal object", "object_id", objectID, "error", err)
	return false, err
}

// QueryPaymentStreamRecord returns the steam record info by account.
func (g *Gnfd) QueryPaymentStreamRecord(ctx context.Context, account string) (stream *paymenttypes.StreamRecord, err error) {
	startTime := time.Now()
	defer func() {
		if err != nil {
			metrics.GnfdChainCounter.WithLabelValues(ChainFailureQueryPaymentStreamRecord).Inc()
			metrics.GnfdChainTime.WithLabelValues(ChainFailureQueryPaymentStreamRecord).Observe(
				time.Since(startTime).Seconds())
			metrics.GnfdChainCounter.WithLabelValues(ChainFailureTotal).Inc()
			metrics.GnfdChainTime.WithLabelValues(ChainFailureTotal).Observe(
				time.Since(startTime).Seconds())
			return
		}
		metrics.GnfdChainCounter.WithLabelValues(ChainSuccessQueryPaymentStreamRecord).Inc()
		metrics.GnfdChainTime.WithLabelValues(ChainSuccessQueryPaymentStreamRecord).Observe(
			time.Since(startTime).Seconds())
		metrics.GnfdChainCounter.WithLabelValues(ChainSuccessTotal).Inc()
		metrics.GnfdChainTime.WithLabelValues(ChainSuccessTotal).Observe(
			time.Since(startTime).Seconds())
	}()

	client := g.getCurrentClient().GnfdClient()
	resp, err := client.StreamRecord(ctx, &paymenttypes.QueryGetStreamRecordRequest{
		Account: account,
	})
	if err != nil {
		log.CtxErrorw(ctx, "failed to query stream record", "account", account, "error", err)
		return nil, err
	}
	return &resp.StreamRecord, nil
}

// VerifyGetObjectPermission verifies get object permission.
func (g *Gnfd) VerifyGetObjectPermission(ctx context.Context, account, bucket, object string) (allow bool, err error) {
	startTime := time.Now()
	defer func() {
		if err != nil {
			metrics.GnfdChainCounter.WithLabelValues(ChainFailureVerifyGetObjectPermission).Inc()
			metrics.GnfdChainTime.WithLabelValues(ChainFailureVerifyGetObjectPermission).Observe(
				time.Since(startTime).Seconds())
			metrics.GnfdChainCounter.WithLabelValues(ChainFailureTotal).Inc()
			metrics.GnfdChainTime.WithLabelValues(ChainFailureTotal).Observe(
				time.Since(startTime).Seconds())
			return
		}
		metrics.GnfdChainCounter.WithLabelValues(ChainSuccessVerifyGetObjectPermission).Inc()
		metrics.GnfdChainTime.WithLabelValues(ChainSuccessVerifyGetObjectPermission).Observe(
			time.Since(startTime).Seconds())
		metrics.GnfdChainCounter.WithLabelValues(ChainSuccessTotal).Inc()
		metrics.GnfdChainTime.WithLabelValues(ChainSuccessTotal).Observe(
			time.Since(startTime).Seconds())
	}()

	client := g.getCurrentClient().GnfdClient()
	resp, err := client.VerifyPermission(ctx, &storagetypes.QueryVerifyPermissionRequest{
		Operator:   account,
		BucketName: bucket,
		ObjectName: object,
		ActionType: permissiontypes.ACTION_GET_OBJECT,
	})
	if err != nil {
		log.CtxErrorw(ctx, "failed to verify get object permission", "account", account, "error", err)
		return false, err
	}
	if resp.GetEffect() == permissiontypes.EFFECT_ALLOW {
		return true, err
	}
	return false, err
}

// VerifyPutObjectPermission verifies put object permission.
func (g *Gnfd) VerifyPutObjectPermission(ctx context.Context, account, bucket, object string) (allow bool, err error) {
	startTime := time.Now()
	defer func() {
		if err != nil {
			metrics.GnfdChainCounter.WithLabelValues(ChainFailureVerifyPutObjectPermission).Inc()
			metrics.GnfdChainTime.WithLabelValues(ChainFailureVerifyPutObjectPermission).Observe(
				time.Since(startTime).Seconds())
			metrics.GnfdChainCounter.WithLabelValues(ChainFailureTotal).Inc()
			metrics.GnfdChainTime.WithLabelValues(ChainFailureTotal).Observe(
				time.Since(startTime).Seconds())
			return
		}
		metrics.GnfdChainCounter.WithLabelValues(ChainSuccessVerifyPutObjectPermission).Inc()
		metrics.GnfdChainTime.WithLabelValues(ChainSuccessVerifyPutObjectPermission).Observe(
			time.Since(startTime).Seconds())
		metrics.GnfdChainCounter.WithLabelValues(ChainSuccessTotal).Inc()
		metrics.GnfdChainTime.WithLabelValues(ChainSuccessTotal).Observe(
			time.Since(startTime).Seconds())
	}()

	_ = object
	client := g.getCurrentClient().GnfdClient()
	resp, err := client.VerifyPermission(ctx, &storagetypes.QueryVerifyPermissionRequest{
		Operator:   account,
		BucketName: bucket,
		// TODO: Polish the function interface according to the semantics
		// ObjectName: object,
		ActionType: permissiontypes.ACTION_CREATE_OBJECT,
	})
	if err != nil {
		log.CtxErrorw(ctx, "failed to verify put object permission", "account", account, "error", err)
		return false, err
	}
	if resp.GetEffect() == permissiontypes.EFFECT_ALLOW {
		return true, err
	}
	return false, err
}

// ConfirmTransaction is used to confirm whether the transaction is on the chain.
func (g *Gnfd) ConfirmTransaction(ctx context.Context, txHash string) (*sdk.TxResponse, error) {
	startTime := time.Now()
	defer func() {
		metrics.GnfdChainTime.WithLabelValues("confirm_transaction").Observe(time.Since(startTime).Seconds())
	}()
	client := g.getCurrentClient().GnfdClient()
	for i := 0; i < ConfirmBlockNumber; i++ {
		txResponse, err := client.GetTx(ctx, &tx.GetTxRequest{Hash: txHash})
		if err != nil {
			if strings.Contains(err.Error(), "not found") {
				// Tx not found, wait for next block and try again
				if err = g.WaitForNextBlock(ctx); err != nil {
					log.Warnf("failed to wait for next block err:%v", err)
				}
				continue
			}
			return nil, err
		}
		// Tx found
		return txResponse.TxResponse, nil
	}
	return nil, fmt.Errorf("failed to confirm transaction, tx_hash=%s", txHash)
}

// WaitForNextBlock is used to chain generate a new block.
func (g *Gnfd) WaitForNextBlock(ctx context.Context) error {
	startTime := time.Now()
	defer func() {
		metrics.GnfdChainTime.WithLabelValues("wait_for_next_block").Observe(time.Since(startTime).Seconds())
	}()
	var (
		err               error
		height            int64
		latestBlockHeight int64
	)
	height, err = g.getLatestBlockHeight(ctx)
	if err != nil {
		return err
	}
	ctxTimeout, cancel := context.WithTimeout(ctx, WaitForNextBlockTimeout)
	defer cancel()

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		if latestBlockHeight, err = g.getLatestBlockHeight(ctx); err != nil {
			return err
		}
		if latestBlockHeight >= height+1 {
			return nil
		}
		select {
		case <-ctxTimeout.Done():
			return fmt.Errorf("timeout exceeded waiting for block")
		case <-ticker.C:
		}
	}
}

func (g *Gnfd) getLatestBlockHeight(ctx context.Context) (int64, error) {
	client := g.getCurrentClient().GnfdClient()
	block, err := client.GetLatestBlock(ctx, &tmservice.GetLatestBlockRequest{})
	if err != nil {
		return 0, err
	}
	return block.SdkBlock.Header.Height, nil
}

// QuerySwapInInfo is used to chain get swapIn info
func (g *Gnfd) QuerySwapInInfo(ctx context.Context, vgfID, gvgID uint32) (swapInInfo *virtualgrouptypes.SwapInInfo, err error) {
	startTime := time.Now()
	defer func() {
		if err != nil {
			metrics.GnfdChainCounter.WithLabelValues(ChainFailureQuerySwapInInfo).Inc()
			metrics.GnfdChainTime.WithLabelValues(ChainFailureQuerySwapInInfo).Observe(
				time.Since(startTime).Seconds())
			metrics.GnfdChainCounter.WithLabelValues(ChainFailureTotal).Inc()
			metrics.GnfdChainTime.WithLabelValues(ChainFailureTotal).Observe(
				time.Since(startTime).Seconds())
			return
		}
		metrics.GnfdChainCounter.WithLabelValues(ChainSuccessQuerySwapInInfo).Inc()
		metrics.GnfdChainTime.WithLabelValues(ChainSuccessQuerySwapInInfo).Observe(
			time.Since(startTime).Seconds())
		metrics.GnfdChainCounter.WithLabelValues(ChainSuccessTotal).Inc()
		metrics.GnfdChainTime.WithLabelValues(ChainSuccessTotal).Observe(
			time.Since(startTime).Seconds())
	}()

	client := g.getCurrentClient().GnfdClient()
	resp, err := client.SwapInInfo(ctx,
		&virtualgrouptypes.QuerySwapInInfoRequest{
			GlobalVirtualGroupFamilyId: vgfID, GlobalVirtualGroupId: gvgID,
		})
	if err != nil {
		log.CtxErrorw(ctx, "failed to query swapIn info", "vgf_id", vgfID, "gvg_id", gvgID, "error", err)
		return nil, err
	}
	return resp.SwapInInfo, nil
}

func (g *Gnfd) QueryShadowObjectInfo(ctx context.Context, bucket, object string) (objectInfo *storagetypes.ShadowObjectInfo, err error) {
	startTime := time.Now()
	defer func() {
		if err != nil {
			metrics.GnfdChainCounter.WithLabelValues(ChainFailureQueryObjectInfo).Inc()
			metrics.GnfdChainTime.WithLabelValues(ChainFailureQueryObjectInfo).Observe(
				time.Since(startTime).Seconds())
			metrics.GnfdChainCounter.WithLabelValues(ChainFailureTotal).Inc()
			metrics.GnfdChainTime.WithLabelValues(ChainFailureTotal).Observe(
				time.Since(startTime).Seconds())
			return
		}
		metrics.GnfdChainCounter.WithLabelValues(ChainSuccessQueryObjectInfo).Inc()
		metrics.GnfdChainTime.WithLabelValues(ChainSuccessQueryObjectInfo).Observe(
			time.Since(startTime).Seconds())
		metrics.GnfdChainCounter.WithLabelValues(ChainSuccessTotal).Inc()
		metrics.GnfdChainTime.WithLabelValues(ChainSuccessTotal).Observe(
			time.Since(startTime).Seconds())
	}()

	client := g.getCurrentClient().GnfdClient()
	resp, err := client.HeadShadowObject(ctx, &storagetypes.QueryHeadShadowObjectRequest{
		BucketName: bucket,
		ObjectName: object,
	})
	if err != nil {
		log.CtxErrorw(ctx, "failed to query object", "bucket_name", bucket, "object_name", object, "error", err)
		return nil, err
	}
	return resp.GetObjectInfo(), nil
}

func (g *Gnfd) VerifyUpdateObjectPermission(ctx context.Context, account, bucket, object string) (allow bool, err error) {
	startTime := time.Now()
	defer func() {
		if err != nil {
			metrics.GnfdChainCounter.WithLabelValues(ChainFailureVerifyPutObjectPermission).Inc()
			metrics.GnfdChainTime.WithLabelValues(ChainFailureVerifyPutObjectPermission).Observe(
				time.Since(startTime).Seconds())
			metrics.GnfdChainCounter.WithLabelValues(ChainFailureTotal).Inc()
			metrics.GnfdChainTime.WithLabelValues(ChainFailureTotal).Observe(
				time.Since(startTime).Seconds())
			return
		}
		metrics.GnfdChainCounter.WithLabelValues(ChainSuccessVerifyPutObjectPermission).Inc()
		metrics.GnfdChainTime.WithLabelValues(ChainSuccessVerifyPutObjectPermission).Observe(
			time.Since(startTime).Seconds())
		metrics.GnfdChainCounter.WithLabelValues(ChainSuccessTotal).Inc()
		metrics.GnfdChainTime.WithLabelValues(ChainSuccessTotal).Observe(
			time.Since(startTime).Seconds())
	}()
	client := g.getCurrentClient().GnfdClient()
	resp, err := client.VerifyPermission(ctx, &storagetypes.QueryVerifyPermissionRequest{
		Operator:   account,
		BucketName: bucket,
		ObjectName: object,
		ActionType: permissiontypes.ACTION_UPDATE_OBJECT_CONTENT,
	})
	if err != nil {
		log.CtxErrorw(ctx, "failed to verify update object content permission", "account", account, "bucket_name", bucket, "object_name", object, "error", err)
		return false, err
	}
	return resp.GetEffect() == permissiontypes.EFFECT_ALLOW, err
}
