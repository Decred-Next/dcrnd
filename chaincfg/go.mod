module github.com/Decred-Next/dcrnd/chaincfg/v3

go 1.16

require (
	github.com/davecgh/go-spew v1.1.1
	github.com/decred/dcrd/chaincfg/chainhash v1.0.2
	github.com/decred/dcrd/dcrec/edwards/v2 v2.0.0
	github.com/decred/dcrd/dcrec/secp256k1/v2 v2.0.0
	github.com/decred/dcrd/wire v1.3.0
)
replace (
	github.com/decred/dcrd/wire v1.3.0 => ../wire
)
