package transaction

import (
	"encoding/json"
	"fmt"

	"github.com/stellar/go/keypair"

	"boscoin.io/sebak/lib/common"
)

type OperationBodyUnfreezeRequest struct {
	OperationBodyImpl
	Target string        `json:"target"`
	Amount common.Amount `json:"amount"`
}

func NewOperationBodyUnfreezeRequest(target string, amount common.Amount) OperationBodyUnfreezeRequest {
	return OperationBodyUnfreezeRequest{
		Target: target,
		Amount: amount,
	}
}

func (o OperationBodyUnfreezeRequest) Serialize() (encoded []byte, err error) {
	encoded, err = json.Marshal(o)
	return
}

func (o OperationBodyUnfreezeRequest) IsWellFormed([]byte) (err error) {
	if _, err = keypair.Parse(o.Target); err != nil {
		return
	}

	if int64(o.Amount) < 1 {
		err = fmt.Errorf("invalid `Amount`")
		return
	}
	return
}

func (o OperationBodyUnfreezeRequest) TargetAddress() string {
	return o.Target
}

func (o OperationBodyUnfreezeRequest) GetAmount() common.Amount {
	return o.Amount
}
