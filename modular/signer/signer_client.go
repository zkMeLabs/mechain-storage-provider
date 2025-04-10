package signer

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"sync"

	"cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkErrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/types/tx"
	ethcmn "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"google.golang.org/grpc"

	"github.com/ethereum/go-ethereum/ethclient"

	"github.com/evmos/evmos/v12/sdk/client"
	"github.com/evmos/evmos/v12/sdk/keys"
	ctypes "github.com/evmos/evmos/v12/sdk/types"
	"github.com/evmos/evmos/v12/types"
	"github.com/evmos/evmos/v12/types/common"
	"github.com/evmos/evmos/v12/x/evm/precompiles/storage"
	"github.com/evmos/evmos/v12/x/evm/precompiles/virtualgroup"
	sptypes "github.com/evmos/evmos/v12/x/sp/types"
	storagetypes "github.com/evmos/evmos/v12/x/storage/types"
	virtualgrouptypes "github.com/evmos/evmos/v12/x/virtualgroup/types"
	"github.com/zkMeLabs/mechain-storage-provider/pkg/log"
)

// SignType is the type of msg signature
type SignType string

// GasInfoType is the type of gas info
type GasInfoType string

const (
	// SignOperator is the type of signature signed by the operator account
	SignOperator SignType = "operator"

	// SignSeal is the type of signature signed by the seal account
	SignSeal SignType = "seal"

	// SignApproval is the type of signature signed by the approval account
	SignApproval SignType = "approval"

	// SignGc is the type of signature signed by the gc account
	SignGc SignType = "gc"

	// BroadcastTxRetry defines the max retry for broadcasting tx on-chain
	BroadcastTxRetry = 3

	Seal                        GasInfoType = "Seal"
	RejectSeal                  GasInfoType = "RejectSeal"
	DelegateCreateObject        GasInfoType = "DelegateCreateObject"
	DelegateUpdateObjectContent GasInfoType = "DelegateUpdateObjectContent"
	DiscontinueBucket           GasInfoType = "DiscontinueBucket"
	CreateGlobalVirtualGroup    GasInfoType = "CreateGlobalVirtualGroup"
	DeleteGlobalVirtualGroup    GasInfoType = "DeleteGlobalVirtualGroup"
	CompleteMigrateBucket       GasInfoType = "CompleteMigrateBucket"
	RejectMigrateBucket         GasInfoType = "RejectMigrateBucket"
	SwapOut                     GasInfoType = "SwapOut"
	ReserveSwapIn               GasInfoType = "ReserveSwapIn"
	CancelSwapIn                GasInfoType = "CancelSwapIn"
	CompleteSwapIn              GasInfoType = "CompleteSwapIn"
	CompleteSwapOut             GasInfoType = "CompleteSwapOut"
	SPExit                      GasInfoType = "SPExit"
	CompleteSPExit              GasInfoType = "CompleteSPExit"
	UpdateSPPrice               GasInfoType = "UpdateSPPrice"
	Deposit                     GasInfoType = "Deposit"
)

type GasInfo struct {
	GasLimit  uint64
	FeeAmount sdk.Coins
}

// MechainChainSignClient the mechain chain client
type MechainChainSignClient struct {
	signer *SignModular

	opLock   sync.Mutex
	sealLock sync.Mutex
	gcLock   sync.Mutex

	gasInfo          map[GasInfoType]GasInfo
	mechainClients   map[SignType]*client.MechainClient
	privateKeys      map[SignType]string
	evmClient        *ethclient.Client
	operatorAccNonce uint64
	sealAccNonce     uint64
	gcAccNonce       uint64
	blsKm            keys.KeyManager
}

// NewMechainChainSignClient return the MechainChainSignClient instance
func NewMechainChainSignClient(rpcAddr, evmRpcAddr, chainID string, gasInfo map[GasInfoType]GasInfo, operatorPrivateKey, fundingPrivateKey,
	sealPrivateKey, approvalPrivateKey, gcPrivateKey string, blsPrivKey string,
) (*MechainChainSignClient, error) {
	// init clients
	// TODO: Get private key from KMS(AWS, GCP, Azure, Aliyun)
	operatorKM, err := keys.NewPrivateKeyManager(operatorPrivateKey)
	if err != nil {
		log.Errorw("failed to new operator private key manager", "error", err)
		return nil, err
	}

	// creat chain client
	evmClient, err := ethclient.Dial(evmRpcAddr)
	if err != nil {
		log.Errorw("failed to new a evm client", "error", err)
		return nil, err
	}

	operatorClient, err := client.NewMechainClient(rpcAddr, evmRpcAddr, chainID, client.WithKeyManager(operatorKM))
	if err != nil {
		log.Errorw("failed to new operator mechain client", "error", err)
		return nil, err
	}
	operatorAccNonce, err := operatorClient.GetNonce(context.Background())
	if err != nil {
		log.Errorw("failed to get operator nonce", "error", err)
		return nil, err
	}

	blsKM, err := keys.NewBlsPrivateKeyManager(blsPrivKey)
	if err != nil {
		log.Errorw("failed to new bls private key manager", "error", err)
		return nil, err
	}

	sealKM, err := keys.NewPrivateKeyManager(sealPrivateKey)
	if err != nil {
		log.Errorw("failed to new seal private key manager", "error", err)
		return nil, err
	}
	sealClient, err := client.NewMechainClient(rpcAddr, evmRpcAddr, chainID, client.WithKeyManager(sealKM))
	if err != nil {
		log.Errorw("failed to new seal mechain client", "error", err)
		return nil, err
	}
	sealAccNonce, err := sealClient.GetNonce(context.Background())
	if err != nil {
		log.Errorw("failed to get seal nonce", "error", err)
		return nil, err
	}

	approvalKM, err := keys.NewPrivateKeyManager(approvalPrivateKey)
	if err != nil {
		log.Errorw("failed to new approval private key manager", "error", err)
		return nil, err
	}
	approvalClient, err := client.NewMechainClient(rpcAddr, evmRpcAddr, chainID, client.WithKeyManager(approvalKM))
	if err != nil {
		log.Errorw("failed to new approval mechain client", "error", err)
		return nil, err
	}

	gcKM, err := keys.NewPrivateKeyManager(gcPrivateKey)
	if err != nil {
		log.Errorw("failed to new gc private key manager", "error", err)
		return nil, err
	}
	gcClient, err := client.NewMechainClient(rpcAddr, evmRpcAddr, chainID, client.WithKeyManager(gcKM))
	if err != nil {
		log.Errorw("failed to new gc mechain client", "error", err)
		return nil, err
	}
	gcAccNonce, err := gcClient.GetNonce(context.Background())
	if err != nil {
		log.Errorw("failed to get gc nonce", "error", err)
		return nil, err
	}

	privateKeys := map[SignType]string{
		SignOperator: operatorPrivateKey,
		SignSeal:     sealPrivateKey,
		SignApproval: approvalPrivateKey,
		SignGc:       gcPrivateKey,
	}

	mechainClients := map[SignType]*client.MechainClient{
		SignOperator: operatorClient,
		SignSeal:     sealClient,
		SignApproval: approvalClient,
		SignGc:       gcClient,
	}

	return &MechainChainSignClient{
		gasInfo:          gasInfo,
		mechainClients:   mechainClients,
		privateKeys:      privateKeys,
		sealAccNonce:     sealAccNonce,
		gcAccNonce:       gcAccNonce,
		operatorAccNonce: operatorAccNonce,
		blsKm:            blsKM,
		evmClient:        evmClient,
	}, nil
}

// GetAddr returns the public address of the private key.
func (client *MechainChainSignClient) GetAddr(scope SignType) (sdk.AccAddress, error) {
	km, err := client.mechainClients[scope].GetKeyManager()
	if err != nil {
		return nil, err
	}
	return km.GetAddr(), nil
}

// Sign returns a msg signature signed by private key.
func (client *MechainChainSignClient) Sign(scope SignType, msg []byte) ([]byte, error) {
	km, err := client.mechainClients[scope].GetKeyManager()
	if err != nil {
		return nil, err
	}
	return km.Sign(msg)
}

// VerifySignature verifies the signature.
func (client *MechainChainSignClient) VerifySignature(scope SignType, msg, sig []byte) bool {
	km, err := client.mechainClients[scope].GetKeyManager()
	if err != nil {
		return false
	}
	return types.VerifySignature(km.GetAddr(), crypto.Keccak256(msg), sig) == nil
}

// SealObject seal the object on the mechain chain.
func (client *MechainChainSignClient) SealObject(ctx context.Context, scope SignType,
	sealObject *storagetypes.MsgSealObject,
) (string, error) {
	if sealObject == nil {
		log.CtxError(ctx, "failed to seal object due to pointer dangling")
		return "", ErrDanglingPointer
	}
	ctx = log.WithValue(ctx, log.CtxKeyBucketName, sealObject.GetBucketName())
	ctx = log.WithValue(ctx, log.CtxKeyObjectName, sealObject.GetObjectName())
	km, err := client.mechainClients[scope].GetKeyManager()
	if err != nil {
		log.CtxErrorw(ctx, "failed to get private key", "error", err)
		return "", ErrSignMsg
	}

	client.sealLock.Lock()
	defer client.sealLock.Unlock()

	msgSealObject := storagetypes.NewMsgSealObject(km.GetAddr(),
		sealObject.GetBucketName(), sealObject.GetObjectName(), sealObject.GetGlobalVirtualGroupId(),
		sealObject.GetSecondarySpBlsAggSignatures())

	mode := tx.BroadcastMode_BROADCAST_MODE_SYNC

	var (
		txHash   string
		nonce    uint64
		nonceErr error
	)
	for i := 0; i < BroadcastTxRetry; i++ {
		nonce = client.sealAccNonce
		txOpt := &ctypes.TxOption{
			NoSimulate: false,
			Mode:       &mode,
			GasLimit:   client.gasInfo[Seal].GasLimit,
			FeeAmount:  client.gasInfo[Seal].FeeAmount,
			Nonce:      nonce,
		}

		txHash, err = client.broadcastTx(ctx, client.mechainClients[scope], []sdk.Msg{msgSealObject}, txOpt)
		if errors.IsOf(err, sdkErrors.ErrWrongSequence) {
			// if nonce mismatch, wait for next block, reset nonce by querying the nonce on chain
			nonce, nonceErr = client.getNonceOnChain(ctx, client.mechainClients[scope])
			if nonceErr != nil {
				log.CtxErrorw(ctx, "failed to get seal account nonce", "error", nonceErr)
				ErrSealObjectOnChain.SetError(fmt.Errorf("failed to get seal account nonce, error: %v", nonceErr))
				return "", ErrSealObjectOnChain
			}
			client.sealAccNonce = nonce
		}

		if err != nil {
			log.CtxErrorw(ctx, "failed to broadcast seal object tx", "retry_number", i, "error", err)
			continue
		}
		client.sealAccNonce = nonce + 1
		log.CtxDebugw(ctx, "succeed to broadcast seal object tx", "tx_hash", txHash, "seal_msg", msgSealObject)
		return txHash, nil
	}

	// failed to broadcast tx
	ErrSealObjectOnChain.SetError(fmt.Errorf("failed to broadcast seal object tx, error: %v", err))
	return "", ErrSealObjectOnChain
}

// SealObjectEvm seal the object on the mechain by evm tx.
func (client *MechainChainSignClient) SealObjectEvm(ctx context.Context, scope SignType,
	sealObject *storagetypes.MsgSealObject,
) (string, error) {
	if sealObject == nil {
		log.CtxError(ctx, "failed to seal object due to pointer dangling")
		return "", ErrDanglingPointer
	}
	ctx = log.WithValue(ctx, log.CtxKeyBucketName, sealObject.GetBucketName())
	ctx = log.WithValue(ctx, log.CtxKeyObjectName, sealObject.GetObjectName())
	km, err := client.mechainClients[scope].GetKeyManager()
	if err != nil {
		log.CtxErrorw(ctx, "failed to get private key", "error", err)
		return "", ErrSignMsg
	}

	cosmosChainId, err := client.mechainClients[scope].GetChainID()
	if err != nil {
		return "", err
	}

	chainId, err := types.ParseChainID(cosmosChainId)
	if err != nil {
		return "", err
	}

	client.sealLock.Lock()
	defer client.sealLock.Unlock()

	msgSealObject := storagetypes.NewMsgSealObject(km.GetAddr(),
		sealObject.GetBucketName(), sealObject.GetObjectName(), sealObject.GetGlobalVirtualGroupId(),
		sealObject.GetSecondarySpBlsAggSignatures())

	var (
		txHash   string
		nonce    uint64
		nonceErr error
	)
	for i := 0; i < BroadcastTxRetry; i++ {
		nonce = client.sealAccNonce

		txOpts, err := CreateTxOpts(ctx, client.evmClient, client.privateKeys[SignSeal], chainId, client.gasInfo[Seal].GasLimit, nonce)
		if err != nil {
			log.CtxErrorw(ctx, "failed to create tx opts", "error", err)
			return "", err
		}

		session, err := CreateStorageSession(client.evmClient, *txOpts, types.StorageAddress)
		if err != nil {
			log.CtxErrorw(ctx, "failed to create session", "error", err)
			return "", err
		}

		txRsp, err := session.SealObject(
			ethcmn.BytesToAddress(km.GetAddr().Bytes()),
			sealObject.GetBucketName(),
			sealObject.GetObjectName(),
			sealObject.GetGlobalVirtualGroupId(),
			base64.StdEncoding.EncodeToString(sealObject.GetSecondarySpBlsAggSignatures()),
		)

		if err != nil {
			if strings.Contains(err.Error(), "invalid nonce") {
				// if nonce mismatch, wait for next block, reset nonce by querying the nonce on chain
				nonce, nonceErr = client.getNonceOnChain(ctx, client.mechainClients[scope])
				if nonceErr != nil {
					log.CtxErrorw(ctx, "failed to get seal account nonce", "error", nonceErr)
					ErrSealObjectOnChain.SetError(fmt.Errorf("failed to get seal account nonce, error: %v", nonceErr))
					return "", ErrSealObjectOnChain
				}
				client.sealAccNonce = nonce
			}

			log.CtxErrorw(ctx, "failed to broadcast seal object tx", "retry_number", i, "error", err)
			continue
		}

		client.sealAccNonce = nonce + 1
		log.CtxDebugw(ctx, "succeed to broadcast seal object tx", "tx_hash", txHash, "seal_msg", msgSealObject)
		return txRsp.Hash().String(), nil
	}

	// failed to broadcast tx
	ErrSealObjectOnChain.SetError(fmt.Errorf("failed to broadcast seal object tx, error: %v", err))
	return "", ErrSealObjectOnChain
}

// RejectUnSealObject reject seal object on the mechain chain.
func (client *MechainChainSignClient) RejectUnSealObject(ctx context.Context, scope SignType,
	rejectObject *storagetypes.MsgRejectSealObject,
) (string, error) {
	if rejectObject == nil {
		log.CtxError(ctx, "failed to reject unseal object due to pointer dangling")
		return "", ErrDanglingPointer
	}
	ctx = log.WithValue(ctx, log.CtxKeyBucketName, rejectObject.GetBucketName())
	ctx = log.WithValue(ctx, log.CtxKeyObjectName, rejectObject.GetObjectName())
	km, err := client.mechainClients[scope].GetKeyManager()
	if err != nil {
		log.CtxErrorw(ctx, "failed to get private key", "error", err)
		return "", ErrSignMsg
	}

	client.sealLock.Lock()
	defer client.sealLock.Unlock()

	msgRejectUnSealObject := storagetypes.NewMsgRejectUnsealedObject(km.GetAddr(), rejectObject.GetBucketName(), rejectObject.GetObjectName())
	mode := tx.BroadcastMode_BROADCAST_MODE_SYNC

	var (
		txHash   string
		nonce    uint64
		nonceErr error
	)

	for i := 0; i < BroadcastTxRetry; i++ {
		nonce = client.sealAccNonce
		txOpt := &ctypes.TxOption{
			NoSimulate: false,
			Mode:       &mode,
			GasLimit:   client.gasInfo[RejectSeal].GasLimit,
			FeeAmount:  client.gasInfo[RejectSeal].FeeAmount,
			Nonce:      nonce,
		}
		txHash, err = client.broadcastTx(ctx, client.mechainClients[scope], []sdk.Msg{msgRejectUnSealObject}, txOpt)
		if errors.IsOf(err, sdkErrors.ErrWrongSequence) {
			// if nonce mismatch, wait for next block, reset nonce by querying the nonce on chain
			nonce, nonceErr = client.getNonceOnChain(ctx, client.mechainClients[scope])
			if nonceErr != nil {
				log.CtxErrorw(ctx, "failed to get seal account nonce", "error", nonceErr)
				ErrRejectUnSealObjectOnChain.SetError(fmt.Errorf("failed to get seal account nonce, error: %v", nonceErr))
				return "", ErrRejectUnSealObjectOnChain
			}
			client.sealAccNonce = nonce
		}

		if err != nil {
			log.CtxErrorw(ctx, "failed to broadcast reject unseal object", "retry_number", i, "error", err)
			continue
		}

		client.sealAccNonce = nonce + 1
		log.CtxDebugw(ctx, "succeed to broadcast reject unseal object tx", "tx_hash", txHash)
		return txHash, nil
	}
	// failed to broadcast tx
	ErrRejectUnSealObjectOnChain.SetError(fmt.Errorf("failed to broadcast reject unseal object tx, error: %v", err))
	return "", ErrRejectUnSealObjectOnChain
}

func (client *MechainChainSignClient) RejectUnSealObjectEvm(ctx context.Context, scope SignType,
	rejectObject *storagetypes.MsgRejectSealObject,
) (string, error) {
	if rejectObject == nil {
		log.CtxError(ctx, "failed to reject unseal object due to pointer dangling")
		return "", ErrDanglingPointer
	}
	ctx = log.WithValue(ctx, log.CtxKeyBucketName, rejectObject.GetBucketName())
	ctx = log.WithValue(ctx, log.CtxKeyObjectName, rejectObject.GetObjectName())
	km, err := client.mechainClients[scope].GetKeyManager()
	if err != nil {
		log.CtxErrorw(ctx, "failed to get private key", "error", err)
		return "", ErrSignMsg
	}

	cosmosChainId, err := client.mechainClients[scope].GetChainID()
	if err != nil {
		return "", err
	}

	chainId, err := types.ParseChainID(cosmosChainId)
	if err != nil {
		return "", err
	}

	client.sealLock.Lock()
	defer client.sealLock.Unlock()

	msgRejectUnSealObject := storagetypes.NewMsgRejectUnsealedObject(km.GetAddr(), rejectObject.GetBucketName(), rejectObject.GetObjectName())

	var (
		txHash   string
		nonce    uint64
		nonceErr error
	)

	for i := 0; i < BroadcastTxRetry; i++ {
		nonce = client.sealAccNonce
		txOpts, err := CreateTxOpts(ctx, client.evmClient, client.privateKeys[scope], chainId, client.gasInfo[RejectSeal].GasLimit, nonce)
		if err != nil {
			log.CtxErrorw(ctx, "failed to create tx opts", "error", err)
			return "", err
		}

		session, err := CreateStorageSession(client.evmClient, *txOpts, types.StorageAddress)
		if err != nil {
			log.CtxErrorw(ctx, "failed to create session", "error", err)
			return "", err
		}

		txRsp, err := session.RejectSealObject(
			rejectObject.GetBucketName(),
			rejectObject.GetObjectName(),
		)
		if err != nil {
			if strings.Contains(err.Error(), "invalid nonce") {
				// if nonce mismatch, wait for next block, reset nonce by querying the nonce on chain
				nonce, nonceErr = client.getNonceOnChain(ctx, client.mechainClients[scope])
				if nonceErr != nil {
					log.CtxErrorw(ctx, "failed to get seal account nonce", "error", nonceErr)
					ErrRejectUnSealObjectOnChain.SetError(fmt.Errorf("failed to get seal account nonce, error: %v", nonceErr))
					return "", ErrRejectUnSealObjectOnChain
				}
				client.sealAccNonce = nonce
			}

			log.CtxErrorw(ctx, "failed to broadcast reject unseal object", "retry_number", i, "error", err)
			continue
		}

		client.sealAccNonce = nonce + 1
		log.CtxDebugw(ctx, "succeed to broadcast reject unseal object tx", "tx_hash", txHash, "reject unseal object msg", msgRejectUnSealObject)
		return txRsp.Hash().String(), nil
	}
	// failed to broadcast tx
	ErrRejectUnSealObjectOnChain.SetError(fmt.Errorf("failed to broadcast reject unseal object tx, error: %v", err))
	return "", ErrRejectUnSealObjectOnChain
}

// DiscontinueBucket stops serving the bucket on the mechain chain.
func (client *MechainChainSignClient) DiscontinueBucket(ctx context.Context, scope SignType, discontinueBucket *storagetypes.MsgDiscontinueBucket) (string, error) {
	log.Infow("signer start to discontinue bucket", "scope", scope)
	km, err := client.mechainClients[scope].GetKeyManager()
	if err != nil {
		log.CtxErrorw(ctx, "failed to get private key", "err", err)
		return "", ErrSignMsg
	}

	client.gcLock.Lock()
	defer client.gcLock.Unlock()
	nonce := client.gcAccNonce

	msgDiscontinueBucket := storagetypes.NewMsgDiscontinueBucket(km.GetAddr(),
		discontinueBucket.BucketName, discontinueBucket.Reason)
	mode := tx.BroadcastMode_BROADCAST_MODE_SYNC
	txOpt := &ctypes.TxOption{ // allow simulation here to save gas cost
		Mode:  &mode,
		Nonce: nonce,
	}

	txHash, err := client.broadcastTx(ctx, client.mechainClients[scope], []sdk.Msg{msgDiscontinueBucket}, txOpt)
	if errors.IsOf(err, sdkErrors.ErrWrongSequence) {
		// if nonce mismatch, wait for next block, reset nonce by querying the nonce on chain
		nonce, nonceErr := client.getNonceOnChain(ctx, client.mechainClients[scope])
		if nonceErr != nil {
			log.CtxErrorw(ctx, "failed to get gc account nonce", "error", nonceErr)
			ErrDiscontinueBucketOnChain.SetError(fmt.Errorf("failed to get gc account nonce, error: %v", nonceErr))
			return "", ErrDiscontinueBucketOnChain
		}
		client.gcAccNonce = nonce
	}

	// failed to broadcast tx
	if err != nil {
		log.CtxErrorw(ctx, "failed to broadcast discontinue bucket", "error", err, "discontinue_bucket", msgDiscontinueBucket.String())
		ErrDiscontinueBucketOnChain.SetError(fmt.Errorf("failed to broadcast discontinue bucket, error: %v", err))
		return "", ErrDiscontinueBucketOnChain
	}
	// update nonce when tx is successful submitted
	client.gcAccNonce = nonce + 1
	return txHash, nil
}

func (client *MechainChainSignClient) DiscontinueBucketEvm(ctx context.Context, scope SignType, discontinueBucket *storagetypes.MsgDiscontinueBucket) (string, error) {
	log.Infow("signer start to discontinue bucket", "scope", scope)
	km, err := client.mechainClients[scope].GetKeyManager()
	if err != nil {
		log.CtxErrorw(ctx, "failed to get private key", "err", err)
		return "", ErrSignMsg
	}
	cosmosChainId, err := client.mechainClients[scope].GetChainID()
	if err != nil {
		return "", err
	}

	chainId, err := types.ParseChainID(cosmosChainId)
	if err != nil {
		return "", err
	}

	client.gcLock.Lock()
	defer client.gcLock.Unlock()
	nonce := client.gcAccNonce

	msgDiscontinueBucket := storagetypes.NewMsgDiscontinueBucket(km.GetAddr(),
		discontinueBucket.BucketName, discontinueBucket.Reason)

	txOpts, err := CreateTxOpts(ctx, client.evmClient, client.privateKeys[scope], chainId, client.gasInfo[DiscontinueBucket].GasLimit, nonce)
	if err != nil {
		log.CtxErrorw(ctx, "failed to create tx opts", "error", err)
		return "", err
	}

	session, err := CreateStorageSession(client.evmClient, *txOpts, types.StorageAddress)
	if err != nil {
		log.CtxErrorw(ctx, "failed to create session", "error", err)
		return "", err
	}

	txRsp, err := session.DiscontinueBucket(
		discontinueBucket.GetBucketName(),
		discontinueBucket.GetReason(),
	)

	if err != nil {
		if strings.Contains(err.Error(), "invalid nonce") {
			// if nonce mismatch, wait for next block, reset nonce by querying the nonce on chain
			nonce, nonceErr := client.getNonceOnChain(ctx, client.mechainClients[scope])
			if nonceErr != nil {
				log.CtxErrorw(ctx, "failed to get gc account nonce", "error", nonceErr)
				ErrDiscontinueBucketOnChain.SetError(fmt.Errorf("failed to get gc account nonce, error: %v", nonceErr))
				return "", ErrDiscontinueBucketOnChain
			}
			client.gcAccNonce = nonce
		}
		log.CtxErrorw(ctx, "failed to broadcast discontinue bucket", "error", err, "discontinue_bucket", msgDiscontinueBucket.String())
		ErrDiscontinueBucketOnChain.SetError(fmt.Errorf("failed to broadcast discontinue bucket, error: %v", err))
		return "", ErrDiscontinueBucketOnChain
	}
	// update nonce when tx is successful submitted
	client.gcAccNonce = nonce + 1
	return txRsp.Hash().String(), nil
}

func (client *MechainChainSignClient) CreateGlobalVirtualGroup(ctx context.Context, scope SignType,
	gvg *virtualgrouptypes.MsgCreateGlobalVirtualGroup,
) (string, error) {
	log.Infow("signer starts to create a new global virtual group", "scope", scope)
	if gvg == nil {
		log.CtxError(ctx, "failed to create virtual group due to pointer dangling")
		return "", ErrDanglingPointer
	}
	km, err := client.mechainClients[scope].GetKeyManager()
	if err != nil {
		log.CtxErrorw(ctx, "failed to get private key", "error", err)
		return "", ErrSignMsg
	}

	client.opLock.Lock()
	defer client.opLock.Unlock()

	msgCreateGlobalVirtualGroup := virtualgrouptypes.NewMsgCreateGlobalVirtualGroup(km.GetAddr(),
		gvg.FamilyId, gvg.GetSecondarySpIds(), gvg.GetDeposit())
	mode := tx.BroadcastMode_BROADCAST_MODE_SYNC

	var (
		txHash   string
		nonce    uint64
		nonceErr error
	)
	for i := 0; i < BroadcastTxRetry; i++ {
		nonce = client.operatorAccNonce
		txOpt := &ctypes.TxOption{
			Mode:      &mode,
			GasLimit:  client.gasInfo[CreateGlobalVirtualGroup].GasLimit,
			FeeAmount: client.gasInfo[CreateGlobalVirtualGroup].FeeAmount,
			Nonce:     nonce,
		}
		txHash, err = client.broadcastTx(ctx, client.mechainClients[scope], []sdk.Msg{msgCreateGlobalVirtualGroup}, txOpt)
		if errors.IsOf(err, sdkErrors.ErrWrongSequence) {
			// if nonce mismatches, waiting for next block, reset nonce by querying the nonce on chain
			nonce, nonceErr = client.getNonceOnChain(ctx, client.mechainClients[scope])
			if nonceErr != nil {
				log.CtxErrorw(ctx, "failed to get operator account nonce", "error", nonceErr)
				ErrCreateGVGOnChain.SetError(fmt.Errorf("failed to get approval account nonce, error: %v", err))
				return "", ErrCreateGVGOnChain
			}
			client.operatorAccNonce = nonce
		}
		if err != nil {
			log.CtxErrorw(ctx, "failed to broadcast global virtual group tx", "global_virtual_group",
				msgCreateGlobalVirtualGroup.String(), "retry_number", i, "error", err)
			continue
		}
		client.operatorAccNonce = nonce + 1
		log.CtxDebugw(ctx, "succeed to broadcast create virtual group tx", "tx_hash", txHash,
			"virtual_group_msg", msgCreateGlobalVirtualGroup)
		return txHash, nil

	}

	// failed to broadcast tx
	ErrCreateGVGOnChain.SetError(fmt.Errorf("failed to broadcast create virtual group tx, error: %v", err))
	return "", ErrCreateGVGOnChain
}

func (client *MechainChainSignClient) CreateGlobalVirtualGroupEvm(ctx context.Context, scope SignType,
	gvg *virtualgrouptypes.MsgCreateGlobalVirtualGroup,
) (string, error) {
	log.Infow("signer starts to create a new global virtual group", "scope", scope)
	if gvg == nil {
		log.CtxError(ctx, "failed to create virtual group due to pointer dangling")
		return "", ErrDanglingPointer
	}
	km, err := client.mechainClients[scope].GetKeyManager()
	if err != nil {
		log.CtxErrorw(ctx, "failed to get private key", "error", err)
		return "", ErrSignMsg
	}
	cosmosChainId, err := client.mechainClients[scope].GetChainID()
	if err != nil {
		return "", err
	}

	chainId, err := types.ParseChainID(cosmosChainId)
	if err != nil {
		return "", err
	}

	client.opLock.Lock()
	defer client.opLock.Unlock()

	msgCreateGlobalVirtualGroup := virtualgrouptypes.NewMsgCreateGlobalVirtualGroup(km.GetAddr(),
		gvg.FamilyId, gvg.GetSecondarySpIds(), gvg.GetDeposit())

	var (
		txHash   string
		nonce    uint64
		nonceErr error
	)
	for i := 0; i < BroadcastTxRetry; i++ {
		nonce = client.operatorAccNonce
		txOpts, err := CreateTxOpts(ctx, client.evmClient, client.privateKeys[scope], chainId, client.gasInfo[CreateGlobalVirtualGroup].GasLimit, nonce)
		if err != nil {
			log.CtxErrorw(ctx, "failed to create tx opts", "error", err)
			return "", err
		}

		session, err := CreateVirtualGroupSession(client.evmClient, *txOpts, types.VirtualGroupAddress)
		if err != nil {
			log.CtxErrorw(ctx, "failed to create session", "error", err)
			return "", err
		}

		deposit := virtualgroup.Coin{
			Denom:  gvg.GetDeposit().Denom,
			Amount: gvg.GetDeposit().Amount.BigInt(),
		}
		txRsp, err := session.CreateGlobalVirtualGroup(
			gvg.FamilyId,
			gvg.GetSecondarySpIds(),
			deposit,
		)

		if err != nil {
			if strings.Contains(err.Error(), "invalid nonce") {
				// if nonce mismatches, waiting for next block, reset nonce by querying the nonce on chain
				nonce, nonceErr = client.getNonceOnChain(ctx, client.mechainClients[scope])
				if nonceErr != nil {
					log.CtxErrorw(ctx, "failed to get operator account nonce", "error", nonceErr)
					ErrCreateGVGOnChain.SetError(fmt.Errorf("failed to get approval account nonce, error: %v", err))
					return "", ErrCreateGVGOnChain
				}
				client.operatorAccNonce = nonce
			}

			log.CtxErrorw(ctx, "failed to broadcast global virtual group tx", "global_virtual_group",
				msgCreateGlobalVirtualGroup.String(), "retry_number", i, "error", err)
			continue
		}
		client.operatorAccNonce = nonce + 1
		log.CtxDebugw(ctx, "succeed to broadcast create virtual group tx", "tx_hash", txHash,
			"virtual_group_msg", msgCreateGlobalVirtualGroup)
		return txRsp.Hash().String(), nil
	}

	// failed to broadcast tx
	ErrCreateGVGOnChain.SetError(fmt.Errorf("failed to broadcast create virtual group tx, error: %v", err))
	return "", ErrCreateGVGOnChain
}

func (client *MechainChainSignClient) CompleteMigrateBucket(ctx context.Context, scope SignType,
	migrateBucket *storagetypes.MsgCompleteMigrateBucket,
) (string, error) {
	log.Infow("signer starts to complete migrate bucket", "scope", scope)
	if migrateBucket == nil {
		log.CtxError(ctx, "complete migrate bucket msg pointer dangling")
		return "", ErrDanglingPointer
	}
	km, err := client.mechainClients[scope].GetKeyManager()
	if err != nil {
		log.CtxErrorw(ctx, "failed to get private key", "error", err)
		return "", ErrSignMsg
	}

	client.opLock.Lock()
	defer client.opLock.Unlock()

	msgCompleteMigrateBucket := storagetypes.NewMsgCompleteMigrateBucket(km.GetAddr(), migrateBucket.GetBucketName(),
		migrateBucket.GetGlobalVirtualGroupFamilyId(), migrateBucket.GetGvgMappings())

	mode := tx.BroadcastMode_BROADCAST_MODE_SYNC

	var (
		txHash   string
		nonce    uint64
		nonceErr error
	)
	for i := 0; i < BroadcastTxRetry; i++ {
		nonce = client.operatorAccNonce
		txOpt := &ctypes.TxOption{
			Mode:  &mode,
			Nonce: nonce,
		}
		txHash, err = client.broadcastTx(ctx, client.mechainClients[scope], []sdk.Msg{msgCompleteMigrateBucket}, txOpt)
		if errors.IsOf(err, sdkErrors.ErrWrongSequence) {
			// if nonce mismatches, waiting for next block, reset nonce by querying the nonce on chain
			nonce, nonceErr = client.getNonceOnChain(ctx, client.mechainClients[scope])
			if nonceErr != nil {
				log.CtxErrorw(ctx, "failed to get operator account nonce", "error", err)
				ErrCompleteMigrateBucketOnChain.SetError(fmt.Errorf("failed to get operator account nonce, error: %v", err))
				return "", ErrCompleteMigrateBucketOnChain
			}
			client.operatorAccNonce = nonce
		}
		if err != nil {
			log.CtxErrorw(ctx, "failed to broadcast complete migrate bucket tx", "retry_number", i, "error", err)
			continue
		}
		client.operatorAccNonce = nonce + 1
		log.CtxDebugw(ctx, "succeed to broadcast complete migrate bucket tx", "tx_hash", txHash, "seal_msg", msgCompleteMigrateBucket)
		return txHash, nil
	}

	// failed to broadcast tx
	ErrCompleteMigrateBucketOnChain.SetError(fmt.Errorf("failed to broadcast complete migrate bucket, error: %v", err))
	return "", ErrCompleteMigrateBucketOnChain
}

func (client *MechainChainSignClient) CompleteMigrateBucketEvm(ctx context.Context, scope SignType,
	migrateBucket *storagetypes.MsgCompleteMigrateBucket,
) (string, error) {
	log.Infow("signer starts to complete migrate bucket", "scope", scope)
	if migrateBucket == nil {
		log.CtxError(ctx, "complete migrate bucket msg pointer dangling")
		return "", ErrDanglingPointer
	}
	km, err := client.mechainClients[scope].GetKeyManager()
	if err != nil {
		log.CtxErrorw(ctx, "failed to get private key", "error", err)
		return "", ErrSignMsg
	}
	cosmosChainId, err := client.mechainClients[scope].GetChainID()
	if err != nil {
		return "", err
	}

	chainId, err := types.ParseChainID(cosmosChainId)
	if err != nil {
		return "", err
	}

	client.opLock.Lock()
	defer client.opLock.Unlock()

	msgCompleteMigrateBucket := storagetypes.NewMsgCompleteMigrateBucket(km.GetAddr(), migrateBucket.GetBucketName(),
		migrateBucket.GetGlobalVirtualGroupFamilyId(), migrateBucket.GetGvgMappings())

	var (
		txHash   string
		nonce    uint64
		nonceErr error
	)
	for i := 0; i < BroadcastTxRetry; i++ {
		nonce = client.operatorAccNonce
		txOpts, err := CreateTxOpts(ctx, client.evmClient, client.privateKeys[scope], chainId, client.gasInfo[CompleteMigrateBucket].GasLimit, nonce)
		if err != nil {
			log.CtxErrorw(ctx, "failed to create tx opts", "error", err)
			return "", err
		}

		session, err := CreateStorageSession(client.evmClient, *txOpts, types.StorageAddress)
		if err != nil {
			log.CtxErrorw(ctx, "failed to create session", "error", err)
			return "", err
		}

		gvgMappings := make([]storage.GVGMapping, 0)
		ptrgvgMappings := migrateBucket.GetGvgMappings()
		if ptrgvgMappings != nil {
			for _, gvgMapping := range ptrgvgMappings {
				gvgMappings = append(gvgMappings, storage.GVGMapping{
					SrcGlobalVirtualGroupId: gvgMapping.SrcGlobalVirtualGroupId,
					DstGlobalVirtualGroupId: gvgMapping.DstGlobalVirtualGroupId,
					SecondarySpBlsSignature: gvgMapping.SecondarySpBlsSignature,
				})
			}
		}

		txRsp, err := session.CompleteMigrateBucket(
			migrateBucket.GetBucketName(),
			migrateBucket.GetGlobalVirtualGroupFamilyId(),
			gvgMappings,
		)

		if err != nil {
			if strings.Contains(err.Error(), "invalid nonce") {
				// if nonce mismatches, waiting for next block, reset nonce by querying the nonce on chain
				nonce, nonceErr = client.getNonceOnChain(ctx, client.mechainClients[scope])
				if nonceErr != nil {
					log.CtxErrorw(ctx, "failed to get operator account nonce", "error", err)
					ErrCompleteMigrateBucketOnChain.SetError(fmt.Errorf("failed to get operator account nonce, error: %v", err))
					return "", ErrCompleteMigrateBucketOnChain
				}
				client.operatorAccNonce = nonce
			}

			log.CtxErrorw(ctx, "failed to broadcast complete migrate bucket tx", "retry_number", i, "error", err)
			continue
		}
		client.operatorAccNonce = nonce + 1
		log.CtxDebugw(ctx, "succeed to broadcast complete migrate bucket tx", "tx_hash", txHash, "seal_msg", msgCompleteMigrateBucket)
		return txRsp.Hash().String(), nil
	}

	// failed to broadcast tx
	ErrCompleteMigrateBucketOnChain.SetError(fmt.Errorf("failed to broadcast complete migrate bucket, error: %v", err))
	return "", ErrCompleteMigrateBucketOnChain
}

func (client *MechainChainSignClient) UpdateSPPrice(ctx context.Context, scope SignType,
	priceInfo *sptypes.MsgUpdateSpStoragePrice,
) (string, error) {
	log.Infow("signer starts to complete update SP price info", "scope", scope)
	if priceInfo == nil {
		log.CtxError(ctx, "complete migrate bucket msg pointer dangling")
		return "", ErrDanglingPointer
	}
	km, err := client.mechainClients[scope].GetKeyManager()
	if err != nil {
		log.CtxErrorw(ctx, "failed to get private key", "error", err)
		return "", ErrSignMsg
	}

	client.opLock.Lock()
	defer client.opLock.Unlock()
	nonce := client.operatorAccNonce

	msgUpdateStorageSPPrice := &sptypes.MsgUpdateSpStoragePrice{
		SpAddress:     km.GetAddr().String(),
		ReadPrice:     priceInfo.ReadPrice,
		FreeReadQuota: priceInfo.FreeReadQuota,
		StorePrice:    priceInfo.StorePrice,
	}
	mode := tx.BroadcastMode_BROADCAST_MODE_SYNC
	txOpt := &ctypes.TxOption{
		Mode:      &mode,
		GasLimit:  client.gasInfo[UpdateSPPrice].GasLimit,
		FeeAmount: client.gasInfo[UpdateSPPrice].FeeAmount,
		Nonce:     nonce,
	}

	txHash, err := client.broadcastTx(ctx, client.mechainClients[scope], []sdk.Msg{msgUpdateStorageSPPrice}, txOpt)
	if errors.IsOf(err, sdkErrors.ErrWrongSequence) {
		// if nonce mismatches, waiting for next block, reset nonce by querying the nonce on chain
		nonce, nonceErr := client.getNonceOnChain(ctx, client.mechainClients[scope])
		if nonceErr != nil {
			log.CtxErrorw(ctx, "failed to get approval account nonce", "error", err)
			ErrUpdateSPPriceOnChain.SetError(fmt.Errorf("failed to get approval account nonce, error: %v", err))
			return "", ErrUpdateSPPriceOnChain
		}
		client.operatorAccNonce = nonce
	}
	// failed to broadcast tx
	if err != nil {
		log.CtxErrorw(ctx, "failed to broadcast update sp price msg", "error", err, "update_sp_price",
			msgUpdateStorageSPPrice.String())
		ErrUpdateSPPriceOnChain.SetError(fmt.Errorf("failed to broadcast msg to update sp price, error: %v", err))
		return "", ErrUpdateSPPriceOnChain
	}

	// update nonce when tx is successfully submitted
	client.operatorAccNonce = nonce + 1
	return txHash, nil
}

func (client *MechainChainSignClient) UpdateSPPriceEvm(ctx context.Context, scope SignType,
	priceInfo *sptypes.MsgUpdateSpStoragePrice,
) (string, error) {
	log.Infow("signer starts to complete update SP price info", "scope", scope)
	if priceInfo == nil {
		log.CtxError(ctx, "complete migrate bucket msg pointer dangling")
		return "", ErrDanglingPointer
	}
	km, err := client.mechainClients[scope].GetKeyManager()
	if err != nil {
		log.CtxErrorw(ctx, "failed to get private key", "error", err)
		return "", ErrSignMsg
	}
	cosmosChainId, err := client.mechainClients[scope].GetChainID()
	if err != nil {
		return "", err
	}

	chainId, err := types.ParseChainID(cosmosChainId)
	if err != nil {
		return "", err
	}

	client.opLock.Lock()
	defer client.opLock.Unlock()
	nonce := client.operatorAccNonce

	msgUpdateStorageSPPrice := &sptypes.MsgUpdateSpStoragePrice{
		SpAddress:     km.GetAddr().String(),
		ReadPrice:     priceInfo.ReadPrice,
		FreeReadQuota: priceInfo.FreeReadQuota,
		StorePrice:    priceInfo.StorePrice,
	}
	txOpts, err := CreateTxOpts(ctx, client.evmClient, client.privateKeys[scope], chainId, client.gasInfo[UpdateSPPrice].GasLimit, nonce)
	if err != nil {
		log.CtxErrorw(ctx, "failed to create tx opts", "error", err)
		return "", err
	}

	session, err := CreateStorageProviderSession(client.evmClient, *txOpts, types.SpAddress)
	if err != nil {
		log.CtxErrorw(ctx, "failed to create session", "error", err)
		return "", err
	}

	txRsp, err := session.UpdateSPPrice(
		priceInfo.ReadPrice.BigInt(),
		priceInfo.FreeReadQuota,
		priceInfo.StorePrice.BigInt(),
	)

	if err != nil {
		if strings.Contains(err.Error(), "invalid nonce") {
			// if nonce mismatches, waiting for next block, reset nonce by querying the nonce on chain
			nonce, nonceErr := client.getNonceOnChain(ctx, client.mechainClients[scope])
			if nonceErr != nil {
				log.CtxErrorw(ctx, "failed to get approval account nonce", "error", err)
				ErrUpdateSPPriceOnChain.SetError(fmt.Errorf("failed to get approval account nonce, error: %v", err))
				return "", ErrUpdateSPPriceOnChain
			}
			client.operatorAccNonce = nonce
		}

		log.CtxErrorw(ctx, "failed to broadcast update sp price msg", "error", err, "update_sp_price",
			msgUpdateStorageSPPrice.String())
		ErrUpdateSPPriceOnChain.SetError(fmt.Errorf("failed to broadcast msg to update sp price, error: %v", err))
		return "", ErrUpdateSPPriceOnChain
	}

	// update nonce when tx is successfully submitted
	client.operatorAccNonce = nonce + 1
	return txRsp.Hash().String(), nil
}

func (client *MechainChainSignClient) SwapOut(ctx context.Context, scope SignType,
	swapOut *virtualgrouptypes.MsgSwapOut,
) (string, error) {
	log.Infow("signer starts to swap out", "scope", scope)
	if swapOut == nil {
		log.CtxError(ctx, "failed to swap out due to pointer dangling")
		return "", ErrDanglingPointer
	}
	km, err := client.mechainClients[scope].GetKeyManager()
	if err != nil {
		log.CtxErrorw(ctx, "failed to get private key", "error", err)
		return "", ErrSignMsg
	}

	client.opLock.Lock()
	defer client.opLock.Unlock()

	msgSwapOut := virtualgrouptypes.NewMsgSwapOut(km.GetAddr(), swapOut.GetGlobalVirtualGroupFamilyId(), swapOut.GetGlobalVirtualGroupIds(),
		swapOut.GetSuccessorSpId())
	msgSwapOut.SuccessorSpApproval = &common.Approval{
		ExpiredHeight: swapOut.SuccessorSpApproval.GetExpiredHeight(),
		Sig:           swapOut.SuccessorSpApproval.GetSig(),
	}
	mode := tx.BroadcastMode_BROADCAST_MODE_SYNC

	var (
		txHash   string
		nonce    uint64
		nonceErr error
	)

	for i := 0; i < BroadcastTxRetry; i++ {
		nonce = client.operatorAccNonce
		txOpt := &ctypes.TxOption{
			Mode:      &mode,
			GasLimit:  client.gasInfo[SwapOut].GasLimit,
			FeeAmount: client.gasInfo[SwapOut].FeeAmount,
			Nonce:     nonce,
		}
		txHash, err = client.broadcastTx(ctx, client.mechainClients[scope], []sdk.Msg{msgSwapOut}, txOpt)
		if errors.IsOf(err, sdkErrors.ErrWrongSequence) {
			// if nonce mismatches, waiting for next block, reset nonce by querying the nonce on chain
			nonce, nonceErr = client.getNonceOnChain(ctx, client.mechainClients[scope])
			if nonceErr != nil {
				log.CtxErrorw(ctx, "failed to get operator account nonce", "error", nonceErr)
				ErrSwapOutOnChain.SetError(fmt.Errorf("failed to get operator account nonce, error: %v", err))
				return "", ErrSwapOutOnChain
			}
			client.operatorAccNonce = nonce
		}
		if err != nil {
			log.CtxErrorw(ctx, "failed to broadcast swap out", "retry_number", i, "swap_out", msgSwapOut.String(), "error", err)
			continue
		}
		client.operatorAccNonce = nonce + 1
		log.CtxDebugw(ctx, "succeed to broadcast start swap out tx", "tx_hash", txHash, "swap_out", msgSwapOut.String())
		return txHash, nil

	}

	// failed to broadcast tx
	ErrSwapOutOnChain.SetError(fmt.Errorf("failed to broadcast swap out tx, error: %v", err))
	return "", ErrSwapOutOnChain
}

func (client *MechainChainSignClient) SwapOutEvm(ctx context.Context, scope SignType,
	swapOut *virtualgrouptypes.MsgSwapOut,
) (string, error) {
	log.Infow("signer starts to swap out", "scope", scope)
	if swapOut == nil {
		log.CtxError(ctx, "failed to swap out due to pointer dangling")
		return "", ErrDanglingPointer
	}
	km, err := client.mechainClients[scope].GetKeyManager()
	if err != nil {
		log.CtxErrorw(ctx, "failed to get private key", "error", err)
		return "", ErrSignMsg
	}
	cosmosChainId, err := client.mechainClients[scope].GetChainID()
	if err != nil {
		return "", err
	}

	chainId, err := types.ParseChainID(cosmosChainId)
	if err != nil {
		return "", err
	}

	client.opLock.Lock()
	defer client.opLock.Unlock()

	msgSwapOut := virtualgrouptypes.NewMsgSwapOut(km.GetAddr(), swapOut.GetGlobalVirtualGroupFamilyId(), swapOut.GetGlobalVirtualGroupIds(),
		swapOut.GetSuccessorSpId())
	msgSwapOut.SuccessorSpApproval = &common.Approval{
		ExpiredHeight: swapOut.SuccessorSpApproval.GetExpiredHeight(),
		Sig:           swapOut.SuccessorSpApproval.GetSig(),
	}

	var (
		txHash   string
		nonce    uint64
		nonceErr error
	)

	for i := 0; i < BroadcastTxRetry; i++ {
		nonce = client.operatorAccNonce
		txOpts, err := CreateTxOpts(ctx, client.evmClient, client.privateKeys[scope], chainId, client.gasInfo[SwapOut].GasLimit, nonce)
		if err != nil {
			log.CtxErrorw(ctx, "failed to create tx opts", "error", err)
			return "", err
		}

		session, err := CreateVirtualGroupSession(client.evmClient, *txOpts, types.VirtualGroupAddress)
		if err != nil {
			log.CtxErrorw(ctx, "failed to create session", "error", err)
			return "", err
		}

		SuccessorSpApproval := virtualgroup.Approval{
			ExpiredHeight:              msgSwapOut.SuccessorSpApproval.ExpiredHeight,
			GlobalVirtualGroupFamilyId: msgSwapOut.SuccessorSpApproval.GlobalVirtualGroupFamilyId,
			Sig:                        msgSwapOut.SuccessorSpApproval.Sig,
		}
		txRsp, err := session.SwapOut(
			swapOut.GetGlobalVirtualGroupFamilyId(),
			swapOut.GetGlobalVirtualGroupIds(),
			swapOut.GetSuccessorSpId(),
			SuccessorSpApproval,
		)

		if err != nil {
			if strings.Contains(err.Error(), "invalid nonce") {
				// if nonce mismatches, waiting for next block, reset nonce by querying the nonce on chain
				nonce, nonceErr = client.getNonceOnChain(ctx, client.mechainClients[scope])
				if nonceErr != nil {
					log.CtxErrorw(ctx, "failed to get operator account nonce", "error", nonceErr)
					ErrSwapOutOnChain.SetError(fmt.Errorf("failed to get operator account nonce, error: %v", err))
					return "", ErrSwapOutOnChain
				}
				client.operatorAccNonce = nonce
			}

			log.CtxErrorw(ctx, "failed to broadcast swap out", "retry_number", i, "swap_out", msgSwapOut.String(), "error", err)
			continue
		}
		client.operatorAccNonce = nonce + 1
		log.CtxDebugw(ctx, "succeed to broadcast start swap out tx", "tx_hash", txHash, "swap_out", msgSwapOut.String())
		return txRsp.Hash().String(), nil
	}

	// failed to broadcast tx
	ErrSwapOutOnChain.SetError(fmt.Errorf("failed to broadcast swap out tx, error: %v", err))
	return "", ErrSwapOutOnChain
}

func (client *MechainChainSignClient) CompleteSwapOut(ctx context.Context, scope SignType,
	completeSwapOut *virtualgrouptypes.MsgCompleteSwapOut,
) (string, error) {
	log.Infow("signer starts to complete swap out", "scope", scope)
	if completeSwapOut == nil {
		log.CtxError(ctx, "complete swap out msg pointer dangling")
		return "", ErrDanglingPointer
	}
	km, err := client.mechainClients[scope].GetKeyManager()
	if err != nil {
		log.CtxErrorw(ctx, "failed to get private key", "error", err)
		return "", ErrSignMsg
	}

	client.opLock.Lock()
	defer client.opLock.Unlock()

	msgCompleteSwapOut := virtualgrouptypes.NewMsgCompleteSwapOut(km.GetAddr(), completeSwapOut.GetGlobalVirtualGroupFamilyId(),
		completeSwapOut.GetGlobalVirtualGroupIds())
	mode := tx.BroadcastMode_BROADCAST_MODE_SYNC

	var (
		txHash   string
		nonce    uint64
		nonceErr error
	)

	for i := 0; i < BroadcastTxRetry; i++ {
		nonce = client.operatorAccNonce
		txOpt := &ctypes.TxOption{
			Mode:      &mode,
			GasLimit:  client.gasInfo[CompleteSwapOut].GasLimit,
			FeeAmount: client.gasInfo[CompleteSwapOut].FeeAmount,
			Nonce:     nonce,
		}
		txHash, err = client.broadcastTx(ctx, client.mechainClients[scope], []sdk.Msg{msgCompleteSwapOut}, txOpt)
		if errors.IsOf(err, sdkErrors.ErrWrongSequence) {
			// if nonce mismatches, waiting for next block, reset nonce by querying the nonce on chain
			nonce, nonceErr = client.getNonceOnChain(ctx, client.mechainClients[scope])
			if nonceErr != nil {
				log.CtxErrorw(ctx, "failed to get operator account nonce", "error", nonceErr)
				ErrCompleteSwapOutOnChain.SetError(fmt.Errorf("failed to get operator account nonce, error: %v", nonceErr))
				return "", ErrCompleteSwapOutOnChain
			}
			client.operatorAccNonce = nonce
		}
		if err != nil {
			log.CtxErrorw(ctx, "failed to broadcast complete swap out tx", "retry_number", i, "error", err)
			continue
		}
		client.operatorAccNonce = nonce + 1
		log.CtxDebugw(ctx, "succeed to broadcast complete swap out tx", "tx_hash", txHash, "seal_msg", msgCompleteSwapOut)
		return txHash, nil
	}

	ErrCompleteSwapOutOnChain.SetError(fmt.Errorf("failed to broadcast complete swap out, error: %v", err))
	return "", ErrCompleteSwapOutOnChain
}

func (client *MechainChainSignClient) CompleteSwapOutEvm(ctx context.Context, scope SignType,
	completeSwapOut *virtualgrouptypes.MsgCompleteSwapOut,
) (string, error) {
	log.Infow("signer starts to complete swap out", "scope", scope)
	if completeSwapOut == nil {
		log.CtxError(ctx, "complete swap out msg pointer dangling")
		return "", ErrDanglingPointer
	}
	km, err := client.mechainClients[scope].GetKeyManager()
	if err != nil {
		log.CtxErrorw(ctx, "failed to get private key", "error", err)
		return "", ErrSignMsg
	}
	cosmosChainId, err := client.mechainClients[scope].GetChainID()
	if err != nil {
		return "", err
	}

	chainId, err := types.ParseChainID(cosmosChainId)
	if err != nil {
		return "", err
	}

	client.opLock.Lock()
	defer client.opLock.Unlock()

	msgCompleteSwapOut := virtualgrouptypes.NewMsgCompleteSwapOut(km.GetAddr(), completeSwapOut.GetGlobalVirtualGroupFamilyId(),
		completeSwapOut.GetGlobalVirtualGroupIds())

	var (
		txHash   string
		nonce    uint64
		nonceErr error
	)

	for i := 0; i < BroadcastTxRetry; i++ {
		nonce = client.operatorAccNonce
		txOpts, err := CreateTxOpts(ctx, client.evmClient, client.privateKeys[scope], chainId, client.gasInfo[CompleteSwapOut].GasLimit, nonce)
		if err != nil {
			log.CtxErrorw(ctx, "failed to create tx opts", "error", err)
			return "", err
		}

		session, err := CreateVirtualGroupSession(client.evmClient, *txOpts, types.VirtualGroupAddress)
		if err != nil {
			log.CtxErrorw(ctx, "failed to create session", "error", err)
			return "", err
		}

		txRsp, err := session.CompleteSwapOut(
			completeSwapOut.GetGlobalVirtualGroupFamilyId(),
			completeSwapOut.GetGlobalVirtualGroupIds(),
		)

		if err != nil {
			if strings.Contains(err.Error(), "invalid nonce") {
				// if nonce mismatches, waiting for next block, reset nonce by querying the nonce on chain
				nonce, nonceErr = client.getNonceOnChain(ctx, client.mechainClients[scope])
				if nonceErr != nil {
					log.CtxErrorw(ctx, "failed to get operator account nonce", "error", nonceErr)
					ErrCompleteSwapOutOnChain.SetError(fmt.Errorf("failed to get operator account nonce, error: %v", nonceErr))
					return "", ErrCompleteSwapOutOnChain
				}
				client.operatorAccNonce = nonce
			}

			log.CtxErrorw(ctx, "failed to broadcast complete swap out tx", "retry_number", i, "error", err)
			continue
		}
		client.operatorAccNonce = nonce + 1
		log.CtxDebugw(ctx, "succeed to broadcast complete swap out tx", "tx_hash", txHash, "seal_msg", msgCompleteSwapOut)
		return txRsp.Hash().String(), nil
	}

	ErrCompleteSwapOutOnChain.SetError(fmt.Errorf("failed to broadcast complete swap out, error: %v", err))
	return "", ErrCompleteSwapOutOnChain
}

func (client *MechainChainSignClient) SPExit(ctx context.Context, scope SignType,
	spExit *virtualgrouptypes.MsgStorageProviderExit,
) (string, error) {
	log.Infow("signer starts to sp exit", "scope", scope)
	if spExit == nil {
		log.CtxError(ctx, "sp exit msg pointer dangling")
		return "", ErrDanglingPointer
	}
	km, err := client.mechainClients[scope].GetKeyManager()
	if err != nil {
		log.CtxErrorw(ctx, "failed to get private key", "error", err)
		return "", ErrSignMsg
	}

	client.opLock.Lock()
	defer client.opLock.Unlock()

	msgSPExit := virtualgrouptypes.NewMsgStorageProviderExit(km.GetAddr())

	mode := tx.BroadcastMode_BROADCAST_MODE_SYNC
	var (
		txHash   string
		nonce    uint64
		nonceErr error
	)

	for i := 0; i < BroadcastTxRetry; i++ {
		nonce = client.operatorAccNonce
		txOpt := &ctypes.TxOption{
			Mode:      &mode,
			GasLimit:  client.gasInfo[SPExit].GasLimit,
			FeeAmount: client.gasInfo[SPExit].FeeAmount,
			Nonce:     nonce,
		}
		txHash, err = client.broadcastTx(ctx, client.mechainClients[scope], []sdk.Msg{msgSPExit}, txOpt)
		if errors.IsOf(err, sdkErrors.ErrWrongSequence) {
			// if nonce mismatches, waiting for next block, reset nonce by querying the nonce on chain
			nonce, nonceErr = client.getNonceOnChain(ctx, client.mechainClients[scope])
			if nonceErr != nil {
				log.CtxErrorw(ctx, "failed to get operator account nonce", "error", nonceErr)
				ErrSPExitOnChain.SetError(fmt.Errorf("failed to get operator account nonce, error: %v", nonceErr))
				return "", ErrSPExitOnChain
			}
			client.operatorAccNonce = nonce
		}
		if err != nil {
			log.CtxErrorw(ctx, "failed to broadcast start sp exit tx", "retry_number", i, "error", err)
			continue
		}
		client.operatorAccNonce = nonce + 1
		log.CtxDebugw(ctx, "succeed to broadcast start sp exit tx", "tx_hash", txHash, "exit_msg", msgSPExit)
		return txHash, nil
	}

	ErrSPExitOnChain.SetError(fmt.Errorf("failed to broadcast start sp exit, error: %v", err))
	return "", ErrSPExitOnChain
}

func (client *MechainChainSignClient) SPExitEvm(ctx context.Context, scope SignType,
	spExit *virtualgrouptypes.MsgStorageProviderExit,
) (string, error) {
	log.Infow("signer starts to sp exit", "scope", scope)
	if spExit == nil {
		log.CtxError(ctx, "sp exit msg pointer dangling")
		return "", ErrDanglingPointer
	}
	km, err := client.mechainClients[scope].GetKeyManager()
	if err != nil {
		log.CtxErrorw(ctx, "failed to get private key", "error", err)
		return "", ErrSignMsg
	}
	cosmosChainId, err := client.mechainClients[scope].GetChainID()
	if err != nil {
		return "", err
	}

	chainId, err := types.ParseChainID(cosmosChainId)
	if err != nil {
		return "", err
	}

	client.opLock.Lock()
	defer client.opLock.Unlock()

	msgSPExit := virtualgrouptypes.NewMsgStorageProviderExit(km.GetAddr())

	var (
		txHash   string
		nonce    uint64
		nonceErr error
	)

	for i := 0; i < BroadcastTxRetry; i++ {
		nonce = client.operatorAccNonce
		txOpts, err := CreateTxOpts(ctx, client.evmClient, client.privateKeys[scope], chainId, client.gasInfo[SPExit].GasLimit, nonce)
		if err != nil {
			log.CtxErrorw(ctx, "failed to create tx opts", "error", err)
			return "", err
		}

		session, err := CreateVirtualGroupSession(client.evmClient, *txOpts, types.VirtualGroupAddress)
		if err != nil {
			log.CtxErrorw(ctx, "failed to create session", "error", err)
			return "", err
		}

		txRsp, err := session.SpExit()

		if err != nil {
			if strings.Contains(err.Error(), "invalid nonce") {
				// if nonce mismatches, waiting for next block, reset nonce by querying the nonce on chain
				nonce, nonceErr = client.getNonceOnChain(ctx, client.mechainClients[scope])
				if nonceErr != nil {
					log.CtxErrorw(ctx, "failed to get operator account nonce", "error", nonceErr)
					ErrSPExitOnChain.SetError(fmt.Errorf("failed to get operator account nonce, error: %v", nonceErr))
					return "", ErrSPExitOnChain
				}
				client.operatorAccNonce = nonce
			}

			log.CtxErrorw(ctx, "failed to broadcast start sp exit tx", "retry_number", i, "error", err)
			continue
		}
		client.operatorAccNonce = nonce + 1
		log.CtxDebugw(ctx, "succeed to broadcast start sp exit tx", "tx_hash", txHash, "exit_msg", msgSPExit)
		return txRsp.Hash().String(), nil
	}

	ErrSPExitOnChain.SetError(fmt.Errorf("failed to broadcast start sp exit, error: %v", err))
	return "", ErrSPExitOnChain
}

func (client *MechainChainSignClient) CompleteSPExit(ctx context.Context, scope SignType,
	completeSPExit *virtualgrouptypes.MsgCompleteStorageProviderExit,
) (string, error) {
	log.Infow("signer starts to complete sp exit", "scope", scope)
	if completeSPExit == nil {
		log.CtxError(ctx, "complete sp exit msg pointer dangling")
		return "", ErrDanglingPointer
	}

	client.opLock.Lock()
	defer client.opLock.Unlock()

	msgCompleteSPExit := &virtualgrouptypes.MsgCompleteStorageProviderExit{
		StorageProvider: completeSPExit.StorageProvider,
		Operator:        completeSPExit.Operator,
	}

	mode := tx.BroadcastMode_BROADCAST_MODE_SYNC

	var (
		txHash        string
		nonce         uint64
		err, nonceErr error
	)
	for i := 0; i < BroadcastTxRetry; i++ {
		nonce = client.operatorAccNonce
		txOpt := &ctypes.TxOption{
			Mode:      &mode,
			GasLimit:  client.gasInfo[CompleteSPExit].GasLimit,
			FeeAmount: client.gasInfo[CompleteSPExit].FeeAmount,
			Nonce:     nonce,
		}
		txHash, err = client.broadcastTx(ctx, client.mechainClients[scope], []sdk.Msg{msgCompleteSPExit}, txOpt)
		if errors.IsOf(err, sdkErrors.ErrWrongSequence) {
			// if nonce mismatches, waiting for next block, reset nonce by querying the nonce on chain
			nonce, nonceErr = client.getNonceOnChain(ctx, client.mechainClients[scope])
			if nonceErr != nil {
				log.CtxErrorw(ctx, "failed to get operator account nonce", "error", nonceErr)
				ErrCompleteSPExitOnChain.SetError(fmt.Errorf("failed to get operator account nonce, error: %v", nonceErr))
				return "", ErrCompleteSPExitOnChain
			}
			client.operatorAccNonce = nonce
		}
		if err != nil {
			log.CtxErrorw(ctx, "failed to broadcast complete sp exit tx", "retry_number", i, "error", err)
			continue
		}
		client.operatorAccNonce = nonce + 1
		log.CtxDebugw(ctx, "succeed to broadcast complete sp exit tx", "tx_hash", txHash, "complete_sp_exit_msg", msgCompleteSPExit)
		return txHash, nil
	}

	ErrCompleteSPExitOnChain.SetError(fmt.Errorf("failed to broadcast complete sp exit, error: %v", err))
	return "", ErrCompleteSPExitOnChain
}

func (client *MechainChainSignClient) CompleteSPExitEvm(ctx context.Context, scope SignType,
	completeSPExit *virtualgrouptypes.MsgCompleteStorageProviderExit,
) (string, error) {
	log.Infow("signer starts to complete sp exit", "scope", scope)
	if completeSPExit == nil {
		log.CtxError(ctx, "complete sp exit msg pointer dangling")
		return "", ErrDanglingPointer
	}
	cosmosChainId, err := client.mechainClients[scope].GetChainID()
	if err != nil {
		return "", err
	}

	chainId, err := types.ParseChainID(cosmosChainId)
	if err != nil {
		return "", err
	}

	client.opLock.Lock()
	defer client.opLock.Unlock()

	msgCompleteSPExit := &virtualgrouptypes.MsgCompleteStorageProviderExit{
		StorageProvider: completeSPExit.StorageProvider,
		Operator:        completeSPExit.Operator,
	}

	var (
		txHash   string
		nonce    uint64
		nonceErr error
	)
	for i := 0; i < BroadcastTxRetry; i++ {
		nonce = client.operatorAccNonce
		txOpts, err := CreateTxOpts(ctx, client.evmClient, client.privateKeys[scope], chainId, client.gasInfo[CompleteSPExit].GasLimit, nonce)
		if err != nil {
			log.CtxErrorw(ctx, "failed to create tx opts", "error", err)
			return "", err
		}

		session, err := CreateVirtualGroupSession(client.evmClient, *txOpts, types.VirtualGroupAddress)
		if err != nil {
			log.CtxErrorw(ctx, "failed to create session", "error", err)
			return "", err
		}

		txRsp, err := session.CompleteSPExit(
			completeSPExit.StorageProvider,
			completeSPExit.Operator,
		)

		if err != nil {
			if strings.Contains(err.Error(), "invalid nonce") {
				// if nonce mismatches, waiting for next block, reset nonce by querying the nonce on chain
				nonce, nonceErr = client.getNonceOnChain(ctx, client.mechainClients[scope])
				if nonceErr != nil {
					log.CtxErrorw(ctx, "failed to get operator account nonce", "error", nonceErr)
					ErrCompleteSPExitOnChain.SetError(fmt.Errorf("failed to get operator account nonce, error: %v", nonceErr))
					return "", ErrCompleteSPExitOnChain
				}
				client.operatorAccNonce = nonce
			}

			log.CtxErrorw(ctx, "failed to broadcast complete sp exit tx", "retry_number", i, "error", err)
			continue
		}
		client.operatorAccNonce = nonce + 1
		log.CtxDebugw(ctx, "succeed to broadcast complete sp exit tx", "tx_hash", txHash, "complete_sp_exit_msg", msgCompleteSPExit)
		return txRsp.Hash().String(), nil
	}

	ErrCompleteSPExitOnChain.SetError(fmt.Errorf("failed to broadcast complete sp exit, error: %v", err))
	return "", ErrCompleteSPExitOnChain
}

func (client *MechainChainSignClient) RejectMigrateBucket(ctx context.Context, scope SignType,
	msg *storagetypes.MsgRejectMigrateBucket,
) (string, error) {
	log.Infow("signer starts to reject migrate bucket", "scope", scope)
	if msg == nil {
		log.CtxError(ctx, "reject migrate bucket msg pointer dangling")
		return "", ErrDanglingPointer
	}
	km, err := client.mechainClients[scope].GetKeyManager()
	if err != nil {
		log.CtxErrorw(ctx, "failed to get private key", "error", err)
		return "", ErrSignMsg
	}

	client.opLock.Lock()
	defer client.opLock.Unlock()

	msgRejectMigrateBucket := storagetypes.NewMsgRejectMigrateBucket(km.GetAddr(), msg.GetBucketName())

	var (
		txHash   string
		nonce    uint64
		nonceErr error
	)
	for i := 0; i < BroadcastTxRetry; i++ {
		nonce = client.operatorAccNonce
		txOpt := &ctypes.TxOption{
			Nonce: nonce,
		}
		txHash, err = client.broadcastTx(ctx, client.mechainClients[scope], []sdk.Msg{msgRejectMigrateBucket}, txOpt)
		if errors.IsOf(err, sdkErrors.ErrWrongSequence) {
			// if nonce mismatches, waiting for next block, reset nonce by querying the nonce on chain
			nonce, nonceErr = client.getNonceOnChain(ctx, client.mechainClients[scope])
			if nonceErr != nil {
				log.CtxErrorw(ctx, "failed to get operator account nonce", "error", err)
				ErrRejectMigrateBucketOnChain.SetError(fmt.Errorf("failed to get operator account nonce, error: %v", err))
				return "", ErrRejectMigrateBucketOnChain
			}
			client.operatorAccNonce = nonce
		}
		if err != nil {
			log.CtxErrorw(ctx, "failed to broadcast reject migrate bucket tx", "retry_number", i, "error", err)
			continue
		}
		client.operatorAccNonce = nonce + 1
		log.CtxDebugw(ctx, "succeed to broadcast reject migrate bucket tx", "tx_hash", txHash, "reject_migrate_bucket_msg", msgRejectMigrateBucket)
		return txHash, nil
	}

	// failed to broadcast tx
	ErrRejectMigrateBucketOnChain.SetError(fmt.Errorf("failed to broadcast reject migrate bucket, error: %v", err))
	return "", ErrRejectMigrateBucketOnChain
}

func (client *MechainChainSignClient) RejectMigrateBucketEvm(ctx context.Context, scope SignType,
	msg *storagetypes.MsgRejectMigrateBucket,
) (string, error) {
	log.Infow("signer starts to reject migrate bucket", "scope", scope)
	if msg == nil {
		log.CtxError(ctx, "reject migrate bucket msg pointer dangling")
		return "", ErrDanglingPointer
	}
	km, err := client.mechainClients[scope].GetKeyManager()
	if err != nil {
		log.CtxErrorw(ctx, "failed to get private key", "error", err)
		return "", ErrSignMsg
	}
	cosmosChainId, err := client.mechainClients[scope].GetChainID()
	if err != nil {
		return "", err
	}

	chainId, err := types.ParseChainID(cosmosChainId)
	if err != nil {
		return "", err
	}

	client.opLock.Lock()
	defer client.opLock.Unlock()

	msgRejectMigrateBucket := storagetypes.NewMsgRejectMigrateBucket(km.GetAddr(), msg.GetBucketName())

	var (
		txHash   string
		nonce    uint64
		nonceErr error
	)
	for i := 0; i < BroadcastTxRetry; i++ {
		nonce = client.operatorAccNonce
		txOpts, err := CreateTxOpts(ctx, client.evmClient, client.privateKeys[scope], chainId, client.gasInfo[RejectMigrateBucket].GasLimit, nonce)
		if err != nil {
			log.CtxErrorw(ctx, "failed to create tx opts", "error", err)
			return "", err
		}

		session, err := CreateStorageSession(client.evmClient, *txOpts, types.StorageAddress)
		if err != nil {
			log.CtxErrorw(ctx, "failed to create session", "error", err)
			return "", err
		}

		txRsp, err := session.RejectMigrateBucket(
			msg.GetBucketName(),
		)

		if err != nil {
			if strings.Contains(err.Error(), "invalid nonce") {
				// if nonce mismatches, waiting for next block, reset nonce by querying the nonce on chain
				nonce, nonceErr = client.getNonceOnChain(ctx, client.mechainClients[scope])
				if nonceErr != nil {
					log.CtxErrorw(ctx, "failed to get operator account nonce", "error", err)
					ErrRejectMigrateBucketOnChain.SetError(fmt.Errorf("failed to get operator account nonce, error: %v", err))
					return "", ErrRejectMigrateBucketOnChain
				}
				client.operatorAccNonce = nonce
			}

			log.CtxErrorw(ctx, "failed to broadcast reject migrate bucket tx", "retry_number", i, "error", err)
			continue
		}
		client.operatorAccNonce = nonce + 1
		log.CtxDebugw(ctx, "succeed to broadcast reject migrate bucket tx", "tx_hash", txHash, "reject_migrate_bucket_msg", msgRejectMigrateBucket)
		return txRsp.Hash().String(), nil
	}

	// failed to broadcast tx
	ErrRejectMigrateBucketOnChain.SetError(fmt.Errorf("failed to broadcast reject migrate bucket, error: %v", err))
	return "", ErrRejectMigrateBucketOnChain
}

func (client *MechainChainSignClient) Deposit(ctx context.Context, scope SignType,
	msg *virtualgrouptypes.MsgDeposit,
) (string, error) {
	log.Infow("signer starts to make deposit into GVG", "scope", scope)
	if msg == nil {
		log.CtxError(ctx, "deposit msg pointer dangling")
		return "", ErrDanglingPointer
	}
	km, err := client.mechainClients[scope].GetKeyManager()
	if err != nil {
		log.CtxErrorw(ctx, "failed to get private key", "error", err)
		return "", ErrSignMsg
	}

	client.opLock.Lock()
	defer client.opLock.Unlock()

	msgDeposit := virtualgrouptypes.NewMsgDeposit(km.GetAddr(), msg.GlobalVirtualGroupId, msg.Deposit)

	var (
		txHash   string
		nonce    uint64
		nonceErr error
	)
	for i := 0; i < BroadcastTxRetry; i++ {
		nonce = client.operatorAccNonce
		txOpt := &ctypes.TxOption{
			Nonce: nonce,
		}
		txHash, err = client.broadcastTx(ctx, client.mechainClients[scope], []sdk.Msg{msgDeposit}, txOpt)
		if errors.IsOf(err, sdkErrors.ErrWrongSequence) {
			// if nonce mismatches, waiting for next block, reset nonce by querying the nonce on chain
			nonce, nonceErr = client.getNonceOnChain(ctx, client.mechainClients[scope])
			if nonceErr != nil {
				log.CtxErrorw(ctx, "failed to get operator account nonce", "error", err)
				ErrDepositOnChain.SetError(fmt.Errorf("failed to get operator account nonce, error: %v", err))
				return "", ErrDepositOnChain
			}
			client.operatorAccNonce = nonce
		}
		if err != nil {
			log.CtxErrorw(ctx, "failed to broadcast deposit tx", "retry_number", i, "error", err)
			continue
		}
		client.operatorAccNonce = nonce + 1
		log.CtxDebugw(ctx, "succeed to broadcast deposit tx", "tx_hash", txHash, "deposit_msg", msgDeposit)
		return txHash, nil
	}

	// failed to broadcast tx
	ErrDepositOnChain.SetError(fmt.Errorf("failed to broadcast deposit, error: %v", err))
	return "", ErrDepositOnChain
}

func (client *MechainChainSignClient) DepositEvm(ctx context.Context, scope SignType,
	msg *virtualgrouptypes.MsgDeposit,
) (string, error) {
	log.Infow("signer starts to make deposit into GVG", "scope", scope)
	if msg == nil {
		log.CtxError(ctx, "deposit msg pointer dangling")
		return "", ErrDanglingPointer
	}
	km, err := client.mechainClients[scope].GetKeyManager()
	if err != nil {
		log.CtxErrorw(ctx, "failed to get private key", "error", err)
		return "", ErrSignMsg
	}
	cosmosChainId, err := client.mechainClients[scope].GetChainID()
	if err != nil {
		return "", err
	}

	chainId, err := types.ParseChainID(cosmosChainId)
	if err != nil {
		return "", err
	}

	client.opLock.Lock()
	defer client.opLock.Unlock()

	msgDeposit := virtualgrouptypes.NewMsgDeposit(km.GetAddr(), msg.GlobalVirtualGroupId, msg.Deposit)

	var (
		txHash   string
		nonce    uint64
		nonceErr error
	)
	for i := 0; i < BroadcastTxRetry; i++ {
		nonce = client.operatorAccNonce
		txOpts, err := CreateTxOpts(ctx, client.evmClient, client.privateKeys[scope], chainId, client.gasInfo[Deposit].GasLimit, nonce)
		if err != nil {
			log.CtxErrorw(ctx, "failed to create tx opts", "error", err)
			return "", err
		}

		session, err := CreateVirtualGroupSession(client.evmClient, *txOpts, types.VirtualGroupAddress)
		if err != nil {
			log.CtxErrorw(ctx, "failed to create session", "error", err)
			return "", err
		}

		deposit := virtualgroup.Coin{
			Denom:  msg.Deposit.Denom,
			Amount: msg.Deposit.Amount.BigInt(),
		}
		txRsp, err := session.Deposit(
			msg.GlobalVirtualGroupId,
			deposit,
		)

		if err != nil {
			if strings.Contains(err.Error(), "invalid nonce") {
				// if nonce mismatches, waiting for next block, reset nonce by querying the nonce on chain
				nonce, nonceErr = client.getNonceOnChain(ctx, client.mechainClients[scope])
				if nonceErr != nil {
					log.CtxErrorw(ctx, "failed to get operator account nonce", "error", err)
					ErrDepositOnChain.SetError(fmt.Errorf("failed to get operator account nonce, error: %v", err))
					return "", ErrDepositOnChain
				}
				client.operatorAccNonce = nonce
			}

			log.CtxErrorw(ctx, "failed to broadcast deposit tx", "retry_number", i, "error", err)
			continue
		}
		client.operatorAccNonce = nonce + 1
		log.CtxDebugw(ctx, "succeed to broadcast deposit tx", "tx_hash", txHash, "deposit_msg", msgDeposit)
		return txRsp.Hash().String(), nil
	}

	// failed to broadcast tx
	ErrDepositOnChain.SetError(fmt.Errorf("failed to broadcast deposit, error: %v", err))
	return "", ErrDepositOnChain
}

func (client *MechainChainSignClient) DeleteGlobalVirtualGroup(ctx context.Context, scope SignType,
	msg *virtualgrouptypes.MsgDeleteGlobalVirtualGroup,
) (string, error) {
	log.Infow("signer starts to delete GVG", "scope", scope)
	if msg == nil {
		log.CtxError(ctx, "delete GVG msg pointer dangling")
		return "", ErrDanglingPointer
	}
	km, err := client.mechainClients[scope].GetKeyManager()
	if err != nil {
		log.CtxErrorw(ctx, "failed to get private key", "error", err)
		return "", ErrSignMsg
	}

	client.opLock.Lock()
	defer client.opLock.Unlock()

	msgDeleteGlobalVirtualGroup := virtualgrouptypes.NewMsgDeleteGlobalVirtualGroup(km.GetAddr(), msg.GetGlobalVirtualGroupId())

	var (
		txHash   string
		nonce    uint64
		nonceErr error
	)
	for i := 0; i < BroadcastTxRetry; i++ {
		nonce = client.operatorAccNonce
		txOpt := &ctypes.TxOption{
			Nonce: nonce,
		}
		txHash, err = client.broadcastTx(ctx, client.mechainClients[scope], []sdk.Msg{msgDeleteGlobalVirtualGroup}, txOpt)
		if errors.IsOf(err, sdkErrors.ErrWrongSequence) {
			// if nonce mismatches, waiting for next block, reset nonce by querying the nonce on chain
			nonce, nonceErr = client.getNonceOnChain(ctx, client.mechainClients[scope])
			if nonceErr != nil {
				log.CtxErrorw(ctx, "failed to get operator account nonce", "error", err)
				ErrDeleteGVGOnChain.SetError(fmt.Errorf("failed to get operator account nonce, error: %v", err))
				return "", ErrDeleteGVGOnChain
			}
			client.operatorAccNonce = nonce
		}
		if err != nil {
			log.CtxErrorw(ctx, "failed to broadcast delete GVG tx", "retry_number", i, "error", err)
			continue
		}
		client.operatorAccNonce = nonce + 1
		log.CtxDebugw(ctx, "succeed to broadcast delete GVG tx", "tx_hash", txHash, "reject_migrate_bucket_msg", msgDeleteGlobalVirtualGroup)
		return txHash, nil
	}

	// failed to broadcast tx
	ErrDeleteGVGOnChain.SetError(fmt.Errorf("failed to broadcast delete GVG, error: %v", err))
	return "", ErrDeleteGVGOnChain
}

func (client *MechainChainSignClient) DeleteGlobalVirtualGroupEvm(ctx context.Context, scope SignType,
	msg *virtualgrouptypes.MsgDeleteGlobalVirtualGroup,
) (string, error) {
	log.Infow("signer starts to delete GVG", "scope", scope)
	if msg == nil {
		log.CtxError(ctx, "delete GVG msg pointer dangling")
		return "", ErrDanglingPointer
	}
	km, err := client.mechainClients[scope].GetKeyManager()
	if err != nil {
		log.CtxErrorw(ctx, "failed to get private key", "error", err)
		return "", ErrSignMsg
	}
	cosmosChainId, err := client.mechainClients[scope].GetChainID()
	if err != nil {
		return "", err
	}

	chainId, err := types.ParseChainID(cosmosChainId)
	if err != nil {
		return "", err
	}

	client.opLock.Lock()
	defer client.opLock.Unlock()

	msgDeleteGlobalVirtualGroup := virtualgrouptypes.NewMsgDeleteGlobalVirtualGroup(km.GetAddr(), msg.GetGlobalVirtualGroupId())

	var (
		txHash   string
		nonce    uint64
		nonceErr error
	)
	for i := 0; i < BroadcastTxRetry; i++ {
		nonce = client.operatorAccNonce
		txOpts, err := CreateTxOpts(ctx, client.evmClient, client.privateKeys[scope], chainId, client.gasInfo[DeleteGlobalVirtualGroup].GasLimit, nonce)
		if err != nil {
			log.CtxErrorw(ctx, "failed to create tx opts", "error", err)
			return "", err
		}

		session, err := CreateVirtualGroupSession(client.evmClient, *txOpts, types.VirtualGroupAddress)
		if err != nil {
			log.CtxErrorw(ctx, "failed to create session", "error", err)
			return "", err
		}

		txRsp, err := session.DeleteGlobalVirtualGroup(
			msg.GetGlobalVirtualGroupId(),
		)

		if err != nil {
			if strings.Contains(err.Error(), "invalid nonce") {
				// if nonce mismatches, waiting for next block, reset nonce by querying the nonce on chain
				nonce, nonceErr = client.getNonceOnChain(ctx, client.mechainClients[scope])
				if nonceErr != nil {
					log.CtxErrorw(ctx, "failed to get operator account nonce", "error", err)
					ErrDeleteGVGOnChain.SetError(fmt.Errorf("failed to get operator account nonce, error: %v", err))
					return "", ErrDeleteGVGOnChain
				}
				client.operatorAccNonce = nonce
			}

			log.CtxErrorw(ctx, "failed to broadcast delete GVG tx", "retry_number", i, "error", err)
			continue
		}
		client.operatorAccNonce = nonce + 1
		log.CtxDebugw(ctx, "succeed to broadcast delete GVG tx", "tx_hash", txHash, "reject_migrate_bucket_msg", msgDeleteGlobalVirtualGroup)
		return txRsp.Hash().String(), nil
	}

	// failed to broadcast tx
	ErrDeleteGVGOnChain.SetError(fmt.Errorf("failed to broadcast delete GVG, error: %v", err))
	return "", ErrDeleteGVGOnChain
}

func (client *MechainChainSignClient) DelegateCreateObject(ctx context.Context, scope SignType,
	msg *storagetypes.MsgDelegateCreateObject,
) (string, error) {
	if msg == nil {
		log.CtxError(ctx, "delegate create object msg pointer dangling")
		return "", ErrDanglingPointer
	}
	km, err := client.mechainClients[scope].GetKeyManager()
	if err != nil {
		log.CtxErrorw(ctx, "failed to get private key", "error", err)
		return "", ErrSignMsg
	}
	client.opLock.Lock()
	defer client.opLock.Unlock()

	msg.Operator = km.GetAddr().String()

	var (
		txHash   string
		nonce    uint64
		nonceErr error
	)
	for i := 0; i < BroadcastTxRetry; i++ {
		nonce = client.operatorAccNonce
		txOpt := &ctypes.TxOption{
			Nonce: nonce,
		}
		txHash, err = client.broadcastTx(ctx, client.mechainClients[scope], []sdk.Msg{msg}, txOpt)
		if errors.IsOf(err, sdkErrors.ErrWrongSequence) {
			// if nonce mismatches, waiting for next block, reset nonce by querying the nonce on chain
			nonce, nonceErr = client.getNonceOnChain(ctx, client.mechainClients[scope])
			if nonceErr != nil {
				log.CtxErrorw(ctx, "failed to get operator account nonce", "error", err)
				ErrDelegateCreateObjectOnChain.SetError(fmt.Errorf("failed to get operator account nonce, error: %v", err))
				return "", ErrDelegateCreateObjectOnChain
			}
			client.operatorAccNonce = nonce
		}
		if err != nil {
			log.CtxErrorw(ctx, "failed to broadcast delegate create object tx", "retry_number", i, "error", err)
			continue
		}
		client.operatorAccNonce = nonce + 1
		log.CtxDebugw(ctx, "succeed to broadcast delegate create object tx", "tx_hash", txHash, "delegate_update_object_msg", msg)
		return txHash, nil
	}

	// failed to broadcast tx
	ErrDelegateCreateObjectOnChain.SetError(fmt.Errorf("failed to delegate create object, error: %v", err))
	return "", ErrDelegateCreateObjectOnChain
}

func (client *MechainChainSignClient) DelegateCreateObjectEvm(ctx context.Context, scope SignType,
	msg *storagetypes.MsgDelegateCreateObject,
) (string, error) {
	if msg == nil {
		log.CtxError(ctx, "delegate create object msg pointer dangling")
		return "", ErrDanglingPointer
	}
	km, err := client.mechainClients[scope].GetKeyManager()
	if err != nil {
		log.CtxErrorw(ctx, "failed to get private key", "error", err)
		return "", ErrSignMsg
	}
	cosmosChainId, err := client.mechainClients[scope].GetChainID()
	if err != nil {
		return "", err
	}

	chainId, err := types.ParseChainID(cosmosChainId)
	if err != nil {
		return "", err
	}

	client.opLock.Lock()
	defer client.opLock.Unlock()

	msg.Operator = km.GetAddr().String()

	var (
		txHash   string
		nonce    uint64
		nonceErr error
	)
	for i := 0; i < BroadcastTxRetry; i++ {
		nonce = client.operatorAccNonce
		txOpts, err := CreateTxOpts(ctx, client.evmClient, client.privateKeys[scope], chainId, client.gasInfo[DelegateCreateObject].GasLimit, nonce)
		if err != nil {
			log.CtxErrorw(ctx, "failed to create tx opts", "error", err)
			return "", err
		}

		session, err := CreateStorageSession(client.evmClient, *txOpts, types.StorageAddress)
		if err != nil {
			log.CtxErrorw(ctx, "failed to create session", "error", err)
			return "", err
		}

		expectChecksums := make([]string, 0)
		for _, checksum := range msg.ExpectChecksums {
			checksumStr := base64.StdEncoding.EncodeToString(checksum)
			expectChecksums = append(expectChecksums, checksumStr)
		}
		txRsp, err := session.DelegateCreateObject(
			msg.Creator,
			msg.BucketName,
			msg.ObjectName,
			msg.PayloadSize,
			msg.ContentType,
			uint8(msg.Visibility),
			expectChecksums,
			uint8(msg.RedundancyType),
		)

		if err != nil {
			if strings.Contains(err.Error(), "invalid nonce") {
				// if nonce mismatches, waiting for next block, reset nonce by querying the nonce on chain
				nonce, nonceErr = client.getNonceOnChain(ctx, client.mechainClients[scope])
				if nonceErr != nil {
					log.CtxErrorw(ctx, "failed to get operator account nonce", "error", err)
					ErrDelegateCreateObjectOnChain.SetError(fmt.Errorf("failed to get operator account nonce, error: %v", err))
					return "", ErrDelegateCreateObjectOnChain
				}
				client.operatorAccNonce = nonce
			}

			log.CtxErrorw(ctx, "failed to broadcast delegate create object tx", "retry_number", i, "error", err)
			continue
		}
		client.operatorAccNonce = nonce + 1
		log.CtxDebugw(ctx, "succeed to broadcast delegate create object tx", "tx_hash", txHash, "delegate_update_object_msg", msg)
		return txRsp.Hash().String(), nil
	}

	// failed to broadcast tx
	ErrDelegateCreateObjectOnChain.SetError(fmt.Errorf("failed to delegate create object, error: %v", err))
	return "", ErrDelegateCreateObjectOnChain
}

func (client *MechainChainSignClient) DelegateUpdateObjectContent(ctx context.Context, scope SignType,
	msg *storagetypes.MsgDelegateUpdateObjectContent,
) (string, error) {
	if msg == nil {
		log.CtxError(ctx, "delegate update object content msg pointer dangling")
		return "", ErrDanglingPointer
	}
	km, err := client.mechainClients[scope].GetKeyManager()
	if err != nil {
		log.CtxErrorw(ctx, "failed to get private key", "error", err)
		return "", ErrSignMsg
	}
	client.opLock.Lock()
	defer client.opLock.Unlock()

	msg.Operator = km.GetAddr().String()

	var (
		txHash   string
		nonce    uint64
		nonceErr error
	)
	for i := 0; i < BroadcastTxRetry; i++ {
		nonce = client.operatorAccNonce
		txOpt := &ctypes.TxOption{
			Nonce: nonce,
		}
		txHash, err = client.broadcastTx(ctx, client.mechainClients[scope], []sdk.Msg{msg}, txOpt)
		if errors.IsOf(err, sdkErrors.ErrWrongSequence) {
			// if nonce mismatches, waiting for next block, reset nonce by querying the nonce on chain
			nonce, nonceErr = client.getNonceOnChain(ctx, client.mechainClients[scope])
			if nonceErr != nil {
				log.CtxErrorw(ctx, "failed to get operator account nonce", "error", err)
				ErrDelegateUpdateObjectContentOnChain.SetError(fmt.Errorf("failed to get operator account nonce, error: %v", err))
				return "", ErrDelegateUpdateObjectContentOnChain
			}
			client.operatorAccNonce = nonce
		}
		if err != nil {
			log.CtxErrorw(ctx, "failed to broadcast delegate update object content tx", "retry_number", i, "error", err)
			continue
		}
		client.operatorAccNonce = nonce + 1
		log.CtxDebugw(ctx, "succeed to broadcast delegate update object content tx", "tx_hash", txHash, "delegate_update_object_content_msg", msg)
		return txHash, nil
	}

	// failed to broadcast tx
	ErrDelegateUpdateObjectContentOnChain.SetError(fmt.Errorf("failed to broadcast delegte update object, error: %v", err))
	return "", ErrDelegateUpdateObjectContentOnChain
}

func (client *MechainChainSignClient) DelegateUpdateObjectContentEvm(ctx context.Context, scope SignType,
	msg *storagetypes.MsgDelegateUpdateObjectContent,
) (string, error) {
	if msg == nil {
		log.CtxError(ctx, "delegate update object content msg pointer dangling")
		return "", ErrDanglingPointer
	}
	km, err := client.mechainClients[scope].GetKeyManager()
	if err != nil {
		log.CtxErrorw(ctx, "failed to get private key", "error", err)
		return "", ErrSignMsg
	}
	cosmosChainId, err := client.mechainClients[scope].GetChainID()
	if err != nil {
		return "", err
	}

	chainId, err := types.ParseChainID(cosmosChainId)
	if err != nil {
		return "", err
	}

	client.opLock.Lock()
	defer client.opLock.Unlock()

	msg.Operator = km.GetAddr().String()

	var (
		txHash   string
		nonce    uint64
		nonceErr error
	)
	for i := 0; i < BroadcastTxRetry; i++ {
		nonce = client.operatorAccNonce
		txOpts, err := CreateTxOpts(ctx, client.evmClient, client.privateKeys[scope], chainId, client.gasInfo[DelegateUpdateObjectContent].GasLimit, nonce)
		if err != nil {
			log.CtxErrorw(ctx, "failed to create tx opts", "error", err)
			return "", err
		}

		session, err := CreateStorageSession(client.evmClient, *txOpts, types.StorageAddress)
		if err != nil {
			log.CtxErrorw(ctx, "failed to create session", "error", err)
			return "", err
		}

		expectChecksums := make([]string, 0)
		for _, checksum := range msg.ExpectChecksums {
			checksumStr := base64.StdEncoding.EncodeToString(checksum)
			expectChecksums = append(expectChecksums, checksumStr)
		}
		txRsp, err := session.DelegateUpdateObjectContent(
			msg.Updater,
			msg.BucketName,
			msg.ObjectName,
			msg.PayloadSize,
			msg.ContentType,
			expectChecksums,
		)

		if err != nil {
			if strings.Contains(err.Error(), "invalid nonce") {
				// if nonce mismatches, waiting for next block, reset nonce by querying the nonce on chain
				nonce, nonceErr = client.getNonceOnChain(ctx, client.mechainClients[scope])
				if nonceErr != nil {
					log.CtxErrorw(ctx, "failed to get operator account nonce", "error", err)
					ErrDelegateUpdateObjectContentOnChain.SetError(fmt.Errorf("failed to get operator account nonce, error: %v", err))
					return "", ErrDelegateUpdateObjectContentOnChain
				}
				client.operatorAccNonce = nonce
			}

			log.CtxErrorw(ctx, "failed to broadcast delegate update object content tx", "retry_number", i, "error", err)
			continue
		}
		client.operatorAccNonce = nonce + 1
		log.CtxDebugw(ctx, "succeed to broadcast delegate update object content tx", "tx_hash", txHash, "delegate_update_object_content_msg", msg)
		return txRsp.Hash().String(), nil
	}

	// failed to broadcast tx
	ErrDelegateUpdateObjectContentOnChain.SetError(fmt.Errorf("failed to broadcast delegte update object, error: %v", err))
	return "", ErrDelegateUpdateObjectContentOnChain
}

func (client *MechainChainSignClient) getNonceOnChain(ctx context.Context, gnfdClient *client.MechainClient) (uint64, error) {
	err := client.signer.baseApp.Consensus().WaitForNextBlock(ctx)
	if err != nil {
		log.CtxErrorw(ctx, "failed to wait next block", "error", err)
		return 0, err
	}
	nonce, err := gnfdClient.GetNonce(ctx)
	if err != nil {
		log.CtxErrorw(ctx, "failed to get nonce on chain", "error", err)
		return 0, err
	}
	return nonce, nil
}

func (client *MechainChainSignClient) broadcastTx(ctx context.Context, gnfdClient *client.MechainClient,
	msgs []sdk.Msg, txOpt *ctypes.TxOption, opts ...grpc.CallOption,
) (string, error) {
	resp, err := gnfdClient.BroadcastTx(ctx, msgs, txOpt, opts...)
	if err != nil {
		if strings.Contains(err.Error(), "account sequence mismatch") {
			return "", sdkErrors.ErrWrongSequence
		}
		return "", errors.Wrap(err, "failed to broadcast tx with mechain client")
	}
	if resp.TxResponse.Code == sdkErrors.ErrWrongSequence.ABCICode() {
		return "", sdkErrors.ErrWrongSequence
	}
	if resp.TxResponse.Code != 0 {
		return "", fmt.Errorf("failed to broadcast tx, resp code: %d, code space: %s", resp.TxResponse.Code, resp.TxResponse.Codespace)
	}
	return resp.TxResponse.TxHash, nil
}

func (client *MechainChainSignClient) ReserveSwapIn(ctx context.Context, scope SignType,
	msg *virtualgrouptypes.MsgReserveSwapIn,
) (string, error) {
	log.Infow("signer starts to reserve swap in", "scope", scope)
	if msg == nil {
		log.CtxError(ctx, "reserve swap in msg pointer dangling")
		return "", ErrDanglingPointer
	}
	km, err := client.mechainClients[scope].GetKeyManager()
	if err != nil {
		log.CtxErrorw(ctx, "failed to get private key", "error", err)
		return "", ErrSignMsg
	}

	client.opLock.Lock()
	defer client.opLock.Unlock()

	msgReserveSwapIn := virtualgrouptypes.NewMsgReserveSwapIn(km.GetAddr(), msg.GetTargetSpId(), msg.GetGlobalVirtualGroupFamilyId(), msg.GetGlobalVirtualGroupId())

	var (
		txHash   string
		nonce    uint64
		nonceErr error
	)
	for i := 0; i < BroadcastTxRetry; i++ {
		nonce = client.operatorAccNonce
		txOpt := &ctypes.TxOption{
			Nonce: nonce,
		}
		txHash, err = client.broadcastTx(ctx, client.mechainClients[scope], []sdk.Msg{msgReserveSwapIn}, txOpt)
		if errors.IsOf(err, sdkErrors.ErrWrongSequence) {
			// if nonce mismatches, waiting for next block, reset nonce by querying the nonce on chain
			nonce, nonceErr = client.getNonceOnChain(ctx, client.mechainClients[scope])
			if nonceErr != nil {
				log.CtxErrorw(ctx, "failed to get operator account nonce", "error", err)
				ErrReserveSwapIn.SetError(fmt.Errorf("failed to get operator account nonce, error: %v", err))
				return "", ErrReserveSwapIn
			}
			client.operatorAccNonce = nonce
		}
		if err != nil {
			log.CtxErrorw(ctx, "failed to broadcast reserve swap in tx", "retry_number", i, "error", err)
			continue
		}
		client.operatorAccNonce = nonce + 1
		log.CtxDebugw(ctx, "succeed to broadcast reserve swap in tx", "tx_hash", txHash, "reserve_swap_in_msg", msgReserveSwapIn)
		return txHash, nil
	}

	// failed to broadcast tx
	ErrReserveSwapIn.SetError(fmt.Errorf("failed to broadcast reserve swap in, error: %v", err))
	return "", ErrReserveSwapIn
}

func (client *MechainChainSignClient) ReserveSwapInEvm(ctx context.Context, scope SignType,
	msg *virtualgrouptypes.MsgReserveSwapIn,
) (string, error) {
	log.Infow("signer starts to reserve swap in", "scope", scope)
	if msg == nil {
		log.CtxError(ctx, "reserve swap in msg pointer dangling")
		return "", ErrDanglingPointer
	}
	km, err := client.mechainClients[scope].GetKeyManager()
	if err != nil {
		log.CtxErrorw(ctx, "failed to get private key", "error", err)
		return "", ErrSignMsg
	}
	cosmosChainId, err := client.mechainClients[scope].GetChainID()
	if err != nil {
		return "", err
	}

	chainId, err := types.ParseChainID(cosmosChainId)
	if err != nil {
		return "", err
	}

	client.opLock.Lock()
	defer client.opLock.Unlock()

	msgReserveSwapIn := virtualgrouptypes.NewMsgReserveSwapIn(km.GetAddr(), msg.GetTargetSpId(), msg.GetGlobalVirtualGroupFamilyId(), msg.GetGlobalVirtualGroupId())

	var (
		txHash   string
		nonce    uint64
		nonceErr error
	)
	for i := 0; i < BroadcastTxRetry; i++ {
		nonce = client.operatorAccNonce
		txOpts, err := CreateTxOpts(ctx, client.evmClient, client.privateKeys[scope], chainId, client.gasInfo[ReserveSwapIn].GasLimit, nonce)
		if err != nil {
			log.CtxErrorw(ctx, "failed to create tx opts", "error", err)
			return "", err
		}

		session, err := CreateVirtualGroupSession(client.evmClient, *txOpts, types.VirtualGroupAddress)
		if err != nil {
			log.CtxErrorw(ctx, "failed to create session", "error", err)
			return "", err
		}

		txRsp, err := session.ReserveSwapIn(
			msg.GetTargetSpId(),
			msg.GetGlobalVirtualGroupFamilyId(),
			msg.GetGlobalVirtualGroupId(),
		)

		if err != nil {
			if strings.Contains(err.Error(), "invalid nonce") {
				// if nonce mismatches, waiting for next block, reset nonce by querying the nonce on chain
				nonce, nonceErr = client.getNonceOnChain(ctx, client.mechainClients[scope])
				if nonceErr != nil {
					log.CtxErrorw(ctx, "failed to get operator account nonce", "error", err)
					ErrReserveSwapIn.SetError(fmt.Errorf("failed to get operator account nonce, error: %v", err))
					return "", ErrReserveSwapIn
				}
				client.operatorAccNonce = nonce
			}

			log.CtxErrorw(ctx, "failed to broadcast reserve swap in tx", "retry_number", i, "error", err)
			continue
		}
		client.operatorAccNonce = nonce + 1
		log.CtxDebugw(ctx, "succeed to broadcast reserve swap in tx", "tx_hash", txHash, "reserve_swap_in_msg", msgReserveSwapIn)
		return txRsp.Hash().String(), nil
	}

	// failed to broadcast tx
	ErrReserveSwapIn.SetError(fmt.Errorf("failed to broadcast reserve swap in, error: %v", err))
	return "", ErrReserveSwapIn
}

func (client *MechainChainSignClient) CompleteSwapIn(ctx context.Context, scope SignType,
	msg *virtualgrouptypes.MsgCompleteSwapIn,
) (string, error) {
	log.Infow("signer starts to complete swap in", "scope", scope)
	if msg == nil {
		log.CtxError(ctx, "complete swap in msg pointer dangling")
		return "", ErrDanglingPointer
	}
	km, err := client.mechainClients[scope].GetKeyManager()
	if err != nil {
		log.CtxErrorw(ctx, "failed to get private key", "error", err)
		return "", ErrSignMsg
	}

	client.opLock.Lock()
	defer client.opLock.Unlock()

	msgCompleteSwapIn := virtualgrouptypes.NewMsgCompleteSwapIn(km.GetAddr(), msg.GetGlobalVirtualGroupFamilyId(), msg.GetGlobalVirtualGroupId())

	var (
		txHash   string
		nonce    uint64
		nonceErr error
	)
	for i := 0; i < BroadcastTxRetry; i++ {
		nonce = client.operatorAccNonce
		txOpt := &ctypes.TxOption{
			Nonce: nonce,
		}
		txHash, err = client.broadcastTx(ctx, client.mechainClients[scope], []sdk.Msg{msgCompleteSwapIn}, txOpt)
		if errors.IsOf(err, sdkErrors.ErrWrongSequence) {
			// if nonce mismatches, waiting for next block, reset nonce by querying the nonce on chain
			nonce, nonceErr = client.getNonceOnChain(ctx, client.mechainClients[scope])
			if nonceErr != nil {
				log.CtxErrorw(ctx, "failed to get operator account nonce", "error", err)
				ErrCompleteSwapIn.SetError(fmt.Errorf("failed to get operator account nonce, error: %v", err))
				return "", ErrCompleteSwapIn
			}
			client.operatorAccNonce = nonce
		}
		if err != nil {
			log.CtxErrorw(ctx, "failed to broadcast complete swap in tx", "retry_number", i, "error", err)
			continue
		}
		client.operatorAccNonce = nonce + 1
		log.CtxDebugw(ctx, "succeed to broadcast complete swap in tx", "tx_hash", txHash, "complete_swap_in_msg", msgCompleteSwapIn)
		return txHash, nil
	}

	// failed to broadcast tx
	ErrCompleteSwapIn.SetError(fmt.Errorf("failed to broadcast rcomplete swap in, error: %v", err))
	return "", ErrCompleteSwapIn
}

func (client *MechainChainSignClient) CompleteSwapInEvm(ctx context.Context, scope SignType,
	msg *virtualgrouptypes.MsgCompleteSwapIn,
) (string, error) {
	log.Infow("signer starts to complete swap in", "scope", scope)
	if msg == nil {
		log.CtxError(ctx, "complete swap in msg pointer dangling")
		return "", ErrDanglingPointer
	}
	km, err := client.mechainClients[scope].GetKeyManager()
	if err != nil {
		log.CtxErrorw(ctx, "failed to get private key", "error", err)
		return "", ErrSignMsg
	}
	cosmosChainId, err := client.mechainClients[scope].GetChainID()
	if err != nil {
		return "", err
	}

	chainId, err := types.ParseChainID(cosmosChainId)
	if err != nil {
		return "", err
	}

	client.opLock.Lock()
	defer client.opLock.Unlock()

	msgCompleteSwapIn := virtualgrouptypes.NewMsgCompleteSwapIn(km.GetAddr(), msg.GetGlobalVirtualGroupFamilyId(), msg.GetGlobalVirtualGroupId())

	var (
		txHash   string
		nonce    uint64
		nonceErr error
	)
	for i := 0; i < BroadcastTxRetry; i++ {
		nonce = client.operatorAccNonce
		txOpts, err := CreateTxOpts(ctx, client.evmClient, client.privateKeys[scope], chainId, client.gasInfo[CompleteSwapIn].GasLimit, nonce)
		if err != nil {
			log.CtxErrorw(ctx, "failed to create tx opts", "error", err)
			return "", err
		}

		session, err := CreateVirtualGroupSession(client.evmClient, *txOpts, types.VirtualGroupAddress)
		if err != nil {
			log.CtxErrorw(ctx, "failed to create session", "error", err)
			return "", err
		}

		txRsp, err := session.CompleteSwapIn(
			msg.GetGlobalVirtualGroupFamilyId(),
			msg.GetGlobalVirtualGroupId(),
		)

		if err != nil {
			if strings.Contains(err.Error(), "invalid nonce") {
				// if nonce mismatches, waiting for next block, reset nonce by querying the nonce on chain
				nonce, nonceErr = client.getNonceOnChain(ctx, client.mechainClients[scope])
				if nonceErr != nil {
					log.CtxErrorw(ctx, "failed to get operator account nonce", "error", err)
					ErrCompleteSwapIn.SetError(fmt.Errorf("failed to get operator account nonce, error: %v", err))
					return "", ErrCompleteSwapIn
				}
				client.operatorAccNonce = nonce
			}

			log.CtxErrorw(ctx, "failed to broadcast complete swap in tx", "retry_number", i, "error", err)
			continue
		}
		client.operatorAccNonce = nonce + 1
		log.CtxDebugw(ctx, "succeed to broadcast complete swap in tx", "tx_hash", txHash, "complete_swap_in_msg", msgCompleteSwapIn)
		return txRsp.Hash().String(), nil
	}

	// failed to broadcast tx
	ErrCompleteSwapIn.SetError(fmt.Errorf("failed to broadcast rcomplete swap in, error: %v", err))
	return "", ErrCompleteSwapIn
}

func (client *MechainChainSignClient) CancelSwapIn(ctx context.Context, scope SignType,
	msg *virtualgrouptypes.MsgCancelSwapIn,
) (string, error) {
	log.Infow("signer starts to cancel swap in", "scope", scope)
	if msg == nil {
		log.CtxError(ctx, "cancel swap in msg pointer dangling")
		return "", ErrDanglingPointer
	}
	km, err := client.mechainClients[scope].GetKeyManager()
	if err != nil {
		log.CtxErrorw(ctx, "failed to get private key", "error", err)
		return "", ErrSignMsg
	}

	client.opLock.Lock()
	defer client.opLock.Unlock()

	msgCancelSwapIn := virtualgrouptypes.NewMsgCancelSwapIn(km.GetAddr(), msg.GetGlobalVirtualGroupFamilyId(), msg.GetGlobalVirtualGroupId())

	var (
		txHash   string
		nonce    uint64
		nonceErr error
	)
	for i := 0; i < BroadcastTxRetry; i++ {
		nonce = client.operatorAccNonce
		txOpt := &ctypes.TxOption{
			Nonce: nonce,
		}
		txHash, err = client.broadcastTx(ctx, client.mechainClients[scope], []sdk.Msg{msgCancelSwapIn}, txOpt)
		if errors.IsOf(err, sdkErrors.ErrWrongSequence) {
			// if nonce mismatches, waiting for next block, reset nonce by querying the nonce on chain
			nonce, nonceErr = client.getNonceOnChain(ctx, client.mechainClients[scope])
			if nonceErr != nil {
				log.CtxErrorw(ctx, "failed to get operator account nonce", "error", err)
				ErrCompleteSwapIn.SetError(fmt.Errorf("failed to get operator account nonce, error: %v", err))
				return "", ErrCompleteSwapIn
			}
			client.operatorAccNonce = nonce
		}
		if err != nil {
			log.CtxErrorw(ctx, "failed to broadcast cancel swap in tx", "retry_number", i, "error", err)
			continue
		}
		client.operatorAccNonce = nonce + 1
		log.CtxDebugw(ctx, "succeed to broadcast cancel swap in tx", "tx_hash", txHash, "cancel_swap_in_msg", msgCancelSwapIn)
		return txHash, nil
	}

	// failed to broadcast tx
	ErrCancelSwapIn.SetError(fmt.Errorf("failed to broadcast cancel swap in, error: %v", err))
	return "", ErrCancelSwapIn
}

func (client *MechainChainSignClient) CancelSwapInEvm(ctx context.Context, scope SignType,
	msg *virtualgrouptypes.MsgCancelSwapIn,
) (string, error) {
	log.Infow("signer starts to cancel swap in", "scope", scope)
	if msg == nil {
		log.CtxError(ctx, "cancel swap in msg pointer dangling")
		return "", ErrDanglingPointer
	}
	km, err := client.mechainClients[scope].GetKeyManager()
	if err != nil {
		log.CtxErrorw(ctx, "failed to get private key", "error", err)
		return "", ErrSignMsg
	}
	cosmosChainId, err := client.mechainClients[scope].GetChainID()
	if err != nil {
		return "", err
	}

	chainId, err := types.ParseChainID(cosmosChainId)
	if err != nil {
		return "", err
	}

	client.opLock.Lock()
	defer client.opLock.Unlock()

	msgCancelSwapIn := virtualgrouptypes.NewMsgCancelSwapIn(km.GetAddr(), msg.GetGlobalVirtualGroupFamilyId(), msg.GetGlobalVirtualGroupId())

	var (
		txHash   string
		nonce    uint64
		nonceErr error
	)
	for i := 0; i < BroadcastTxRetry; i++ {
		nonce = client.operatorAccNonce
		txOpts, err := CreateTxOpts(ctx, client.evmClient, client.privateKeys[scope], chainId, client.gasInfo[CancelSwapIn].GasLimit, nonce)
		if err != nil {
			log.CtxErrorw(ctx, "failed to create tx opts", "error", err)
			return "", err
		}

		session, err := CreateVirtualGroupSession(client.evmClient, *txOpts, types.VirtualGroupAddress)
		if err != nil {
			log.CtxErrorw(ctx, "failed to create session", "error", err)
			return "", err
		}

		txRsp, err := session.CancelSwapIn(
			msg.GetGlobalVirtualGroupFamilyId(),
			msg.GetGlobalVirtualGroupId(),
		)

		if err != nil {
			if strings.Contains(err.Error(), "invalid nonce") {
				// if nonce mismatches, waiting for next block, reset nonce by querying the nonce on chain
				nonce, nonceErr = client.getNonceOnChain(ctx, client.mechainClients[scope])
				if nonceErr != nil {
					log.CtxErrorw(ctx, "failed to get operator account nonce", "error", err)
					ErrCompleteSwapIn.SetError(fmt.Errorf("failed to get operator account nonce, error: %v", err))
					return "", ErrCompleteSwapIn
				}
				client.operatorAccNonce = nonce
			}

			log.CtxErrorw(ctx, "failed to broadcast cancel swap in tx", "retry_number", i, "error", err)
			continue
		}
		client.operatorAccNonce = nonce + 1
		log.CtxDebugw(ctx, "succeed to broadcast cancel swap in tx", "tx_hash", txHash, "cancel_swap_in_msg", msgCancelSwapIn)
		return txRsp.Hash().String(), nil
	}

	// failed to broadcast tx
	ErrCancelSwapIn.SetError(fmt.Errorf("failed to broadcast cancel swap in, error: %v", err))
	return "", ErrCancelSwapIn
}

// SealObjectV2 seal the object on the mechain chain.
func (client *MechainChainSignClient) SealObjectV2(ctx context.Context, scope SignType,
	sealObject *storagetypes.MsgSealObjectV2,
) (string, error) {
	if sealObject == nil {
		log.CtxError(ctx, "failed to seal object due to pointer dangling")
		return "", ErrDanglingPointer
	}
	ctx = log.WithValue(ctx, log.CtxKeyBucketName, sealObject.GetBucketName())
	ctx = log.WithValue(ctx, log.CtxKeyObjectName, sealObject.GetObjectName())
	km, err := client.mechainClients[scope].GetKeyManager()
	if err != nil {
		log.CtxErrorw(ctx, "failed to get private key", "error", err)
		return "", ErrSignMsg
	}

	client.sealLock.Lock()
	defer client.sealLock.Unlock()

	msgSealObject := storagetypes.NewMsgSealObjectV2(km.GetAddr(),
		sealObject.GetBucketName(), sealObject.GetObjectName(), sealObject.GetGlobalVirtualGroupId(),
		sealObject.GetSecondarySpBlsAggSignatures(), sealObject.GetExpectChecksums())

	mode := tx.BroadcastMode_BROADCAST_MODE_SYNC

	var (
		txHash   string
		nonce    uint64
		nonceErr error
	)
	for i := 0; i < BroadcastTxRetry; i++ {
		nonce = client.sealAccNonce
		txOpt := &ctypes.TxOption{
			NoSimulate: false,
			Mode:       &mode,
			GasLimit:   client.gasInfo[Seal].GasLimit,
			FeeAmount:  client.gasInfo[Seal].FeeAmount,
			Nonce:      nonce,
		}

		txHash, err = client.broadcastTx(ctx, client.mechainClients[scope], []sdk.Msg{msgSealObject}, txOpt)
		if errors.IsOf(err, sdkErrors.ErrWrongSequence) {
			// if nonce mismatch, wait for next block, reset nonce by querying the nonce on chain
			nonce, nonceErr = client.getNonceOnChain(ctx, client.mechainClients[scope])
			if nonceErr != nil {
				log.CtxErrorw(ctx, "failed to get seal account nonce", "error", nonceErr)
				ErrSealObjectOnChain.SetError(fmt.Errorf("failed to get seal account nonce, error: %v", nonceErr))
				return "", ErrSealObjectOnChain
			}
			client.sealAccNonce = nonce
		}

		if err != nil {
			log.CtxErrorw(ctx, "failed to broadcast seal object tx", "retry_number", i, "error", err)
			continue
		}
		client.sealAccNonce = nonce + 1
		log.CtxDebugw(ctx, "succeed to broadcast seal object tx", "tx_hash", txHash, "seal_msg", msgSealObject)
		return txHash, nil
	}

	// failed to broadcast tx
	ErrSealObjectOnChain.SetError(fmt.Errorf("failed to broadcast seal object tx, error: %v", err))
	return "", ErrSealObjectOnChain
}

func (client *MechainChainSignClient) SealObjectV2Evm(ctx context.Context, scope SignType,
	sealObject *storagetypes.MsgSealObjectV2,
) (string, error) {
	if sealObject == nil {
		log.CtxError(ctx, "failed to seal object due to pointer dangling")
		return "", ErrDanglingPointer
	}
	ctx = log.WithValue(ctx, log.CtxKeyBucketName, sealObject.GetBucketName())
	ctx = log.WithValue(ctx, log.CtxKeyObjectName, sealObject.GetObjectName())
	km, err := client.mechainClients[scope].GetKeyManager()
	if err != nil {
		log.CtxErrorw(ctx, "failed to get private key", "error", err)
		return "", ErrSignMsg
	}
	cosmosChainId, err := client.mechainClients[scope].GetChainID()
	if err != nil {
		return "", err
	}

	chainId, err := types.ParseChainID(cosmosChainId)
	if err != nil {
		return "", err
	}

	client.sealLock.Lock()
	defer client.sealLock.Unlock()

	msgSealObject := storagetypes.NewMsgSealObjectV2(km.GetAddr(),
		sealObject.GetBucketName(), sealObject.GetObjectName(), sealObject.GetGlobalVirtualGroupId(),
		sealObject.GetSecondarySpBlsAggSignatures(), sealObject.GetExpectChecksums())

	var (
		txHash   string
		nonce    uint64
		nonceErr error
	)
	for i := 0; i < BroadcastTxRetry; i++ {
		nonce = client.sealAccNonce
		txOpts, err := CreateTxOpts(ctx, client.evmClient, client.privateKeys[SignSeal], chainId, client.gasInfo[Seal].GasLimit, nonce)
		if err != nil {
			log.CtxErrorw(ctx, "failed to create tx opts", "error", err)
			return "", err
		}

		session, err := CreateStorageSession(client.evmClient, *txOpts, types.StorageAddress)
		if err != nil {
			log.CtxErrorw(ctx, "failed to create session", "error", err)
			return "", err
		}
		expectChecksums := make([]string, 0)
		for _, checksum := range msgSealObject.ExpectChecksums {
			checksumStr := base64.StdEncoding.EncodeToString(checksum)
			expectChecksums = append(expectChecksums, checksumStr)
		}

		txRsp, err := session.SealObjectV2(
			ethcmn.BytesToAddress(km.GetAddr().Bytes()),
			sealObject.GetBucketName(),
			sealObject.GetObjectName(),
			sealObject.GetGlobalVirtualGroupId(),
			base64.StdEncoding.EncodeToString(sealObject.GetSecondarySpBlsAggSignatures()),
			expectChecksums,
		)

		if err != nil {
			if strings.Contains(err.Error(), "invalid nonce") {
				// if nonce mismatch, wait for next block, reset nonce by querying the nonce on chain
				nonce, nonceErr = client.getNonceOnChain(ctx, client.mechainClients[scope])
				if nonceErr != nil {
					log.CtxErrorw(ctx, "failed to get seal account nonce", "error", nonceErr)
					ErrSealObjectOnChain.SetError(fmt.Errorf("failed to get seal account nonce, error: %v", nonceErr))
					return "", ErrSealObjectOnChain
				}
				client.sealAccNonce = nonce
			}

			log.CtxErrorw(ctx, "failed to broadcast seal object tx", "retry_number", i, "error", err)
			continue
		}
		client.sealAccNonce = nonce + 1
		log.CtxDebugw(ctx, "succeed to broadcast seal object tx", "tx_hash", txHash, "seal_msg", msgSealObject)
		return txRsp.Hash().String(), nil
	}

	// failed to broadcast tx
	ErrSealObjectOnChain.SetError(fmt.Errorf("failed to broadcast seal object tx, error: %v", err))
	return "", ErrSealObjectOnChain
}
