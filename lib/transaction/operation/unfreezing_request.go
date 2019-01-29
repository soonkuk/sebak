package operation

import (
	"boscoin.io/sebak/lib/common"
)

type UnfreezeRequest struct {
	Threshold OperationThreshold
}

func NewUnfreezeRequest() UnfreezeRequest {
	return UnfreezeRequest{
		Threshold: Medium,
	}
}

func (o UnfreezeRequest) IsWellFormed(common.Config) (err error) {
	return
}

func (o UnfreezeRequest) HasFee() bool {
	return false
}

func (o UnfreezeRequest) HasThreshold() bool {
	return true
}

func (o UnfreezeRequest) GetThreshold() OperationThreshold {
	return o.Threshold
}
