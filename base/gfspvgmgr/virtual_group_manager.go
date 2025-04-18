package gfspvgmgr

import (
	"context"
	"crypto/tls"
	"fmt"
	"math/rand"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	sptypes "github.com/evmos/evmos/v12/x/sp/types"
	virtualgrouptypes "github.com/evmos/evmos/v12/x/virtualgroup/types"
	"github.com/zkMeLabs/mechain-storage-provider/base/gfspclient"
	"github.com/zkMeLabs/mechain-storage-provider/base/types/gfsperrors"
	"github.com/zkMeLabs/mechain-storage-provider/core/consensus"
	"github.com/zkMeLabs/mechain-storage-provider/core/vgmgr"
	"github.com/zkMeLabs/mechain-storage-provider/pkg/log"
	"github.com/zkMeLabs/mechain-storage-provider/pkg/metrics"
	"github.com/zkMeLabs/mechain-storage-provider/util"
)

var _ vgmgr.VirtualGroupManager = &virtualGroupManager{}

const (
	VirtualGroupManagerSpace            = "VirtualGroupManager"
	RefreshMetaInterval                 = 5 * time.Second
	MaxStorageUsageRatio                = 0.95
	DefaultInitialGVGStakingStorageSize = uint64(1) * 1024 * 1024 * 1024 * 256 // 256GB per GVG, chain side DefaultMaxStoreSizePerFamily is 64 TB
	additionalGVGStakingStorageSize     = uint64(1) * 1024 * 1024 * 1024 * 512 // 0.5TB

	defaultSPCheckTimeout               = 1 * time.Minute
	defaultSPHealthCheckerInterval      = 10 * time.Second
	defaultSPHealthCheckerRetryInterval = 1 * time.Second
	defaultSPHealthCheckerMaxRetries    = 5
	httpStatusPath                      = "/status"

	emptyGVGSafeDeletePeriod = int64(60) * 60 * 24
)

var (
	ErrFailedPickVGF = gfsperrors.Register(VirtualGroupManagerSpace, http.StatusInternalServerError, 540001,
		"failed to pick virtual group family, need to create global virtual group")
	ErrFailedPickGVG = gfsperrors.Register(VirtualGroupManagerSpace, http.StatusInternalServerError, 540002,
		"failed to pick global virtual group, need to stake more storage size")
	ErrStaledMetadata = gfsperrors.Register(VirtualGroupManagerSpace, http.StatusInternalServerError, 540003,
		"metadata is staled, need to force refresh metadata")
	ErrFailedPickDestSP = gfsperrors.Register(VirtualGroupManagerSpace, http.StatusInternalServerError, 540004,
		"failed to pick dest sp")
)

// virtualGroupFamilyManager is built by metadata data source.
type virtualGroupFamilyManager struct {
	vgfIDToVgf map[uint32]*vgmgr.VirtualGroupFamilyMeta // is used to pick VGF
}

// FreeStorageSizeWeightPicker is used to pick index by storage usage,
// The more free storage usage, the greater the probability of being picked.
type FreeStorageSizeWeightPicker struct {
	freeStorageSizeWeightMap map[uint32]float64
}

func (picker *FreeStorageSizeWeightPicker) addVirtualGroupFamily(vgf *vgmgr.VirtualGroupFamilyMeta) {
	if float64(vgf.FamilyUsedStorageSize) >= MaxStorageUsageRatio*float64(vgf.FamilyStakingStorageSize) || vgf.FamilyStakingStorageSize == 0 {
		return
	}
	picker.freeStorageSizeWeightMap[vgf.ID] = float64(vgf.FamilyStakingStorageSize-vgf.FamilyUsedStorageSize) / float64(vgf.FamilyStakingStorageSize)
}

func (picker *FreeStorageSizeWeightPicker) addGlobalVirtualGroup(gvg *vgmgr.GlobalVirtualGroupMeta) {
	if float64(gvg.UsedStorageSize) >= MaxStorageUsageRatio*float64(gvg.StakingStorageSize) {
		return
	}
	picker.freeStorageSizeWeightMap[gvg.ID] = float64(gvg.StakingStorageSize-gvg.UsedStorageSize) / float64(gvg.StakingStorageSize)
}

func (picker *FreeStorageSizeWeightPicker) pickIndex() (uint32, error) {
	var (
		sumWeight     float64
		pickWeight    float64
		tempSumWeight float64
	)
	for _, value := range picker.freeStorageSizeWeightMap {
		sumWeight += value
	}
	pickWeight = rand.Float64() * sumWeight
	for key, value := range picker.freeStorageSizeWeightMap {
		tempSumWeight += value
		if tempSumWeight > pickWeight {
			return key, nil
		}
	}
	return 0, fmt.Errorf("failed to pick weighted random index")
}

func (vgfm *virtualGroupFamilyManager) pickVirtualGroupFamily(filter *vgmgr.PickVGFFilter, filterByGvgList *vgmgr.PickVGFByGVGFilter, healthChecker *HealthChecker) (*vgmgr.VirtualGroupFamilyMeta, error) {
	var (
		picker   FreeStorageSizeWeightPicker
		familyID uint32
		err      error
	)
	picker.freeStorageSizeWeightMap = make(map[uint32]float64)
	for id, f := range vgfm.vgfIDToVgf {
		if filter != nil && !filter.Check(id) {
			continue
		}
		if !filterByGvgList.Check(f.GVGMap) {
			continue
		}
		if healthChecker != nil && !healthChecker.isVGFHealthy(f) {
			continue
		}
		picker.addVirtualGroupFamily(f)
	}

	if familyID, err = picker.pickIndex(); err != nil {
		log.Errorw("failed to pick vgf", "error", err)
		return nil, ErrFailedPickVGF
	}
	return vgfm.vgfIDToVgf[familyID], nil
}

func (vgfm *virtualGroupFamilyManager) pickGlobalVirtualGroup(vgfID uint32, filter, excludeGVGsFilter vgmgr.ExcludeFilter, healthChecker *HealthChecker) (*vgmgr.GlobalVirtualGroupMeta, error) {
	var (
		picker               FreeStorageSizeWeightPicker
		globalVirtualGroupID uint32
		err                  error
	)
	picker.freeStorageSizeWeightMap = make(map[uint32]float64)
	if _, existed := vgfm.vgfIDToVgf[vgfID]; !existed {
		return nil, ErrStaledMetadata
	}
	for _, g := range vgfm.vgfIDToVgf[vgfID].GVGMap {
		if filter != nil && filter.Apply(g.ID) {
			continue
		}
		if excludeGVGsFilter != nil && excludeGVGsFilter.Apply(g.ID) {
			continue
		}
		if healthChecker != nil && !healthChecker.isGVGHealthy(g) {
			continue
		}
		picker.addGlobalVirtualGroup(g)
	}

	if globalVirtualGroupID, err = picker.pickIndex(); err != nil {
		log.Errorw("failed to pick gvg", "vgf_id", vgfID, "error", err)
		return nil, ErrFailedPickGVG
	}
	return vgfm.vgfIDToVgf[vgfID].GVGMap[globalVirtualGroupID], nil
}

func (vgfm *virtualGroupFamilyManager) pickGlobalVirtualGroupForBucketMigrate(vgfFilter vgmgr.VGFPickFilter, gvgFilter vgmgr.GVGPickFilter) (*vgmgr.GlobalVirtualGroupMeta, error) {
	var (
		picker               FreeStorageSizeWeightPicker
		globalVirtualGroupID uint32
		err                  error
	)

	for vgfID, vgf := range vgfm.vgfIDToVgf {
		if vgfFilter != nil && !vgfFilter.Check(vgfID) {
			continue
		}
		if !gvgFilter.CheckFamily(vgfID) {
			continue
		}
		picker.freeStorageSizeWeightMap = make(map[uint32]float64)
		for _, gvg := range vgf.GVGMap {
			log.Debugw("prepare to add pickGlobalVirtualGroupForBucketMigrate", "gvg", gvg)
			if gvgFilter.CheckGVG(gvg) {
				picker.addGlobalVirtualGroup(gvg)
				log.Debugw("add pickGlobalVirtualGroupForBucketMigrate", "gvg", gvg)
			}
		}
		if globalVirtualGroupID, err = picker.pickIndex(); err != nil {
			log.Errorw("failed to pick gvg at current vgf", "vgf_id", vgfID, "error", err)
			continue
		}
		return vgfm.vgfIDToVgf[vgfID].GVGMap[globalVirtualGroupID], nil
	}
	log.Errorw("failed to pick gvg for migrate bucket")
	return nil, ErrFailedPickGVG
}

type spManager struct {
	selfSP   *sptypes.StorageProvider
	otherSPs []*sptypes.StorageProvider
}

func (sm *spManager) generateVirtualGroupMeta(genPolicy vgmgr.GenerateGVGSecondarySPsPolicy, filter, excludeSPsFilter vgmgr.ExcludeFilter, healthChecker *HealthChecker) (*vgmgr.GlobalVirtualGroupMeta, error) {
	for _, sp := range sm.otherSPs {
		if !sp.IsInService() {
			continue
		}
		if filter != nil && filter.Apply(sp.Id) {
			continue
		}
		if excludeSPsFilter != nil && excludeSPsFilter.Apply(sp.Id) {
			continue
		}
		if healthChecker != nil && !healthChecker.isSPHealthy(sp.GetId()) {
			continue
		}
		genPolicy.AddCandidateSP(sp.GetId())
	}
	secondarySPIDs, err := genPolicy.GenerateGVGSecondarySPs()
	if err != nil {
		return nil, err
	}

	return &vgmgr.GlobalVirtualGroupMeta{
		PrimarySPID:        sm.selfSP.Id,
		SecondarySPIDs:     secondarySPIDs,
		StakingStorageSize: DefaultInitialGVGStakingStorageSize,
	}, nil
}

func (sm *spManager) pickSPByFilter(filter vgmgr.SPPickFilter, healthChecker *HealthChecker) (*sptypes.StorageProvider, error) {
	for _, destSP := range sm.otherSPs {
		if !destSP.IsInService() {
			continue
		}
		if healthChecker != nil && !healthChecker.isSPHealthy(destSP.GetId()) {
			continue
		}
		if pickSucceed := filter.Check(destSP.GetId()); pickSucceed {
			return destSP, nil
		}
	}
	return nil, ErrFailedPickDestSP
}

func (sm *spManager) querySPByID(spID uint32) (*sptypes.StorageProvider, error) {
	if sm.selfSP.GetId() == spID {
		return sm.selfSP, nil
	}
	for _, sp := range sm.otherSPs {
		if sp.GetId() == spID {
			return sp, nil
		}
	}
	return nil, ErrFailedPickDestSP
}

type virtualGroupManager struct {
	selfOperatorAddress string
	chainClient         consensus.Consensus // query VG params from chain
	gfspClient          gfspclient.GfSpClientAPI
	mutex               sync.RWMutex
	selfSPID            uint32
	spManager           *spManager // is used to generate a new gvg
	vgParams            *virtualgrouptypes.Params
	vgfManager          *virtualGroupFamilyManager
	freezeSPPool        *FreezeSPPool
	healthChecker       *HealthChecker
	gvgGCMap            sync.Map // Keep track of empty GVG and the time for GC. Once a GVG is detected empty, it will be put into gvgGCMap, and delete it if it is still empty after 1 day
}

// NewVirtualGroupManager returns a virtual group manager interface.
func NewVirtualGroupManager(selfOperatorAddress string, chainClient consensus.Consensus, gfspClient gfspclient.GfSpClientAPI, enableHealthyChecker bool) (vgmgr.VirtualGroupManager, error) {
	var healthChecker *HealthChecker
	if enableHealthyChecker {
		healthChecker = NewHealthChecker(chainClient)
	} else {
		healthChecker = nil
	}
	vgm := &virtualGroupManager{
		selfOperatorAddress: selfOperatorAddress,
		chainClient:         chainClient,
		gfspClient:          gfspClient,
		freezeSPPool:        NewFreezeSPPool(),
		healthChecker:       healthChecker,
		gvgGCMap:            sync.Map{},
	}
	vgm.refreshGVGMeta(true)
	go func() {
		RefreshMetaTicker := time.NewTicker(RefreshMetaInterval)
		for range RefreshMetaTicker.C {
			// log.Info("start to refresh virtual group manager meta")
			vgm.refreshGVGMeta(false)
			// log.Info("finish to refresh virtual group manager meta")
		}
	}()
	go vgm.releaseSPAndGVGLoop()
	if vgm.healthChecker != nil {
		go vgm.healthChecker.Start()
	}
	return vgm, nil
}

// refreshGVGMeta is used to refresh virtual group manager metadata in background.
func (vgm *virtualGroupManager) refreshGVGMeta(byChain bool) {
	var (
		err         error
		spList      []*sptypes.StorageProvider
		selfSP      *sptypes.StorageProvider
		otherSPList []*sptypes.StorageProvider
		spID        uint32
		spm         *spManager
		vgParams    *virtualgrouptypes.Params
		vgfList     []*virtualgrouptypes.GlobalVirtualGroupFamily
		vgfm        *virtualGroupFamilyManager
		spMap       map[uint32]*sptypes.StorageProvider
	)

	spMap = make(map[uint32]*sptypes.StorageProvider)
	toSPEndpoints := func(spIDs []uint32) []string {
		spInfoEndpoints := make([]string, 0)
		for _, id := range spIDs {
			spInfoEndpoints = append(spInfoEndpoints, spMap[id].GetEndpoint())
		}
		return spInfoEndpoints
	}
	// query meta
	if spList, err = vgm.chainClient.ListSPs(context.Background()); err != nil {
		log.Errorw("failed to list sps", "error", err)
		return
	}
	sort.Slice(spList, func(i, j int) bool {
		return spList[i].Id < spList[j].Id
	})

	for _, sp := range spList {
		spMap[sp.Id] = sp
	}
	for i, sp := range spList {
		if strings.EqualFold(vgm.selfOperatorAddress, sp.OperatorAddress) {
			spID = sp.Id
			selfSP = sp
			otherSPList = append(spList[:i], spList[i+1:]...)
		}
	}
	spm = &spManager{
		selfSP:   selfSP,
		otherSPs: otherSPList,
	}
	// log.Infow("list sp info", "primary_sp", primarySP, "secondary_sps", secondarySPList, "sp_map", spMap)
	// add other SP list into health checker, except self sp, self sp should always be healthy
	if vgm.healthChecker != nil {
		vgm.healthChecker.addAllSP(otherSPList)
	}

	if spID == 0 {
		log.Error("failed to refresh due to current sp is not in sp list")
		return
	}

	if vgParams, err = vgm.chainClient.QueryVirtualGroupParams(context.Background()); err != nil {
		log.Errorw("failed to query virtual group params", "error", err)
		return
	}
	// log.Infow("query virtual group params", "params", vgParams)

	vgfm = &virtualGroupFamilyManager{
		vgfIDToVgf: make(map[uint32]*vgmgr.VirtualGroupFamilyMeta),
	}
	if vgfList, err = vgm.chainClient.ListVirtualGroupFamilies(context.Background(), spID); err != nil {
		log.Errorw("failed to list virtual group family", "error", err)
		return
	}

	// log.Infow("list virtual group family info", "vgf_list", vgfList)
	for _, vgf := range vgfList {
		vgfm.vgfIDToVgf[vgf.Id] = &vgmgr.VirtualGroupFamilyMeta{
			ID:          vgf.Id,
			PrimarySPID: spID,
			GVGMap:      make(map[uint32]*vgmgr.GlobalVirtualGroupMeta),
		}
		for _, gvgID := range vgf.GlobalVirtualGroupIds {
			var gvg *virtualgrouptypes.GlobalVirtualGroup
			if byChain {
				if gvg, err = vgm.chainClient.QueryGlobalVirtualGroup(context.Background(), gvgID); err != nil {
					log.Errorw("failed to query global virtual group from chain", "error", err)
					return
				}
			} else {
				gvg, err = vgm.gfspClient.GetGlobalVirtualGroupByGvgID(context.Background(), gvgID)
				if err != nil || gvg == nil {
					log.Errorw("failed to query global virtual group from meta", "gvg_id", gvgID, "error", err)
					return
				}
			}
			deposited, deleted, err := vgm.monitorGVGUsage(gvg, vgParams, byChain)
			if err != nil {
				log.Errorw("failed to monitor global virtual group usage", "gvgID", gvg.Id, "error", err)
				return
			}
			if deleted {
				continue
			}
			if deposited {
				time.Sleep(RefreshMetaInterval)
				gvg, err = vgm.chainClient.QueryGlobalVirtualGroup(context.Background(), gvg.Id)
				if err != nil {
					log.Errorw("failed to query global virtual group", "error", err)
					return
				}
			}
			gvgMeta := &vgmgr.GlobalVirtualGroupMeta{
				ID:                   gvg.GetId(),
				FamilyID:             vgf.Id,
				PrimarySPID:          spID,
				SecondarySPIDs:       gvg.GetSecondarySpIds(),
				SecondarySPEndpoints: toSPEndpoints(gvg.GetSecondarySpIds()),
				UsedStorageSize:      gvg.GetStoredSize(),
				StakingStorageSize:   util.TotalStakingStoreSizeOfGVG(gvg, vgParams.GvgStakingPerBytes),
			}
			log.Infow("query global virtual group info", "gvg_info", gvg, "gvg_meta", gvgMeta)
			vgfm.vgfIDToVgf[vgf.Id].GVGMap[gvg.GetId()] = gvgMeta
			vgfm.vgfIDToVgf[vgf.Id].FamilyUsedStorageSize += gvgMeta.UsedStorageSize
			vgfm.vgfIDToVgf[vgf.Id].FamilyStakingStorageSize += gvgMeta.StakingStorageSize
		}
	}

	// update meta
	vgm.mutex.Lock()
	vgm.selfSPID = spID
	vgm.spManager = spm
	vgm.vgParams = vgParams
	vgm.vgfManager = vgfm
	vgm.mutex.Unlock()
}

// PickVirtualGroupFamily pick a virtual group family(If failed to pick,
// new VGF will be automatically created on the chain) in get create bucket approval workflow.
func (vgm *virtualGroupManager) PickVirtualGroupFamily(filterByGvgList *vgmgr.PickVGFByGVGFilter) (*vgmgr.VirtualGroupFamilyMeta, error) {
	filter, err := vgm.genVgfFilter()
	if err != nil {
		return nil, err
	}
	vgm.mutex.RLock()
	defer vgm.mutex.RUnlock()
	return vgm.vgfManager.pickVirtualGroupFamily(filter, filterByGvgList, vgm.healthChecker)
}

// PickGlobalVirtualGroup picks a global virtual group(If failed to pick,
// new GVG will be created by primary SP) in replicate/seal object workflow.
func (vgm *virtualGroupManager) PickGlobalVirtualGroup(vgfID uint32, excludeGVGsFilter vgmgr.ExcludeFilter) (*vgmgr.GlobalVirtualGroupMeta, error) {
	vgm.mutex.RLock()
	defer vgm.mutex.RUnlock()
	return vgm.vgfManager.pickGlobalVirtualGroup(vgfID, vgmgr.NewExcludeIDFilter(vgm.freezeSPPool.GetFreezeGVGsInFamily(vgfID)), excludeGVGsFilter, vgm.healthChecker)
}

// PickGlobalVirtualGroupForBucketMigrate picks a global virtual group(If failed to pick,
// new GVG will be created by primary SP) in migrate bucket workflow.
func (vgm *virtualGroupManager) PickGlobalVirtualGroupForBucketMigrate(filter vgmgr.GVGPickFilter) (*vgmgr.GlobalVirtualGroupMeta, error) {
	vgm.mutex.RLock()
	defer vgm.mutex.RUnlock()
	vgfFilter, err := vgm.genVgfFilter()
	if err != nil {
		return nil, err
	}
	return vgm.vgfManager.pickGlobalVirtualGroupForBucketMigrate(vgfFilter, filter)
}

// PickMigrateDestGlobalVirtualGroup picks a global virtual group(If failed to pick,
// new GVG will be created by primary SP) in replicate/seal object workflow.
func (vgm *virtualGroupManager) PickMigrateDestGlobalVirtualGroup(vgfID uint32, excludeGVGsFilter vgmgr.ExcludeFilter) (*vgmgr.GlobalVirtualGroupMeta, error) {
	vgm.mutex.RLock()
	defer vgm.mutex.RUnlock()
	return vgm.vgfManager.pickGlobalVirtualGroup(vgfID, vgmgr.NewExcludeIDFilter(vgm.freezeSPPool.GetFreezeGVGsInFamily(vgfID)), excludeGVGsFilter, vgm.healthChecker)
}

// ForceRefreshMeta is used to query metadata service and refresh the virtual group manager meta.
// if pick func returns ErrStaledMetadata, the caller need force refresh from metadata and retry pick.
// TODO: in the future background thread can pre-allocate gvg and reduce impact on foreground thread.
func (vgm *virtualGroupManager) ForceRefreshMeta() error {
	// sleep 5 seconds for waiting a new block
	time.Sleep(RefreshMetaInterval)
	vgm.refreshGVGMeta(true)
	return nil
}

// GenerateGlobalVirtualGroupMeta is used to generate a gvg meta.
func (vgm *virtualGroupManager) GenerateGlobalVirtualGroupMeta(genPolicy vgmgr.GenerateGVGSecondarySPsPolicy, excludeSPsFilter vgmgr.ExcludeFilter) (*vgmgr.GlobalVirtualGroupMeta, error) {
	vgm.mutex.RLock()
	defer vgm.mutex.RUnlock()
	return vgm.spManager.generateVirtualGroupMeta(genPolicy, vgmgr.NewExcludeIDFilter(vgm.freezeSPPool.GetFreezeSPIDs()), excludeSPsFilter, vgm.healthChecker)
}

// PickSPByFilter is used to pick sp by filter check.
func (vgm *virtualGroupManager) PickSPByFilter(filter vgmgr.SPPickFilter) (*sptypes.StorageProvider, error) {
	vgm.mutex.RLock()
	defer vgm.mutex.RUnlock()
	return vgm.spManager.pickSPByFilter(filter, vgm.healthChecker)
}

// QuerySPByID return sp info by sp id.
func (vgm *virtualGroupManager) QuerySPByID(spID uint32) (*sptypes.StorageProvider, error) {
	vgm.mutex.RLock()
	sp, err := vgm.spManager.querySPByID(spID)
	vgm.mutex.RUnlock()
	if err == nil {
		return sp, nil
	}
	// query chain if it is not found in memory topology.
	return vgm.chainClient.QuerySPByID(context.Background(), spID)
}

func (vgm *virtualGroupManager) genVgfFilter() (*vgmgr.PickVGFFilter, error) {
	vgm.mutex.RLock()
	vgfIDs := make([]uint32, len(vgm.vgfManager.vgfIDToVgf))
	if len(vgfIDs) == 0 {
		vgm.mutex.RUnlock()
		return nil, ErrFailedPickVGF
	}
	i := 0
	for k := range vgm.vgfManager.vgfIDToVgf {
		vgfIDs[i] = k
		i++
	}
	vgm.mutex.RUnlock()
	availableVgfIDs, err := vgm.chainClient.AvailableGlobalVirtualGroupFamilies(context.Background(), vgfIDs)
	if err != nil {
		log.Errorw("failed to get available virtual group families", "error", err)
		return nil, nil
	}
	return vgmgr.NewPickVGFFilter(availableVgfIDs), nil
}

// FreezeSPAndGVGs freeze a secondary SP and its GVGs
func (vgm *virtualGroupManager) FreezeSPAndGVGs(spID uint32, gvgs []*virtualgrouptypes.GlobalVirtualGroup) {
	vgm.freezeSPPool.FreezeSPAndGVGs(spID, gvgs)
}

func (vgm *virtualGroupManager) ReleaseAllSP() {
	vgm.freezeSPPool.ReleaseAllSP()
}

// releaseSPAndGVGLoop runs periodically to release SP from the freeze pool
func (vgm *virtualGroupManager) releaseSPAndGVGLoop() {
	ticker := time.NewTicker(ReleaseSPJobInterval)
	for range ticker.C {
		vgm.mutex.RLock()
		if vgm.spManager == nil {
			log.Warnw("failed to init sp manager")
			vgm.mutex.RUnlock()
			continue
		}
		vgm.mutex.RUnlock()

		vgm.freezeSPPool.ReleaseSP()
		vgm.mutex.RLock()
		aliveSP := make([]*sptypes.StorageProvider, 0)
		for _, sp := range vgm.spManager.otherSPs {
			if sp.IsInService() {
				aliveSP = append(aliveSP, sp)
			}
		}
		vgm.mutex.RUnlock()
		params, err := vgm.chainClient.QueryStorageParamsByTimestamp(context.Background(), time.Now().Unix())
		if err != nil {
			continue
		}
		if len(aliveSP)-len(vgm.freezeSPPool.GetFreezeSPIDs()) < int(params.GetRedundantDataChunkNum()+params.GetRedundantParityChunkNum()) {
			vgm.freezeSPPool.ReleaseAllSP()
		}
	}
}

func (vgm *virtualGroupManager) monitorGVGUsage(gvg *virtualgrouptypes.GlobalVirtualGroup, vgParams *virtualgrouptypes.Params, byChain bool) (deposited, deleted bool, err error) {
	needDeposit := func(gvg *virtualgrouptypes.GlobalVirtualGroup, vgParams *virtualgrouptypes.Params) bool {
		return float64(gvg.GetStoredSize()) >= MaxStorageUsageRatio*float64(util.TotalStakingStoreSizeOfGVG(gvg, vgParams.GvgStakingPerBytes))
	}
	isEmpty := func(gvg *virtualgrouptypes.GlobalVirtualGroup) bool {
		return gvg.StoredSize == 0
	}

	curTime := time.Now().Unix()
	if isEmpty(gvg) {
		// Remove any GVG from the gvgGCMap if it is no longer empty.
		vgm.gvgGCMap.Range(func(k, v interface{}) bool {
			safeDeleteTime := v.(int64)
			if curTime > safeDeleteTime+int64(time.Minute.Seconds()) {
				vgm.gvgGCMap.Delete(k)
			}
			return true
		})

		if !byChain {
			gvgID := gvg.Id
			gvg, err = vgm.chainClient.QueryGlobalVirtualGroup(context.Background(), gvgID)
			if err != nil {
				log.Errorw("failed to query global virtual group", "gvg_id", gvgID, "error", err)
				return
			}
			if !isEmpty(gvg) {
				log.Warnw("the gvg is not empty by querying from chain ", "gvg", gvg)
				return
			}
		}
		log.Infow("found GVG is empty", "GVG", gvg)
		var safeDeleteTime int64
		val, found := vgm.gvgGCMap.Load(gvg.Id)
		if !found {
			safeDeleteTime = curTime + emptyGVGSafeDeletePeriod
			vgm.gvgGCMap.Store(gvg.Id, safeDeleteTime)
			return
		}
		safeDeleteTime = val.(int64)
		if curTime < safeDeleteTime {
			log.Infow("GVG will be deleted at", "safe_delete_time", safeDeleteTime)
			return
		}
		log.Infow("start to delete GVG", "GVG", gvg)
		var deleteGVGHash string
		deleteGVGHash, err = vgm.gfspClient.DeleteGlobalVirtualGroup(context.Background(), &virtualgrouptypes.MsgDeleteGlobalVirtualGroup{
			GlobalVirtualGroupId: gvg.Id,
		})
		if err != nil {
			log.Errorw("failed to delete global virtual group", "gvg_id", gvg.Id, "tx_hash", deleteGVGHash, "error", err)
			return
		}
		vgm.gvgGCMap.Delete(gvg.Id)
		log.Infow("successfully delete GVG", "GVG", gvg, "tx_hash", deleteGVGHash)
		deleted = true
		return
	}

	if !needDeposit(gvg, vgParams) {
		return
	}

	msgDeposit := &virtualgrouptypes.MsgDeposit{
		GlobalVirtualGroupId: gvg.Id,
		Deposit: sdk.Coin{
			Denom:  vgParams.GetDepositDenom(),
			Amount: vgParams.GvgStakingPerBytes.Mul(math.NewIntFromUint64(additionalGVGStakingStorageSize)),
		},
	}
	if !byChain {
		gvgID := gvg.Id
		gvg, err = vgm.chainClient.QueryGlobalVirtualGroup(context.Background(), gvgID)
		if err != nil {
			log.Errorw("failed to query global virtual group", "gvg_id", gvgID, "error", err)
			return
		}
		if !needDeposit(gvg, vgParams) {
			log.Warnw("the gvg does not need deposit by querying from chain", gvg, gvg)
			return
		}
	}
	log.Infow("found GVG need to deposit", "GVG", gvg)
	var depositTxHash string
	depositTxHash, err = vgm.gfspClient.Deposit(context.Background(), msgDeposit)
	if err != nil {
		log.Errorw("failed to deposit global virtual group", "gvg_id", gvg.Id, "tx_hash", depositTxHash, "error", err)
		return
	}
	log.Infow("successfully deposit GVG", "GVG", gvg, "tx_hash", depositTxHash)
	deposited = true
	return
}

type HealthChecker struct {
	mutex        sync.RWMutex                        // The mutex is used to guard sps and unhealthySPs
	sps          map[uint32]*sptypes.StorageProvider // all sps,  id -> StorageProvider
	unhealthySPs map[uint32]*sptypes.StorageProvider

	chainClient consensus.Consensus // query VG params from chain
}

func NewHealthChecker(chainClient consensus.Consensus) *HealthChecker {
	sps := make(map[uint32]*sptypes.StorageProvider)
	unhealthySPs := make(map[uint32]*sptypes.StorageProvider)
	return &HealthChecker{sps: sps, unhealthySPs: unhealthySPs, chainClient: chainClient}
}

func (checker *HealthChecker) addAllSP(sps []*sptypes.StorageProvider) {
	checker.mutex.Lock()
	defer checker.mutex.Unlock()
	checker.sps = make(map[uint32]*sptypes.StorageProvider)
	for _, sp := range sps {
		checker.sps[sp.Id] = sp
	}
}

func (checker *HealthChecker) isSPHealthy(spID uint32) bool {
	checker.mutex.RLock()
	defer checker.mutex.RUnlock()

	if sp, exists := checker.sps[spID]; exists {
		if _, unhealthyExists := checker.unhealthySPs[spID]; unhealthyExists {
			log.CtxErrorw(context.Background(), "the sp is treated as unhealthy", "sp", sp, "sps", checker.sps, "unhealthy_sps", checker.unhealthySPs)
			return false
		} else {
			log.CtxInfow(context.Background(), "the sp isn't exist in unhealthy sp map, is treated as healthy", "sp", sp, "sps", checker.sps, "unhealthy_sps", checker.unhealthySPs)
			return true
		}
	}
	log.CtxInfow(context.Background(), "the sp isn't exist in sps map, is treated as healthy", "check_sp_id", spID, "sps", checker.sps, "unhealthy_sps", checker.unhealthySPs)
	return true
}

// isGVGHealthy GVG healthy means gvg primary sp & secondary sps is healthy
func (checker *HealthChecker) isGVGHealthy(gvg *vgmgr.GlobalVirtualGroupMeta) bool {
	// gvg secondary sp
	for _, spID := range gvg.SecondarySPIDs {
		if !checker.isSPHealthy(spID) {
			log.CtxErrorw(context.Background(), "the gvg is treated as unhealthy", "gvg", gvg)
			return false
		}
	}

	return true
}

// isVGFHealthy vgf healthy means at least one gvg is healthy
func (checker *HealthChecker) isVGFHealthy(vgf *vgmgr.VirtualGroupFamilyMeta) bool {
	gvgs := vgf.GVGMap
	if len(gvgs) == 0 {
		// vgf has no gvg treated as healthy
		return true
	}

	healthyCount := len(gvgs)
	for _, gvg := range gvgs {
		if !checker.isGVGHealthy(gvg) {
			healthyCount--
		}
	}
	if healthyCount == 0 {
		log.CtxErrorw(context.Background(), "the vgf is treated as unhealthy", "vgf", vgf)
	}
	return healthyCount > 0
}

func (checker *HealthChecker) checkAllSPHealth() {
	spTemp := make(map[uint32]*sptypes.StorageProvider)
	unhealthyTemp := make(map[uint32]*sptypes.StorageProvider)
	checker.mutex.RLock()
	for key, value := range checker.sps {
		spTemp[key] = value
	}
	checker.mutex.RUnlock()

	unhealthyCnt := 0
	for _, sp := range spTemp {
		if !checker.checkSPHealth(sp) {
			unhealthyCnt++
			unhealthyTemp[sp.GetId()] = sp
		} else {
			checker.mutex.Lock()
			// an SP is removed from unhealthy pool only when it is confirmed back to normal
			delete(checker.unhealthySPs, sp.Id)
			checker.mutex.Unlock()
		}
	}

	if unhealthyCnt == 0 {
		// quickly return, this is fast-path.
		log.Infow("all SPs are healthy", "unhealthy_sps", checker.unhealthySPs,
			"sps", checker.sps, "unhealthy_sp_cnt", unhealthyCnt)
		return
	}

	params, err := checker.chainClient.QueryStorageParamsByTimestamp(context.Background(), time.Now().Unix())
	if err != nil {
		log.Errorw("failed to query storage params from chain", "error", err)
		return
	}
	// Only when more than defaultMinAvailableSPThreshold valid sp, the check is valid
	if len(spTemp)-unhealthyCnt >= int(params.GetRedundantDataChunkNum()+params.GetRedundantParityChunkNum()) {
		checker.mutex.Lock()
		checker.unhealthySPs = unhealthyTemp
		checker.mutex.Unlock()
		log.Infow("succeed to place these sp into unhealthy status", "unhealthy_sps", checker.unhealthySPs,
			"sps", checker.sps, "unhealthy_sp_cnt", unhealthyCnt)
	} else {
		log.Errorw("the current checker determines that most SPs are abnormal", "unhealthy_sps", checker.unhealthySPs,
			"sps", checker.sps, "unhealthy_sp_cnt", unhealthyCnt)
	}
	log.Infow("succeed to check all sp health status", "unhealthy_sps", checker.unhealthySPs, "all_sps", checker.sps)
}

func (checker *HealthChecker) checkSPHealth(sp *sptypes.StorageProvider) bool {
	if !sp.IsInService() {
		log.CtxInfow(context.Background(), "the sp is not in service, sp is treated as unhealthy", "sp", sp)
		return false
	}

	ctxTimeout, cancel := context.WithTimeout(context.Background(), defaultSPCheckTimeout)
	defer cancel()
	endpoint := sp.GetEndpoint()

	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
		},
		Timeout: defaultSPCheckTimeout,
	}

	// Create an HTTP request to test the validity of the endpoint
	urlToCheck := fmt.Sprintf("%s%s", endpoint, httpStatusPath)
	for attempt := 0; attempt < defaultSPHealthCheckerMaxRetries; attempt++ {
		start := time.Now()
		req, err := http.NewRequestWithContext(ctxTimeout, http.MethodGet, urlToCheck, nil)
		if err != nil {
			log.CtxErrorw(context.Background(), "failed to create request", "sp", sp, "error", err)
			return false
		}

		resp, err := client.Do(req)
		duration := time.Since(start)
		metrics.SPHealthCheckerTime.WithLabelValues(strconv.Itoa(int(sp.Id))).Observe(duration.Seconds())
		if err != nil {
			log.CtxErrorw(context.Background(), "failed to connect to sp", "sp", sp, "error", err, "duration", duration)
			time.Sleep(defaultSPHealthCheckerRetryInterval)
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusOK {
			log.CtxInfow(context.Background(), "succeed to check the sp healthy", "sp", sp, "duration", duration)
			return true
		} else {
			metrics.SPHealthCheckerFailureCounter.WithLabelValues(strconv.Itoa(int(sp.Id))).Inc()
			log.CtxErrorw(context.Background(), "failed to check sp healthy", "sp", sp, "http_status_code", resp.StatusCode, "duration", duration)
			time.Sleep(defaultSPHealthCheckerRetryInterval)
		}
	}

	log.CtxErrorw(context.Background(), "failed to check sp healthy after retries", "sp", sp)
	return false
}

func (checker *HealthChecker) Start() {
	go func() {
		healthCheckerTicker := time.NewTicker(defaultSPHealthCheckerInterval)
		for range healthCheckerTicker.C {
			log.Debug("start to sp health checker")
			checker.checkAllSPHealth()
			log.Debug("finished to sp health checker")
		}
	}()
}
