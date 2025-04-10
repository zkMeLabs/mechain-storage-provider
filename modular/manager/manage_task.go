package manager

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/zkMeLabs/mechain-storage-provider/core/piecestore"
	"golang.org/x/exp/slices"

	storagetypes "github.com/evmos/evmos/v12/x/storage/types"
	virtualgrouptypes "github.com/evmos/evmos/v12/x/virtualgroup/types"
	"github.com/zkMeLabs/mechain-storage-provider/base/gfspvgmgr"
	"github.com/zkMeLabs/mechain-storage-provider/base/types/gfsperrors"
	"github.com/zkMeLabs/mechain-storage-provider/base/types/gfspserver"
	"github.com/zkMeLabs/mechain-storage-provider/base/types/gfsptask"
	"github.com/zkMeLabs/mechain-storage-provider/core/module"
	"github.com/zkMeLabs/mechain-storage-provider/core/rcmgr"
	"github.com/zkMeLabs/mechain-storage-provider/core/spdb"
	"github.com/zkMeLabs/mechain-storage-provider/core/task"
	"github.com/zkMeLabs/mechain-storage-provider/core/taskqueue"
	"github.com/zkMeLabs/mechain-storage-provider/core/vgmgr"
	"github.com/zkMeLabs/mechain-storage-provider/pkg/log"
	"github.com/zkMeLabs/mechain-storage-provider/pkg/metrics"
	"github.com/zkMeLabs/mechain-storage-provider/store/types"
	"github.com/zkMeLabs/mechain-storage-provider/util"
)

var (
	ErrDanglingTask         = gfsperrors.Register(module.ManageModularName, http.StatusBadRequest, 60001, "OoooH... request lost")
	ErrRepeatedTask         = gfsperrors.Register(module.ManageModularName, http.StatusNotAcceptable, 60002, "request repeated")
	ErrExceedTask           = gfsperrors.Register(module.ManageModularName, http.StatusNotAcceptable, 60003, "OoooH... request exceed, try again later")
	ErrCanceledTask         = gfsperrors.Register(module.ManageModularName, http.StatusBadRequest, 60004, "task canceled")
	ErrFutureSupport        = gfsperrors.Register(module.ManageModularName, http.StatusNotFound, 60005, "future support")
	ErrNotifyMigrateSwapOut = gfsperrors.Register(module.ManageModularName, http.StatusNotAcceptable, 60006, "failed to notify swap out start")
)

const bucketMigrationGCWaitTime = 10 * time.Second

func ErrGfSpDBWithDetail(detail string) *gfsperrors.GfSpError {
	return gfsperrors.Register(module.ManageModularName, http.StatusInternalServerError, 65201, detail)
}

func (m *ManageModular) DispatchTask(ctx context.Context, limit rcmgr.Limit) (task.Task, error) {
	for {
		select {
		case <-ctx.Done():
			log.CtxErrorw(ctx, "dispatch task context is canceled")
			return nil, nil
		case dispatchTask := <-m.taskCh:
			atomic.AddInt64(&m.backupTaskNum, -1)
			if !limit.NotLess(dispatchTask.EstimateLimit()) {
				log.CtxErrorw(ctx, "resource exceed", "executor_limit", limit.String(), "task_limit", dispatchTask.EstimateLimit().String(), "task_info", dispatchTask.Info())
				go func() {
					m.taskCh <- dispatchTask
					atomic.AddInt64(&m.backupTaskNum, 1)
				}()
				continue
			}
			dispatchTask.IncRetry()
			dispatchTask.SetError(nil)
			dispatchTask.SetUpdateTime(time.Now().Unix())
			dispatchTask.SetAddress(util.GetRPCRemoteAddress(ctx))
			m.repushTask(dispatchTask)
			log.CtxDebugw(ctx, "dispatch task to executor", "key_info", dispatchTask.Info())
			return dispatchTask, nil
		}
	}
}

func (m *ManageModular) HandleCreateUploadObjectTask(ctx context.Context, task task.UploadObjectTask) error {
	if task == nil {
		log.CtxErrorw(ctx, "failed to handle begin upload object due to task pointer dangling")
		return ErrDanglingTask
	}
	if m.UploadingObjectNumber() >= m.maxUploadObjectNumber {
		log.CtxErrorw(ctx, "uploading object exceed", "uploading", m.uploadQueue.Len(),
			"replicating", m.replicateQueue.Len(), "sealing", m.sealQueue.Len())
		return ErrExceedTask
	}
	if m.TaskUploading(ctx, task) {
		log.CtxErrorw(ctx, "uploading object repeated", "task_info", task.Info())
		return ErrRepeatedTask
	}
	if err := m.uploadQueue.Push(task); err != nil {
		log.CtxErrorw(ctx, "failed to push upload object task to queue", "task_info", task.Info(), "error", err)
		return err
	}
	if err := m.baseApp.GfSpDB().InsertUploadProgress(task.GetObjectInfo().Id.Uint64(), task.GetIsAgentUpload()); err != nil {
		if strings.Contains(err.Error(), "Duplicate entry") {
			log.Infow("insert upload progress with duplicate entry", "task_info", task.Info())
			return nil
		} else {
			log.CtxErrorw(ctx, "failed to create upload object progress", "task_info", task.Info(), "error", err)
			return ErrGfSpDBWithDetail("failed to create upload object progress, task_info: " + task.Info() + ", error: " + err.Error())
		}
	}
	return nil
}

func (m *ManageModular) HandleDoneUploadObjectTask(ctx context.Context, task task.UploadObjectTask) error {
	if task == nil || task.GetObjectInfo() == nil || task.GetStorageParams() == nil {
		log.CtxErrorw(ctx, "failed to handle done upload object due to pointer dangling")
		return ErrDanglingTask
	}
	m.uploadQueue.PopByKey(task.Key())
	uploading := m.TaskUploading(ctx, task)
	if uploading {
		log.CtxErrorw(ctx, "uploading object repeated")
		return ErrRepeatedTask
	}
	if task.Error() != nil {
		go func() {
			err := m.baseApp.GfSpDB().UpdateUploadProgress(&spdb.UploadObjectMeta{
				ObjectID:         task.GetObjectInfo().Id.Uint64(),
				TaskState:        types.TaskState_TASK_STATE_UPLOAD_OBJECT_ERROR,
				ErrorDescription: task.Error().Error(),
			})
			if err != nil {
				log.Errorw("failed to update object task state", "task_info", task.Info(), "error", err)
			}
			log.Errorw("reports failed update object task", "task_info", task.Info(), "error", task.Error())
		}()
		metrics.ManagerCounter.WithLabelValues(ManagerFailureUpload).Inc()
		metrics.ManagerTime.WithLabelValues(ManagerFailureUpload).Observe(
			time.Since(time.Unix(task.GetCreateTime(), 0)).Seconds())
		return nil
	} else {
		metrics.ManagerCounter.WithLabelValues(ManagerSuccessUpload).Inc()
		metrics.ManagerTime.WithLabelValues(ManagerSuccessUpload).Observe(
			time.Since(time.Unix(task.GetCreateTime(), 0)).Seconds())
	}
	log.Debugw("UploadObjectTask info", "task", task)
	return m.pickGVGAndReplicate(ctx, task.GetVirtualGroupFamilyId(), task, task.GetIsAgentUpload())
}

func (m *ManageModular) pickGVGAndReplicate(ctx context.Context, vgfID uint32, task task.ObjectTask, isAgentUpload bool) error {
	startPickGVGTime := time.Now()
	gvgMeta, err := m.pickGlobalVirtualGroup(ctx, vgfID, task.GetStorageParams())
	log.CtxInfow(ctx, "pick global virtual group", "time_cost", time.Since(startPickGVGTime).Seconds(), "gvg_meta", gvgMeta, "error", err)
	if err != nil {
		// If there is no way to create a new GVG, release all sp from freeze Pool, better than not serving requests.
		m.virtualGroupManager.ReleaseAllSP()
		return err
	}
	replicateTask := &gfsptask.GfSpReplicatePieceTask{}
	replicateTask.InitReplicatePieceTask(task.GetObjectInfo(), task.GetStorageParams(),
		m.baseApp.TaskPriority(replicateTask),
		m.baseApp.TaskTimeout(replicateTask, task.GetObjectInfo().GetPayloadSize()),
		m.baseApp.TaskMaxRetry(replicateTask), isAgentUpload)
	replicateTask.GlobalVirtualGroupId = gvgMeta.ID
	replicateTask.SecondaryEndpoints = gvgMeta.SecondarySPEndpoints
	log.Debugw("replicate task info", "task", replicateTask, "gvg_meta", gvgMeta)
	replicateTask.SetCreateTime(task.GetCreateTime())
	replicateTask.SetLogs(task.GetLogs())
	replicateTask.SetRetry(task.GetRetry())
	replicateTask.AppendLog("manager-create-replicate-task")
	err = m.replicateQueue.Push(replicateTask)
	if err != nil {
		log.CtxErrorw(ctx, "failed to push replicate piece task to queue", "error", err)
		return err
	}
	go m.backUpTask()
	go func() {
		err = m.baseApp.GfSpDB().UpdateUploadProgress(&spdb.UploadObjectMeta{
			ObjectID:             task.GetObjectInfo().Id.Uint64(),
			TaskState:            types.TaskState_TASK_STATE_REPLICATE_OBJECT_DOING,
			GlobalVirtualGroupID: gvgMeta.ID,
			SecondaryEndpoints:   gvgMeta.SecondarySPEndpoints,
		})
		if err != nil {
			log.Errorw("failed to update object task state", "task_info", task.Info(), "error", err)
			return
		}
		log.Debugw("succeed to done upload object and waiting for scheduling to replicate piece", "task_info", task.Info())
	}()
	return nil
}

func (m *ManageModular) HandleCreateResumableUploadObjectTask(ctx context.Context, task task.ResumableUploadObjectTask) error {
	if task == nil {
		log.CtxErrorw(ctx, "failed to handle begin upload object due to task pointer dangling")
		return ErrDanglingTask
	}
	if m.UploadingObjectNumber() >= m.maxUploadObjectNumber {
		log.CtxErrorw(ctx, "uploading object exceed", "uploading", m.uploadQueue.Len(),
			"replicating", m.replicateQueue.Len(), "sealing", m.sealQueue.Len(), "resumable uploading", m.resumableUploadQueue.Len())
		return ErrExceedTask
	}
	if m.TaskUploading(ctx, task) {
		log.CtxErrorw(ctx, "uploading object repeated", "task_info", task.Info())
		return ErrRepeatedTask
	}
	if err := m.resumableUploadQueue.Push(task); err != nil {
		log.CtxErrorw(ctx, "failed to push resumable upload object task to queue", "task_info", task.Info(), "error", err)
		return err
	}
	if err := m.baseApp.GfSpDB().InsertUploadProgress(task.GetObjectInfo().Id.Uint64(), task.GetIsAgentUpload()); err != nil {
		if strings.Contains(err.Error(), "Duplicate entry") {
			return nil
		} else {
			log.CtxErrorw(ctx, "failed to create resumable upload object progress", "task_info", task.Info(), "error", err)
			return ErrGfSpDBWithDetail("failed to create resumable upload object progress, task_info: " + task.Info() + ", error: " + err.Error())
		}
	}
	return nil
}

func (m *ManageModular) HandleDoneResumableUploadObjectTask(ctx context.Context, task task.ResumableUploadObjectTask) error {
	if task == nil || task.GetObjectInfo() == nil || task.GetStorageParams() == nil {
		log.CtxErrorw(ctx, "failed to handle done upload object, pointer dangling")
		return ErrDanglingTask
	}
	m.resumableUploadQueue.PopByKey(task.Key())

	uploading := m.TaskUploading(ctx, task)
	if uploading {
		log.CtxErrorw(ctx, "uploading object repeated")
		return ErrRepeatedTask
	}
	if task.Error() != nil {
		go func() error {
			err := m.baseApp.GfSpDB().UpdateUploadProgress(&spdb.UploadObjectMeta{
				ObjectID:         task.GetObjectInfo().Id.Uint64(),
				TaskState:        types.TaskState_TASK_STATE_UPLOAD_OBJECT_ERROR,
				ErrorDescription: task.Error().Error(),
			})
			if err != nil {
				log.CtxErrorw(ctx, "failed to resumable update object task state", "error", err)
			}
			log.CtxErrorw(ctx, "reports failed resumable update object task", "task_info", task.Info(), "error", task.Error())
			return nil
		}()
		metrics.ManagerCounter.WithLabelValues(ManagerFailureUpload).Inc()
		metrics.ManagerTime.WithLabelValues(ManagerFailureUpload).Observe(
			time.Since(time.Unix(task.GetCreateTime(), 0)).Seconds())
		return nil
	} else {
		metrics.ManagerCounter.WithLabelValues(ManagerSuccessUpload).Inc()
		metrics.ManagerTime.WithLabelValues(ManagerSuccessUpload).Observe(
			time.Since(time.Unix(task.GetCreateTime(), 0)).Seconds())
	}

	// During a resumable upload, the uploader reports each uploaded segment to the manager.
	// Once all segments are reported as completed, the replication process can begin.
	if !task.GetCompleted() {
		return nil
	}

	startPickGVGTime := time.Now()
	gvgMeta, err := m.pickGlobalVirtualGroup(ctx, task.GetVirtualGroupFamilyId(), task.GetStorageParams())
	log.CtxInfow(ctx, "pick global virtual group", "time_cost", time.Since(startPickGVGTime).Seconds(), "gvg_meta", gvgMeta, "error", err)
	if err != nil {
		log.CtxErrorw(ctx, "failed to pick global virtual group", "time_cost", time.Since(startPickGVGTime).Seconds(), "error", err)
		return err
	}

	replicateTask := &gfsptask.GfSpReplicatePieceTask{}
	replicateTask.InitReplicatePieceTask(task.GetObjectInfo(), task.GetStorageParams(),
		m.baseApp.TaskPriority(replicateTask),
		m.baseApp.TaskTimeout(replicateTask, task.GetObjectInfo().GetPayloadSize()),
		m.baseApp.TaskMaxRetry(replicateTask), task.GetIsAgentUpload())
	replicateTask.GlobalVirtualGroupId = gvgMeta.ID
	replicateTask.SecondaryEndpoints = gvgMeta.SecondarySPEndpoints
	log.Debugw("replicate task info", "task", replicateTask, "gvg_meta", gvgMeta)
	err = m.replicateQueue.Push(replicateTask)
	if err != nil {
		log.CtxErrorw(ctx, "failed to push replicate piece task to queue", "error", err)
		return err
	}
	go m.backUpTask()
	go func() error {
		err = m.baseApp.GfSpDB().UpdateUploadProgress(&spdb.UploadObjectMeta{
			ObjectID:             task.GetObjectInfo().Id.Uint64(),
			TaskState:            types.TaskState_TASK_STATE_REPLICATE_OBJECT_DOING,
			GlobalVirtualGroupID: gvgMeta.ID,
			SecondaryEndpoints:   gvgMeta.SecondarySPEndpoints,
		})
		if err != nil {
			log.CtxErrorw(ctx, "failed to update object task state", "error", err)
			return ErrGfSpDBWithDetail("failed to update object task state, error: " + err.Error())
		}
		log.CtxDebugw(ctx, "succeed to done upload object and waiting for scheduling to replicate piece")
		return nil
	}()
	return nil
}

func (m *ManageModular) HandleReplicatePieceTask(ctx context.Context, task task.ReplicatePieceTask) error {
	if task == nil || task.GetObjectInfo() == nil || task.GetStorageParams() == nil {
		log.CtxErrorw(ctx, "failed to handle replicate piece due to pointer dangling")
		return ErrDanglingTask
	}
	if task.Error() != nil {
		log.CtxErrorw(ctx, "failed to replicate piece task", "task_info", task.Info(), "error", task.Error())
		_ = m.handleFailedReplicatePieceTask(ctx, task)
		metrics.ManagerCounter.WithLabelValues(ManagerFailureReplicate).Inc()
		metrics.ManagerTime.WithLabelValues(ManagerFailureReplicate).Observe(
			time.Since(time.Unix(task.GetUpdateTime(), 0)).Seconds())
		return nil
	} else {
		metrics.ManagerCounter.WithLabelValues(ManagerSuccessReplicate).Inc()
		metrics.ManagerTime.WithLabelValues(ManagerSuccessReplicate).Observe(
			time.Since(time.Unix(task.GetUpdateTime(), 0)).Seconds())
	}
	m.replicateQueue.PopByKey(task.Key())
	if m.TaskUploading(ctx, task) {
		log.CtxErrorw(ctx, "replicate piece object task repeated")
		return ErrRepeatedTask
	}
	if task.GetSealed() {
		task.AppendLog(fmt.Sprintf("manager-handle-succeed-replicate-task-retry:%d", task.GetRetry()))
		go func() {
			_ = m.baseApp.GfSpDB().InsertPutEvent(task)
			log.Debugw("replicate piece object task has combined seal object task", "task_info", task.Info())
			if err := m.baseApp.GfSpDB().UpdateUploadProgress(&spdb.UploadObjectMeta{
				ObjectID:  task.GetObjectInfo().Id.Uint64(),
				TaskState: types.TaskState_TASK_STATE_SEAL_OBJECT_DONE,
			}); err != nil {
				log.Errorw("failed to update object task state", "task_info", task.Info(), "error", err)
			}
			log.Errorw("succeed to update object task state", "task_info", task.Info())
			_ = m.baseApp.GfSpDB().DeleteUploadProgress(task.GetObjectInfo().Id.Uint64())

			if task.GetIsAgentUpload() {
				_ = m.baseApp.GfSpDB().DeleteReplicatePieceChecksumsByObjectID(task.GetObjectInfo().Id.Uint64())
			}

			if task.GetObjectInfo().GetIsUpdating() {
				shadowIntegrityMeta, err := m.baseApp.GfSpDB().GetShadowObjectIntegrity(task.GetObjectInfo().Id.Uint64(), piecestore.PrimarySPRedundancyIndex)
				if err != nil {
					log.Debugw("get object integrity meta", "task_info", task.Info(), "error", err)
					return
				}
				gcStaleVersionObjectTask := &gfsptask.GfSpGCStaleVersionObjectTask{}
				gcStaleVersionObjectTask.InitGCStaleVersionObjectTask(m.baseApp.TaskPriority(gcStaleVersionObjectTask),
					shadowIntegrityMeta.ObjectID,
					shadowIntegrityMeta.RedundancyIndex,
					shadowIntegrityMeta.IntegrityChecksum,
					shadowIntegrityMeta.PieceChecksumList,
					shadowIntegrityMeta.Version,
					shadowIntegrityMeta.ObjectSize,
					m.baseApp.TaskTimeout(gcStaleVersionObjectTask, 0))
				err = m.gcStaleVersionObjectQueue.Push(gcStaleVersionObjectTask)
				log.CtxDebugw(ctx, "push gc stale version object task to queue", "task_info", task.Info(), "error", err)
			}
		}()
		metrics.ManagerCounter.WithLabelValues(ManagerSuccessReplicateAndSeal).Inc()
		metrics.ManagerTime.WithLabelValues(ManagerSuccessReplicateAndSeal).Observe(
			time.Since(time.Unix(task.GetUpdateTime(), 0)).Seconds())
		return nil
	} else {
		task.AppendLog("manager-handle-succeed-replicate-failed-seal")
		metrics.ManagerCounter.WithLabelValues(ManagerFailureReplicateAndSeal).Inc()
		metrics.ManagerTime.WithLabelValues(ManagerFailureReplicateAndSeal).Observe(
			time.Since(time.Unix(task.GetUpdateTime(), 0)).Seconds())
	}

	log.CtxDebugw(ctx, "replicate piece object task fails to combine seal object task", "task_info", task.Info())
	sealObject := &gfsptask.GfSpSealObjectTask{}
	sealObject.InitSealObjectTask(task.GetGlobalVirtualGroupId(), task.GetObjectInfo(), task.GetStorageParams(),
		m.baseApp.TaskPriority(sealObject), task.GetSecondaryEndpoints(), task.GetSecondarySignatures(),
		m.baseApp.TaskTimeout(sealObject, 0), m.baseApp.TaskMaxRetry(sealObject), task.GetIsAgentUpload())
	sealObject.SetCreateTime(task.GetCreateTime())
	sealObject.SetLogs(task.GetLogs())
	sealObject.AppendLog("manager-create-seal-task")
	err := m.sealQueue.Push(sealObject)
	if err != nil {
		log.CtxErrorw(ctx, "failed to push seal object task to queue", "task_info", task.Info(), "error", err)
		return err
	}
	go m.backUpTask()
	go func() {
		if err = m.baseApp.GfSpDB().UpdateUploadProgress(&spdb.UploadObjectMeta{
			ObjectID:             task.GetObjectInfo().Id.Uint64(),
			TaskState:            types.TaskState_TASK_STATE_SEAL_OBJECT_DOING,
			GlobalVirtualGroupID: task.GetGlobalVirtualGroupId(),
			SecondaryEndpoints:   task.GetSecondaryEndpoints(),
			SecondarySignatures:  task.GetSecondarySignatures(),
			ErrorDescription:     "",
		}); err != nil {
			log.Errorw("failed to update object task state", "task_info", task.Info(), "task_info", task.Info(), "error", err)
			return
		}
		log.Debugw("succeed to done replicate piece and waiting for scheduling to seal object", "task_info", task.Info())
	}()
	return nil
}

func (m *ManageModular) handleFailedReplicatePieceTask(ctx context.Context, handleTask task.ReplicatePieceTask) error {
	shadowTask := handleTask
	oldTask := m.replicateQueue.PopByKey(handleTask.Key())
	if m.TaskUploading(ctx, handleTask) {
		log.CtxErrorw(ctx, "replicate piece task repeated", "task_info", handleTask.Info())
		return ErrRepeatedTask
	}
	if oldTask == nil {
		log.CtxErrorw(ctx, "task has been canceled", "task_info", handleTask.Info())
		return ErrCanceledTask
	}
	handleTask = oldTask.(task.ReplicatePieceTask)
	if !handleTask.ExceedRetry() {
		handleTask.AppendLog(fmt.Sprintf("manager-handle-failed-replicate-task-repush:%d", shadowTask.GetRetry()))
		handleTask.AppendLog(shadowTask.GetLogs())
		handleTask.SetUpdateTime(time.Now().Unix())
		if shadowTask.GetNotAvailableSpIdx() != -1 {
			objectInfo, queryErr := m.baseApp.Consensus().QueryObjectInfoByID(ctx, util.Uint64ToString(handleTask.GetObjectInfo().Id.Uint64()))
			if queryErr != nil {
				log.Errorw("failed to query object info", "object", handleTask.GetObjectInfo(), "error", queryErr)
				return queryErr
			}
			if objectInfo.GetObjectStatus() == storagetypes.OBJECT_STATUS_SEALED && !objectInfo.GetIsUpdating() {
				log.CtxInfow(ctx, "object already sealed, abort replicate task", "object_info", objectInfo)
				m.replicateQueue.PopByKey(handleTask.Key())
				return nil
			}

			gvgID := handleTask.GetGlobalVirtualGroupId()
			gvg, queryErr := m.baseApp.Consensus().QueryGlobalVirtualGroup(context.Background(), gvgID)
			if queryErr != nil {
				log.Errorw("failed to query global virtual group from chain", "gvgID", gvgID, "error", queryErr)
				return queryErr
			}
			sspID := gvg.GetSecondarySpIds()[shadowTask.GetNotAvailableSpIdx()]
			sspJoinGVGs, queryErr := m.baseApp.GfSpClient().ListGlobalVirtualGroupsBySecondarySP(ctx, sspID)
			if queryErr != nil {
				log.Errorw("failed to list GVGs by secondary sp", "spID", sspID, "error", queryErr)
				return queryErr
			}
			shouldFreezeGVGs := make([]*virtualgrouptypes.GlobalVirtualGroup, 0)
			selfSPID, queryErr := m.getSPID()
			if queryErr != nil {
				log.CtxErrorw(ctx, "failed to get self sp id", "error", queryErr)
				return queryErr
			}
			for _, g := range sspJoinGVGs {
				if g.GetPrimarySpId() == selfSPID {
					shouldFreezeGVGs = append(shouldFreezeGVGs, g)
				}
			}
			m.virtualGroupManager.FreezeSPAndGVGs(sspID, shouldFreezeGVGs)
			rePickAndReplicateErr := m.pickGVGAndReplicate(ctx, gvg.FamilyId, handleTask, handleTask.GetIsAgentUpload())
			log.CtxDebugw(ctx, "add failed sp to freeze pool, re-pick and push task again",
				"failed_sp_id", sspID, "task_info", handleTask.Info(),
				"excludedGVGs", shouldFreezeGVGs, "error", rePickAndReplicateErr)
			return rePickAndReplicateErr
		} else {
			pushErr := m.replicateQueue.Push(handleTask)
			log.CtxDebugw(ctx, "push task again to retry", "task_info", handleTask.Info(), "error", pushErr)
			return pushErr
		}
	} else {
		shadowTask.AppendLog(fmt.Sprintf("manager-handle-failed-replicate-task-error:%s-retry:%d", shadowTask.Error().Error(), shadowTask.GetRetry()))
		metrics.ManagerCounter.WithLabelValues(ManagerCancelReplicate).Inc()
		metrics.ManagerTime.WithLabelValues(ManagerCancelReplicate).Observe(
			time.Since(time.Unix(handleTask.GetCreateTime(), 0)).Seconds())
		go func() {
			_ = m.baseApp.GfSpDB().InsertPutEvent(shadowTask)
			if err := m.baseApp.GfSpDB().UpdateUploadProgress(&spdb.UploadObjectMeta{
				ObjectID:         handleTask.GetObjectInfo().Id.Uint64(),
				TaskState:        types.TaskState_TASK_STATE_REPLICATE_OBJECT_ERROR,
				ErrorDescription: "exceed_replicate_retry",
			}); err != nil {
				log.Errorw("failed to update object task state", "task_info", handleTask.Info(), "error", err)
				return
			}
			log.Errorw("succeed to update object task state", "task_info", handleTask.Info())
		}()
		log.CtxWarnw(ctx, "delete expired replicate piece task", "task_info", handleTask.Info())
	}
	return nil
}

func (m *ManageModular) HandleSealObjectTask(ctx context.Context, task task.SealObjectTask) error {
	if task == nil {
		log.CtxErrorw(ctx, "failed to handle seal object due to task pointer dangling")
		return ErrDanglingTask
	}
	if task.Error() != nil {
		log.CtxErrorw(ctx, "handler error seal object task", "task_info", task.Info(), "error", task.Error())
		_ = m.handleFailedSealObjectTask(ctx, task)
		metrics.ManagerCounter.WithLabelValues(ManagerFailureSeal).Inc()
		metrics.ManagerTime.WithLabelValues(ManagerFailureSeal).Observe(
			time.Since(time.Unix(task.GetUpdateTime(), 0)).Seconds())
		return nil
	} else {
		metrics.ManagerCounter.WithLabelValues(ManagerSuccessSeal).Inc()
		metrics.ManagerTime.WithLabelValues(ManagerSuccessSeal).Observe(
			time.Since(time.Unix(task.GetUpdateTime(), 0)).Seconds())
	}
	go func() {
		m.sealQueue.PopByKey(task.Key())
		task.AppendLog(fmt.Sprintf("manager-handle-succeed-seal-task-retry:%d", task.GetRetry()))
		_ = m.baseApp.GfSpDB().InsertPutEvent(task)
		if err := m.baseApp.GfSpDB().UpdateUploadProgress(&spdb.UploadObjectMeta{
			ObjectID:  task.GetObjectInfo().Id.Uint64(),
			TaskState: types.TaskState_TASK_STATE_SEAL_OBJECT_DONE,
		}); err != nil {
			log.Errorw("failed to update object task state", "task_info", task.Info(), "error", err)
			return
		}
		// delete this upload db record
		_ = m.baseApp.GfSpDB().DeleteUploadProgress(task.GetObjectInfo().Id.Uint64())
		log.Debugw("succeed to seal object on chain", "task_info", task.Info())
	}()
	return nil
}

func (m *ManageModular) handleFailedSealObjectTask(ctx context.Context, handleTask task.SealObjectTask) error {
	shadowTask := handleTask
	oldTask := m.sealQueue.PopByKey(handleTask.Key())
	if m.TaskUploading(ctx, handleTask) {
		log.CtxErrorw(ctx, "seal object task repeated", "task_info", handleTask.Info())
		return ErrRepeatedTask
	}
	if oldTask == nil {
		log.CtxErrorw(ctx, "task has been canceled", "task_info", handleTask.Info())
		return ErrCanceledTask
	}
	handleTask = oldTask.(task.SealObjectTask)
	if !handleTask.ExceedRetry() {
		handleTask.AppendLog(fmt.Sprintf("manager-handle-failed-seal-task-error:%s-repush:%d", shadowTask.Error().Error(), shadowTask.GetRetry()))
		handleTask.AppendLog(shadowTask.GetLogs())
		handleTask.SetUpdateTime(time.Now().Unix())
		err := m.sealQueue.Push(handleTask)
		log.CtxDebugw(ctx, "push task again to retry", "task_info", handleTask.Info(), "error", err)
		return nil
	} else {
		shadowTask.AppendLog(fmt.Sprintf("manager-handle-failed-seal-task-error:%s-retry:%d", shadowTask.Error().Error(), handleTask.GetRetry()))
		_ = m.baseApp.GfSpDB().InsertPutEvent(shadowTask)
		metrics.ManagerCounter.WithLabelValues(ManagerCancelSeal).Inc()
		metrics.ManagerTime.WithLabelValues(ManagerCancelSeal).Observe(
			time.Since(time.Unix(handleTask.GetCreateTime(), 0)).Seconds())
		go func() {
			if err := m.baseApp.GfSpDB().UpdateUploadProgress(&spdb.UploadObjectMeta{
				ObjectID:         handleTask.GetObjectInfo().Id.Uint64(),
				TaskState:        types.TaskState_TASK_STATE_SEAL_OBJECT_ERROR,
				ErrorDescription: "exceed_seal_retry",
			}); err != nil {
				log.Errorw("failed to update object task state", "task_info", handleTask.Info(), "error", err)
				return
			}
			log.Errorw("succeed to update object task state", "task_info", handleTask.Info())
		}()
		log.CtxWarnw(ctx, "delete expired seal object task", "task_info", handleTask.Info())
	}
	return nil
}

func (m *ManageModular) HandleReceivePieceTask(ctx context.Context, task task.ReceivePieceTask) error {
	if task.GetSealed() {
		go m.receiveQueue.PopByKey(task.Key())
		metrics.ManagerCounter.WithLabelValues(ManagerSuccessConfirmReceive).Inc()
		metrics.ManagerTime.WithLabelValues(ManagerSuccessConfirmReceive).Observe(
			time.Since(time.Unix(task.GetCreateTime(), 0)).Seconds())
		log.CtxDebugw(ctx, "succeed to confirm receive piece seal on chain")
	} else if task.Error() != nil {
		_ = m.handleFailedReceivePieceTask(ctx, task)
		metrics.ManagerCounter.WithLabelValues(ManagerFailureConfirmReceive).Inc()
		metrics.ManagerTime.WithLabelValues(ManagerFailureConfirmReceive).Observe(
			time.Since(time.Unix(task.GetCreateTime(), 0)).Seconds())
		return nil
	} else {
		go func() {
			task.SetRetry(0)
			task.SetMaxRetry(m.baseApp.TaskMaxRetry(task))
			task.SetTimeout(m.baseApp.TaskTimeout(task, 0))
			task.SetPriority(m.baseApp.TaskPriority(task))
			task.SetUpdateTime(time.Now().Unix())
			err := m.receiveQueue.Push(task)
			log.CtxErrorw(ctx, "push receive task to queue", "error", err)
			if err == nil {
				go m.backUpTask()
			}
		}()
	}
	return nil
}

func (m *ManageModular) handleFailedReceivePieceTask(ctx context.Context, handleTask task.ReceivePieceTask) error {
	oldTask := m.receiveQueue.PopByKey(handleTask.Key())
	if oldTask == nil {
		log.CtxErrorw(ctx, "task has been canceled", "task_info", handleTask.Info())
		return ErrCanceledTask
	}
	handleTask = oldTask.(task.ReceivePieceTask)
	if !handleTask.ExceedRetry() {
		handleTask.SetUpdateTime(time.Now().Unix())
		err := m.receiveQueue.Push(handleTask)
		log.CtxDebugw(ctx, "push task again to retry", "task_info", handleTask.Info(), "error", err)
	} else {
		log.CtxErrorw(ctx, "delete expired confirm receive piece task", "task_info", handleTask.Info())
		// TODO: confirm it
	}
	return nil
}

func (m *ManageModular) HandleGCObjectTask(ctx context.Context, gcTask task.GCObjectTask) error {
	if gcTask == nil {
		log.CtxErrorw(ctx, "failed to handle gc object due to task pointer dangling")
		return ErrDanglingTask
	}
	if !m.gcObjectQueue.Has(gcTask.Key()) {
		log.CtxErrorw(ctx, "task is not in the gc queue", "task_info", gcTask.Info())
		return ErrCanceledTask
	}
	if gcTask.GetCurrentBlockNumber() > gcTask.GetEndBlockNumber() {
		log.CtxInfow(ctx, "succeed to finish the gc object task", "task_info", gcTask.Info())
		m.gcObjectQueue.PopByKey(gcTask.Key())
		m.baseApp.GfSpDB().DeleteGCObjectProgress(gcTask.Key().String())
		return nil
	}
	gcTask.SetUpdateTime(time.Now().Unix())
	oldTask := m.gcObjectQueue.PopByKey(gcTask.Key())
	if oldTask != nil {
		if oldTask.(task.GCObjectTask).GetCurrentBlockNumber() > gcTask.GetCurrentBlockNumber() ||
			(oldTask.(task.GCObjectTask).GetCurrentBlockNumber() == gcTask.GetCurrentBlockNumber() &&
				oldTask.(task.GCObjectTask).GetLastDeletedObjectId() > gcTask.GetLastDeletedObjectId()) {
			log.CtxErrorw(ctx, "the reported gc object task is expired", "report_info", gcTask.Info(),
				"current_info", oldTask.Info())
			return ErrCanceledTask
		}
	} else {
		log.CtxErrorw(ctx, "the reported gc object task is canceled", "report_info", gcTask.Info())
		return ErrCanceledTask
	}
	err := m.gcObjectQueue.Push(gcTask)
	log.CtxInfow(ctx, "push gc object task to queue again", "from", oldTask, "to", gcTask, "error", err)
	currentGCBlockID, deletedObjectID := gcTask.GetGCObjectProgress()
	err = m.baseApp.GfSpDB().UpdateGCObjectProgress(&spdb.GCObjectMeta{
		TaskKey:             gcTask.Key().String(),
		CurrentBlockHeight:  currentGCBlockID,
		LastDeletedObjectID: deletedObjectID,
	})
	log.CtxInfow(ctx, "update the gc object task progress", "from", oldTask, "to", gcTask, "error", err)
	return nil
}

func (m *ManageModular) HandleGCZombiePieceTask(ctx context.Context, gcZombiePieceTask task.GCZombiePieceTask) error {
	if gcZombiePieceTask == nil {
		log.CtxErrorw(ctx, "failed to handle gc zombie due to task pointer dangling")
		return ErrDanglingTask
	}
	if !m.gcZombieQueue.Has(gcZombiePieceTask.Key()) {
		log.CtxErrorw(ctx, "failed to handle due to task is not in the gc zombie queue", "task_info", gcZombiePieceTask.Info())
		return ErrCanceledTask
	}
	if gcZombiePieceTask.Error() == nil {
		log.CtxInfow(ctx, "succeed to finish the gc zombie task", "task_info", gcZombiePieceTask.Info())
		m.gcZombieQueue.PopByKey(gcZombiePieceTask.Key())
		return nil
	}
	gcZombiePieceTask.SetUpdateTime(time.Now().Unix())
	oldTask := m.gcZombieQueue.PopByKey(gcZombiePieceTask.Key())
	if oldTask != nil {
		if oldTask.ExceedRetry() {
			log.CtxErrorw(ctx, "the reported gc zombie task is expired", "task_info", gcZombiePieceTask.Info(),
				"current_info", oldTask.Info())
			return ErrCanceledTask
		}
	} else {
		log.CtxErrorw(ctx, "the reported gc zombie task is canceled", "task_info", gcZombiePieceTask.Info())
		return ErrCanceledTask
	}
	err := m.gcZombieQueue.Push(gcZombiePieceTask)
	log.CtxInfow(ctx, "succeed to push gc object task to queue again", "from", oldTask, "to", gcZombiePieceTask, "error", err)
	// Note: The persistence of GC zombie progress in the database is not implemented at the moment.
	// However, this lack of persistence does not impact correctness since GC zombie periodically scans all objects.
	return nil
}

func (m *ManageModular) HandleGCStaleVersionObjectTask(ctx context.Context, gcStaleVersionObjectTask task.GCStaleVersionObjectTask) error {
	if gcStaleVersionObjectTask == nil {
		log.CtxErrorw(ctx, "failed to handle gc stale version due to task pointer dangling")
		return ErrDanglingTask
	}
	if !m.gcStaleVersionObjectQueue.Has(gcStaleVersionObjectTask.Key()) {
		log.CtxErrorw(ctx, "failed to handle due to task is not in the gc stale version objet queue", "task_info", gcStaleVersionObjectTask.Info())
		return ErrCanceledTask
	}
	if gcStaleVersionObjectTask.Error() == nil {
		log.CtxInfow(ctx, "succeed to finish the gc stale version objet task", "task_info", gcStaleVersionObjectTask.Info())
		m.gcStaleVersionObjectQueue.PopByKey(gcStaleVersionObjectTask.Key())
		return nil
	}
	gcStaleVersionObjectTask.SetUpdateTime(time.Now().Unix())
	oldTask := m.gcStaleVersionObjectQueue.PopByKey(gcStaleVersionObjectTask.Key())
	if oldTask != nil {
		if oldTask.ExceedRetry() {
			log.CtxErrorw(ctx, "the reported gc stale version objet task is expired", "task_info", gcStaleVersionObjectTask.Info(),
				"current_info", oldTask.Info())
			return ErrCanceledTask
		}
	} else {
		log.CtxErrorw(ctx, "the reported gc stale version objet task is canceled", "task_info", gcStaleVersionObjectTask.Info())
		return ErrCanceledTask
	}
	err := m.gcStaleVersionObjectQueue.Push(gcStaleVersionObjectTask)
	log.CtxInfow(ctx, "succeed to push gc object task to queue again", "from", oldTask, "to", gcStaleVersionObjectTask, "error", err)
	return nil
}

func (m *ManageModular) HandleGCMetaTask(ctx context.Context, gcMetaTask task.GCMetaTask) error {
	if gcMetaTask == nil {
		log.CtxError(ctx, "failed to handle gc meta task due to gc meta task pointer dangling")
		return ErrDanglingTask
	}
	if !m.gcMetaQueue.Has(gcMetaTask.Key()) {
		log.CtxErrorw(ctx, "failed to handle gc meta task due to task is not in the gc meta queue", "task_info", gcMetaTask.Info())
		return ErrCanceledTask
	}
	if gcMetaTask.Error() == nil {
		log.CtxInfow(ctx, "succeed to finish the gc meta task", "task_info", gcMetaTask.Info())
		m.gcMetaQueue.PopByKey(gcMetaTask.Key())
		return nil
	}
	gcMetaTask.SetUpdateTime(time.Now().Unix())
	gcMetaTask.SetError(nil)
	oldTask := m.gcMetaQueue.PopByKey(gcMetaTask.Key())
	if oldTask != nil {
		if oldTask.ExceedRetry() {
			log.CtxErrorw(ctx, "the reported gc meta task is expired", "report_info", gcMetaTask.Info(),
				"current_info", oldTask.Info())
			return ErrCanceledTask
		}
	} else {
		log.CtxErrorw(ctx, "the reported gc meta task is canceled", "report_info", gcMetaTask.Info())
		return ErrCanceledTask
	}
	err := m.gcMetaQueue.Push(gcMetaTask)
	log.CtxInfow(ctx, "succeed to push gc meta task to queue again", "from", oldTask, "to", gcMetaTask, "error", err)
	return nil
}

func (m *ManageModular) GenerateGCBucketMigrationTask(ctx context.Context, bucketID uint64) {
	// src sp should wait meta data
	<-time.After(bucketMigrationGCWaitTime)
	var (
		bucketSize uint64
		err        error
	)
	// get bucket quota and check, lock quota
	if bucketSize, err = m.getBucketTotalSize(ctx, bucketID); err != nil {
		log.Errorw("failed to get bucket total size", "bucket_id", bucketID)
		return
	}

	// success generate gc task, gc for bucket migration src sp
	gcBucketMigrationTask := &gfsptask.GfSpGCBucketMigrationTask{}
	gcBucketMigrationTask.InitGCBucketMigrationTask(m.baseApp.TaskPriority(gcBucketMigrationTask), bucketID,
		m.baseApp.TaskTimeout(gcBucketMigrationTask, bucketSize), m.baseApp.TaskMaxRetry(gcBucketMigrationTask))
	if err = m.HandleCreateGCBucketMigrationTask(ctx, gcBucketMigrationTask); err != nil {
		log.CtxErrorw(ctx, "failed to begin gc bucket migration task", "info", gcBucketMigrationTask.Info(), "error", err)
	}
	log.CtxInfow(ctx, "succeed to generate bucket migration gc task and push to queue", "bucket_id", bucketID, "gcBucketMigrationTask", gcBucketMigrationTask)
}

func (m *ManageModular) HandleCreateGCBucketMigrationTask(ctx context.Context, task task.GCBucketMigrationTask) error {
	if task == nil {
		log.CtxErrorw(ctx, "failed to handle begin gc bucket migration due to task pointer dangling")
		return ErrDanglingTask
	}
	if m.gcBucketMigrationQueue.Has(task.Key()) {
		log.CtxErrorw(ctx, "uploading object repeated", "task_info", task.Info())
		return ErrRepeatedTask
	}
	if err := m.gcBucketMigrationQueue.Push(task); err != nil {
		log.CtxErrorw(ctx, "failed to push upload object task to queue", "task_info", task.Info(), "error", err)
		return err
	}
	return nil
}

func (m *ManageModular) HandleGCBucketMigrationTask(ctx context.Context, gcBucketMigrationTask task.GCBucketMigrationTask) error {
	var err error
	if gcBucketMigrationTask == nil {
		log.CtxError(ctx, "failed to handle gc bucket migration due to gc bucket migration task pointer dangling")
		return ErrDanglingTask
	}
	if !m.gcBucketMigrationQueue.Has(gcBucketMigrationTask.Key()) {
		log.CtxErrorw(ctx, "failed to handle gc bucket migration task due to task is not in the gc bucket migration queue", "task_info", gcBucketMigrationTask.Info())
		return ErrCanceledTask
	}
	if err = m.bucketMigrateScheduler.UpdateBucketMigrationGCProgress(ctx, gcBucketMigrationTask); err != nil {
		return err
	}
	if gcBucketMigrationTask.Error() == nil {
		// success
		if gcBucketMigrationTask.GetFinished() {
			log.CtxInfow(ctx, "succeed to finish the gc bucket migration task", "task_info", gcBucketMigrationTask.Info())
			m.gcBucketMigrationQueue.PopByKey(gcBucketMigrationTask.Key())
		}

		return nil
	}
	gcBucketMigrationTask.SetUpdateTime(time.Now().Unix())
	gcBucketMigrationTask.SetError(nil)
	oldTask := m.gcBucketMigrationQueue.PopByKey(gcBucketMigrationTask.Key())
	if oldTask != nil {
		if oldTask.ExceedRetry() {
			log.CtxErrorw(ctx, "the reported gc object gc bucket migration task is expired", "report_info", gcBucketMigrationTask.Info(),
				"current_info", oldTask.Info())
			return ErrCanceledTask
		}
	} else {
		log.CtxErrorw(ctx, "the reported gc object gc bucket migration task is canceled", "report_info", gcBucketMigrationTask.Info())
		return ErrCanceledTask
	}
	err = m.gcBucketMigrationQueue.Push(gcBucketMigrationTask)
	log.CtxInfow(ctx, "succeed to push gc bucket migration task to queue again", "from", oldTask, "to", gcBucketMigrationTask, "error", err)
	return err
}

func (m *ManageModular) HandleDownloadObjectTask(ctx context.Context, task task.DownloadObjectTask) error {
	m.downloadQueue.Push(task)
	log.CtxDebugw(ctx, "add download object task to queue")
	return nil
}

func (m *ManageModular) HandleChallengePieceTask(ctx context.Context, task task.ChallengePieceTask) error {
	m.challengeQueue.Push(task)
	log.CtxDebugw(ctx, "add challenge piece task to queue")
	return nil
}

func (m *ManageModular) HandleRecoverPieceTask(ctx context.Context, task task.RecoveryPieceTask) error {
	if task == nil || task.GetObjectInfo() == nil || task.GetStorageParams() == nil {
		log.CtxErrorw(ctx, "failed to handle recovery piece due to pointer dangling")
		return ErrDanglingTask
	}

	if task.GetRecovered() {
		m.recoveryQueue.PopByKey(task.Key())
		m.recoverMtx.Lock()
		delete(m.recoveryTaskMap, task.Key().String())
		m.recoverMtx.Unlock()
		log.CtxInfow(ctx, "finished recovery", "task_info", task.Info())

		if task.BySuccessorSP() {
			objectID := task.GetObjectInfo().Id.Uint64()
			m.recoverObjectStats.addSegmentRecord(objectID, true, task.GetSegmentIdx())
			return nil
		}
	}

	if task.Error() != nil {
		log.CtxErrorw(ctx, "handler error recovery piece task", "task_info", task.Info(), "error", task.Error())
		return m.handleFailedRecoverPieceTask(ctx, task)
	}

	if m.TaskRecovering(ctx, task) {
		log.CtxErrorw(ctx, "recovering object repeated", "task_info", task.Info())
		return ErrRepeatedTask
	}

	task.SetUpdateTime(time.Now().Unix())
	if err := m.recoveryQueue.Push(task); err != nil {
		log.CtxErrorw(ctx, "failed to push recovery object task to queue", "task_info", task.Info(), "error", err)
		return err
	}
	m.recoverMtx.Lock()
	m.recoveryTaskMap[task.Key().String()] = task.Key().String()
	m.recoverMtx.Unlock()
	return nil
}

func (m *ManageModular) handleFailedRecoverPieceTask(ctx context.Context, handleTask task.RecoveryPieceTask) error {
	oldTask := m.recoveryQueue.PopByKey(handleTask.Key())
	if oldTask == nil {
		log.CtxErrorw(ctx, "task has been canceled", "task_info", handleTask.Info())
		return ErrCanceledTask
	}
	handleTask = oldTask.(task.RecoveryPieceTask)
	if !handleTask.ExceedRetry() {
		handleTask.SetUpdateTime(time.Now().Unix())
		err := m.recoveryQueue.Push(handleTask)
		log.CtxDebugw(ctx, "push task again to retry", "task_info", handleTask.Info(), "error", err)
	} else {
		if !slices.Contains(m.recoveryFailedList, handleTask.GetObjectInfo().ObjectName) {
			m.recoveryFailedList = append(m.recoveryFailedList, handleTask.GetObjectInfo().ObjectName)
		}
		m.recoverMtx.Lock()
		delete(m.recoveryTaskMap, handleTask.Key().String())
		m.recoverMtx.Unlock()

		if handleTask.BySuccessorSP() {
			objectID := handleTask.GetObjectInfo().Id.Uint64()
			m.recoverObjectStats.addSegmentRecord(objectID, false, handleTask.GetSegmentIdx())
			if m.recoverObjectStats.isRecoverFailed(objectID) {
				object := &spdb.RecoverFailedObject{
					ObjectID:        handleTask.GetObjectInfo().Id.Uint64(),
					VirtualGroupID:  handleTask.GetGVGID(),
					RedundancyIndex: handleTask.GetEcIdx(),
				}
				err := m.baseApp.GfSpDB().InsertRecoverFailedObject(object)
				if err != nil {
					log.CtxErrorw(ctx, "failed to insert recover failed object entry", "task_info", handleTask.Info(), "error", err)
					return ErrGfSpDBWithDetail("failed to insert recover failed object entry, task_info: " + handleTask.Info() + ", error: " + err.Error())
				}
			}
			return nil
		}
	}
	return nil
}

func (m *ManageModular) HandleMigrateGVGTask(ctx context.Context, task task.MigrateGVGTask) error {
	if task == nil {
		log.CtxErrorw(ctx, "failed to handle migrate gvg due to pointer dangling")
		return ErrDanglingTask
	}
	var (
		err, pushErr      error
		migratedBytesSize uint64
	)

	cancelTask := false
	if task.GetBucketID() != 0 {
		// if there is no execute plan, we should cancel this task
		if _, err = m.bucketMigrateScheduler.getExecutePlanByBucketID(task.GetBucketID()); err != nil {
			cancelTask = true
		}
	}

	pushErr = m.migrateGVGQueuePopByLimitAndPushAgain(task, !cancelTask)
	if pushErr != nil {
		log.CtxErrorw(ctx, "failed to push task to migrate gvg queue", "task", task, "error", pushErr)
		return pushErr
	}

	//  if cancel migrate bucket, migrated recoup quota
	if cancelTask {
		if migratedBytesSize, err = m.bucketMigrateScheduler.getMigratedBytesSize(task.GetBucketID()); err != nil {
			log.CtxErrorw(ctx, "failed to get migrated bytes size", "task", task, "error", err)
		}
		postMsg := &gfsptask.GfSpBucketMigrationInfo{BucketId: task.GetBucketID(), Finished: task.GetFinished(), MigratedBytesSize: migratedBytesSize}
		log.CtxInfow(ctx, "start to cancel migrate task and send post migrate bucket to src sp", "post_msg", postMsg, "task", task)
		if err = m.bucketMigrateScheduler.PostMigrateBucket(postMsg, nil); err != nil {
			log.CtxErrorw(ctx, "failed to post migrate bucket", "msg", postMsg, "error", err)
		}
		return err
	}

	if task.GetBucketID() != 0 {
		err = m.bucketMigrateScheduler.UpdateMigrateProgress(task)
	} else {
		err = m.spExitScheduler.UpdateMigrateProgress(task)
	}

	log.CtxInfow(ctx, "succeed to handle migrate gvg task", "task", task, "error", err)
	return err
}

func (m *ManageModular) QueryTasks(ctx context.Context, subKey task.TKey) ([]task.Task, error) {
	uploadTasks, _ := taskqueue.ScanTQueueBySubKey(m.uploadQueue, subKey)
	replicateTasks, _ := taskqueue.ScanTQueueWithLimitBySubKey(m.replicateQueue, subKey)
	sealTasks, _ := taskqueue.ScanTQueueWithLimitBySubKey(m.sealQueue, subKey)
	receiveTasks, _ := taskqueue.ScanTQueueWithLimitBySubKey(m.receiveQueue, subKey)
	gcObjectTasks, _ := taskqueue.ScanTQueueWithLimitBySubKey(m.gcObjectQueue, subKey)
	gcZombieTasks, _ := taskqueue.ScanTQueueWithLimitBySubKey(m.gcZombieQueue, subKey)
	gcMetaTasks, _ := taskqueue.ScanTQueueWithLimitBySubKey(m.gcMetaQueue, subKey)
	downloadTasks, _ := taskqueue.ScanTQueueBySubKey(m.downloadQueue, subKey)
	challengeTasks, _ := taskqueue.ScanTQueueBySubKey(m.challengeQueue, subKey)
	recoveryTasks, _ := taskqueue.ScanTQueueWithLimitBySubKey(m.recoveryQueue, subKey)
	migrateGVGTasks, _ := taskqueue.ScanTQueueWithLimitBySubKey(m.migrateGVGQueue, subKey)
	gcBucketMigrationTasks, _ := taskqueue.ScanTQueueWithLimitBySubKey(m.gcBucketMigrationQueue, subKey)
	gcStaleVersionObjectTasks, _ := taskqueue.ScanTQueueWithLimitBySubKey(m.gcStaleVersionObjectQueue, subKey)

	var tasks []task.Task
	tasks = append(tasks, uploadTasks...)
	tasks = append(tasks, replicateTasks...)
	tasks = append(tasks, receiveTasks...)
	tasks = append(tasks, sealTasks...)
	tasks = append(tasks, gcObjectTasks...)
	tasks = append(tasks, gcZombieTasks...)
	tasks = append(tasks, gcMetaTasks...)
	tasks = append(tasks, downloadTasks...)
	tasks = append(tasks, challengeTasks...)
	tasks = append(tasks, recoveryTasks...)
	tasks = append(tasks, migrateGVGTasks...)
	tasks = append(tasks, gcBucketMigrationTasks...)
	tasks = append(tasks, gcStaleVersionObjectTasks...)
	return tasks, nil
}

func (m *ManageModular) QueryBucketMigrate(ctx context.Context) (res *gfspserver.GfSpQueryBucketMigrateResponse, err error) {
	if m.bucketMigrateScheduler != nil {
		res, err = m.bucketMigrateScheduler.listExecutePlan()
	} else {
		res, err = nil, errors.New("bucketMigrateScheduler not exit")
	}

	return res, err
}

func (m *ManageModular) QuerySpExit(ctx context.Context) (res *gfspserver.GfSpQuerySpExitResponse, err error) {
	if m.spExitScheduler != nil {
		res, err = m.spExitScheduler.ListSPExitPlan()
	} else {
		res, err = nil, errors.New("spExitScheduler not exit")
	}

	return res, err
}

// PickVirtualGroupFamily is used to pick a suitable vgf for creating bucket.
func (m *ManageModular) PickVirtualGroupFamily(ctx context.Context, task task.ApprovalCreateBucketTask) (uint32, error) {
	var (
		err error
		vgf *vgmgr.VirtualGroupFamilyMeta
	)
	if vgf, err = m.virtualGroupManager.PickVirtualGroupFamily(vgmgr.NewPickVGFByGVGFilter(m.spBlackList)); err != nil {
		log.CtxErrorw(ctx, "failed to pick virtual group family", "task_info", task.Info(), "error", err)
		// create a new gvg, and retry pick.
		if err = m.createGlobalVirtualGroup(0, nil); err != nil {
			log.CtxErrorw(ctx, "failed to create global virtual group", "task_info", task.Info(), "error", err)
			return 0, err
		}
		m.virtualGroupManager.ForceRefreshMeta()
		if vgf, err = m.virtualGroupManager.PickVirtualGroupFamily(vgmgr.NewPickVGFByGVGFilter(m.spBlackList)); err != nil {
			log.CtxErrorw(ctx, "failed to pick vgf", "task_info", task.Info(), "error", err)
			return 0, err
		}
		return vgf.ID, nil
	}
	return vgf.ID, nil
}

var _ vgmgr.GenerateGVGSecondarySPsPolicy = &GenerateGVGSecondarySPsPolicyByPrefer{}

type GenerateGVGSecondarySPsPolicyByPrefer struct {
	expectedSecondarySPNumber int
	preferSPIDMap             map[uint32]bool
	preferSPIDList            []uint32
	backupSPIDList            []uint32
}

func NewGenerateGVGSecondarySPsPolicyByPrefer(p *storagetypes.Params, preferSPIDList []uint32) *GenerateGVGSecondarySPsPolicyByPrefer {
	policy := &GenerateGVGSecondarySPsPolicyByPrefer{
		expectedSecondarySPNumber: int(p.GetRedundantDataChunkNum() + p.GetRedundantParityChunkNum()),
		preferSPIDMap:             make(map[uint32]bool),
		preferSPIDList:            make([]uint32, 0),
		backupSPIDList:            make([]uint32, 0),
	}
	for _, spID := range preferSPIDList {
		policy.preferSPIDMap[spID] = true
	}
	return policy
}

func (p *GenerateGVGSecondarySPsPolicyByPrefer) AddCandidateSP(spID uint32) {
	if _, found := p.preferSPIDMap[spID]; found {
		p.preferSPIDList = append(p.preferSPIDList, spID)
	} else {
		p.backupSPIDList = append(p.backupSPIDList, spID)
	}
}

func (p *GenerateGVGSecondarySPsPolicyByPrefer) GenerateGVGSecondarySPs() ([]uint32, error) {
	if p.expectedSecondarySPNumber > len(p.preferSPIDList)+len(p.backupSPIDList) {
		return nil, fmt.Errorf("no enough sp")
	}
	resultSPList := make([]uint32, 0)
	resultSPList = append(resultSPList, p.preferSPIDList...)
	resultSPList = append(resultSPList, p.backupSPIDList...)
	return resultSPList[0:p.expectedSecondarySPNumber], nil
}

func (m *ManageModular) createGlobalVirtualGroup(vgfID uint32, params *storagetypes.Params) error {
	var err error
	if params == nil {
		if params, err = m.baseApp.Consensus().QueryStorageParamsByTimestamp(context.Background(), time.Now().Unix()); err != nil {
			return err
		}
	}
	gvgMeta, err := m.virtualGroupManager.GenerateGlobalVirtualGroupMeta(NewGenerateGVGSecondarySPsPolicyByPrefer(params, m.gvgPreferSPList), vgmgr.NewExcludeIDFilter(gfspvgmgr.NewIDSetFromList(m.spBlackList)))
	if err != nil {
		return err
	}
	log.Infow("begin to create a gvg", "gvg_meta", gvgMeta)
	virtualGroupParams, err := m.baseApp.Consensus().QueryVirtualGroupParams(context.Background())
	if err != nil {
		return err
	}
	return m.baseApp.GfSpClient().CreateGlobalVirtualGroup(context.Background(), &gfspserver.GfSpCreateGlobalVirtualGroup{
		VirtualGroupFamilyId: vgfID,
		PrimarySpAddress:     m.baseApp.OperatorAddress(), // it is useless
		SecondarySpIds:       gvgMeta.SecondarySPIDs,
		Deposit: &sdk.Coin{
			Denom:  virtualGroupParams.GetDepositDenom(),
			Amount: virtualGroupParams.GvgStakingPerBytes.Mul(math.NewIntFromUint64(gvgMeta.StakingStorageSize)),
		},
	})
}

// pickGlobalVirtualGroup is used to pick a suitable gvg for replicating object.
func (m *ManageModular) pickGlobalVirtualGroup(ctx context.Context, vgfID uint32, param *storagetypes.Params) (*vgmgr.GlobalVirtualGroupMeta, error) {
	var (
		err error
		gvg *vgmgr.GlobalVirtualGroupMeta
	)

	if gvg, err = m.virtualGroupManager.PickGlobalVirtualGroup(vgfID, vgmgr.NewExcludeIDFilter(m.gvgBlackList)); err != nil {
		// create a new gvg, and retry pick.
		if err = m.createGlobalVirtualGroup(vgfID, param); err != nil {
			log.CtxErrorw(ctx, "failed to create global virtual group", "vgf_id", vgfID, "error", err)
			return gvg, err
		}
		m.virtualGroupManager.ForceRefreshMeta()
		if gvg, err = m.virtualGroupManager.PickGlobalVirtualGroup(vgfID, vgmgr.NewExcludeIDFilter(m.gvgBlackList)); err != nil {
			log.CtxErrorw(ctx, "failed to pick gvg", "vgf_id", vgfID, "error", err)
			return gvg, err
		}
		return gvg, nil
	}
	log.CtxDebugw(ctx, "succeed to pick gvg", "gvg", gvg)
	return gvg, nil
}
