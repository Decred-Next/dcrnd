module github.com/Decred-Next/dcrnd/blockchain/v3

go 1.11

require (
	github.com/decred/dcrd/blockchain/stake/v2 v2.0.2
	github.com/decred/dcrd/blockchain/standalone v1.1.0
	github.com/decred/dcrd/chaincfg/chainhash v1.0.2
	github.com/decred/dcrd/chaincfg/v2 v2.3.0
	github.com/decred/dcrd/database/v2 v2.0.1
	github.com/decred/dcrd/dcrec v1.0.0
	github.com/decred/dcrd/dcrec/secp256k1/v2 v2.0.0
	github.com/decred/dcrd/dcrutil/v2 v2.0.1
	github.com/decred/dcrd/gcs/v2 v2.0.0
	github.com/decred/dcrd/txscript/v2 v2.1.0
	github.com/decred/dcrd/wire v1.3.0
	github.com/decred/slog v1.0.0
)

replace (
	github.com/decred/dcrd/blockchain/standalone v1.1.0 => ./standalone
	github.com/decred/dcrd/chaincfg/v2 v2.3.0 => ../chaincfg
)
