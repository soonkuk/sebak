package operation

import (
	"boscoin.io/sebak/lib/common"
	"boscoin.io/sebak/lib/common/keypair"
	"boscoin.io/sebak/lib/errors"
)

type CreateAccount struct {
	Target    string             `json:"target"`
	Amount    common.Amount      `json:"amount"`
	Linked    string             `json:"linked,omitempty"`
	Threshold OperationThreshold `json:"threshold"`
}

func NewCreateAccount(target string, amount common.Amount, linked string) CreateAccount {
	return CreateAccount{
		Target:    target,
		Amount:    amount,
		Linked:    linked,
		Threshold: Medium,
	}
}

// Implement transaction/operation : IsWellFormed
func (o CreateAccount) IsWellFormed(common.Config) (err error) {
	if _, err = keypair.Parse(o.Target); err != nil {
		return
	}

	if int64(o.Amount) < 1 {
		err = errors.OperationAmountUnderflow
		return
	}

	if o.Amount < common.BaseReserve {
		err = errors.InsufficientAmountNewAccount
		return
	}

	return
}

func (o CreateAccount) TargetAddress() string {
	return o.Target
}

func (o CreateAccount) GetAmount() common.Amount {
	return o.Amount
}

func (o CreateAccount) isCreateFrozenAccount() bool {
	return o.Linked != ""
}

func (o CreateAccount) HasFee() bool {
	if o.isCreateFrozenAccount() {
		return false
	}
	return true
}

func (o CreateAccount) HasThreshold() bool {
	return true
}

func (o CreateAccount) GetThreshold() OperationThreshold {
	return o.Threshold
}
