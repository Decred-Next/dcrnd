// Copyright (c) 2013-2017 The btcsuite developers
// Copyright (c) 2015-2020 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package txscript

import (
	"fmt"
	"math/big"
	"strings"

	secp256k1 "github.com/Decred-Next/dcrnd/dcrec/secp256k1/version31/v8"
	"github.com/Decred-Next/dcrnd/wire/v8"
)

// ScriptFlags is a bitmask defining additional operations or tests that will be
// done when executing a script pair.
type ScriptFlags uint32

const (
	// ScriptDiscourageUpgradableNops defines whether to verify that
	// currently unused opcodes in the NOP and UNKNOWN families are reserved
	// for future upgrades.  This flag must not be used for consensus
	// critical code nor applied to blocks as this flag is only for stricter
	// standard transaction checks.  This flag is only applied when the
	// above opcodes are executed.
	ScriptDiscourageUpgradableNops ScriptFlags = 1 << iota

	// ScriptVerifyCheckLockTimeVerify defines whether to verify that
	// a transaction output is spendable based on the locktime.
	// This is BIP0065.
	ScriptVerifyCheckLockTimeVerify

	// ScriptVerifyCheckSequenceVerify defines whether to allow execution
	// pathways of a script to be restricted based on the age of the output
	// being spent.  This is BIP0112.
	ScriptVerifyCheckSequenceVerify

	// ScriptVerifyCleanStack defines that the stack must contain only
	// one stack element after evaluation and that the element must be
	// true if interpreted as a boolean.  This is rule 6 of BIP0062.
	// This flag should never be used without the ScriptBip16 flag.
	ScriptVerifyCleanStack

	// ScriptVerifySigPushOnly defines that signature scripts must contain
	// only pushed data.  This is rule 2 of BIP0062.
	ScriptVerifySigPushOnly

	// ScriptVerifySHA256 defines whether to treat opcode 192 (previously
	// OP_UNKNOWN192) as the OP_SHA256 opcode which consumes the top item of
	// the data stack and replaces it with the sha256 of it.
	ScriptVerifySHA256
)

const (
	// MaxStackSize is the maximum combined height of stack and alt stack
	// during execution.
	MaxStackSize = 1024

	// MaxScriptSize is the maximum allowed length of a raw script.
	MaxScriptSize = 16384

	// noCondDisableDepth is the nesting depth which indicates that no
	// conditional opcodes have been encountered that cause the current
	// execution state to be disabled.
	noCondDisableDepth = -1
)

// halforder is used to tame ECDSA malleability (see BIP0062).
var halfOrder = new(big.Int).Rsh(secp256k1.S256().N, 1)

// Engine is the virtual machine that executes scripts.
type Engine struct {
	// The following fields are set when the engine is created and must not be
	// changed afterwards.  The entries of the signature cache are mutated
	// during execution, however, the cache pointer itself is not changed.
	//
	// flags specifies the additional flags which modify the execution behavior
	// of the engine.
	//
	// tx identifies the transaction that contains the input which in turn
	// contains the signature script being executed.
	//
	// txIdx identifies the input index within the transaction that contains
	// the signature script being executed.
	//
	// version specifies the version of the public key script to execute.  Since
	// signature scripts redeem public keys scripts, this means the same version
	// also extends to signature scripts and redeem scripts in the case of
	// pay-to-script-hash.
	//
	// isP2SH specifies that the public key script is of a special form that
	// indicates it is a pay-to-script-hash and therefore the execution must be
	// treated as such.
	//
	// sigCache caches the results of signature verifications.  This is useful
	// since transaction scripts are often executed more than once from various
	// contexts (e.g. new block templates, when transactions are first seen
	// prior to being mined, part of full block verification, etc).
	flags    ScriptFlags
	tx       wire.MsgTx
	txIdx    int
	version  uint16
	isP2SH   bool
	sigCache *SigCache

	// The following fields handle keeping track of the current execution state
	// of the engine.
	//
	// scripts houses the raw scripts that are executed by the engine.  This
	// includes the signature script as well as the public key script.  It also
	// includes the redeem script in the case of pay-to-script-hash.
	//
	// scriptIdx tracks the index into the scripts array for the current program
	// counter.
	//
	// opcodeIdx tracks the number of the opcode within the current script for
	// the current program counter.  Note that it differs from the actual byte
	// index into the script and is really only used for disassembly purposes.
	//
	// lastCodeSep specifies the position within the current script of the last
	// OP_CODESEPARATOR.
	//
	// tokenizer provides the token stream of the current script being executed
	// and doubles as state tracking for the program counter within the script.
	//
	// savedFirstStack keeps a copy of the stack from the first script when
	// performing pay-to-script-hash execution.
	//
	// dstack is the primary data stack the various opcodes push and pop data
	// to and from during execution.
	//
	// astack is the alternate data stack the various opcodes push and pop data
	// to and from during execution.
	//
	// numOps tracks the total number of non-push operations in a script and is
	// primarily used to enforce maximum limits.
	scripts         [][]byte
	scriptIdx       int
	opcodeIdx       int
	lastCodeSep     int
	tokenizer       ScriptTokenizer
	savedFirstStack [][]byte
	dstack          stack
	astack          stack
	numOps          int

	// The following fields keep track of the current conditional execution
	// state of the engine with support for multiple nested conditional
	// execution opcodes.
	//
	// Each time a conditional opcode is encountered the conditional nesting
	// depth is incremented.  This is the case even in an unexecuted branch so
	// proper nesting is maintained.  On the other hand, when a conditional
	// branch is terminated, the nesting depth is decremented.
	//
	// Whenever one of the aforementioned conditional opcodes that indicates
	// branch execution needs to be disabled is encountered, execution of any
	// opcodes in that branch, and any nested conditional branches, is disabled
	// until the disabled conditional branch is terminated.
	//
	// In other words, only the current nesting depth and the nesting depth that
	// caused branch execution to be disabled needs to be tracked and execution
	// becomes enabled again once the nesting depth is reduced to that depth.
	//
	// For example, consider the following script and nesting depth diagram:
	//
	//  TRUE IF FALSE IF <opcodes> TRUE IF <opcodes> ENDIF ENDIF ENDIF <opcodes>
	//  |      |        |                 |               |     |     |        |
	//  |      |        |                  ----depth 3----      |     |        |
	//  |      |         ----------depth 2----------------------      |        |
	//  |       -------------------depth 1----------------------------         |
	//   --------------------------depth 0-------------------------------------
	//
	// The first IF is TRUE, so branch execution is unchanged and the current
	// nesting depth is increased from 0 to 1.  The second IF is FALSE, so
	// branch execution is disabled at nesting depth 1 and the current nesting
	// depth is increased from 1 to 2.  Branch execution is already disabled for
	// the third IF, so its value has no effect, but the current nesting depth
	// is increased from 2 to 3.  The first ENDIF reduces the current nesting
	// depth from 3 to 2.  The second ENDIF reduces the current nesting depth
	// from 2 to 1 and since the branch execution was disabled at depth 1,
	// branch execution is enabled again.  The third ENDIF reduces the nesting
	// depth from 1 to 0.
	//
	// condNestDepth is the current conditional execution nesting depth.
	//
	// condDisableDepth is the nesting depth that caused conditional branch
	// execution to be disabled, or the value `noCondDisableDepth`.
	condNestDepth    int32
	condDisableDepth int32
}

// hasFlag returns whether the script engine instance has the passed flag set.
func (vm *Engine) hasFlag(flag ScriptFlags) bool {
	return vm.flags&flag == flag
}

// isBranchExecuting returns whether or not the current conditional branch is
// actively executing.  For example, when the data stack has an OP_FALSE on it
// and an OP_IF is encountered, the branch is inactive until an OP_ELSE or
// OP_ENDIF is encountered.  It properly handles nested conditionals.
func (vm *Engine) isBranchExecuting() bool {
	return vm.condDisableDepth == noCondDisableDepth
}

// isOpcodeDisabled returns whether or not the opcode is disabled and thus is
// always bad to see in the instruction stream (even if turned off by a
// conditional).
func isOpcodeDisabled(opcode byte) bool {
	switch opcode {
	case OP_CODESEPARATOR:
		return true
	default:
		return false
	}
}

// isOpcodeAlwaysIllegal returns whether or not the opcode is always illegal
// when passed over by the program counter even if in a non-executed branch (it
// isn't a coincidence that they are conditionals).
func isOpcodeAlwaysIllegal(opcode byte) bool {
	switch opcode {
	case OP_VERIF:
		return true
	case OP_VERNOTIF:
		return true
	default:
		return false
	}
}

// isOpcodeConditional returns whether or not the opcode is a conditional opcode
// which changes the conditional execution stack when executed.
func isOpcodeConditional(opcode byte) bool {
	switch opcode {
	case OP_IF:
		return true
	case OP_NOTIF:
		return true
	case OP_ELSE:
		return true
	case OP_ENDIF:
		return true
	default:
		return false
	}
}

// checkMinimalDataPush returns whether or not the provided opcode is the
// smallest possible way to represent the given data.  For example, the value 15
// could be pushed with OP_DATA_1 15 (among other variations); however, OP_15 is
// a single opcode that represents the same value and is only a single byte
// versus two bytes.
func checkMinimalDataPush(op *opcode, data []byte) error {
	opcode := op.value
	dataLen := len(data)
	switch {
	case dataLen == 0 && opcode != OP_0:
		str := fmt.Sprintf("zero length data push is encoded with opcode %s "+
			"instead of OP_0", op.name)
		return scriptError(ErrMinimalData, str)
	case dataLen == 1 && data[0] >= 1 && data[0] <= 16:
		if opcode != OP_1+data[0]-1 {
			// Should have used OP_1 .. OP_16
			str := fmt.Sprintf("data push of the value %d encoded with opcode "+
				"%s instead of OP_%d", data[0], op.name, data[0])
			return scriptError(ErrMinimalData, str)
		}
	case dataLen == 1 && data[0] == 0x81:
		if opcode != OP_1NEGATE {
			str := fmt.Sprintf("data push of the value -1 encoded with opcode "+
				"%s instead of OP_1NEGATE", op.name)
			return scriptError(ErrMinimalData, str)
		}
	case dataLen <= 75:
		if int(opcode) != dataLen {
			// Should have used a direct push
			str := fmt.Sprintf("data push of %d bytes encoded with opcode %s "+
				"instead of OP_DATA_%d", dataLen, op.name, dataLen)
			return scriptError(ErrMinimalData, str)
		}
	case dataLen <= 255:
		if opcode != OP_PUSHDATA1 {
			str := fmt.Sprintf("data push of %d bytes encoded with opcode %s "+
				"instead of OP_PUSHDATA1", dataLen, op.name)
			return scriptError(ErrMinimalData, str)
		}
	case dataLen <= 65535:
		if opcode != OP_PUSHDATA2 {
			str := fmt.Sprintf("data push of %d bytes encoded with opcode %s "+
				"instead of OP_PUSHDATA2", dataLen, op.name)
			return scriptError(ErrMinimalData, str)
		}
	}
	return nil
}

// executeOpcode performs execution on the passed opcode.  It takes into account
// whether or not it is hidden by conditionals, but some rules still must be
// tested in this case.
func (vm *Engine) executeOpcode(op *opcode, data []byte) error {
	// Disabled opcodes are fail on program counter.
	if isOpcodeDisabled(op.value) {
		str := fmt.Sprintf("attempt to execute disabled opcode %s", op.name)
		return scriptError(ErrDisabledOpcode, str)
	}

	// Always-illegal opcodes are fail on program counter.
	if isOpcodeAlwaysIllegal(op.value) {
		str := fmt.Sprintf("attempt to execute reserved opcode %s", op.name)
		return scriptError(ErrReservedOpcode, str)
	}

	// Note that this includes OP_RESERVED which counts as a push operation.
	if op.value > OP_16 {
		vm.numOps++
		if vm.numOps > MaxOpsPerScript {
			str := fmt.Sprintf("exceeded max operation limit of %d",
				MaxOpsPerScript)
			return scriptError(ErrTooManyOperations, str)
		}
	} else if len(data) > MaxScriptElementSize {
		str := fmt.Sprintf("element size %d exceeds max allowed size %d",
			len(data), MaxScriptElementSize)
		return scriptError(ErrElementTooBig, str)
	}

	// Nothing left to do when this is not a conditional opcode and it is
	// not in an executing branch.
	if !vm.isBranchExecuting() && !isOpcodeConditional(op.value) {
		return nil
	}

	// Ensure all executed data push opcodes use the minimal encoding.
	if vm.isBranchExecuting() && op.value <= OP_PUSHDATA4 {
		if err := checkMinimalDataPush(op, data); err != nil {
			return err
		}
	}

	return op.opfunc(op, data, vm)
}

// checkValidPC returns an error if the current script position is not valid for
// execution.
func (vm *Engine) checkValidPC() error {
	if vm.scriptIdx >= len(vm.scripts) {
		str := fmt.Sprintf("program counter beyond input scripts (script idx "+
			"%d, total scripts %d)", vm.scriptIdx, len(vm.scripts))
		return scriptError(ErrInvalidProgramCounter, str)
	}
	return nil
}

// DisasmPC returns the string for the disassembly of the opcode that will be
// next to execute when Step is called.
func (vm *Engine) DisasmPC() (string, error) {
	if err := vm.checkValidPC(); err != nil {
		return "", err
	}

	// Create a copy of the current tokenizer and parse the next opcode in the
	// copy to avoid mutating the current one.
	peekTokenizer := vm.tokenizer
	if !peekTokenizer.Next() {
		// Note that due to the fact that all scripts are checked for parse
		// failures before this code ever runs, there should never be an error
		// here, but check again to be safe in case a refactor breaks that
		// assumption or new script versions are introduced with different
		// semantics.
		if err := peekTokenizer.Err(); err != nil {
			return "", err
		}

		// Note that this should be impossible to hit in practice because the
		// only way it could happen would be for the final opcode of a script to
		// already be parsed without the script index having been updated, which
		// is not the case since stepping the script always increments the
		// script index when parsing and executing the final opcode of a script.
		//
		// However, check again to be safe in case a refactor breaks that
		// assumption or new script versions are introduced with different
		// semantics.
		str := fmt.Sprintf("program counter beyond script index %d (bytes %x)",
			vm.scriptIdx, vm.scripts[vm.scriptIdx])
		return "", scriptError(ErrInvalidProgramCounter, str)
	}

	var buf strings.Builder
	disasmOpcode(&buf, peekTokenizer.op, peekTokenizer.Data(), false)
	return fmt.Sprintf("%02x:%04x: %s", vm.scriptIdx, vm.opcodeIdx,
		buf.String()), nil
}

// DisasmScript returns the disassembly string for the script at the requested
// offset index.  Index 0 is the signature script and 1 is the public key
// script.  In the case of pay-to-script-hash, index 2 is the redeem script once
// the execution has progressed far enough to have successfully verified script
// hash and thus add the script to the scripts to execute.
func (vm *Engine) DisasmScript(idx int) (string, error) {
	if idx >= len(vm.scripts) {
		str := fmt.Sprintf("script index %d >= total scripts %d", idx,
			len(vm.scripts))
		return "", scriptError(ErrInvalidIndex, str)
	}

	var disbuf strings.Builder
	script := vm.scripts[idx]
	tokenizer := MakeScriptTokenizer(vm.version, script)
	var opcodeIdx int
	for tokenizer.Next() {
		disbuf.WriteString(fmt.Sprintf("%02x:%04x: ", idx, opcodeIdx))
		disasmOpcode(&disbuf, tokenizer.op, tokenizer.Data(), false)
		disbuf.WriteByte('\n')
		opcodeIdx++
	}
	return disbuf.String(), tokenizer.Err()
}

// CheckErrorCondition returns nil if the running script has ended and was
// successful, leaving a true boolean on the stack.  An error otherwise,
// including if the script has not finished.
func (vm *Engine) CheckErrorCondition(finalScript bool) error {
	// Check execution is actually done by ensuring the script index is after
	// the final script in the array script.
	if vm.scriptIdx < len(vm.scripts) {
		return scriptError(ErrScriptUnfinished,
			"error check when script unfinished")
	}

	// The final script must end with exactly one data stack item when the
	// verify clean stack flag is set.  Otherwise, there must be at least one
	// data stack item in order to interpret it as a boolean.
	if finalScript && vm.hasFlag(ScriptVerifyCleanStack) &&
		vm.dstack.Depth() != 1 {

		str := fmt.Sprintf("stack must contain exactly one item (contains %d)",
			vm.dstack.Depth())
		return scriptError(ErrCleanStack, str)
	} else if vm.dstack.Depth() < 1 {
		return scriptError(ErrEmptyStack,
			"stack empty at end of script execution")
	}

	v, err := vm.dstack.PopBool()
	if err != nil {
		return err
	}
	if !v {
		// Log interesting data.
		log.Tracef("%v", newLogClosure(func() string {
			var buf strings.Builder
			buf.WriteString("scripts failed:\n")
			for i := range vm.scripts {
				dis, _ := vm.DisasmScript(i)
				buf.WriteString(fmt.Sprintf("script%d:\n", i))
				buf.WriteString(dis)
			}
			return buf.String()
		}))
		return scriptError(ErrEvalFalse,
			"false stack entry at end of script execution")
	}
	return nil
}

// Step executes the next instruction and moves the program counter to the next
// opcode in the script, or the next script if the current has ended.  Step will
// return true in the case that the last opcode was successfully executed.
//
// The result of calling Step or any other method is undefined if an error is
// returned.
func (vm *Engine) Step() (done bool, err error) {
	// Verify the engine is pointing to a valid program counter.
	if err := vm.checkValidPC(); err != nil {
		return true, err
	}

	// Attempt to parse the next opcode from the current script.
	if !vm.tokenizer.Next() {
		// Note that due to the fact that all scripts are checked for parse
		// failures before this code ever runs, there should never be an error
		// here, but check again to be safe in case a refactor breaks that
		// assumption or new script versions are introduced with different
		// semantics.
		if err := vm.tokenizer.Err(); err != nil {
			return false, err
		}

		str := fmt.Sprintf("attempt to step beyond script index %d (bytes %x)",
			vm.scriptIdx, vm.scripts[vm.scriptIdx])
		return true, scriptError(ErrInvalidProgramCounter, str)
	}

	// Execute the opcode while taking into account several things such as
	// disabled opcodes, illegal opcodes, maximum allowed operations per script,
	// maximum script element sizes, and conditionals.
	err = vm.executeOpcode(vm.tokenizer.op, vm.tokenizer.Data())
	if err != nil {
		return true, err
	}

	// The number of elements in the combination of the data and alt stacks
	// must not exceed the maximum number of stack elements allowed.
	combinedStackSize := vm.dstack.Depth() + vm.astack.Depth()
	if combinedStackSize > MaxStackSize {
		str := fmt.Sprintf("combined stack size %d > max allowed %d",
			combinedStackSize, MaxStackSize)
		return false, scriptError(ErrStackOverflow, str)
	}

	// Prepare for next instruction.
	vm.opcodeIdx++
	if vm.tokenizer.Done() {
		// Illegal to have a conditional that straddles two scripts.
		if vm.condNestDepth != 0 {
			return false, scriptError(ErrUnbalancedConditional,
				"end of script reached in conditional execution")
		}

		// Alt stack doesn't persist between scripts.
		_ = vm.astack.DropN(vm.astack.Depth())

		// The number of operations is per script.
		vm.numOps = 0

		// Reset the opcode index for the next script.
		vm.opcodeIdx = 0

		// Advance to the next script as needed.
		switch {
		case vm.scriptIdx == 0 && vm.isP2SH:
			vm.scriptIdx++
			vm.savedFirstStack = vm.GetStack()

		case vm.scriptIdx == 1 && vm.isP2SH:
			// Put us past the end for CheckErrorCondition()
			vm.scriptIdx++

			// Check script ran successfully.
			err := vm.CheckErrorCondition(false)
			if err != nil {
				return false, err
			}

			// Obtain the redeem script from the first stack and ensure it
			// parses.
			script := vm.savedFirstStack[len(vm.savedFirstStack)-1]
			if err := checkScriptParses(vm.version, script); err != nil {
				return false, err
			}
			vm.scripts = append(vm.scripts, script)

			// Set stack to be the stack from first script minus the redeem
			// script itself
			vm.SetStack(vm.savedFirstStack[:len(vm.savedFirstStack)-1])

		default:
			vm.scriptIdx++
		}

		// Skip empty scripts.
		if vm.scriptIdx < len(vm.scripts) && len(vm.scripts[vm.scriptIdx]) == 0 {
			vm.scriptIdx++
		}

		vm.lastCodeSep = 0
		if vm.scriptIdx >= len(vm.scripts) {
			return true, nil
		}

		// Finally, update the current tokenizer used to parse through scripts
		// one opcode at a time to start from the beginning of the new script
		// associated with the program counter.
		vm.tokenizer = MakeScriptTokenizer(vm.version, vm.scripts[vm.scriptIdx])
	}

	return false, nil
}

// Execute will execute all scripts in the script engine and return either nil
// for successful validation or an error if one occurred.
func (vm *Engine) Execute() (err error) {
	// All script versions other than 0 currently execute without issue,
	// making all outputs to them anyone can pay. In the future this
	// will allow for the addition of new scripting languages.
	if vm.version != 0 {
		return nil
	}

	done := false
	for !done {
		log.Tracef("%v", newLogClosure(func() string {
			dis, err := vm.DisasmPC()
			if err != nil {
				return fmt.Sprintf("stepping - failed to disasm pc: %v", err)
			}
			return fmt.Sprintf("stepping %v", dis)
		}))

		done, err = vm.Step()
		if err != nil {
			return err
		}
		log.Tracef("%v", newLogClosure(func() string {
			var dstr, astr string

			// Log the non-empty stacks when tracing.
			if vm.dstack.Depth() != 0 {
				dstr = "Stack:\n" + vm.dstack.String()
			}
			if vm.astack.Depth() != 0 {
				astr = "AltStack:\n" + vm.astack.String()
			}

			return dstr + astr
		}))
	}

	return vm.CheckErrorCondition(true)
}

// subScript returns the script since the last OP_CODESEPARATOR.
func (vm *Engine) subScript() []byte {
	return vm.scripts[vm.scriptIdx][vm.lastCodeSep:]
}

// checkHashTypeEncoding returns whether or not the passed hashtype adheres to
// the strict encoding requirements.
func (vm *Engine) checkHashTypeEncoding(hashType SigHashType) error {
	sigHashType := hashType & ^SigHashAnyOneCanPay
	if sigHashType < SigHashAll || sigHashType > SigHashSingle {
		str := fmt.Sprintf("invalid hash type 0x%x", hashType)
		return scriptError(ErrInvalidSigHashType, str)
	}
	return nil
}

// isStrictPubKeyEncoding returns whether or not the passed public key adheres
// to the strict encoding requirements.
func isStrictPubKeyEncoding(pubKey []byte) bool {
	if len(pubKey) == 33 && (pubKey[0] == 0x02 || pubKey[0] == 0x03) {
		// Compressed
		return true
	}
	if len(pubKey) == 65 && pubKey[0] == 0x04 {
		// Uncompressed
		return true
	}
	return false
}

// checkPubKeyEncoding returns an error if the passed public key does not
// adhere to the strict encoding requirements.
func (vm *Engine) checkPubKeyEncoding(pubKey []byte) error {
	if !isStrictPubKeyEncoding(pubKey) {
		return scriptError(ErrPubKeyType, "unsupported public key type")
	}
	return nil
}

// checkSignatureEncoding returns whether or not the passed signature adheres to
// the strict encoding requirements.
func (vm *Engine) checkSignatureEncoding(sig []byte) error {
	// The format of a DER encoded signature is as follows:
	//
	// 0x30 <total length> 0x02 <length of R> <R> 0x02 <length of S> <S>
	//   - 0x30 is the ASN.1 identifier for a sequence
	//   - Total length is 1 byte and specifies length of all remaining data
	//   - 0x02 is the ASN.1 identifier that specifies an integer follows
	//   - Length of R is 1 byte and specifies how many bytes R occupies
	//   - R is the arbitrary length big-endian encoded number which
	//     represents the R value of the signature.  DER encoding dictates
	//     that the value must be encoded using the minimum possible number
	//     of bytes.  This implies the first byte can only be null if the
	//     highest bit of the next byte is set in order to prevent it from
	//     being interpreted as a negative number.
	//   - 0x02 is once again the ASN.1 integer identifier
	//   - Length of S is 1 byte and specifies how many bytes S occupies
	//   - S is the arbitrary length big-endian encoded number which
	//     represents the S value of the signature.  The encoding rules are
	//     identical as those for R.
	const (
		asn1SequenceID = 0x30
		asn1IntegerID  = 0x02

		// minSigLen is the minimum length of a DER encoded signature and is
		// when both R and S are 1 byte each.
		//
		// 0x30 + <1-byte> + 0x02 + 0x01 + <byte> + 0x2 + 0x01 + <byte>
		minSigLen = 8

		// maxSigLen is the maximum length of a DER encoded signature and is
		// when both R and S are 33 bytes each.  It is 33 bytes because a
		// 256-bit integer requires 32 bytes and an additional leading null byte
		// might required if the high bit is set in the value.
		//
		// 0x30 + <1-byte> + 0x02 + 0x21 + <33 bytes> + 0x2 + 0x21 + <33 bytes>
		maxSigLen = 72

		// sequenceOffset is the byte offset within the signature of the
		// expected ASN.1 sequence identifier.
		sequenceOffset = 0

		// dataLenOffset is the byte offset within the signature of the expected
		// total length of all remaining data in the signature.
		dataLenOffset = 1

		// rTypeOffset is the byte offset within the signature of the ASN.1
		// identifier for R and is expected to indicate an ASN.1 integer.
		rTypeOffset = 2

		// rLenOffset is the byte offset within the signature of the length of
		// R.
		rLenOffset = 3

		// rOffset is the byte offset within the signature of R.
		rOffset = 4
	)

	// The signature must adhere to the minimum and maximum allowed length.
	sigLen := len(sig)
	if sigLen < minSigLen {
		str := fmt.Sprintf("malformed signature: too short: %d < %d", sigLen,
			minSigLen)
		return scriptError(ErrSigTooShort, str)
	}
	if sigLen > maxSigLen {
		str := fmt.Sprintf("malformed signature: too long: %d > %d", sigLen,
			maxSigLen)
		return scriptError(ErrSigTooLong, str)
	}

	// The signature must start with the ASN.1 sequence identifier.
	if sig[sequenceOffset] != asn1SequenceID {
		str := fmt.Sprintf("malformed signature: format has wrong type: %#x",
			sig[sequenceOffset])
		return scriptError(ErrSigInvalidSeqID, str)
	}

	// The signature must indicate the correct amount of data for all elements
	// related to R and S.
	if int(sig[dataLenOffset]) != sigLen-2 {
		str := fmt.Sprintf("malformed signature: bad length: %d != %d",
			sig[dataLenOffset], sigLen-2)
		return scriptError(ErrSigInvalidDataLen, str)
	}

	// Calculate the offsets of the elements related to S and ensure S is inside
	// the signature.
	//
	// rLen specifies the length of the big-endian encoded number which
	// represents the R value of the signature.
	//
	// sTypeOffset is the offset of the ASN.1 identifier for S and, like its R
	// counterpart, is expected to indicate an ASN.1 integer.
	//
	// sLenOffset and sOffset are the byte offsets within the signature of the
	// length of S and S itself, respectively.
	rLen := int(sig[rLenOffset])
	sTypeOffset := rOffset + rLen
	sLenOffset := sTypeOffset + 1
	if sTypeOffset >= sigLen {
		str := "malformed signature: S type indicator missing"
		return scriptError(ErrSigMissingSTypeID, str)
	}
	if sLenOffset >= sigLen {
		str := "malformed signature: S length missing"
		return scriptError(ErrSigMissingSLen, str)
	}

	// The lengths of R and S must match the overall length of the signature.
	//
	// sLen specifies the length of the big-endian encoded number which
	// represents the S value of the signature.
	sOffset := sLenOffset + 1
	sLen := int(sig[sLenOffset])
	if sOffset+sLen != sigLen {
		str := "malformed signature: invalid S length"
		return scriptError(ErrSigInvalidSLen, str)
	}

	// R elements must be ASN.1 integers.
	if sig[rTypeOffset] != asn1IntegerID {
		str := fmt.Sprintf("malformed signature: R integer marker: %#x != %#x",
			sig[rTypeOffset], asn1IntegerID)
		return scriptError(ErrSigInvalidRIntID, str)
	}

	// Zero-length integers are not allowed for R.
	if rLen == 0 {
		str := "malformed signature: R length is zero"
		return scriptError(ErrSigZeroRLen, str)
	}

	// R must not be negative.
	if sig[rOffset]&0x80 != 0 {
		str := "malformed signature: R is negative"
		return scriptError(ErrSigNegativeR, str)
	}

	// Null bytes at the start of R are not allowed, unless R would otherwise be
	// interpreted as a negative number.
	if rLen > 1 && sig[rOffset] == 0x00 && sig[rOffset+1]&0x80 == 0 {
		str := "malformed signature: R value has too much padding"
		return scriptError(ErrSigTooMuchRPadding, str)
	}

	// S elements must be ASN.1 integers.
	if sig[sTypeOffset] != asn1IntegerID {
		str := fmt.Sprintf("malformed signature: S integer marker: %#x != %#x",
			sig[sTypeOffset], asn1IntegerID)
		return scriptError(ErrSigInvalidSIntID, str)
	}

	// Zero-length integers are not allowed for S.
	if sLen == 0 {
		str := "malformed signature: S length is zero"
		return scriptError(ErrSigZeroSLen, str)
	}

	// S must not be negative.
	if sig[sOffset]&0x80 != 0 {
		str := "malformed signature: S is negative"
		return scriptError(ErrSigNegativeS, str)
	}

	// Null bytes at the start of S are not allowed, unless S would otherwise be
	// interpreted as a negative number.
	if sLen > 1 && sig[sOffset] == 0x00 && sig[sOffset+1]&0x80 == 0 {
		str := "malformed signature: S value has too much padding"
		return scriptError(ErrSigTooMuchSPadding, str)
	}

	// Verify the S value is <= half the order of the curve.  This check is done
	// because when it is higher, the complement modulo the order can be used
	// instead which is a shorter encoding by 1 byte.
	sValue := new(big.Int).SetBytes(sig[sOffset : sOffset+sLen])
	if sValue.Cmp(halfOrder) > 0 {
		return scriptError(ErrSigHighS, "signature is not canonical due to "+
			"unnecessarily high S value")
	}

	return nil
}

// getStack returns the contents of stack as a byte array bottom up
func getStack(stack *stack) [][]byte {
	array := make([][]byte, stack.Depth())
	for i := range array {
		// PeekByteArry can't fail due to overflow, already checked
		array[len(array)-i-1], _ = stack.PeekByteArray(int32(i))
	}
	return array
}

// setStack sets the stack to the contents of the array where the last item in
// the array is the top item in the stack.
func setStack(stack *stack, data [][]byte) {
	// This can not error. Only errors are for invalid arguments.
	_ = stack.DropN(stack.Depth())

	for i := range data {
		stack.PushByteArray(data[i])
	}
}

// GetStack returns the contents of the primary stack as an array. where the
// last item in the array is the top of the stack.
func (vm *Engine) GetStack() [][]byte {
	return getStack(&vm.dstack)
}

// SetStack sets the contents of the primary stack to the contents of the
// provided array where the last item in the array will be the top of the stack.
func (vm *Engine) SetStack(data [][]byte) {
	setStack(&vm.dstack, data)
}

// GetAltStack returns the contents of the alternate stack as an array where the
// last item in the array is the top of the stack.
func (vm *Engine) GetAltStack() [][]byte {
	return getStack(&vm.astack)
}

// SetAltStack sets the contents of the alternate stack to the contents of the
// provided array where the last item in the array will be the top of the stack.
func (vm *Engine) SetAltStack(data [][]byte) {
	setStack(&vm.astack, data)
}

// NewEngine returns a new script engine for the provided public key script,
// transaction, and input index.  The flags modify the behavior of the script
// engine according to the description provided by each flag.
func NewEngine(scriptPubKey []byte, tx *wire.MsgTx, txIdx int, flags ScriptFlags, scriptVersion uint16, sigCache *SigCache) (*Engine, error) {
	// The provided transaction input index must refer to a valid input.
	if txIdx < 0 || txIdx >= len(tx.TxIn) {
		str := fmt.Sprintf("transaction input index %d is negative or "+
			">= %d", txIdx, len(tx.TxIn))
		return nil, scriptError(ErrInvalidIndex, str)
	}
	scriptSig := tx.TxIn[txIdx].SignatureScript

	// When both the signature script and public key script are empty the result
	// is necessarily an error since the stack would end up being empty which is
	// equivalent to a false top element.  Thus, just return the relevant error
	// now as an optimization.
	if len(scriptSig) == 0 && len(scriptPubKey) == 0 {
		return nil, scriptError(ErrEvalFalse,
			"false stack entry at end of script execution")
	}

	// The signature script must only contain data pushes when the associated
	// flag is set.
	vm := Engine{version: scriptVersion, flags: flags, sigCache: sigCache}
	if vm.hasFlag(ScriptVerifySigPushOnly) && !IsPushOnlyScript(scriptSig) {
		return nil, scriptError(ErrNotPushOnly,
			"signature script is not push only")
	}

	// The signature script must only contain data pushes for P2SH which is
	// determined based on the form of the public key script.
	if isAnyKindOfScriptHash(scriptPubKey) {
		// Notice that the push only checks have already been done when the flag
		// to verify signature scripts are push only is set above, so avoid
		// checking again.
		alreadyChecked := vm.hasFlag(ScriptVerifySigPushOnly)
		if !alreadyChecked && !IsPushOnlyScript(scriptSig) {
			return nil, scriptError(ErrNotPushOnly,
				"pay to script hash is not push only")
		}
		vm.isP2SH = true
	}

	// Redeem scripts for pay to script hash outputs are not allowed to use any
	// stake tag opcodes if the script version is 0.
	if scriptVersion == 0 {
		err := hasP2SHRedeemScriptStakeOpCodes(scriptVersion, scriptSig,
			scriptPubKey)
		if err != nil {
			return nil, err
		}
	}

	// The engine stores the scripts using a slice.  This allows multiple
	// scripts to be executed in sequence.  For example, with a
	// pay-to-script-hash transaction, there will be ultimately be a third
	// script to execute.
	scripts := [][]byte{scriptSig, scriptPubKey}
	for _, scr := range scripts {
		if len(scr) > MaxScriptSize {
			str := fmt.Sprintf("script size %d is larger than max allowed "+
				"size %d", len(scr), MaxScriptSize)
			return nil, scriptError(ErrScriptTooBig, str)
		}

		// Ensure the scripts can be fully parsed up front according to version
		// 0 semantics.  This is required because when script versioning was
		// introduced, the semantics of script parsing were not properly updated
		// to handle versions as well, and therefore the consensus rules
		// currently dictate that creating an engine with newer script versions
		// must fail if those scripts fail to parse according the version 0
		// script semantics.  This needs to be corrected via a consensus vote at
		// some point.
		//
		// It is worth noting that aside from the aforementioned issue, this
		// would ordinarily be optional since a script that fails to parse would
		// eventually fail later when executing the opcodes as well.
		//
		// However, without checking up front, it would be possible for
		// malicious actors to intentionally craft scripts that involve a bunch
		// of relatively expensive operations before a malformed opcode in order
		// to attempt resource exhaustion attacks.  There are other protections
		// in place, such as maximums, to also help mitigate these style of
		// attacks, but since it's quite quick and allocation free to ensure
		// scripts fully parse before executing them, it is reasonable to make
		// the minor speed tradeoff.
		//
		// A future consensus vote should alter this to use the actual script
		// version instead of version 0 and avoid parsing for unsupported script
		// versions if soft fork capability is desired.
		const consensusVersion = 0
		if err := checkScriptParses(consensusVersion, scr); err != nil {
			return nil, err
		}
	}
	vm.scripts = scripts

	// Advance the program counter to the public key script if the signature
	// script is empty since there is nothing to execute for it in that case.
	if len(scriptSig) == 0 {
		vm.scriptIdx++
	}

	// Setup the current tokenizer used to parse through the script one opcode
	// at a time with the script associated with the program counter.
	vm.tokenizer = MakeScriptTokenizer(scriptVersion, scripts[vm.scriptIdx])

	vm.tx = *tx
	vm.txIdx = txIdx
	vm.condDisableDepth = noCondDisableDepth

	return &vm, nil
}