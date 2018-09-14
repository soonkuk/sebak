package transaction

import (
	"encoding/json"

	"boscoin.io/sebak/lib/storage"
)

type OperationBodyVotingResult struct {
	OperationBodyImpl
	Proposal     string
	BallotStamps map[string]string
	Voters       map[string]interface{}
	Results      map[string]uint
}

/* func NewOperationBodyVotingResult(hash string, ballotstamps string, voters string, result string) OperationBodyVotingResult {
	return OperationBodyVotingResult{
		Proposal:     hash,
		BallotStamps: string,
		Voters:       string,
		Results:      string,
	}
}
*/

func (o OperationBodyVotingResult) Serialize() (encoded []byte, err error) {
	encoded, err = json.Marshal(o)
	return
}

func (o OperationBodyVotingResult) IsWellFormed([]byte) (err error) {

	return
}

func (o OperationBodyVotingResult) Validate(st *storage.LevelDBBackend) (err error) {

	return
}

func (o OperationBodyVotingResult) BalloStampsHash() string {
	return ""
}
