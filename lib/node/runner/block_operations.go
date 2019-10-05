package runner

import (
	"fmt"
	"sync"
	"time"

	logging "github.com/inconshreveable/log15"

	"boscoin.io/sebak/lib/block"
	"boscoin.io/sebak/lib/common"
	"boscoin.io/sebak/lib/errors"
	"boscoin.io/sebak/lib/storage"
)

type SavingBlockOperations struct {
	st  *storage.LevelDBBackend
	log logging.Logger

	saveBlock          chan block.Block
	checkedBlockHeight uint64 // block.Block.Height
}

func NewSavingBlockOperations(st *storage.LevelDBBackend, logger logging.Logger) *SavingBlockOperations {
	if logger == nil {
		logger = log
	}

	logger = logger.New(logging.Ctx{"m": "SavingBlockOperations"})
	sb := &SavingBlockOperations{
		st:        st,
		log:       logger,
		saveBlock: make(chan block.Block, 10),
	}
	sb.checkedBlockHeight = sb.getCheckedBlockHeight()
	sb.log.Debug("last checked block is", "height", sb.checkedBlockHeight)

	return sb
}

func (sb *SavingBlockOperations) getCheckedBlockKey() string {
	return fmt.Sprintf("%s-last-checked-block", common.InternalPrefix)
}

func (sb *SavingBlockOperations) getCheckedBlockHeight() uint64 {
	var checked uint64
	if err := sb.st.Get(sb.getCheckedBlockKey(), &checked); err != nil {
		sb.log.Error("failed to check CheckedBlock", "error", err)
		return common.GenesisBlockHeight
	}

	return checked
}

func (sb *SavingBlockOperations) saveCheckedBlock(height uint64) {
	sb.log.Debug("save CheckedBlock", "height", height)

	var found bool
	var err error
	if found, err = sb.st.Has(sb.getCheckedBlockKey()); err != nil {
		sb.log.Error("failed to get CheckedBlock", "error", err)
		return
	}

	if !found {
		if err = sb.st.New(sb.getCheckedBlockKey(), height); err != nil {
			sb.log.Error("failed to save new CheckedBlock", "error", err)
			return
		}
	} else {
		if err = sb.st.Set(sb.getCheckedBlockKey(), height); err != nil {
			sb.log.Error("failed to set CheckedBlock", "error", err)
			return
		}
	}
	sb.checkedBlockHeight = height

	return
}

// arg로 받은 height의 다음 block을 반환
func (sb *SavingBlockOperations) getNextBlock(height uint64) (nextBlock block.Block, err error) {
	if height < sb.checkedBlockHeight {
		height = sb.checkedBlockHeight
	}

	height++

	nextBlock, err = block.GetBlockByHeight(sb.st, height)

	return
}

func (sb *SavingBlockOperations) Check() (err error) {
	sb.log.Debug("start to SavingBlockOperations.Check()", "height", sb.checkedBlockHeight)

	defer func() {
		sb.log.Debug("finished to check")
	}()

	return sb.check(sb.checkedBlockHeight)
}

// continuousCheck will check the missing `BlockOperation`s continuously; if it
// is failed, still try.
func (sb *SavingBlockOperations) continuousCheck() {
	for {
		if err := sb.check(sb.checkedBlockHeight); err != nil {
			sb.log.Error("failed to check", "error", err)
		}

		time.Sleep(5 * time.Second)
	}
}

func (sb *SavingBlockOperations) checkBlockWorker(id int, blocks <-chan block.Block, errChan chan<- error) {
	var err error
	var st *storage.LevelDBBackend

	for blk := range blocks {
		if st, err = sb.st.OpenBatch(); err != nil {
			errChan <- err
			return
		}

		if err = sb.CheckByBlock(st, blk); err != nil {
			st.Discard()
			errChan <- err
			return
		}

		if err = st.Commit(); err != nil {
			sb.log.Error("failed to commit", "block", blk.Hash, "height", blk.Height, "error", err)
			st.Discard()
			errChan <- err
			return
		}

		errChan <- nil
	}
}

// check checks whether `BlockOperation`s of latest `block.Block` are saved; if
// not it will try to catch up to the last.
func (sb *SavingBlockOperations) check(startBlockHeight uint64) (err error) {
	latestBlockHeight := block.GetLatestBlock(sb.st).Height
	if latestBlockHeight == common.GenesisBlockHeight {
		return
	}
	if latestBlockHeight <= startBlockHeight {
		return
	}

	blocks := make(chan block.Block, 100)
	errChan := make(chan error, 100)
	defer close(errChan)
	defer close(blocks)

	// worker가 너무 많아도 성능이슈가 있기 때문에 숫자에 제한을 둠
	numWorker := int((latestBlockHeight - startBlockHeight) / 2)
	if numWorker > 100 {
		numWorker = 100
	} else if numWorker < 1 {
		numWorker = 1
	}

	// worker에게 일을 시킴
	for i := 1; i <= numWorker; i++ {
		go sb.checkBlockWorker(i, blocks, errChan)
	}

	var lock sync.Mutex
	closed := false
	defer func() {
		lock.Lock()
		defer lock.Unlock()

		closed = true
	}()

	// block을 가져와서 blocks라는 go 채널로 계속 넘겨줌.
	go func() {
		var height uint64 = startBlockHeight
		var blk block.Block
		for {
			// checked된 블록의 다음블록부터 가져오고 가져오는 과정중에 에러발생하면 go루틴 중단
			if blk, err = sb.getNextBlock(height); err != nil {
				err = errors.FailedToSaveBlockOperaton.Clone().SetData("error", err)
				errChan <- err
				return
			}

			// check과정은 에러가 발생해서 끝나지 않는 한 latest blockheight에 도달해서 종료되고 go 루틴 중단
			if closed {
				break
			}

			// blocks채널로 저장된 block을 가져와서 넘겨줌
			blocks <- blk
			height = blk.Height
			// 가져온 block의 height가 최근 저장된 blockheight와 같다면 더 이상 block이 없다고 생각하고 종료
			// IMO : 추가로 생성된 latest block이 있을 수 있으므로 한 번 더 확인을 하면 좋지 않을까?
			if blk.Height == latestBlockHeight {
				break
			}
		}
	}()

	var errs uint64
// 제일 마지막순서로 계속 에러를 체크하고 있다
errorCheck:
	for {
		// errChan로 들어오는 err는 checkBlockWorker가 보내는 것인데 check block의 문제가 없다면 nill로 보내온다.
		select {
		case e := <-errChan:
			// 여기서 err는 err를 확인하는 것과 block들이 check된 갯수를 확인하는 2가지 용도로 사용된다.
			errs++
			// 만약 문제있는 err가 들어오면 과정을 바로 멈추고
			if e != nil {
				err = e
				break errorCheck
			}
			// nil 에러의 갯수가 작업하려는 블록하이트 숫자를 다 채우면 역시 errorCheck를 빠져나간다.
			if errs == (latestBlockHeight - startBlockHeight) {
				break errorCheck
			}
		}
	}

	// err가 발생해서 끝나던지 아니면 latest blockheight가 도달해서 끝나던지
	if err != nil {
		err = errors.FailedToSaveBlockOperaton.Clone().SetData("error", err)
	} else {
		// 왜 latestBlockHeight로 하는지 이해가 안간다
		sb.saveCheckedBlock(latestBlockHeight)
	}

	return
}

func (sb *SavingBlockOperations) savingBlockOperationsWorker(id int, st *storage.LevelDBBackend, blk block.Block, txs <-chan string, errChan chan<- error) {
	for hash := range txs {
		errChan <- sb.CheckTransactionByBlock(st, blk, hash)
	}
}

// CheckByBlock은 SavingBlockOperationWorker들에게 일을 시키는 함수
func (sb *SavingBlockOperations) CheckByBlock(st *storage.LevelDBBackend, blk block.Block) (err error) {
	if blk.Height > common.GenesisBlockHeight { // ProposerTransaction
		if err = sb.CheckTransactionByBlock(st, blk, blk.ProposerTransaction); err != nil {
			return
		}
	}
	if len(blk.Transactions) < 1 {
		return
	}

	txs := make(chan string, 100)
	errChan := make(chan error, 100)
	defer close(errChan)

	numWorker := int(len(blk.Transactions) / 2)
	if numWorker > 100 {
		numWorker = 100
	} else if numWorker < 1 {
		numWorker = 1
	}

	for i := 1; i <= numWorker; i++ {
		go sb.savingBlockOperationsWorker(i, st, blk, txs, errChan)
	}

	go func() {
		for _, hash := range blk.Transactions {
			txs <- hash
		}
		close(txs)
	}()

	var errs []error
	var returned int
// checkBlockWorker와 SavingBlockWorker와의 차이점은 에러처리방식의 차이이다
// checkBlockWorker에게서 온 에러는 받으면 check는 바로 종료하는데 
// SavingBlockWorker에게서 에러를 받으면 바로 종료하지 않고
// 에러의 숫자가 txs의 숫자만큼 될때까지 기다렸다가 처리한다
// 그래서 checkBlockWorker의 경우 errChan이 갑자기 닫혀서 프로세서가 죽게 된다.
// 동기화처리와 비동기화 처리의 sync가 맞지 않아서 생기는 경우이다
errorCheck:
	for {
		select {
		case err = <-errChan:
			returned++
			if err != nil {
				errs = append(errs, err)
			}
			if returned == len(blk.Transactions) {
				break errorCheck
			}
		}
	}

	if len(errs) > 0 {
		err = errors.FailedToSaveBlockOperaton.Clone().SetData("errors", errs)
		return
	}

	return
}

// CheckTransactionByBlock은 Transaction을 storage에서 가져와서 hash값을 알아내고
// 알아낸 tx hash값으로 TransactionPool에서 Transaction의 메세지를 가져와서
// BlockOperation들을 storage에 저장한다.
func (sb *SavingBlockOperations) CheckTransactionByBlock(st *storage.LevelDBBackend, blk block.Block, hash string) (err error) {
	var bt block.BlockTransaction
	if bt, err = block.GetBlockTransaction(st, hash); err != nil {
		sb.log.Error("failed to get BlockTransaction", "block", blk.Hash, "transaction", hash, "error", err)
		return
	}

	if bt.Transaction().IsEmpty() {
		var tp block.TransactionPool
		if tp, err = block.GetTransactionPool(st, hash); err != nil {
			sb.log.Error("failed to get Transaction from TransactionPool", "transaction", hash, "error", err)
			return
		}

		bt.Message = tp.Message
	}

	for i, op := range bt.Transaction().B.Operations {
		opHash := block.NewBlockOperationKey(common.MustMakeObjectHashString(op), hash)

		var exists bool
		if exists, err = block.ExistsBlockOperation(st, opHash); err != nil {
			sb.log.Error(
				"failed to check ExistsBlockOperation",
				"block", blk.Hash,
				"transaction", hash,
				"operation-index", i,
			)
			return
		}

		if !exists {
			if err = bt.SaveBlockOperation(st, op); err != nil {
				return err
			}
		}
	}

	return
}

func (sb *SavingBlockOperations) Start() {
	go sb.continuousCheck()
	go sb.startSaving()

	return
}

func (sb *SavingBlockOperations) startSaving() {
	sb.log.Debug("start saving")

	for {
		select {
		case blk := <-sb.saveBlock:
			if err := sb.save(blk); err != nil {
				// NOTE if failed, the `continuousCheck()` will fill the missings.
				sb.log.Error("failed to save BlockOperation", "block", blk.Hash, "error", err)
			}
		}
	}
}

func (sb *SavingBlockOperations) Save(blk block.Block) {
	go func() {
		sb.saveBlock <- blk
	}()
}

func (sb *SavingBlockOperations) save(blk block.Block) (err error) {
	sb.log.Debug("starting to save BlockOperation", "block", blk.Hash)
	defer func() {
		if err != nil {
			sb.log.Error("could not save BlockOperation", "block", blk, "error", err)
		} else {
			sb.log.Debug("done saving BlockOperation", "block", blk.Hash)
		}
	}()

	var st *storage.LevelDBBackend
	if st, err = sb.st.OpenBatch(); err != nil {
		return
	}

	if err = sb.CheckByBlock(st, blk); err != nil {
		st.Discard()
	} else {
		err = st.Commit()
	}

	return
}
