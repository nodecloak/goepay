package cryptochain

import (
	"context"
	"fmt"
	"math/big"
	"strings"
	"time"
)

type EthereumAdapter struct{ node *node }

func NewEthereum(c Config) (*EthereumAdapter, error) {
	n, err := newNode(c)
	if err != nil {
		return nil, err
	}
	return &EthereumAdapter{n}, nil
}
func (a *EthereumAdapter) Chain() Chain { return Ethereum }

type ethereumBlock struct {
	Number    string `json:"number"`
	Hash      string `json:"hash"`
	Timestamp string `json:"timestamp"`
}
type ethereumLog struct {
	Address         string   `json:"address"`
	Topics          []string `json:"topics"`
	Data            string   `json:"data"`
	BlockNumber     string   `json:"blockNumber"`
	BlockHash       string   `json:"blockHash"`
	TransactionHash string   `json:"transactionHash"`
	LogIndex        string   `json:"logIndex"`
	Removed         bool     `json:"removed"`
}
type ethereumReceipt struct {
	TransactionHash   string        `json:"transactionHash"`
	BlockNumber       string        `json:"blockNumber"`
	BlockHash         string        `json:"blockHash"`
	Status            string        `json:"status"`
	GasUsed           string        `json:"gasUsed"`
	EffectiveGasPrice string        `json:"effectiveGasPrice"`
	Logs              []ethereumLog `json:"logs"`
}

func (a *EthereumAdapter) block(ctx context.Context, tag string) (*ethereumBlock, error) {
	var b *ethereumBlock
	if err := a.node.rpc(ctx, "eth_getBlockByNumber", []any{tag, false}, &b); err != nil {
		return nil, err
	}
	if b == nil {
		return nil, ErrInvalidResponse
	}
	if _, err := hexQuantity(b.Number); err != nil {
		return nil, err
	}
	hash, err := normalizeHash(Ethereum, b.Hash)
	if err != nil {
		return nil, err
	}
	b.Hash = hash
	return b, nil
}
func (a *EthereumAdapter) VerifyNetwork(ctx context.Context) error {
	var chainID string
	if err := a.node.rpc(ctx, "eth_chainId", []any{}, &chainID); err != nil {
		return err
	}
	if chainID != "0x1" {
		return ErrWrongNetwork
	}
	b, err := a.block(ctx, "0x0")
	if err != nil {
		return err
	}
	if b.Number != "0x0" || b.Hash != EthereumMainnetGenesis {
		return ErrWrongNetwork
	}
	return nil
}
func (a *EthereumAdapter) finalizedHeight(ctx context.Context) (uint64, error) {
	b, err := a.block(ctx, "finalized")
	if err != nil {
		return 0, err
	}
	return hexQuantity(b.Number)
}
func (a *EthereumAdapter) FinalizedHeight(ctx context.Context) (uint64, error) {
	if err := a.VerifyNetwork(ctx); err != nil {
		return 0, err
	}
	return a.finalizedHeight(ctx)
}
func (a *EthereumAdapter) canonicalBlockHash(ctx context.Context, height uint64) (string, error) {
	b, err := a.block(ctx, uintQuantity(height))
	if err != nil {
		return "", err
	}
	h, _ := hexQuantity(b.Number)
	if h != height {
		return "", ErrInvalidResponse
	}
	return b.Hash, nil
}
func (a *EthereumAdapter) CanonicalBlockHash(ctx context.Context, height uint64) (string, error) {
	if err := a.VerifyNetwork(ctx); err != nil {
		return "", err
	}
	finalized, err := a.finalizedHeight(ctx)
	if err != nil {
		return "", err
	}
	if height > finalized {
		return "", ErrUnfinalized
	}
	return a.canonicalBlockHash(ctx, height)
}

func (a *EthereumAdapter) receipt(ctx context.Context, txID string) (*ethereumReceipt, error) {
	var r *ethereumReceipt
	if err := a.node.rpc(ctx, "eth_getTransactionReceipt", []any{txID}, &r); err != nil {
		return nil, err
	}
	if r != nil {
		h, err := normalizeHash(Ethereum, r.TransactionHash)
		if err != nil || h != txID {
			return nil, ErrInvalidResponse
		}
		if r.Status != "0x0" && r.Status != "0x1" {
			return nil, fmt.Errorf("%w: receipt status", ErrInvalidResponse)
		}
	}
	return r, nil
}

func ethereumTransfer(log ethereumLog) (Transfer, bool, error) {
	if log.Removed {
		return Transfer{}, false, nil
	}
	from, to, amount, match, err := parseTransferEvent(Ethereum, log.Address, log.Topics, log.Data)
	if err != nil || !match {
		return Transfer{}, false, err
	}
	txID, err := normalizeHash(Ethereum, log.TransactionHash)
	if err != nil {
		return Transfer{}, false, err
	}
	blockHash, err := normalizeHash(Ethereum, log.BlockHash)
	if err != nil {
		return Transfer{}, false, err
	}
	height, err := hexQuantity(log.BlockNumber)
	if err != nil {
		return Transfer{}, false, err
	}
	index, err := hexQuantity(log.LogIndex)
	if err != nil {
		return Transfer{}, false, err
	}
	return Transfer{Chain: Ethereum, TxID: txID, EventIndex: index, BlockHeight: height, BlockHash: blockHash, From: from, To: to, Contract: EthereumUSDTContract, AtomicAmount: amount}, true, nil
}

func receiptIncludesTransfer(r *ethereumReceipt, t Transfer) (bool, error) {
	for _, l := range r.Logs {
		candidate, match, err := ethereumTransfer(l)
		if err != nil {
			return false, err
		}
		if match && candidate == t {
			return true, nil
		}
	}
	return false, nil
}

func (a *EthereumAdapter) ScanTransfers(ctx context.Context, from, to uint64) ([]Transfer, error) {
	if err := a.VerifyNetwork(ctx); err != nil {
		return nil, err
	}
	finalized, err := a.finalizedHeight(ctx)
	if err != nil {
		return nil, err
	}
	if err := checkRange(from, to, finalized, a.node.maxScanBlocks); err != nil {
		return nil, err
	}
	var logs []ethereumLog
	if err := a.node.rpc(ctx, "eth_getLogs", []any{map[string]any{"fromBlock": uintQuantity(from), "toBlock": uintQuantity(to), "address": EthereumUSDTContract, "topics": []string{"0x" + transferTopic}}}, &logs); err != nil {
		return nil, err
	}
	var out []Transfer
	seen := make(map[string]Transfer)
	receipts := make(map[string]*ethereumReceipt)
	blocks := make(map[uint64]*ethereumBlock)
	for _, log := range logs {
		t, match, err := ethereumTransfer(log)
		if err != nil {
			return nil, err
		}
		if !match {
			continue
		}
		if t.BlockHeight < from || t.BlockHeight > to {
			return nil, fmt.Errorf("%w: log outside range", ErrInvalidResponse)
		}
		key := fmt.Sprintf("%s:%d", t.TxID, t.EventIndex)
		if previous, ok := seen[key]; ok {
			if previous != t {
				return nil, fmt.Errorf("%w: conflicting duplicate event", ErrInvalidResponse)
			}
			continue
		}
		seen[key] = t
		r, ok := receipts[t.TxID]
		if !ok {
			r, err = a.receipt(ctx, t.TxID)
			if err != nil {
				return nil, err
			}
			if r == nil {
				return nil, fmt.Errorf("%w: missing receipt", ErrInvalidResponse)
			}
			receipts[t.TxID] = r
		}
		height, err := hexQuantity(r.BlockNumber)
		if err != nil || height != t.BlockHeight {
			return nil, fmt.Errorf("%w: receipt block number", ErrInvalidResponse)
		}
		rHash, err := normalizeHash(Ethereum, r.BlockHash)
		if err != nil || rHash != t.BlockHash {
			return nil, fmt.Errorf("%w: receipt block hash", ErrInvalidResponse)
		}
		b, ok := blocks[t.BlockHeight]
		if !ok {
			b, err = a.block(ctx, uintQuantity(t.BlockHeight))
			if err != nil {
				return nil, err
			}
			h, _ := hexQuantity(b.Number)
			if h != t.BlockHeight {
				return nil, ErrInvalidResponse
			}
			blocks[t.BlockHeight] = b
		}
		if b.Hash != t.BlockHash {
			return nil, fmt.Errorf("%w: noncanonical event", ErrInvalidResponse)
		}
		if r.Status != "0x1" {
			continue
		}
		included, err := receiptIncludesTransfer(r, t)
		if err != nil {
			return nil, err
		}
		if !included {
			return nil, fmt.Errorf("%w: event absent from receipt", ErrInvalidResponse)
		}
		timestamp, err := hexQuantity(b.Timestamp)
		if err != nil || timestamp == 0 || timestamp > 253402300799 {
			return nil, fmt.Errorf("%w: block timestamp", ErrInvalidResponse)
		}
		t.OccurredAt = time.Unix(int64(timestamp), 0).UTC()
		out = append(out, t)
	}
	sortTransfers(out)
	return out, nil
}

func (a *EthereumAdapter) Lookup(ctx context.Context, txID string) (TransactionStatus, error) {
	status := TransactionStatus{Chain: Ethereum}
	id, err := normalizeHash(Ethereum, txID)
	if err != nil {
		return status, err
	}
	status.TxID = id
	if err := a.VerifyNetwork(ctx); err != nil {
		return status, err
	}
	r, err := a.receipt(ctx, id)
	if err != nil {
		return status, err
	}
	if r == nil {
		return status, nil
	}
	height, err := hexQuantity(r.BlockNumber)
	if err != nil {
		return status, err
	}
	hash, err := normalizeHash(Ethereum, r.BlockHash)
	if err != nil {
		return status, err
	}
	b, err := a.block(ctx, uintQuantity(height))
	if err != nil {
		return status, err
	}
	actualHeight, _ := hexQuantity(b.Number)
	if b.Hash != hash || actualHeight != height {
		return status, fmt.Errorf("%w: noncanonical receipt", ErrInvalidResponse)
	}
	gasUsed, err := hexQuantity(r.GasUsed)
	if err != nil {
		return status, fmt.Errorf("%w: missing or invalid gas used", ErrInvalidResponse)
	}
	gasPrice, err := hexBigQuantity(r.EffectiveGasPrice)
	if err != nil {
		return status, fmt.Errorf("%w: missing or invalid effective gas price", ErrInvalidResponse)
	}
	fee := new(big.Int).Mul(new(big.Int).SetUint64(gasUsed), gasPrice)
	if fee.BitLen() > 256 {
		return status, fmt.Errorf("%w: network fee overflow", ErrInvalidResponse)
	}
	status.FeeAtomic = fee.String()
	finalized, err := a.finalizedHeight(ctx)
	if err != nil {
		return status, err
	}
	status.Found = true
	status.BlockHeight = height
	status.BlockHash = hash
	status.Finalized = height <= finalized
	status.Success = r.Status == "0x1"
	if status.Success && status.Finalized {
		timestamp, err := hexQuantity(b.Timestamp)
		if err != nil || timestamp == 0 || timestamp > 253402300799 {
			return TransactionStatus{Chain: Ethereum, TxID: id}, ErrInvalidResponse
		}
		seen := make(map[uint64]Transfer)
		for _, l := range r.Logs {
			t, match, err := ethereumTransfer(l)
			if err != nil {
				return TransactionStatus{Chain: Ethereum, TxID: id}, err
			}
			if !match {
				continue
			}
			if t.TxID != id || t.BlockHeight != height || t.BlockHash != hash {
				return TransactionStatus{Chain: Ethereum, TxID: id}, ErrInvalidResponse
			}
			if previous, ok := seen[t.EventIndex]; ok {
				if previous != t {
					return TransactionStatus{Chain: Ethereum, TxID: id}, ErrInvalidResponse
				}
				continue
			}
			seen[t.EventIndex] = t
			t.OccurredAt = time.Unix(int64(timestamp), 0).UTC()
			status.Transfers = append(status.Transfers, t)
		}
	}
	return status, nil
}

func (a *EthereumAdapter) Balance(ctx context.Context, address string) (string, error) {
	addr, err := NormalizeAddress(Ethereum, address)
	if err != nil {
		return "", err
	}
	if err := a.VerifyNetwork(ctx); err != nil {
		return "", err
	}
	body := "0x70a08231" + strings.Repeat("0", 24) + addr[2:]
	var result string
	if err := a.node.rpc(ctx, "eth_call", []any{map[string]any{"to": EthereumUSDTContract, "data": body}, "finalized"}, &result); err != nil {
		return "", err
	}
	return uint256Result(result)
}

var _ Adapter = (*EthereumAdapter)(nil)
