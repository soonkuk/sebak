package runner

import (
	"sync"
	"time"

	"boscoin.io/sebak/lib/ballot"
	"boscoin.io/sebak/lib/block"
	"boscoin.io/sebak/lib/common"
	"boscoin.io/sebak/lib/consensus"
	"boscoin.io/sebak/lib/metrics"
	"boscoin.io/sebak/lib/node"
	"boscoin.io/sebak/lib/voting"
)

// ISAACStateManager manages the ISAACState.
// The most important function `Start()` is called in startStateManager() function in node_runner.go by goroutine.
type ISAACStateManager struct {
	sync.RWMutex

	nr                     *NodeRunner
	state                  consensus.ISAACState
	stateTransit           chan consensus.ISAACState
	stop                   chan struct{}
	blockTimeBuffer        time.Duration              // the time to wait to adjust the block creation time.
	transitSignal          func(consensus.ISAACState) // the function is called when the ISAACState is changed.
	firstProposedBlockTime time.Time                  // the time at which the first consensus block was saved(height 2). It is used for calculating `blockTimeBuffer`.

	Conf common.Config
}

func NewISAACStateManager(nr *NodeRunner, conf common.Config) *ISAACStateManager {
	p := &ISAACStateManager{
		nr: nr,
		state: consensus.ISAACState{
			Round:       0,
			Height:      0,
			BallotState: ballot.StateINIT,
		},
		stateTransit:    make(chan consensus.ISAACState),
		stop:            make(chan struct{}),
		blockTimeBuffer: 2 * time.Second,
		transitSignal:   func(consensus.ISAACState) {},

		Conf: conf,
	}

	p.setTheFirstProposedBlockTime()

	return p
}

//setTheFirstProposedBlockTime func는 최초 proposed된 block의 time을
//return한다. firstProposedBlockTime은 blocktime buffer를 계산할 때 사용된다.
func (sm *ISAACStateManager) setTheFirstProposedBlockTime() {
	if !sm.firstProposedBlockTime.IsZero() {
		return
	}

	b := sm.nr.Consensus().LatestBlock()
	if b.Height == common.GenesisBlockHeight {
		return
	}

	blk, err := block.GetBlockByHeight(sm.nr.Storage(), common.FirstProposedBlockHeight)
	if err != nil {
		return
	}
	sm.firstProposedBlockTime, _ = common.ParseISO8601(blk.ProposedTime)
	sm.nr.Log().Debug("set first proposed block time", "time", sm.firstProposedBlockTime)
}

func (sm *ISAACStateManager) setBlockTimeBuffer() {
	sm.nr.Log().Debug("begin ISAACStateManager.setBlockTimeBuffer()", "ISAACState", sm.State())
	sm.setTheFirstProposedBlockTime()
	b := sm.nr.Consensus().LatestBlock()

	if b.Height <= common.FirstProposedBlockHeight {
		return
	}

	ballotProposedTime := getBallotProposedTime(b.ProposedTime)
	sm.blockTimeBuffer = calculateBlockTimeBuffer(
		b.Height,
		sm.Conf.BlockTime,
		time.Now().Sub(sm.firstProposedBlockTime),
		time.Now().Sub(ballotProposedTime),
		sm.Conf.BlockTimeDelta,
	)
	sm.nr.Log().Debug(
		"calculated blockTimeBuffer",
		"blockTimeBuffer", sm.blockTimeBuffer,
		"firstProposedBlockTime", sm.firstProposedBlockTime,
		"height", b.Height,
		"proposedTime", b.ProposedTime,
		"now", time.Now(),
	)

	return
}
func getBallotProposedTime(timeStr string) time.Time {
	ballotProposedTime, _ := common.ParseISO8601(timeStr)
	return ballotProposedTime
}

// BlockTimeBuffer is the time to wait for adjusting
// the block creation time(default 5 sec).
// If the average block creation time is less than the goal,
// the BlockTimeBuffer becomes longer and vice versa.
//
// The block time for the next height(nextBlockTime) is
// `expected time to 'height + 1' - actual time to 'height'`.
// For calculating it, we need height, goal and sinceGenesis.
//
// delta is used to prevent extreme changes in block time.
// If goal is 5 sec and `delta` is 1 sec,
// then the actual block creation time is 4 to 6 sec.
//
// Finally, `nextBlockTime - untilNow` is BlockTimeBuffer.
func calculateBlockTimeBuffer(height uint64, goal, sinceGenesis, untilNow, delta time.Duration) time.Duration {
	var blockTimeBuffer time.Duration
	
	//nextBlockTime은 블롯 생성시간을 5초로 했을 때 다음 blockheight의 예상 시간 빼기 현재까지
	//실제 걸린시간이다.
	nextBlockTime := 5*time.Second*time.Duration(height+1) - sinceGenesis
	//nextBlock
	min := goal - delta
	max := goal + delta
	if nextBlockTime < min {
		nextBlockTime = min
	} else if nextBlockTime > max {
		nextBlockTime = max
	}

	blockTimeBuffer = nextBlockTime - untilNow

	if blockTimeBuffer < 0 {
		blockTimeBuffer = 0
	}

	return blockTimeBuffer
}

// production에서는 실제 사용되고 있지 않음 테스트 용도
func (sm *ISAACStateManager) SetTransitSignal(f func(consensus.ISAACState)) {
	sm.Lock()
	defer sm.Unlock()
	sm.transitSignal = f
}

// ballot의 height, round가 statemanager의 현재 ballot의 것보다
// 나중이면 sm의 stateTransit채널로 target isaacstate를 보낸다.
func (sm *ISAACStateManager) TransitISAACState(height uint64, round uint64, ballotState ballot.State) {
	sm.RLock()
	current := sm.state
	sm.RUnlock()
	sm.nr.Log().Debug(
		"ISAACStateManager.TransitISAACState()",
		"current", current,
		"height", height,
		"round", round,
		"ballotState", ballotState,
	)

	target := consensus.ISAACState{
		Height:      height,
		Round:       round,
		BallotState: ballotState,
	}

	if current.IsLater(target) {
		sm.nr.Log().Debug(
			"target is later than current",
			"current", current,
			"target", target,
		)
		go func(t consensus.ISAACState) {
			sm.stateTransit <- t
		}(target)
	}
}

// NextRound func은 ISAACStateManager의 현재 상태에서 같은 blockheight의 다음 round로 이동시키고자 할 때 사용한다. 
func (sm *ISAACStateManager) NextRound() {
	state := sm.State()
	sm.nr.Log().Debug("begin ISAACStateManager.NextRound()", "height", state.Height, "round", state.Round, "state", state.BallotState)
	newRound := sm.nr.Consensus().LatestVotingBasis().Round + 1
	sm.TransitISAACState(state.Height, newRound, ballot.StateINIT)
}

// NextHeight func은 ISAACStateManager의 현재 상태에서 다음 blockheight로 이동시킬 때 사용한다.
func (sm *ISAACStateManager) NextHeight() {
	h := sm.nr.consensus.LatestBlock().Height
	sm.nr.Log().Debug("begin ISAACStateManager.NextHeight()", "height", h)
	sm.TransitISAACState(h, 0, ballot.StateINIT)
}


// In `Start()` method a node proposes ballot.
// Or it sets or resets timeout. If it is expired, it broadcasts B(`EXP`).
// And it manages the node round.
func (sm *ISAACStateManager) Start() {
	sm.nr.localNode.SetConsensus()
	sm.nr.Log().Debug("begin ISAACStateManager.Start()", "ISAACState", sm.State())
	go func() {
		// 1시간 후로 타이머를 설정한다.
		timer := time.NewTimer(time.Duration(1 * time.Hour))
		begin := time.Now() // measure for block interval time
		for {
			select {
			// 1시간 후로 설정한 타이머가 끝나면  
			case <-timer.C:
				sm.nr.Log().Debug("timeout", "ISAACState", sm.State())
				switch sm.State().BallotState {
				case ballot.StateINIT:
					sm.setBallotState(ballot.StateSIGN)
					sm.transitSignal(sm.State())
					sm.resetTimer(timer, ballot.StateSIGN)
				case ballot.StateSIGN:
					if sm.nr.localNode.State() == node.StateCONSENSUS {
						if sm.nr.BallotSendRecord().Sent(sm.State()) {
							sm.nr.Log().Debug("break; BallotSendRecord().Sent(sm.State) == true", "ISAACState", sm.State())
							break
						}
						go sm.broadcastExpiredBallot(sm.State().Round, ballot.StateSIGN)
					}
				case ballot.StateACCEPT:
					if sm.nr.localNode.State() == node.StateCONSENSUS {
						if sm.nr.BallotSendRecord().Sent(sm.State()) {
							sm.nr.Log().Debug("break; BallotSendRecord().Sent(sm.State) == true", "ISAACState", sm.State())
							break
						}
						go sm.broadcastExpiredBallot(sm.State().Round, ballot.StateACCEPT)
					}
				case ballot.StateALLCONFIRM:
					sm.nr.Log().Error("timeout", "ISAACState", sm.State())
					sm.NextRound()
				}
			// statemTransit 채널에서 메세지를 받은 경우 채널에서 받은 상태가 stateManager의
			// 현재 state보다 늦은 state이면 
			case state := <-sm.stateTransit:
				current := sm.State()
				if !current.IsLater(state) {
					sm.nr.Log().Debug("break; target is before than or equal to current", "current", current, "target", state)
					break
				}

				if state.BallotState == ballot.StateINIT {
					begin = metrics.Consensus.SetBlockIntervalSeconds(begin)

					if sm.nr.localNode.State() == node.StateCONSENSUS {
						sm.proposeOrWait(timer, state.Round)
					}
				} else {
					sm.resetTimer(timer, state.BallotState)
				}
				sm.setState(state)
				// sm.transitSignal()가 현재는 설정되어 있지 않고 empty 함수이기 때문에
				// 아무 동작도 안한다.
				sm.transitSignal(state)

			case <-sm.stop:
				return
			}
		}
	}()
}

func (sm *ISAACStateManager) broadcastExpiredBallot(round uint64, state ballot.State) {
	sm.nr.Log().Debug("begin ISAACStateManager.broadcastExpiredBallot", "round", round, "ballotState", state)

	b := sm.nr.consensus.LatestBlock()
	basis := voting.Basis{
		Round:     round,
		Height:    b.Height,
		BlockHash: b.Hash,
		TotalTxs:  b.TotalTxs,
		TotalOps:  b.TotalOps,
	}

	newExpiredBallot, err := sm.nr.consensus.GenerateExpiredBallot(basis, state)
	if err != nil {
		sm.nr.Log().Error("an error generating an expired ballot", "err", err)
	} else {
		sm.nr.BroadcastBallot(newExpiredBallot)
	}

	return
}

func (sm *ISAACStateManager) resetTimer(timer *time.Timer, state ballot.State) {
	switch state {
	case ballot.StateINIT:
		timer.Reset(sm.Conf.TimeoutINIT)
	case ballot.StateSIGN:
		timer.Reset(sm.Conf.TimeoutSIGN)
	case ballot.StateACCEPT:
		timer.Reset(sm.Conf.TimeoutACCEPT)
	case ballot.StateALLCONFIRM:
		timer.Reset(sm.Conf.TimeoutALLCONFIRM)
	}
}

// proposeOrWait func makes node to propose or to wait.
// If nr.localNode is selected as a proposer, the proposer proposes new ballot,
// else the node waits for receiving ballot from the other proposer.
func (sm *ISAACStateManager) proposeOrWait(timer *time.Timer, round uint64) {
	timer.Reset(time.Duration(1 * time.Hour))
	sm.setBlockTimeBuffer()
	height := sm.nr.consensus.LatestBlock().Height
	proposer := sm.nr.Consensus().SelectProposer(height, round)
	log.Debug("selected proposer", "proposer", proposer)

	if proposer == sm.nr.localNode.Address() {
		time.Sleep(sm.blockTimeBuffer)
		if _, err := sm.nr.proposeNewBallot(round); err == nil {
			log.Debug("propose new ballot", "proposer", proposer, "round", round, "ballotState", ballot.StateSIGN)
		} else {
			log.Error("failed to proposeNewBallot", "height", height, "error", err)
		}
		timer.Reset(sm.Conf.TimeoutINIT)
	} else {
		timer.Reset(sm.blockTimeBuffer + sm.Conf.TimeoutINIT)
	}
}

func (sm *ISAACStateManager) State() consensus.ISAACState {
	sm.RLock()
	defer sm.RUnlock()
	return sm.state
}

func (sm *ISAACStateManager) setState(state consensus.ISAACState) {
	sm.Lock()
	defer sm.Unlock()
	sm.nr.Log().Debug("begin ISAACStateManager.setState()", "state", state)
	sm.state = state

	return
}

func (sm *ISAACStateManager) setBallotState(ballotState ballot.State) {
	sm.Lock()
	defer sm.Unlock()
	sm.nr.Log().Debug("begin ISAACStateManager.setBallotState()", "state", sm.state)
	sm.state.BallotState = ballotState

	return
}

func (sm *ISAACStateManager) Stop() {
	go func() {
		sm.stop <- struct{}{}
	}()
}
