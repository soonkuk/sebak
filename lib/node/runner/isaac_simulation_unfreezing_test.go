/*
	In this file, there are unittests assume that one node receive a message from validators,
	and how the state of the node changes.
*/

package runner

import (
	"testing"

	"boscoin.io/sebak/lib/block"
	"boscoin.io/sebak/lib/common"
	"github.com/stretchr/testify/require"
)

/*
TestUnfreezingSimulation indicates the following:
	1. There are 3 nodes.
	2. The series of transaction are generated as follows, CreateAccount tx - Freezing tx - UnfreezingRequest tx - Unfreezing tx
	3. The node receives the SIGN, ACCEPT messages in order from the other two validator nodes.
	4. The node receives a ballot that exceeds the threshold, and the block is confirmed.
	5. Unfreezing tx will not be processed untill X period pass.
*/
func TestUnfreezingSimulation(t *testing.T) {
	nr, nodes, _ := createNodeRunnerForTesting(3, common.NewTestConfig(), nil)

	st := nr.storage

	proposer := nr.localNode

	// Generate create-account transaction
	tx, _, kpNewAccount := GetCreateAccountTransaction(uint64(0), uint64(500000000000))

	b1, _ := MakeConsensusAndBlock(t, tx, nr, nodes, proposer)
	require.Equal(t, b1.Height, uint64(2))

	// Generate create-frozen-account transaction
	tx2, _, kpFrozenAccount := GetFreezingTransaction(kpNewAccount, uint64(0), uint64(100000000000))

	b2, _ := MakeConsensusAndBlock(t, tx2, nr, nodes, proposer)

	ba, _ := block.GetBlockAccount(st, kpFrozenAccount.Address())

	require.Equal(t, b2.Height, uint64(3))
	require.Equal(t, uint64(ba.Balance), uint64(100000000000))

	// Generate unfreezing-request transaction
	tx3, _ := GetUnfreezingRequestTransaction(kpFrozenAccount, uint64(0))

	b3, _ := MakeConsensusAndBlock(t, tx3, nr, nodes, proposer)

	ba, _ = block.GetBlockAccount(st, kpFrozenAccount.Address())

	require.Equal(t, b3.Height, uint64(4))
	require.Equal(t, uint64(ba.Balance), uint64(100000000000))

	// Generate transaction for increasing blockheight
	tx4, _, _ := GetCreateAccountTransaction(uint64(1), uint64(1000000))

	b4, _ := MakeConsensusAndBlock(t, tx4, nr, nodes, proposer)
	require.Equal(t, b4.Height, uint64(5))

	// Generate transaction for increasing blockheight
	tx5, _, _ := GetCreateAccountTransaction(uint64(2), uint64(1000000))

	b5, _ := MakeConsensusAndBlock(t, tx5, nr, nodes, proposer)
	require.Equal(t, b5.Height, uint64(6))

	// Generate unfreezing-transaction not yet reached unfreezing blockheight
	tx6, _ := GetUnfreezingTransaction(kpFrozenAccount, kpNewAccount, uint64(1), uint64(100000000000))

	err := ValidateTx(nr.Storage(), tx6)
	require.Error(t, err)

	round := uint64(0)
	_, err = nr.proposeNewBallot(round)
	require.NoError(t, err)

	ba, _ = block.GetBlockAccount(st, kpFrozenAccount.Address())
	require.Equal(t, uint64(ba.Balance), uint64(100000000000))

	// Generate transaction for increasing blockheight
	tx7, _, _ := GetCreateAccountTransaction(uint64(3), uint64(1000000))

	b7, _ := MakeConsensusAndBlock(t, tx7, nr, nodes, proposer)
	require.Equal(t, b7.Height, uint64(7))

	tx8, _, _ := GetCreateAccountTransaction(uint64(4), uint64(1000000))

	b8, _ := MakeConsensusAndBlock(t, tx8, nr, nodes, proposer)
	require.Equal(t, b8.Height, uint64(8))

	tx9, _, _ := GetCreateAccountTransaction(uint64(5), uint64(1000000))

	b9, _ := MakeConsensusAndBlock(t, tx9, nr, nodes, proposer)
	require.Equal(t, b9.Height, uint64(9))

	// Generate unfreezing transaction
	tx10, _ := GetUnfreezingTransaction(kpFrozenAccount, kpNewAccount, uint64(1), uint64(100000000000))

	err = ValidateTx(nr.Storage(), tx10)
	require.Error(t, err)

	ba, _ = block.GetBlockAccount(st, kpFrozenAccount.Address())

	require.Equal(t, uint64(ba.Balance), uint64(100000000000))
}
