//module github.com/decred/dcrd

module github.com/Decred-Next/dcrnd

go 1.11

require (
	github.com/btcsuite/winsvc v1.0.0
	github.com/decred/base58 v1.0.1
	github.com/decred/dcrd v1.3.0
	github.com/decred/dcrd/addrmgr v1.1.0
	github.com/decred/dcrd/blockchain/stake/v2 v2.0.2
	github.com/decred/dcrd/blockchain/standalone v1.1.0
	github.com/decred/dcrd/blockchain/v2 v2.1.0
	github.com/decred/dcrd/certgen v1.1.0
	github.com/decred/dcrd/chaincfg/chainhash v1.0.2
	github.com/decred/dcrd/chaincfg/v2 v2.3.0
	github.com/decred/dcrd/connmgr/v2 v2.1.0
	github.com/decred/dcrd/crypto/ripemd160 v1.0.0
	github.com/decred/dcrd/database/v2 v2.0.1
	github.com/decred/dcrd/dcrec v1.0.0
	github.com/decred/dcrd/dcrec/secp256k1/v2 v2.0.0
	github.com/decred/dcrd/dcrjson/v3 v3.0.1
	github.com/decred/dcrd/dcrutil/v2 v2.0.1
	github.com/decred/dcrd/fees/v2 v2.0.0
	github.com/decred/dcrd/gcs/v2 v2.0.1
	github.com/decred/dcrd/hdkeychain/v2 v2.1.0
	github.com/decred/dcrd/lru v1.0.0
	github.com/decred/dcrd/mempool/v3 v3.1.0
	github.com/decred/dcrd/mining/v2 v2.0.1
	github.com/decred/dcrd/peer/v2 v2.1.0
	github.com/decred/dcrd/rpc/jsonrpc/types/v2 v2.0.0
	github.com/decred/dcrd/rpcclient/v5 v5.0.0
	github.com/decred/dcrd/txscript/v2 v2.1.0
	github.com/decred/dcrd/wire v1.3.0
	github.com/decred/dcrwallet/rpc/jsonrpc/types v1.4.0
	github.com/decred/go-socks v1.1.0
	github.com/decred/slog v1.0.0
	github.com/gorilla/websocket v1.4.1
	github.com/jessevdk/go-flags v1.4.0
	github.com/jrick/bitset v1.0.0
	github.com/jrick/logrotate v1.0.0
	github.com/kr/pretty v0.3.0 // indirect
	golang.org/x/crypto v0.0.0-20190611184440-5c40567a22f8
	golang.org/x/sync v0.0.0-20181221193216-37e7f081c4d4
)

replace (
	github.com/decred/dcrd/addrmgr v1.1.0 => ./addrmgr
	github.com/decred/dcrd/blockchain/stake/v2 v2.0.2 => ./blockchain/stake
	github.com/decred/dcrd/blockchain/standalone v1.1.0 => ./blockchain/standalone
	github.com/decred/dcrd/blockchain/v2 v2.1.0 => ./blockchain
	github.com/decred/dcrd/chaincfg/v2 v2.3.0 => ./chaincfg
	github.com/decred/dcrd/connmgr/v2 v2.1.0 => ./connmgr
	github.com/decred/dcrd/mining/v2 v2.0.1 => ./mining
	github.com/decred/dcrd/peer/v2 v2.1.0 => ./peer
	github.com/decred/dcrd/wire v1.3.0 => ./wire
)
