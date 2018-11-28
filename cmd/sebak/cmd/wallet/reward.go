//
// Implement CLI for payment, account creation, and freezing requests
//
package wallet

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"

	"github.com/spf13/cobra"

	cmdcommon "boscoin.io/sebak/cmd/sebak/common"
	"boscoin.io/sebak/lib/block"
	"boscoin.io/sebak/lib/common"
	"boscoin.io/sebak/lib/common/keypair"
	"boscoin.io/sebak/lib/network"
	"boscoin.io/sebak/lib/transaction"
	"boscoin.io/sebak/lib/transaction/operation"
)

const (
	defaultRewardPeriodBlocks uint64 = 500
	defaultRewardAmount common.Amount = 1200
)

var (
	RewardCmd *cobra.Command
)

func init() {
	RewardCmd = &cobra.Command{
		Use:   "reward <sender secret seed> <start blockheight> <round>",
		Short: "Send <amount> BOScoin of reward for the period of rounds from <start blockheight> to membership accounts",
		Args:  cobra.ExactArgs(4),
		Run: func(c *cobra.Command, args []string) {
			var err error
			var sender keypair.KP
			var start uint64
			var round uint64
			var endpoint *common.Endpoint

			// Sender's secret seed
			if sender, err = keypair.Parse(args[0]); err != nil {
				cmdcommon.PrintFlagsError(c, "<sender secret seed>", err)
			} else if _, ok := sender.(*keypair.Full); !ok {
				cmdcommon.PrintFlagsError(c, "<sender secret seed>", fmt.Errorf("Provided key is an address, not a secret seed"))
			}
			// Start blockheight
			if start, err = strconv.ParseUint(args[1], 10, 64); err != nil {
				cmdcommon.PrintFlagsError(c, "<start blockheight>", err)
			}
			// Number of rounds during reward period.
			if round, err = strconv.ParseUint(args[2], 10, 64); err != nil {
				cmdcommon.PrintFlagsError(c, "<round>", err)
			}

			// Check a network ID was provided
			if len(flagNetworkID) == 0 {
				cmdcommon.PrintFlagsError(c, "--network-id", fmt.Errorf("A --network-id needs to be provided"))
			}

			if endpoint, err = common.ParseEndpoint(flagEndpoint); err != nil {
				cmdcommon.PrintFlagsError(c, "--endpoint", err)
			}

			// TODO: Validate input transaction (does the sender have enough money?)

			// At the moment this is a rather crude implementation: There is no support for pooling of transaction,
			// 1 operation == 1 transaction
			// var tx transaction.Transaction
			var connection *common.HTTP2Client
			var senderAccount block.BlockAccount

			// Keep-alive ignores timeout/idle timeout
			if connection, err = common.NewHTTP2Client(0, 0, true); err != nil {
				log.Fatal("Error while creating network client: ", err)
				os.Exit(1)
			}
			client := network.NewHTTP2NetworkClient(endpoint, connection)

			if senderAccount, err = getAccountDetails(client, sender); err != nil {
				log.Fatal("Could not fetch sender account: ", err)
				os.Exit(1)
			}

			if flagVerbose == true {
				fmt.Println("Account before transaction: ", senderAccount)
			}

			// Check that account's balance is enough before sending the transaction
			{
				totalAmount := defaultRewardAmount * round
				_, err = senderAccount.GetBalance().Sub(totalAmount)
				if err != nil {
					fmt.Printf("Attempting to draft %v GON, but sender account only have %v GON\n",
						totalAmount, senderAccount.GetBalance())
					os.Exit(1)
				}
			}

			// TODO: Get Premembership record
			membership := &MembershipReward{
				PreMembershipRecord:  make(map[string][]uint64),
				MembershipRounds:     make(map[string][]bool),
				FrozenAccountsRecord: make(map[string][]FrozenAccount),
				FrozenStateRounds:    make(map[string][]uint64),
				RewardRounds:         make(map[string][]common.Amount),
				MemberCountRounds: []uint64{},
				Amount:               amount,
				Round:                round,
				BlockHeight:          start,
			}

			list1 := []uint64{100, 300}
			list2 := []uint64{800, 900}
			membership.PreMembershipRecord["GDEPYGGALPJ5HENXCNOQJPPDOQMA2YAXPERZ4XEAKVFFJJEVP4ZBK6QI"] = list1
			membership.PreMembershipRecord["GCGEDYCWXWHZLLGNB6TFROPEVULMCOTYQMZVGVWCD5NS4VA4BKV2XC3C"] = list2

			for address := range membership.PreMembershipRecord {
				var fa []FrozenAccount
				if fa, err = getFrozenAccountsDetails(client, address); err != nil {
					fmt.Println(err.Error())
					fmt.Println("Attempting to get frozen accounts, but error occurs")
				}
				membership.FrozenAccountsRecord[address] = fa
			}
			for address := range membership.PreMembershipRecord {
				var current bool
				var next bool
				var r uint64
				current = true
				for r = 0; r < membership.Round; r++ {
					next = membership.IsMember(address, r, current)
					current = next
					membership.FrozenStateRounds[address] = append(membership.FrozenStateRounds[address], uint64(0))
					membership.IsFrozen(address, r)
			}
			membership.CalcMember()
			
			membership.RewardRounds[address] = append(membership.RewardRounds[address], common.Amount(0))
			membership.Calc(address, round)
		}
			fmt.Println(membership.MembershipRounds)
			fmt.Println(membership.FrozenStateRounds)
			fmt.Println(membership.RewardRounds)

			// TODO: Validate that the account doesn't already exists
			// tx = makeTransactionRewardPayment(sender, senderAccount.SequenceID, ops...)

			// tx.Sign(sender, []byte(flagNetworkID))

			// Send request
			/* var retbody []byte
			if flagDry == true || flagVerbose == true {
				fmt.Println(tx)
			}
			if flagDry == false {
				if retbody, err = client.SendTransaction(tx); err != nil {
					log.Fatal("Network error: ", err, " body: ", string(retbody))
					os.Exit(1)
				}
			}
			if flagVerbose == true {
				time.Sleep(5 * time.Second)
				if recv, err := getSenderDetails(client, receiver); err != nil {
					fmt.Println("Account ", receiver.Address(), " did not appear after 5 seconds")
				} else {
					fmt.Println("Receiver account after 5 seconds: ", recv)
				}
			} */
		},
	}
	RewardCmd.Flags().StringVar(&flagEndpoint, "endpoint", flagEndpoint, "endpoint to send the transaction to (https / memory address)")
	RewardCmd.Flags().StringVar(&flagNetworkID, "network-id", flagNetworkID, "network id")
	RewardCmd.Flags().BoolVar(&flagDry, "dry-run", flagDry, "Print the transaction instead of sending it")
	RewardCmd.Flags().BoolVar(&flagVerbose, "verbose", flagVerbose, "Print extra data (transaction sent, before/after balance...)")
}

///
/// Make a Reward payment transaction.
///
/// Params:
///   kpSource = Sender's keypair.Full seed/address
///   seqid    = SequenceID of the last transaction
///   ops       = Payment operations
///
/// Returns:
///   `sebak.Transaction` = The generated `Transaction` creating the account
///
func makeTransactionRewardPayment(kpSource keypair.KP, seqid uint64, ops ...operation.Operation) transaction.Transaction {

	txBody := transaction.Body{
		Source:     kpSource.Address(),
		Fee:        common.BaseFee,
		SequenceID: seqid,
		Operations: ops,
	}

	tx := transaction.Transaction{
		H: transaction.Header{
			Version: common.TransactionVersionV1,
			Created: common.NowISO8601(),
			Hash:    txBody.MakeHashString(),
		},
		B: txBody,
	}

	return tx
}

func getAccountDetails(conn *network.HTTP2NetworkClient, sender keypair.KP) (block.BlockAccount, error) {
	var ba block.BlockAccount
	var err error
	var retBody []byte

	//response, err = c.client.Post(u.String(), body, headers)
	if retBody, err = conn.Get("/api/v1/accounts/" + sender.Address()); err != nil {
		return ba, err
	}

	err = json.Unmarshal(retBody, &ba)
	fmt.Println(ba)
	return ba, err
}

func getFrozenAccountsDetails(conn *network.HTTP2NetworkClient, address string) (fa []FrozenAccount, err error) {
	var retBody []byte
	var embedded map[string]interface{}
	var record interface{}
	var list []interface{}
	var exists bool
	var ok bool
	var f FrozenAccount

	//response, err = c.client.Post(u.String(), body, headers)
	if retBody, err = conn.Get("api/v1/accounts/" + address + "/frozen-accounts"); err != nil {
		return
	}

	data := make(map[string]interface{})
	if err = json.Unmarshal(retBody, &data); err != nil {
		return
	}

	if embedded, ok = data["_embedded"].(map[string]interface{}); !ok {
		fmt.Println("Frozen account response Type assertion Error")
		os.Exit(1)
	}
	if record, exists = embedded["records"]; !exists {
		fmt.Println("Frozen account response Key Error")
		os.Exit(1)
	}
	if record == nil {
		return
	}
	if list, ok = record.([]interface{}); !ok {
		fmt.Println("Frozen account response Type assertion Error")
	}
	for i := 0; i < len(list); i++ {
		bytes, _ := json.Marshal(list[i])
		if err = json.Unmarshal(bytes, &f); err != nil {
			return
		}
		fa = append(fa, f)
	}
	return
}

type MembershipReward struct {
	PreMembershipRecord  map[ /* membership account address */ string][]uint64
	MembershipRounds     map[ /* membership account address */ string][]bool
	FrozenAccountsRecord map[ /* membership account address */ string][]FrozenAccount
	FrozenStateRounds    map[ /* membership account address */ string][]uint64
	MemberCountRounds []uint64
	RewardRounds         map[ /* membership account address */ string][]common.Amount
	Amount               common.Amount // Total reward amount
	Round                uint64        // Count of round
	BlockHeight          uint64        // Blockheight of starting checkpoint
}

type FrozenAccount struct {
	CreateBlockHeight     uint64        `json:"create_block_height"`
	Linked                string        `json:"linked"`
	State                 string        `json:"state"`
	Address               string        `json:"address"`
	Amount                common.Amount `json:"amount"`
	UnfreezingBlockHeight uint64        `json:"unfreezing_block_height"`
}

func (m *MembershipReward) IsMember(address string, round uint64, frozen bool) (nextStatus bool) {
	var list []uint64
	var currentStatus bool
	list = m.PreMembershipRecord[address]
	startCheckpoint := m.BlockHeight + round*defaultRewardPeriodBlocks
	endCheckpoint := startCheckpoint + defaultRewardPeriodBlocks
	// variable `frozen` shows the freezing status of starting checkpoint.
	// If `frozen` is true, it means that a frozen account is in frozen status at starting checkpoint.
	// It there is a blockheight record between starting checkpoint and endng checkpoint of 17280 blocks,
	// it means that there was a change in status.
	// Start(Frozen status) -> blockheight record(change to unfrozen) -> End(Unfrozen status)
	// Start(Frozen status) -> no record(no change) -> End(Frozen status)
	// Start(Unfrozen) -> blockheight record(frozen) -> End(Frozen status)
	// Start(Unfrozen) -> no record(no change) -> End(Unfrozen status)
	currentStatus = frozen
	nextStatus = frozen
	for _, v := range list {
		if currentStatus == false {
			if (v >= startCheckpoint) && (v < endCheckpoint) {
				nextStatus = !nextStatus
				continue
			}
			continue
		}
		if v >= endCheckpoint || v < startCheckpoint {
			continue
		}
		currentStatus = !currentStatus
		nextStatus = !nextStatus
	}
	m.MembershipRounds[address] = append(m.MembershipRounds[address], currentStatus)
	return
}

func (m *MembershipReward) IsFrozen(address string, round uint64) {
	startCheckpoint := m.BlockHeight + round*defaultRewardPeriodBlocks
	endCheckpoint := startCheckpoint + defaultRewardPeriodBlocks
	for _, fa := range m.FrozenAccountsRecord[address] {
		if fa.UnfreezingBlockHeight == 0 {
			if fa.CreateBlockHeight < startCheckpoint {
				m.FrozenStateRounds[address][round]++
				continue
			}
		} else {
			if fa.CreateBlockHeight < startCheckpoint && fa.UnfreezingBlockHeight+common.UnfreezingPeriod >= endCheckpoint {
				m.FrozenStateRounds[address][round]++
				continue
			}
		}
	}
	return
}

func (m *MembershipReward) CalcMember() {
	for address, v:= range m.MembershipRounds {
		for round, b := range v {
			if b && m.FrozenStateRounds[address][round] > 0{
				m.MemberCountRounds[address] = m.

			}
		} 
			
		}
	}
}

func (m *MembershipReward) Calc(address string, round uint64) {
	if m.MembershipRounds[address][round] {
		m.RewardRounds[address][round] = 
	}
}
