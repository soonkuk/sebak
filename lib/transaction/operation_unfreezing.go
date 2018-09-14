package transaction

import (
	"encoding/json"
	"fmt"

	"github.com/stellar/go/keypair"

	"boscoin.io/sebak/lib/common"
)

type OperationBodyUnfreeze struct {
	OperationBodyImpl
	Target string        `json:"target"`
	Amount common.Amount `json:"amount"`
}

func NewOperationBodyUnfreeze(target string, amount common.Amount) OperationBodyUnfreeze {
	return OperationBodyUnfreeze{
		Target: target,
		Amount: amount,
	}
}

func (o OperationBodyUnfreeze) Serialize() (encoded []byte, err error) {
	encoded, err = json.Marshal(o)
	return
}

func (o OperationBodyUnfreeze) IsWellFormed([]byte) (err error) {
	if _, err = keypair.Parse(o.Target); err != nil {
		return
	}

	if int64(o.Amount) < 1 {
		err = fmt.Errorf("invalid `Amount`")
		return
	}

	return
}

func (o OperationBodyUnfreeze) TargetAddress() string {
	return o.Target
}

func (o OperationBodyUnfreeze) GetAmount() common.Amount {
	return o.Amount
}
