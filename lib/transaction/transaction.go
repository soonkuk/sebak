package transaction

import (
	"encoding/json"
	"time"

	"github.com/btcsuite/btcutil/base58"

	"boscoin.io/sebak/lib/common"
	"boscoin.io/sebak/lib/common/keypair"
	"boscoin.io/sebak/lib/errors"
	"boscoin.io/sebak/lib/transaction/operation"
)

// TODO versioning

type Transaction struct {
	H Header
	B Body
}

type envelop struct {
	T string
	H Header
	B Body
}

type Header struct {
	Version string `json:"version"`
	Created string `json:"created"`
	// Hash of this transaction
	// This is cached and not serialized when sent, because the remote node
	// has to validate it anyway.
	Hash      string `json:"-"`
	Signature string `json:"signature"`
}

type Body struct {
	Source     string                `json:"source"`
	Fee        common.Amount         `json:"fee"`
	SequenceID uint64                `json:"sequence_id"`
	Operations []operation.Operation `json:"operations"`
	TimeBound  TimeBound             `json:"timeBound"`
}

func (tb Body) MakeHash() []byte {
	return common.MustMakeObjectHash(tb)
}

func (tb Body) MakeHashString() string {
	return base58.Encode(tb.MakeHash())
}

func (t *Transaction) UnmarshalJSON(b []byte) (err error) {
	var tj envelop
	if err = json.Unmarshal(b, &tj); err != nil {
		return
	}

	t.H = tj.H
	t.B = tj.B
	t.H.Hash = t.B.MakeHashString()
	return
}

func NewTransaction(source string, sequenceID uint64, timeBound TimeBound, ops ...operation.Operation) (tx Transaction, err error) {
	if len(ops) < 1 {
		err = errors.TransactionEmptyOperations
		return
	}

	var opsHaveFee int
	for _, op := range ops {
		if op.HasFee() {
			opsHaveFee++
		}
	}
	fee := common.Amount(0)
	if opsHaveFee > 0 {
		fee = common.BaseFee.MustMult(opsHaveFee)
	}

	txBody := Body{
		Source:     source,
		Fee:        fee,
		SequenceID: sequenceID,
		Operations: ops,
		timeBound:  timeBound,
	}

	tx = Transaction{
		H: Header{
			Version: common.TransactionVersionV1,
			Created: common.NowISO8601(),
			Hash:    txBody.MakeHashString(),
		},
		B: txBody,
	}

	return
}

var TransactionWellFormedCheckerFuncs = []common.CheckerFunc{
	CheckOverOperationsLimit,
	CheckSource,
	CheckBaseFee,
	CheckOperationTypes,
	CheckOperations,
	CheckVerifySignature,
	CheckTimeBound,
}

func (tx Transaction) IsWellFormed(conf common.Config) (err error) {
	// TODO check `Version` format with SemVer

	checker := &Checker{
		DefaultChecker: common.DefaultChecker{Funcs: TransactionWellFormedCheckerFuncs},
		NetworkID:      conf.NetworkID,
		Transaction:    tx,
		Conf:           conf,
	}
	if err = common.RunChecker(checker, common.DefaultDeferFunc); err != nil {
		if _, ok := err.(*errors.Error); !ok {
			err = errors.InvalidTransaction.Clone().SetData("error", err.Error())
		}
		return
	}

	return
}

func (tx Transaction) GetType() common.MessageType {
	return common.TransactionMessage
}

func (tx Transaction) Equal(m common.Message) bool {
	return tx.H.Hash == m.GetHash()
}

func (tx Transaction) IsValidSequenceID(sequenceID uint64) bool {
	return tx.B.SequenceID == sequenceID
}

func (tx Transaction) GetHash() string {
	return tx.H.Hash
}

func (tx Transaction) Source() string {
	return tx.B.Source
}

func (tx Transaction) Version() string {
	return tx.H.Version
}

// TotalAmount returns the sum of Amount of operations.
//
// Returns:
//   the total monetary value of this transaction,
//   which is the sum of its operations,
//   optionally with fees
//
// Params:
//   withFee = If fee should be included in the total
//
func (tx Transaction) TotalAmount(withFee bool) common.Amount {
	// Note that the transaction shouldn't be constructed invalid
	// (the sum of its Operations should not exceed the maximum supply)
	var amount common.Amount
	for _, op := range tx.B.Operations {
		if pop, ok := op.B.(operation.Payable); ok {
			amount = amount.MustAdd(pop.GetAmount())
		}
	}

	if withFee {
		amount = amount.MustAdd(tx.B.Fee)
	}

	return amount
}

// TotalBaseFee returns the minimum fee of transaction.
func (tx Transaction) TotalBaseFee() common.Amount {
	var opsHaveFee int
	for _, op := range tx.B.Operations {
		if op.HasFee() {
			opsHaveFee++
		}
	}
	if opsHaveFee < 1 {
		return common.Amount(0)
	}

	return common.BaseFee.MustMult(opsHaveFee)
}

// Threshold returns the maximum threshold from operations.
func (tx Transaction) Threshold() operation.OperationThreshold {
	var threshold operation.OperationThreshold
	for _, op := range tx.B.Operations {
		if op.B.HasThreshold() {
			opbm := op.B.(operation.MultiSignable)
			if opbm.GetThreshold() > threshold {
				threshold = opbm.GetThreshold()
			}
		}
	}
	return threshold
}

func (tx Transaction) Serialize() (encoded []byte, err error) {
	encoded, err = json.Marshal(tx)
	return
}

func (tx Transaction) String() string {
	encoded, _ := json.MarshalIndent(tx, "", "  ")
	return string(encoded)
}

func (tx *Transaction) Sign(source string, kp keypair.KP, networkID []byte) {
	tx.B.Source = source
	tx.H.Hash = tx.B.MakeHashString()
	signature, _ := keypair.MakeSignature(kp, networkID, tx.H.Hash)

	tx.H.Signature = base58.Encode(signature)

	return
}

func (tx Transaction) IsEmpty() bool {
	return len(tx.GetHash()) < 1
}

func (tx Transaction) IsValidVersion(version string) bool {
	return tx.H.Version == version
}

type TimeBound struct {
	lowerBound time.Time
	upperBound time.Time
}

func NewTimeBound(lowerBound time.Time, upperBound time.Time) *TimeBound {
	return &TimeBound{
		lowerBound: lowerBound,
		upperBound: upperBound,
	}
}
