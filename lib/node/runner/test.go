package runner

import (
	"sync"
	"testing"

	"github.com/stellar/go/keypair"
	"github.com/stretchr/testify/require"

	"boscoin.io/sebak/lib/ballot"
	"boscoin.io/sebak/lib/block"
	"boscoin.io/sebak/lib/common"
	"boscoin.io/sebak/lib/consensus"
	"boscoin.io/sebak/lib/consensus/round"
	"boscoin.io/sebak/lib/network"
	"boscoin.io/sebak/lib/node"
	"boscoin.io/sebak/lib/storage"
	"boscoin.io/sebak/lib/transaction"
)

var networkID []byte = []byte("sebak-test-network")

var (
	kp           *keypair.Full
	account      *block.BlockAccount
	genesisBlock block.Block
)

func init() {
	kp, _ = keypair.Random()
}

func MakeNodeRunner() (*NodeRunner, *node.LocalNode) {
	_, n, localNode := network.CreateMemoryNetwork(nil)

	policy, _ := consensus.NewDefaultVotingThresholdPolicy(66, 66)

	connectionManager := network.NewConnectionManager(
		localNode,
		n,
		policy,
		localNode.GetValidators(),
	)
	connectionManager.SetProposerCalculator(network.NewSimpleProposerCalculator(connectionManager))

	is, _ := consensus.NewISAAC(networkID, localNode, policy, connectionManager)
	st, _ := storage.NewTestMemoryLevelDBBackend()
	conf := consensus.NewISAACConfiguration()
	nodeRunner, _ := NewNodeRunner(string(networkID), localNode, policy, n, is, st, conf)
	return nodeRunner, localNode
}

func GetTransaction(t *testing.T) (tx transaction.Transaction, txByte []byte) {
	initialBalance := common.Amount(1)
	kpNewAccount, _ := keypair.Random()
	tx = transaction.MakeTransactionCreateAccount(kp, kpNewAccount.Address(), initialBalance)
	tx.B.SequenceID = account.SequenceID
	tx.Sign(kp, networkID)

	var err error

	txByte, err = tx.Serialize()
	require.Nil(t, err)

	return
}

func GetCreateAccountTransaction(t *testing.T, sequenceID uint64) (tx transaction.Transaction, txByte []byte, kpNewAccount *keypair.Full) {
	initialBalance := common.Amount(1000000000000)
	kpNewAccount, _ = keypair.Random()
	tx = transaction.MakeTransactionCreateAccount(kp, kpNewAccount.Address(), initialBalance)
	tx.B.SequenceID = sequenceID
	tx.Sign(kp, networkID)
	var err error

	txByte, err = tx.Serialize()
	require.Nil(t, err)

	return
}

func GetFreezingTransaction(t *testing.T, kpSource *keypair.Full, sequenceID uint64) (tx transaction.Transaction, txByte []byte, kpNewAccount *keypair.Full) {
	initialBalance := common.Amount(100000000000)
	kpNewAccount, _ = keypair.Random()

	tx = transaction.MakeTransactionCreateFrozenAccount(kpSource, kpNewAccount.Address(), initialBalance, kpSource.Address())
	tx.B.SequenceID = sequenceID
	tx.Sign(kpSource, networkID)

	var err error

	txByte, err = tx.Serialize()
	require.Nil(t, err)

	return
}

func GetUnfreezingRequestTransaction(t *testing.T, kpSource *keypair.Full, kpTarget *keypair.Full, sequenceID uint64) (tx transaction.Transaction, txByte []byte) {
	unfreezingAmount := common.Amount(99999990000)

	tx = transaction.MakeTransactionUnfreezingRequest(kpSource, kpTarget.Address(), unfreezingAmount)
	tx.B.SequenceID = sequenceID
	tx.Sign(kpSource, networkID)

	var err error

	txByte, err = tx.Serialize()
	require.Nil(t, err)

	return
}

func GetUnfreezingTransaction(t *testing.T, kpSource *keypair.Full, kpTarget *keypair.Full, sequenceID uint64) (tx transaction.Transaction, txByte []byte) {
	unfreezingAmount := common.Amount(99999980000)

	tx = transaction.MakeTransactionUnfreezing(kpSource, kpTarget.Address(), unfreezingAmount)
	tx.B.SequenceID = sequenceID
	tx.Sign(kpSource, networkID)

	var err error

	txByte, err = tx.Serialize()
	require.Nil(t, err)

	return
}

func TestGenerateNewSequenceID() uint64 {
	return 0
}

type SelfProposerCalculator struct {
	nodeRunner *NodeRunner
}

func (c SelfProposerCalculator) Calculate(_ uint64, _ uint64) string {
	return c.nodeRunner.localNode.Address()
}

type TheOtherProposerCalculator struct {
	nodeRunner *NodeRunner
}

func (c TheOtherProposerCalculator) Calculate(_ uint64, _ uint64) string {
	for _, v := range c.nodeRunner.ConnectionManager().AllValidators() {
		if v != c.nodeRunner.localNode.Address() {
			return v
		}
	}
	panic("There is no the other validators")
}

type SelfProposerThenNotProposer struct {
	nodeRunner *NodeRunner
}

func (c *SelfProposerThenNotProposer) Calculate(blockHeight uint64, roundNumber uint64) string {
	if blockHeight < 2 && roundNumber == 0 {
		return c.nodeRunner.localNode.Address()
	} else {
		for _, v := range c.nodeRunner.ConnectionManager().AllValidators() {
			if v != c.nodeRunner.localNode.Address() {
				return v
			}
		}
		panic("There is no the other validators")
	}
}

func GenerateBallot(t *testing.T, proposer *node.LocalNode, round round.Round, tx transaction.Transaction, ballotState ballot.State, sender *node.LocalNode) *ballot.Ballot {
	b := ballot.NewBallot(proposer, round, []string{tx.GetHash()})
	b.SetVote(ballot.StateINIT, ballot.VotingYES)
	b.Sign(proposer.Keypair(), networkID)

	b.SetSource(sender.Address())
	b.SetVote(ballotState, ballot.VotingYES)
	b.Sign(sender.Keypair(), networkID)

	err := b.IsWellFormed(networkID)
	require.Nil(t, err)

	return b
}

func GenerateEmptyTxBallot(t *testing.T, proposer *node.LocalNode, round round.Round, ballotState ballot.State, sender *node.LocalNode) *ballot.Ballot {
	b := ballot.NewBallot(proposer, round, []string{})
	b.SetVote(ballot.StateINIT, ballot.VotingYES)
	b.Sign(proposer.Keypair(), networkID)

	b.SetSource(sender.Address())
	b.SetVote(ballotState, ballot.VotingYES)
	b.Sign(sender.Keypair(), networkID)

	err := b.IsWellFormed(networkID)
	require.Nil(t, err)

	return b
}

func ReceiveBallot(t *testing.T, nodeRunner *NodeRunner, ballot *ballot.Ballot) error {
	data, err := ballot.Serialize()
	require.Nil(t, err)

	ballotMessage := common.NetworkMessage{Type: common.BallotMessage, Data: data}
	err = nodeRunner.handleBallotMessage(ballotMessage)
	return err
}

type TestBroadcaster struct {
	sync.RWMutex
	messages []common.Message
	recv     chan struct{}
}

func NewTestBroadcaster(r chan struct{}) *TestBroadcaster {
	p := &TestBroadcaster{}
	p.messages = []common.Message{}
	p.recv = r
	return p
}

func (b *TestBroadcaster) Broadcast(message common.Message, _ func(string, error)) {
	b.Lock()
	defer b.Unlock()
	b.messages = append(b.messages, message)
	if b.recv != nil {
		b.recv <- struct{}{}
	}
	return
}

func (b *TestBroadcaster) Messages() []common.Message {
	b.RLock()
	defer b.RUnlock()
	messages := make([]common.Message, len(b.messages))
	copy(messages, b.messages)
	return messages
}

func MakeConsensusAndBlock(t *testing.T, tx transaction.Transaction, txByte []byte, nodeRunner *NodeRunner, nodeRunners []*NodeRunner, proposer *node.LocalNode) (block block.Block, err error) {

	message := common.NetworkMessage{Type: common.TransactionMessage, Data: txByte}
	err = nodeRunner.handleTransaction(message)
	if err != nil {
		return
	}

	require.Nil(t, err)
	require.True(t, nodeRunner.Consensus().TransactionPool.Has(tx.GetHash()))

	// Generate proposed ballot in nodeRunner
	roundNumber := uint64(0)
	err = nodeRunner.proposeNewBallot(roundNumber)
	if err != nil {
		return
	}

	require.Nil(t, err)

	b := nodeRunner.Consensus().LatestConfirmedBlock()

	round := round.Round{
		Number:      roundNumber,
		BlockHeight: b.Height,
		BlockHash:   b.Hash,
		TotalTxs:    b.TotalTxs,
	}
	runningRounds := nodeRunner.Consensus().RunningRounds

	// Check that the transaction is in RunningRounds
	rr := runningRounds[round.Hash()]
	txHashs := rr.Transactions[proposer.Address()]
	require.Equal(t, tx.GetHash(), txHashs[0])

	ballotSIGN1 := GenerateBallot(t, proposer, round, tx, ballot.StateSIGN, nodeRunners[1].localNode)

	err = ReceiveBallot(t, nodeRunner, ballotSIGN1)
	if err != nil {
		return
	}

	require.Nil(t, err)

	ballotSIGN2 := GenerateBallot(t, proposer, round, tx, ballot.StateSIGN, nodeRunners[2].localNode)
	err = ReceiveBallot(t, nodeRunner, ballotSIGN2)
	if err != nil {
		return
	}

	require.Nil(t, err)

	require.Equal(t, 2, len(rr.Voted[proposer.Address()].GetResult(ballot.StateSIGN)))

	ballotACCEPT1 := GenerateBallot(t, proposer, round, tx, ballot.StateACCEPT, nodeRunners[1].localNode)
	err = ReceiveBallot(t, nodeRunner, ballotACCEPT1)
	if err != nil {
		return
	}

	require.Nil(t, err)

	ballotACCEPT2 := GenerateBallot(t, proposer, round, tx, ballot.StateACCEPT, nodeRunners[2].localNode)
	err = ReceiveBallot(t, nodeRunner, ballotACCEPT2)

	_, ok := err.(CheckerStopCloseConsensus)
	require.True(t, ok)

	require.Equal(t, 2, len(rr.Voted[proposer.Address()].GetResult(ballot.StateACCEPT)))

	block = nodeRunner.Consensus().LatestConfirmedBlock()

	require.Equal(t, proposer.Address(), block.Proposer)
	require.Equal(t, 1, len(block.Transactions))
	require.Equal(t, tx.GetHash(), block.Transactions[0])
	return
}
