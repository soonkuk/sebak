package api

import (
	"net/http"
	"strconv"

	"boscoin.io/sebak/lib/block"
	"boscoin.io/sebak/lib/common"
	"boscoin.io/sebak/lib/network/httputils"
	"boscoin.io/sebak/lib/node/runner/api/resource"
	"boscoin.io/sebak/lib/storage"
)

func (api NetworkHandlerAPI) GetBlocksHandler(w http.ResponseWriter, r *http.Request) {
	p, err := NewPageQuery(r)
	if err != nil {
		httputils.WriteJSONError(w, err)
		return
	}

	var (
		firstCursor []byte
		cursor      []byte // cursor as height
		blocks      []resource.Resource
	)

	var option *storage.WalkOption
	{
		height, err := strconv.ParseUint(string(p.Cursor()), 10, 64)
		if err != nil {
			height = common.GenesisBlockHeight // default cursor is genesis block height
		}
		option = storage.NewWalkOption(block.GetBlockKeyPrefixHeight(height), p.Limit(), p.Reverse(), false)
	}

	{
		err := block.WalkBlocks(api.storage, option, func(b *block.Block, key []byte) (next bool, err error) {
			blocks = append(blocks, resource.NewBlock(b))
			height := b.Height
			if height > 1 {
				if option.Reverse {
					height--
				} else {
					height++
				}
			}
			cursor = []byte(strconv.FormatUint(height, 10))
			if len(firstCursor) == 0 {
				firstCursor = append(firstCursor, cursor...)
			}
			return true, nil
		})
		if err != nil {
			httputils.WriteJSONError(w, err)
			return

		}
	}

	list := p.ResourceList(blocks, firstCursor, cursor)
	httputils.MustWriteJSON(w, 200, list)
}
