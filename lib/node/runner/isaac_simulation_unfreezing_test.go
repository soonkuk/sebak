/*
	In this file, there are unittests assume that one node receive a message from validators,
	and how the state of the node changes.
*/

package runner

import (
	"testing"

	"boscoin.io/sebak/lib/block"
	"boscoin.io/sebak/lib/consensus"
	"boscoin.io/sebak/lib/error"
	"github.com/stretchr/testify/require"
)

/*
TestUnfreezingSimulation indicates the following:
	1. There are 3 nodes.
	2. The series of transaction are generated as follows, CreateAccount tx - Freezing tx - UnfreezingRequest tx - Unfreezing tx
	3. The node receives the SIGN, ACCEPT messages in order from the other two validator nodes.
	4. The node receives a ballot that exceeds the threshold, and the block is confirmed.
	5. Unfreezing tx will be processed after X period.
*/
func TestUnfreezingSimulation(t *testing.T) {
	nodeRunners := createTestNodeRunner(3, consensus.NewISAACConfiguration())
	nodeRunner := nodeRunners[0]

	st := nodeRunner.storage

	// `nodeRunner` is proposer's runner
	nodeRunner.Consensus().ConnectionManager().SetProposerCalculator(SelfProposerCalculator{
		nodeRunner: nodeRunner,
	})
	proposer := nodeRunner.localNode
	nodeRunner.Consensus().SetLatestConsensusedBlock(genesisBlock)

	// Generate create-account transaction
	tx, txByte, kpNewAccount := GetCreateAccountTransaction(t, uint64(0))

	b1, _ := MakeConsensusAndBlock(t, tx, txByte, nodeRunner, nodeRunners, proposer)
	require.Equal(t, b1.Height, uint64(2))

	// Generate create-frozen-account transaction
	tx2, txByte2, kpFrozenAccount := GetFreezingTransaction(t, kpNewAccount, uint64(0))

	b2, _ := MakeConsensusAndBlock(t, tx2, txByte2, nodeRunner, nodeRunners, proposer)

	ba, _ := block.GetBlockAccount(st, kpFrozenAccount.Address())

	require.Equal(t, b2.Height, uint64(3))
	require.Equal(t, uint64(ba.Balance), uint64(100000000000))

	// Generate unfreezing-request transaction
	tx3, txByte3 := GetUnfreezingRequestTransaction(t, kpFrozenAccount, kpNewAccount, uint64(0))

	b3, _ := MakeConsensusAndBlock(t, tx3, txByte3, nodeRunner, nodeRunners, proposer)

	ba, _ = block.GetBlockAccount(st, kpFrozenAccount.Address())

	require.Equal(t, b3.Height, uint64(4))
	require.Equal(t, uint64(ba.Balance), uint64(99999990000))

	// Generate transaction for increasing blockheight
	tx4, txByte4, _ := GetCreateAccountTransaction(t, uint64(1))

	b4, _ := MakeConsensusAndBlock(t, tx4, txByte4, nodeRunner, nodeRunners, proposer)
	require.Equal(t, b4.Height, uint64(5))

	// Generate transaction for increasing blockheight
	tx5, txByte5, _ := GetCreateAccountTransaction(t, uint64(2))

	b5, _ := MakeConsensusAndBlock(t, tx5, txByte5, nodeRunner, nodeRunners, proposer)
	require.Equal(t, b5.Height, uint64(6))

	// Generate unfreezing-transaction not yet reached unfreezing blockheight
	tx6, txByte6 := GetUnfreezingTransaction(t, kpFrozenAccount, kpNewAccount, uint64(1))

	_, err := MakeConsensusAndBlock(t, tx6, txByte6, nodeRunner, nodeRunners, proposer)
	require.Error(t, err, errors.ErrorUnfreezingNotReachedExpiration)

	ba, _ = block.GetBlockAccount(st, kpFrozenAccount.Address())
	require.Equal(t, uint64(ba.Balance), uint64(99999990000))

	// Generate transaction for increasing blockheight
	tx7, txByte7, _ := GetCreateAccountTransaction(t, uint64(3))

	b7, _ := MakeConsensusAndBlock(t, tx7, txByte7, nodeRunner, nodeRunners, proposer)
	require.Equal(t, b7.Height, uint64(7))

	tx8, txByte8, _ := GetCreateAccountTransaction(t, uint64(4))

	b8, _ := MakeConsensusAndBlock(t, tx8, txByte8, nodeRunner, nodeRunners, proposer)
	require.Equal(t, b8.Height, uint64(8))

	tx9, txByte9, _ := GetCreateAccountTransaction(t, uint64(5))

	b9, _ := MakeConsensusAndBlock(t, tx9, txByte9, nodeRunner, nodeRunners, proposer)
	require.Equal(t, b9.Height, uint64(9))

	// Generate unfreezing transaction
	tx10, txByte10 := GetUnfreezingTransaction(t, kpFrozenAccount, kpNewAccount, uint64(1))

	b10, _ := MakeConsensusAndBlock(t, tx10, txByte10, nodeRunner, nodeRunners, proposer)
	ba, _ = block.GetBlockAccount(st, kpFrozenAccount.Address())

	require.Equal(t, b10.Height, uint64(10))
	require.Equal(t, uint64(ba.Balance), uint64(0))
}
