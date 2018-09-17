package runner

import (
	"boscoin.io/sebak/lib/ballot"
	"boscoin.io/sebak/lib/block"
	"boscoin.io/sebak/lib/common"
	"boscoin.io/sebak/lib/error"
	"boscoin.io/sebak/lib/node"
	"boscoin.io/sebak/lib/storage"
	"boscoin.io/sebak/lib/transaction"
)

type BallotTransactionChecker struct {
	common.DefaultChecker

	NodeRunner *NodeRunner
	LocalNode  node.Node
	NetworkID  []byte

	Transactions         []string
	VotingHole           ballot.VotingHole
	ValidTransactions    []string
	validTransactionsMap map[string]bool
	CheckAll             bool
}

func (checker *BallotTransactionChecker) InvalidTransactions() (invalids []string) {
	for _, hash := range checker.Transactions {
		if _, found := checker.validTransactionsMap[hash]; found {
			continue
		}

		invalids = append(invalids, hash)
	}

	return
}

func (checker *BallotTransactionChecker) setValidTransactions(hashes []string) {
	checker.ValidTransactions = hashes

	checker.validTransactionsMap = map[string]bool{}
	for _, hash := range hashes {
		checker.validTransactionsMap[hash] = true
	}

	return
}

// TransactionsIsNew checks the incoming transaction is
// already stored or not.
func IsNew(c common.Checker, args ...interface{}) (err error) {
	checker := c.(*BallotTransactionChecker)

	var validTransactions []string
	for _, hash := range checker.Transactions {
		// check transaction is already stored
		var found bool
		if found, err = block.ExistBlockTransaction(checker.NodeRunner.Storage(), hash); err != nil || found {
			if !checker.CheckAll {
				err = errors.ErrorNewButKnownMessage
				return
			}
			continue
		}
		validTransactions = append(validTransactions, hash)
	}

	err = nil
	checker.setValidTransactions(validTransactions)

	return
}

// GetMissingTransaction will get the missing
// tranactions, that is, not in `TransactionPool` from proposer.
func GetMissingTransaction(c common.Checker, args ...interface{}) (err error) {
	checker := c.(*BallotTransactionChecker)

	var validTransactions []string
	for _, hash := range checker.ValidTransactions {
		if !checker.NodeRunner.Consensus().TransactionPool.Has(hash) {
			// TODO get transaction from proposer and check
			// `Transaction.IsWellFormed()`
			continue
		}
		validTransactions = append(validTransactions, hash)
	}

	checker.setValidTransactions(validTransactions)

	return
}

// BallotTransactionsSourceCheck checks there are transactions which has same
// source in the `Transactions`.
func BallotTransactionsSameSource(c common.Checker, args ...interface{}) (err error) {
	checker := c.(*BallotTransactionChecker)

	var validTransactions []string
	sources := map[string]bool{}
	for _, hash := range checker.ValidTransactions {
		tx, _ := checker.NodeRunner.Consensus().TransactionPool.Get(hash)
		if found := common.InStringMap(sources, tx.B.Source); found {
			if !checker.CheckAll {
				err = errors.ErrorTransactionSameSource
				return
			}
			continue
		}

		sources[tx.B.Source] = true
		validTransactions = append(validTransactions, hash)
	}
	err = nil
	checker.setValidTransactions(validTransactions)

	return
}

// BallotTransactionsSourceCheck calls `Transaction.Validate()`.
func BallotTransactionsSourceCheck(c common.Checker, args ...interface{}) (err error) {
	checker := c.(*BallotTransactionChecker)

	var validTransactions []string
	for _, hash := range checker.ValidTransactions {
		tx, _ := checker.NodeRunner.Consensus().TransactionPool.Get(hash)

		if err = ValidateTx(checker.NodeRunner.Storage(), tx); err != nil {
			if !checker.CheckAll {
				return
			}
			continue
		}
		validTransactions = append(validTransactions, hash)
	}

	err = nil
	checker.setValidTransactions(validTransactions)

	return
}

// Validate checks,
// * source account exists
// * sequenceID is valid
// * source has enough balance to pay
// * and it's `Operations`
func ValidateTx(st *storage.LevelDBBackend, tx transaction.Transaction) (err error) {
	// check, source exists
	var ba *block.BlockAccount
	if ba, err = block.GetBlockAccount(st, tx.B.Source); err != nil {
		err = errors.ErrorBlockAccountDoesNotExists
		return
	}

	// check, sequenceID is based on latest sequenceID
	if !tx.IsValidSequenceID(ba.SequenceID) {
		err = errors.ErrorTransactionInvalidSequenceID
		return
	}

	// get the balance at sequenceID
	var bac block.BlockAccountSequenceID
	bac, err = block.GetBlockAccountSequenceID(st, tx.B.Source, tx.B.SequenceID)
	if err != nil {
		return
	}

	totalAmount := tx.TotalAmount(true)

	// check, have enough balance at sequenceID
	if bac.Balance < totalAmount {
		err = errors.ErrorTransactionExcessAbilityToPay
		return
	}

	for _, op := range tx.B.Operations {
		if err = ValidateOp(st, ba, op); err != nil {
			return
		}
	}

	return
}

func ValidateOp(st *storage.LevelDBBackend, source *block.BlockAccount, op transaction.Operation) (err error) {
	switch op.H.Type {
	case transaction.OperationCreateAccount:
		var ok bool
		var casted transaction.OperationBodyCreateAccount
		if casted, ok = op.B.(transaction.OperationBodyCreateAccount); !ok {
			err = errors.ErrorTypeOperationBodyNotMatched
			return
		}
		var exists bool
		if exists, err = block.ExistBlockAccount(st, op.B.(transaction.OperationBodyCreateAccount).Target); err == nil && exists {
			err = errors.ErrorBlockAccountAlreadyExists
			return
		}
		// If it's a frozen account we check that only whole units are frozen
		if casted.Linked != "" && (casted.Amount%common.Unit) != 0 {
			return errors.ErrorFrozenAccountCreationWholeUnit // FIXME
		}
	case transaction.OperationPayment:
		var ok bool
		var casted transaction.OperationBodyPayment
		if casted, ok = op.B.(transaction.OperationBodyPayment); !ok {
			err = errors.ErrorTypeOperationBodyNotMatched
			return
		}
		var taccount *block.BlockAccount
		if taccount, err = block.GetBlockAccount(st, casted.Target); err != nil {
			err = errors.ErrorBlockAccountDoesNotExists
			return
		}
		// If it's a frozen account, it cannot receive payment
		if taccount.Linked != "" {
			err = errors.ErrorFrozenAccountNoDeposit
			return
		}
		// If it's a frozen account, everything must be withdrawn
		if source.Linked != "" {
			var expected common.Amount
			expected, err = source.Balance.Sub(common.BaseFee)
			if casted.Amount != expected {
				err = errors.ErrorFrozenAccountMustWithdrawEverything
				return
			}
		}
	default:
		err = errors.ErrorUnknownOperationType
		return
	}
	return
}
