//
// Struct that bridges together components of a node
//
// NodeRunner bridges together the connection, storage and `LocalNode`.
// In this regard, it can be seen as a single node, and is used as such
// in unit tests.
//
package runner

import (
	"net/http"
	"time"

	logging "github.com/inconshreveable/log15"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"boscoin.io/sebak/lib/ballot"
	"boscoin.io/sebak/lib/block"
	"boscoin.io/sebak/lib/common"
	"boscoin.io/sebak/lib/consensus"
	"boscoin.io/sebak/lib/consensus/round"
	"boscoin.io/sebak/lib/error"
	"boscoin.io/sebak/lib/network"
	"boscoin.io/sebak/lib/network/api"
	"boscoin.io/sebak/lib/node"
	"boscoin.io/sebak/lib/storage"
	"boscoin.io/sebak/lib/transaction"
)

var DefaultHandleBaseBallotCheckerFuncs = []common.CheckerFunc{
	BallotUnmarshal,
	BallotNotFromKnownValidators,
	BallotAlreadyFinished,
}

var DefaultHandleINITBallotCheckerFuncs = []common.CheckerFunc{
	BallotAlreadyVoted,
	BallotVote,
	BallotIsSameProposer,
	BallotValidateOperationBodyCollectTxFee,
	BallotValidateOperationBodyInflation,
	BallotGetMissingTransaction,
	INITBallotValidateTransactions,
	SIGNBallotBroadcast,
	TransitStateToSIGN,
}

var DefaultHandleSIGNBallotCheckerFuncs = []common.CheckerFunc{
	BallotAlreadyVoted,
	BallotVote,
	BallotIsSameProposer,
	BallotCheckResult,
	ACCEPTBallotBroadcast,
	TransitStateToACCEPT,
}

var DefaultHandleACCEPTBallotCheckerFuncs = []common.CheckerFunc{
	BallotAlreadyVoted,
	BallotVote,
	BallotIsSameProposer,
	BallotCheckResult,
	FinishedBallotStore,
}

type NodeRunner struct {
	networkID         []byte
	localNode         *node.LocalNode
	policy            ballot.VotingThresholdPolicy
	network           network.Network
	consensus         *consensus.ISAAC
	connectionManager network.ConnectionManager
	storage           *storage.LevelDBBackend
	isaacStateManager *ISAACStateManager

	handleBaseBallotCheckerFuncs   []common.CheckerFunc
	handleINITBallotCheckerFuncs   []common.CheckerFunc
	handleSIGNBallotCheckerFuncs   []common.CheckerFunc
	handleACCEPTBallotCheckerFuncs []common.CheckerFunc

	handleBallotCheckerDeferFunc common.CheckerDeferFunc

	log logging.Logger

	CommonAccountAddress string
	InitialBalance       common.Amount

	Conf common.Config
}

func NewNodeRunner(
	networkID string,
	localNode *node.LocalNode,
	policy ballot.VotingThresholdPolicy,
	n network.Network,
	c *consensus.ISAAC,
	storage *storage.LevelDBBackend,
	conf common.Config,
) (nr *NodeRunner, err error) {
	nr = &NodeRunner{
		networkID: []byte(networkID),
		localNode: localNode,
		policy:    policy,
		network:   n,
		consensus: c,
		storage:   storage,
		log:       log.New(logging.Ctx{"node": localNode.Alias()}),
		Conf:      conf,
	}
	nr.isaacStateManager = NewISAACStateManager(nr, conf)

	nr.policy.SetValidators(len(nr.localNode.GetValidators()))

	nr.connectionManager = c.ConnectionManager()
	nr.network.AddWatcher(nr.connectionManager.ConnectionWatcher)

	nr.SetHandleBaseBallotCheckerFuncs(DefaultHandleBaseBallotCheckerFuncs...)
	nr.SetHandleINITBallotCheckerFuncs(DefaultHandleINITBallotCheckerFuncs...)
	nr.SetHandleSIGNBallotCheckerFuncs(DefaultHandleSIGNBallotCheckerFuncs...)
	nr.SetHandleACCEPTBallotCheckerFuncs(DefaultHandleACCEPTBallotCheckerFuncs...)

	{
		// find common account
		var commonAccount *block.BlockAccount
		if commonAccount, err = GetCommonAccount(nr.storage); err != nil {
			return
		}
		nr.CommonAccountAddress = commonAccount.Address
		nr.log.Debug("common account found", "address", nr.CommonAccountAddress)

		// get the initial balance of geness account
		if nr.InitialBalance, err = GetGenesisBalance(nr.storage); err != nil {
			return
		}
		nr.log.Debug("initial balance found", "amount", nr.InitialBalance)
		nr.InitialBalance.Invariant()
	}

	return
}

func (nr *NodeRunner) Ready() {
	// node handlers
	nodeHandler := NewNetworkHandlerNode(
		nr.localNode,
		nr.network,
		nr.storage,
		nr.consensus,
		network.UrlPathPrefixNode,
		nr.Conf,
	)

	nr.network.AddHandler(nodeHandler.HandlerURLPattern(NodeInfoHandlerPattern), nodeHandler.NodeInfoHandler)
	nr.network.AddHandler(nodeHandler.HandlerURLPattern(ConnectHandlerPattern), nodeHandler.ConnectHandler).
		Methods("POST").
		Headers("Content-Type", "application/json")
	nr.network.AddHandler(nodeHandler.HandlerURLPattern(MessageHandlerPattern), nodeHandler.MessageHandler).
		Methods("POST").
		Headers("Content-Type", "application/json")
	nr.network.AddHandler(nodeHandler.HandlerURLPattern(BallotHandlerPattern), nodeHandler.BallotHandler).
		Methods("POST").
		Headers("Content-Type", "application/json")
	nr.network.AddHandler(nodeHandler.HandlerURLPattern(GetBlocksPattern), nodeHandler.GetBlocksHandler).
		Methods("GET", "POST").
		MatcherFunc(common.PostAndJSONMatcher)
	nr.network.AddHandler(nodeHandler.HandlerURLPattern(GetTransactionPattern), nodeHandler.GetNodeTransactionsHandler).
		Methods("GET", "POST").
		MatcherFunc(common.PostAndJSONMatcher)
	nr.network.AddHandler("/metrics", promhttp.Handler().ServeHTTP)

	// api handlers
	apiHandler := api.NewNetworkHandlerAPI(nr.localNode, nr.network, nr.storage, network.UrlPathPrefixAPI)
	nr.network.AddHandler(
		apiHandler.HandlerURLPattern(api.GetAccountHandlerPattern),
		apiHandler.GetAccountHandler,
	).Methods("GET")
	nr.network.AddHandler(
		apiHandler.HandlerURLPattern(api.GetAccountTransactionsHandlerPattern),
		apiHandler.GetTransactionsByAccountHandler,
	).Methods("GET")
	nr.network.AddHandler(
		apiHandler.HandlerURLPattern(api.GetAccountOperationsHandlerPattern),
		apiHandler.GetOperationsByAccountHandler,
	).Methods("GET")
	nr.network.AddHandler(
		apiHandler.HandlerURLPattern(api.GetTransactionByHashHandlerPattern),
		apiHandler.GetTransactionByHashHandler,
	).Methods("GET")
	nr.network.AddHandler(
		apiHandler.HandlerURLPattern(api.GetTransactionOperationsHandlerPattern),
		apiHandler.GetOperationsByTxHashHandler,
	).Methods("GET")

	TransactionsHandler := func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			nodeHandler.MessageHandler(w, r)
			return
		}

		apiHandler.GetTransactionsHandler(w, r)
		return
	}

	nr.network.AddHandler(
		apiHandler.HandlerURLPattern(api.GetTransactionsHandlerPattern),
		TransactionsHandler,
	).Methods("GET", "POST").MatcherFunc(common.PostAndJSONMatcher)

	nr.network.Ready()
}

func (nr *NodeRunner) Start() (err error) {
	nr.log.Debug("NodeRunner started")
	nr.Ready()

	go nr.handleMessages()
	go nr.ConnectValidators()
	go nr.InitRound()

	if err = nr.network.Start(); err != nil {
		return
	}

	return
}

func (nr *NodeRunner) Stop() {
	nr.network.Stop()
	nr.isaacStateManager.Stop()
}

func (nr *NodeRunner) Node() *node.LocalNode {
	return nr.localNode
}

func (nr *NodeRunner) NetworkID() []byte {
	return nr.networkID
}

func (nr *NodeRunner) Network() network.Network {
	return nr.network
}

func (nr *NodeRunner) Consensus() *consensus.ISAAC {
	return nr.consensus
}

func (nr *NodeRunner) ConnectionManager() network.ConnectionManager {
	return nr.connectionManager
}

func (nr *NodeRunner) Storage() *storage.LevelDBBackend {
	return nr.storage
}

func (nr *NodeRunner) Policy() ballot.VotingThresholdPolicy {
	return nr.policy
}

func (nr *NodeRunner) Log() logging.Logger {
	return nr.log
}

func (nr *NodeRunner) ISAACStateManager() *ISAACStateManager {
	return nr.isaacStateManager
}

func (nr *NodeRunner) ConnectValidators() {
	ticker := time.NewTicker(time.Millisecond * 5)
	for _ = range ticker.C {
		if !nr.network.IsReady() {
			continue
		}

		ticker.Stop()
		break
	}
	nr.log.Debug("current node is ready")
	nr.log.Debug("trying to connect to the validators", "validators", nr.localNode.GetValidators())

	nr.log.Debug("initializing connectionManager for validators")
	nr.connectionManager.Start()
}

func (nr *NodeRunner) SetHandleBaseBallotCheckerFuncs(f ...common.CheckerFunc) {
	nr.handleBaseBallotCheckerFuncs = f
}

func (nr *NodeRunner) SetHandleINITBallotCheckerFuncs(f ...common.CheckerFunc) {
	nr.handleINITBallotCheckerFuncs = f
}

func (nr *NodeRunner) SetHandleSIGNBallotCheckerFuncs(f ...common.CheckerFunc) {
	nr.handleSIGNBallotCheckerFuncs = f
}

func (nr *NodeRunner) SetHandleACCEPTBallotCheckerFuncs(f ...common.CheckerFunc) {
	nr.handleACCEPTBallotCheckerFuncs = f
}

// Read from the network channel and forwards to `handleMessage`
func (nr *NodeRunner) handleMessages() {
	for message := range nr.network.ReceiveMessage() {
		nr.handleMessage(message)
	}
}

// Handles a single message received from a client
func (nr *NodeRunner) handleMessage(message common.NetworkMessage) {
	var err error

	if message.IsEmpty() {
		nr.log.Error("got empty message")
		return
	}
	switch message.Type {
	case common.ConnectMessage:
		if _, err := node.NewValidatorFromString(message.Data); err != nil {
			nr.log.Error("invalid validator data was received", "data", message.Data, "error", err)
			return
		}
	case common.BallotMessage:
		err = nr.handleBallotMessage(message)
	default:
		err = errors.New("got unknown message")
	}

	if err != nil {
		if _, ok := err.(common.CheckerStop); ok {
			return
		}
		nr.log.Debug("failed to handle message", "message", string(message.Data), "error", err)
	}
}

func (nr *NodeRunner) handleBallotMessage(message common.NetworkMessage) (err error) {
	nr.log.Debug("got ballot", "message", message.Head(50))

	baseChecker := &BallotChecker{
		DefaultChecker: common.DefaultChecker{Funcs: nr.handleBaseBallotCheckerFuncs},
		NodeRunner:     nr,
		LocalNode:      nr.localNode,
		NetworkID:      nr.networkID,
		Message:        message,
		Log:            nr.Log(),
		VotingHole:     ballot.VotingNOTYET,
	}
	err = common.RunChecker(baseChecker, nr.handleBallotCheckerDeferFunc)
	if err != nil {
		if _, ok := err.(common.CheckerErrorStop); !ok {
			nr.log.Debug("failed to handle ballot", "error", err, "message", string(message.Data))
			return
		}
	}

	var checkerFuncs []common.CheckerFunc
	switch baseChecker.Ballot.State() {
	case ballot.StateINIT:
		checkerFuncs = DefaultHandleINITBallotCheckerFuncs
	case ballot.StateSIGN:
		checkerFuncs = DefaultHandleSIGNBallotCheckerFuncs
	case ballot.StateACCEPT:
		checkerFuncs = DefaultHandleACCEPTBallotCheckerFuncs
	}

	checker := &BallotChecker{
		DefaultChecker: common.DefaultChecker{Funcs: checkerFuncs},
		NodeRunner:     nr,
		LocalNode:      nr.localNode,
		NetworkID:      nr.networkID,
		Message:        message,
		Ballot:         baseChecker.Ballot,
		VotingHole:     baseChecker.VotingHole,
		IsNew:          baseChecker.IsNew,
		Log:            baseChecker.Log,
	}
	err = common.RunChecker(checker, nr.handleBallotCheckerDeferFunc)
	if err != nil {
		if stopped, ok := err.(common.CheckerStop); ok {
			nr.log.Debug(
				"stopped to handle ballot",
				"state", baseChecker.Ballot.State(),
				"reason", stopped.Error(),
			)
		} else {
			nr.log.Debug("failed to handle ballot", "error", err, "state", baseChecker.Ballot.State(), "message", string(message.Data))
			return
		}
	}

	return
}

func (nr *NodeRunner) InitRound() {
	// get latest blocks
	var err error
	var latestBlock block.Block
	if latestBlock, err = block.GetLatestBlock(nr.storage); err != nil {
		panic(err)
	}

	nr.consensus.SetLatestBlock(latestBlock)
	nr.consensus.SetLatestRound(round.Round{})

	ticker := time.NewTicker(time.Millisecond * 5)
	for _ = range ticker.C {
		var notFound bool
		connected := nr.connectionManager.AllConnected()
		if len(connected) < 1 {
			continue
		}

		for address, _ := range nr.localNode.GetValidators() {
			if _, found := common.InStringArray(connected, address); !found {
				notFound = true
				break
			}
		}
		if !notFound {
			ticker.Stop()
			break
		}
	}

	nr.log.Debug(
		"caught up network and connected to all validators",
		"connected", nr.Policy().Connected(),
		"validators", nr.Policy().Validators(),
	)

	nr.StartStateManager()
}

func (nr *NodeRunner) StartStateManager() {
	// check whether current running rounds exist
	if len(nr.consensus.RunningRounds) > 0 {
		return
	}

	nr.isaacStateManager.Start()
	nr.isaacStateManager.NextHeight()
	return
}

func (nr *NodeRunner) StopStateManager() {
	// check whether current running rounds exist
	nr.isaacStateManager.Stop()
	return
}

func (nr *NodeRunner) TransitISAACState(round round.Round, ballotState ballot.State) {
	nr.isaacStateManager.TransitISAACState(round.BlockHeight, round.Number, ballotState)
}

var NewBallotTransactionCheckerFuncs = []common.CheckerFunc{
	IsNew,
	BallotTransactionsSameSource,
	BallotTransactionsSourceCheck,
}

func (nr *NodeRunner) proposeNewBallot(roundNumber uint64) (ballot.Ballot, error) {
	b := nr.consensus.LatestBlock()
	round := round.Round{
		Number:      roundNumber,
		BlockHeight: b.Height,
		BlockHash:   b.Hash,
		TotalTxs:    b.TotalTxs,
	}

	// collect incoming transactions from `Pool`
	availableTransactions := nr.consensus.TransactionPool.AvailableTransactions(nr.Conf.TxsLimit)
	nr.log.Debug("new round proposed", "round", round, "transactions", availableTransactions)

	transactionsChecker := &BallotTransactionChecker{
		DefaultChecker:        common.DefaultChecker{Funcs: NewBallotTransactionCheckerFuncs},
		NodeRunner:            nr,
		LocalNode:             nr.localNode,
		NetworkID:             nr.networkID,
		Transactions:          availableTransactions,
		CheckTransactionsOnly: true,
		VotingHole:            ballot.VotingNOTYET,
	}

	if err := common.RunChecker(transactionsChecker, common.DefaultDeferFunc); err != nil {
		if _, ok := err.(common.CheckerErrorStop); !ok {
			nr.log.Error("error occurred in BallotTransactionChecker", "error", err)
		}
	}

	// remove invalid transactions
	nr.Consensus().TransactionPool.Remove(transactionsChecker.InvalidTransactions()...)

	theBallot := ballot.NewBallot(nr.localNode.Address(), round, transactionsChecker.ValidTransactions)
	theBallot.SetVote(ballot.StateINIT, ballot.VotingYES)

	var validTransactions []transaction.Transaction
	for _, hash := range transactionsChecker.ValidTransactions {
		if tx, found := nr.consensus.TransactionPool.Get(hash); !found {
			return ballot.Ballot{}, errors.ErrorTransactionNotFound
		} else {
			validTransactions = append(validTransactions, tx)
		}
	}

	opc, err := ballot.NewCollectTxFeeFromBallot(*theBallot, nr.CommonAccountAddress, validTransactions...)
	if err != nil {
		return ballot.Ballot{}, err
	}

	opi, err := ballot.NewInflationFromBallot(*theBallot, nr.CommonAccountAddress, nr.InitialBalance)
	if err != nil {
		return ballot.Ballot{}, err
	}

	ptx, err := ballot.NewProposerTransactionFromBallot(*theBallot, opc, opi)
	if err != nil {
		return ballot.Ballot{}, err
	}

	theBallot.SetProposerTransaction(ptx)
	theBallot.Sign(nr.localNode.Keypair(), nr.networkID)

	nr.log.Debug("new ballot created", "ballot", theBallot)

	nr.ConnectionManager().Broadcast(*theBallot)

	return *theBallot, nil
}
