#!/usr/bin/env bash
SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)

source "${SCRIPT_DIR}/env.info"

MECHAIND_CMD=$(realpath "${SCRIPT_DIR}/../../../mechain/build/mechaind")

SP_CMD="mechain-sp"

function generate() {
    home=$1
    SP_INFO="$home/sp.info"
    echo "add keys..."
    $MECHAIND_CMD keys add sp --keyring-backend test --home "$home" >"$home"/info 2>&1
    $MECHAIND_CMD keys add sp_fund --keyring-backend test --home "$home" >"$home"/fund_info 2>&1
    $MECHAIND_CMD keys add sp_seal --keyring-backend test --home "$home" >"$home"/seal_info 2>&1
    $MECHAIND_CMD keys add sp_bls --keyring-backend test --home "$home" --algo eth_bls >"$home"/bls_info 2>&1
    $MECHAIND_CMD keys add sp_approval --keyring-backend test --home "$home" >"$home"/approval_info 2>&1
    $MECHAIND_CMD keys add sp_gc --keyring-backend test --home "$home" >"$home"/gc_info 2>&1
    $MECHAIND_CMD keys add sp_maintenance --keyring-backend test --home "$home" >"$home"/maintenance_info 2>&1

    echo "show keys..."
    op_address=("$($MECHAIND_CMD keys show sp -a --keyring-backend test --home "$home")")
    fund_addr=("$($MECHAIND_CMD keys show sp_fund -a --keyring-backend test --home "$home")")
    seal_addr=("$($MECHAIND_CMD keys show sp_seal -a --keyring-backend test --home "$home")")
    approval_addr=("$($MECHAIND_CMD keys show sp_approval -a --keyring-backend test --home "$home")")
    gc_addr=("$($MECHAIND_CMD keys show sp_gc -a --keyring-backend test --home "$home")")
    maintenance_addr=("$($MECHAIND_CMD keys show sp_maintenance -a --keyring-backend test --home "$home")")

    operator_priv_key=("$(echo "y" | $MECHAIND_CMD keys export sp --unarmored-hex --unsafe --keyring-backend test --home "$home")")
    fund_priv_key=("$(echo "y" | $MECHAIND_CMD keys export sp_fund --unarmored-hex --unsafe --keyring-backend test --home "$home")")
    seal_priv_key=("$(echo "y" | $MECHAIND_CMD keys export sp_seal --unarmored-hex --unsafe --keyring-backend test --home "$home")")
    approval_priv_key=("$(echo "y" | $MECHAIND_CMD keys export sp_approval --unarmored-hex --unsafe --keyring-backend test --home "$home")")
    gc_priv_key=("$(echo "y" | $MECHAIND_CMD keys export sp_gc --unarmored-hex --unsafe --keyring-backend test --home "$home")")
    maintenance_priv_key=("$(echo "y" | $MECHAIND_CMD keys export sp_maintenance --unarmored-hex --unsafe --keyring-backend test --home "$home")")
    bls_pub_key=("$($MECHAIND_CMD keys show sp_bls --keyring-backend test --home "$home" --output json | jq -r .pubkey_hex)")
    bls_priv_key=("$(echo "y" | $MECHAIND_CMD keys export sp_bls --unarmored-hex --unsafe --keyring-backend test --home "$home")")

    echo "generated validator proposal file..."

    echo "OPERATOR_ADDRESS=\"${op_address}\"" >>"$SP_INFO"
    echo "OPERATOR_PRIVATE_KEY=\"${operator_priv_key}\"" >>"$SP_INFO"
    echo "FUNDING_PRIVATE_KEY=\"${fund_priv_key}\"" >>"$SP_INFO"
    echo "SEAL_PRIVATE_KEY=\"${seal_priv_key}\"" >>"$SP_INFO"
    echo "APPROVAL_PRIVATE_KEY=\"${approval_priv_key}\"" >>"$SP_INFO"
    echo "GC_PRIVATE_KEY=\"${gc_priv_key}\"" >>"$SP_INFO"
    echo "BLS_PRIVATE_KEY=\"${bls_priv_key}\"" >>"$SP_INFO"

    if [ $? -eq 0 ]; then
        echo "create_validator_proposal.json has been generated successfully at $OUTPUT_FILE."
    else
        echo "Error: Failed to create create_validator_proposal.json."
    fi
    echo op_address: "$op_address"
    # echo DELEGATOR_ADDR: "$DELEGATOR_ADDR"

    echo "send tokens..."
    echo "mechaind tx bank send validator0 $op_address 10000000000000000000000000azkme --home /app/validator0 --keyring-backend test --node http://localhost:26657 -y --fees 6000000azkme"
    # echo "mechaind tx bank send validator0 $DELEGATOR_ADDR 10000000000000000000000000azkme --home /app/validator0 --keyring-backend test --node http://localhost:26657 -y --fees 6000000azkme"
}

function make_config() {
    echo "make config.toml..."
    home=$1
    SP_INFO="$home/sp.info"
    source "$SP_INFO"
    # app
    sed -i -e "s/GRPCAddress = '.*'/GRPCAddress = '0.0.0.0:${SP_START_PORT}'/g" "$home/config.toml"

    # db
    sed -i -e "s/User = '.*'/User = '${DB_USER}'/g" "$home/config.toml"
    sed -i -e "s/Passwd = '.*'/Passwd = '${DB_PWD}'/g" "$home/config.toml"
    sed -i -e "s/^Address = '.*'/Address = '${DB_ADDRESS}'/g" "$home/config.toml"
    sed -i -e "s/Database = '.*'/Database = '${DB_DATABASE}'/g" "$home/config.toml"

    # chain
    sed -i -e "s/ChainID = '.*'/ChainID = '${CHAIN_ID}'/g" "$home/config.toml"
    sed -i -e "s/ChainAddress = \[.*\]/ChainAddress = \['http:\/\/${CHAIN_HTTP_ENDPOINT}'\]/g" "$home/config.toml"
    sed -i -e "s/RpcAddress = \[.*\]/RpcAddress = \['http:\/\/${CHAIN_EVM_ENDPOINT}'\]/g" "$home/config.toml"

    # sp account
    sed -i -e "s/SpOperatorAddress = '.*'/SpOperatorAddress = '${OPERATOR_ADDRESS}'/g" "$home/config.toml"
    sed -i -e "s/OperatorPrivateKey = '.*'/OperatorPrivateKey = '${OPERATOR_PRIVATE_KEY}'/g" "$home/config.toml"
    sed -i -e "s/FundingPrivateKey = '.*'/FundingPrivateKey = '${FUNDING_PRIVATE_KEY}'/g" "$home/config.toml"
    sed -i -e "s/SealPrivateKey = '.*'/SealPrivateKey = '${SEAL_PRIVATE_KEY}'/g" "$home/config.toml"
    sed -i -e "s/ApprovalPrivateKey = '.*'/ApprovalPrivateKey = '${APPROVAL_PRIVATE_KEY}'/g" "$home/config.toml"
    sed -i -e "s/GcPrivateKey = '.*'/GcPrivateKey = '${GC_PRIVATE_KEY}'/g" "$home/config.toml"
    sed -i -e "s/BlsPrivateKey = '.*'/BlsPrivateKey = '${BLS_PRIVATE_KEY}'/g" "$home/config.toml"

    # gateway
    sed -i -e "s/DomainName = '.*'/DomainName = 'gnfd.test-sp.com'/g" "$home/config.toml"
    sed -i -e "s/^HTTPAddress = '.*'/HTTPAddress = '0.0.0.0:${SP_START_ENDPOINT_PORT}'/g" "$home/config.toml"

    # metadata
    sed -i -e "s/IsMasterDB = .*/IsMasterDB = true/g" "$home/config.toml"
    sed -i -e "s/BsDBSwitchCheckIntervalSec = .*/BsDBSwitchCheckIntervalSec = 30/g" "$home/config.toml"

    # p2p
    sed -i -e "s/P2PAddress = '.*'/P2PAddress = '0.0.0.0:9633'/g" "$home/config.toml"
    sed -i -e "s/Bootstrap = \[\]/Bootstrap = \[\'16Uiu2HAmG4KTyFsK71BVwjY4z6WwcNBVb6vAiuuL9ASWdqiTzNZH@0.0.0.0:9633\'\]/g" "$home/config.toml"

    sed -i -e "s/MaxExecuteNumber = .*/MaxExecuteNumber = 1/g" "$home/config.toml"

    # metrics and pprof
    #sed -i -e "s/DisableMetrics = false/DisableMetrics = true/" $home/config.toml
    #sed -i -e "s/DisablePProf = false/DisablePProf = true/" $home/config.toml
    #sed -i -e "s/DisableProbe = false/DisableProbe = true/" $home/config.toml
    metrics_address="0.0.0.0:"$((SP_START_PORT + 367))
    sed -i -e "s/MetricsHTTPAddress = '.*'/MetricsHTTPAddress = '${metrics_address}'/g" "$home/config.toml"
    pprof_address="0.0.0.0:"$((SP_START_PORT + 368))
    sed -i -e "s/PProfHTTPAddress = '.*'/PProfHTTPAddress = '${pprof_address}'/g" "$home/config.toml"
    probe_address="0.0.0.0:"$((SP_START_PORT + 369))
    sed -i -e "s/ProbeHTTPAddress = '.*'/ProbeHTTPAddress = '${probe_address}'/g" "$home/config.toml"

    # blocksyncer
    sed -i -e "s/Modules = \[\]/Modules = \[\'epoch\',\'bucket\',\'object\',\'payment\',\'group\',\'permission\',\'storage_provider\'\,\'prefix_tree\'\,\'virtual_group\'\,\'sp_exit_events\'\,\'object_id_map\'\,\'general\'\]/g" "$home/config.toml"
    WORKERS=10
    sed -i -e "s/Workers = 0/Workers = ${WORKERS}/g" "$home/config.toml"
    sed -i -e "s/BsDBWriteAddress = '.*'/BsDBWriteAddress = '${ADDRESS}'/g" "$home/config.toml"

    # manager
    sed -i -e "s/SubscribeSPExitEventIntervalMillisecond = .*/SubscribeSPExitEventIntervalMillisecond = 100/g" "$home/config.toml"
    sed -i -e "s/SubscribeSwapOutExitEventIntervalMillisecond = .*/SubscribeSwapOutExitEventIntervalMillisecond = 100/g" "$home/config.toml"
    sed -i -e "s/SubscribeBucketMigrateEventIntervalMillisecond = .*/SubscribeBucketMigrateEventIntervalMillisecond = 20/g" "$home/config.toml"
    sed -i -e "s/GVGPreferSPList = \[\]/GVGPreferSPList = \[1,2,3,4,5,6,7,8\]/g" "$home/config.toml"
    sed -i -e "s/EnableGCZombie = .*/EnableGCZombie = true/g" "$home/config.toml"
    sed -i -e "s/EnableGCMeta = .*/EnableGCMeta = true/g" "$home/config.toml"
    sed -i -e "s/GCMetaTimeInterval = .*/GCMetaTimeInterval = 3/g" "$home/config.toml"
    sed -i -e "s/GCZombiePieceTimeInterval = .*/GCZombiePieceTimeInterval = 3/g" "$home/config.toml"
    sed -i -e "s/GCZombieSafeObjectIDDistance = .*/GCZombieSafeObjectIDDistance = 1/g" "$home/config.toml"
    sed -i -e "s/GCZombiePieceObjectIDInterval = .*/GCZombiePieceObjectIDInterval = 5/g" "$home/config.toml"
    sed -i -e "s/EnableTaskRetryScheduler = .*/EnableTaskRetryScheduler = true/g" "$home/config.toml"
    sed -i -e "s/RejectUnsealThresholdSecond = .*/RejectUnsealThresholdSecond = 600/g" "$home/config.toml"
    sed -i -e "s/EnableHealthyChecker = .*/EnableHealthyChecker = true/g" "$home/config.toml"
    sed -i -e "s/EnableGCStaleVersionObject = .*/EnableGCStaleVersionObject = true/g" "$home/config.toml"
    sed -i -e "s/EnableGCExpiredOffChainAuthKeys = .*/EnableGCExpiredOffChainAuthKeys = true/g" "$home/config.toml"
    sed -i -e "s/GCExpiredOffChainAuthKeysTimeInterval = .*/GCExpiredOffChainAuthKeysTimeInterval = 86400/g" "$home/config.toml"
    sed -i -e "s/GasLimit = 0/GasLimit = 180000/g" "$home/config.toml"
    sed -i -e "s/FeeAmount = 0/FeeAmount = 12000000/g" "$home/config.toml"

    echo "succeed to generate $home/config.toml in ""${home}"
}

function start() {
    echo "start chain..."
    home=$1
    cd "$home" || exit
    SP_CMD start --config config.toml >"$home"/node.log 2>&1 &
    cd || exit -
}

function test() {
    echo "test chain..."
    curl http://localhost:26657/status | jq
}

function clean() {
    echo "clean chain..."
    home=$1
    rm -rf "$home"/*
}

CMD=$1
home=$2

case ${CMD} in
generate)
    echo "===== init ===="
    generate "$home"
    echo "===== end ===="
    ;;
config)
    echo "===== make_config ===="
    make_config "$home"
    echo "===== end ===="
    ;;
start)
    echo "===== start ===="
    start "$home"
    echo "===== end ===="
    ;;
test)
    echo "===== test ===="
    test
    echo "===== end ===="
    ;;
clean)
    echo "===== clean ===="
    clean "$home"
    echo "===== end ===="
    ;;
all)
    echo "===== all ===="

    echo "===== end ===="
    ;;
*)
    echo "Usage: $0 init | start | test | config"
    ;;
esac
