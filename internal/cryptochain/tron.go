package cryptochain

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

type TronAdapter struct{ node *node }

func NewTron(c Config) (*TronAdapter, error) {
	n, err := newNode(c)
	if err != nil {
		return nil, err
	}
	return &TronAdapter{n}, nil
}
func (a *TronAdapter) Chain() Chain { return Tron }

type tronTransaction struct {
	TxID  string `json:"txID"`
	Error string `json:"Error"`
	Ret   []struct {
		ContractRet string `json:"contractRet"`
	} `json:"ret"`
	RawData struct {
		Contract []struct {
			Type string `json:"type"`
		} `json:"contract"`
	} `json:"raw_data"`
}
type tronBlock struct {
	ID     string `json:"blockID"`
	Header struct {
		Raw struct {
			Number    uint64 `json:"number"`
			Timestamp uint64 `json:"timestamp"`
		} `json:"raw_data"`
	} `json:"block_header"`
	Transactions []tronTransaction `json:"transactions"`
	Error        string            `json:"Error"`
}
type tronReceipt struct {
	ID          string       `json:"id"`
	Fee         *json.Number `json:"fee"`
	BlockNumber uint64       `json:"blockNumber"`
	Result      string       `json:"result"`
	Receipt     struct {
		Result string `json:"result"`
	} `json:"receipt"`
	Logs []struct {
		Address string   `json:"address"`
		Topics  []string `json:"topics"`
		Data    string   `json:"data"`
	} `json:"log"`
	Error string `json:"Error"`
}

func (a *TronAdapter) VerifyNetwork(ctx context.Context) error {
	for _, route := range []string{"/walletsolidity/getblockbynum", "/wallet/getblockbynum"} {
		var block tronBlock
		if err := a.node.post(ctx, route, map[string]any{"num": 0}, &block); err != nil {
			return err
		}
		if block.Error != "" {
			return ErrInvalidResponse
		}
		if !strings.EqualFold(block.ID, TronMainnetGenesis) || block.Header.Raw.Number != 0 {
			return ErrWrongNetwork
		}
	}
	return nil
}

func validateTronBlock(b tronBlock, height *uint64) (string, error) {
	if b.Error != "" {
		return "", ErrInvalidResponse
	}
	raw, err := decodeHex(b.ID, 32)
	if err != nil {
		return "", err
	}
	if binary.BigEndian.Uint64(raw[:8]) != b.Header.Raw.Number {
		return "", fmt.Errorf("%w: block height/hash", ErrInvalidResponse)
	}
	if height != nil && *height != b.Header.Raw.Number {
		return "", fmt.Errorf("%w: wrong block", ErrInvalidResponse)
	}
	return hex.EncodeToString(raw), nil
}

func (a *TronAdapter) finalizedHeight(ctx context.Context) (uint64, error) {
	var b tronBlock
	if err := a.node.post(ctx, "/walletsolidity/getnowblock", map[string]any{}, &b); err != nil {
		return 0, err
	}
	if _, err := validateTronBlock(b, nil); err != nil {
		return 0, err
	}
	return b.Header.Raw.Number, nil
}
func (a *TronAdapter) FinalizedHeight(ctx context.Context) (uint64, error) {
	if err := a.VerifyNetwork(ctx); err != nil {
		return 0, err
	}
	return a.finalizedHeight(ctx)
}

func (a *TronAdapter) block(ctx context.Context, height uint64) (tronBlock, error) {
	var b tronBlock
	if err := a.node.post(ctx, "/walletsolidity/getblockbynum", map[string]any{"num": height}, &b); err != nil {
		return b, err
	}
	_, err := validateTronBlock(b, &height)
	return b, err
}
func (a *TronAdapter) CanonicalBlockHash(ctx context.Context, height uint64) (string, error) {
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
	b, err := a.block(ctx, height)
	if err != nil {
		return "", err
	}
	return strings.ToLower(b.ID), nil
}

func (a *TronAdapter) receipt(ctx context.Context, txID string) (tronReceipt, error) {
	var r tronReceipt
	if err := a.node.post(ctx, "/walletsolidity/gettransactioninfobyid", map[string]any{"value": txID}, &r); err != nil {
		return r, err
	}
	if r.Error != "" {
		return r, ErrInvalidResponse
	}
	if r.ID != "" && !strings.EqualFold(r.ID, txID) {
		return r, fmt.Errorf("%w: receipt transaction", ErrInvalidResponse)
	}
	return r, nil
}

func tronReceiptTransfers(txID, blockHash string, height uint64, r tronReceipt) ([]Transfer, error) {
	if r.ID == "" || r.BlockNumber != height {
		return nil, fmt.Errorf("%w: missing or wrong receipt block", ErrInvalidResponse)
	}
	if r.Result == "FAILED" || r.Receipt.Result != "SUCCESS" {
		if r.Receipt.Result == "" && r.Result != "FAILED" {
			return nil, fmt.Errorf("%w: missing execution status", ErrInvalidResponse)
		}
		return nil, nil
	}
	var out []Transfer
	for i, log := range r.Logs {
		from, to, amount, match, err := parseTransferEvent(Tron, log.Address, log.Topics, log.Data)
		if err != nil {
			return nil, err
		}
		if !match {
			continue
		}
		out = append(out, Transfer{Chain: Tron, TxID: txID, EventIndex: uint64(i), BlockHeight: height, BlockHash: blockHash, From: from, To: to, Contract: TronUSDTContract, AtomicAmount: amount})
	}
	sortTransfers(out)
	return out, nil
}

func (a *TronAdapter) ScanTransfers(ctx context.Context, from, to uint64) ([]Transfer, error) {
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
	var out []Transfer
	seen := make(map[string]bool)
	for height := from; ; height++ {
		b, err := a.block(ctx, height)
		if err != nil {
			return nil, err
		}
		for _, tx := range b.Transactions {
			id, err := normalizeHash(Tron, tx.TxID)
			if err != nil {
				return nil, err
			}
			if seen[id] {
				continue
			}
			seen[id] = true
			needsReceipt := false
			for _, c := range tx.RawData.Contract {
				if c.Type == "TriggerSmartContract" || c.Type == "CreateSmartContract" {
					needsReceipt = true
				}
			}
			if !needsReceipt {
				continue
			}
			r, err := a.receipt(ctx, id)
			if err != nil {
				return nil, err
			}
			transfers, err := tronReceiptTransfers(id, strings.ToLower(b.ID), height, r)
			if err != nil {
				return nil, err
			}
			if b.Header.Raw.Timestamp == 0 || b.Header.Raw.Timestamp > 253402300799000 {
				return nil, fmt.Errorf("%w: block timestamp", ErrInvalidResponse)
			}
			for i := range transfers {
				transfers[i].OccurredAt = time.UnixMilli(int64(b.Header.Raw.Timestamp)).UTC()
			}
			out = append(out, transfers...)
		}
		if height == to {
			break
		}
	}
	sortTransfers(out)
	return out, nil
}

func (a *TronAdapter) Lookup(ctx context.Context, txID string) (TransactionStatus, error) {
	status := TransactionStatus{Chain: Tron}
	id, err := normalizeHash(Tron, txID)
	if err != nil {
		return status, err
	}
	status.TxID = id
	if err := a.VerifyNetwork(ctx); err != nil {
		return status, err
	}
	var tx tronTransaction
	if err := a.node.post(ctx, "/walletsolidity/gettransactionbyid", map[string]any{"value": id}, &tx); err != nil {
		return status, err
	}
	if tx.Error != "" {
		return status, ErrInvalidResponse
	}
	if tx.TxID == "" {
		return status, nil
	}
	if !strings.EqualFold(tx.TxID, id) {
		return status, ErrInvalidResponse
	}
	r, err := a.receipt(ctx, id)
	if err != nil {
		return status, err
	}
	if r.ID == "" {
		return status, nil
	}
	finalized, err := a.finalizedHeight(ctx)
	if err != nil {
		return status, err
	}
	if r.BlockNumber > finalized {
		return status, ErrInvalidResponse
	}
	b, err := a.block(ctx, r.BlockNumber)
	if err != nil {
		return status, err
	}
	included := false
	for _, t := range b.Transactions {
		if strings.EqualFold(t.TxID, id) {
			included = true
			break
		}
	}
	if !included {
		return status, fmt.Errorf("%w: transaction absent from block", ErrInvalidResponse)
	}
	if r.Fee == nil {
		return status, fmt.Errorf("%w: missing network fee", ErrInvalidResponse)
	}
	fee, err := strconv.ParseUint(r.Fee.String(), 10, 64)
	if err != nil || r.Fee.String() != strconv.FormatUint(fee, 10) {
		return status, fmt.Errorf("%w: invalid network fee", ErrInvalidResponse)
	}
	status.FeeAtomic = strconv.FormatUint(fee, 10)
	status.Found = true
	status.Finalized = true
	status.BlockHeight = r.BlockNumber
	status.BlockHash = strings.ToLower(b.ID)
	status.Success = r.Result != "FAILED" && r.Receipt.Result == "SUCCESS"
	if r.Receipt.Result == "" && r.Result != "FAILED" {
		return TransactionStatus{Chain: Tron, TxID: id}, nil
	}
	for _, ret := range tx.Ret {
		if ret.ContractRet != "" && ret.ContractRet != "SUCCESS" {
			status.Success = false
		}
	}
	if status.Success {
		status.Transfers, err = tronReceiptTransfers(id, status.BlockHash, status.BlockHeight, r)
		if err != nil {
			return TransactionStatus{Chain: Tron, TxID: id}, err
		}
		if b.Header.Raw.Timestamp == 0 || b.Header.Raw.Timestamp > 253402300799000 {
			return TransactionStatus{Chain: Tron, TxID: id}, ErrInvalidResponse
		}
		for i := range status.Transfers {
			status.Transfers[i].OccurredAt = time.UnixMilli(int64(b.Header.Raw.Timestamp)).UTC()
		}
	}
	return status, nil
}

func (a *TronAdapter) Balance(ctx context.Context, address string) (string, error) {
	addr, err := NormalizeAddress(Tron, address)
	if err != nil {
		return "", err
	}
	if err := a.VerifyNetwork(ctx); err != nil {
		return "", err
	}
	body, _ := addressBytes(Tron, addr)
	var result struct {
		Result struct {
			Result bool `json:"result"`
		} `json:"result"`
		ConstantResult []string `json:"constant_result"`
		Error          string   `json:"Error"`
	}
	if err := a.node.post(ctx, "/walletsolidity/triggerconstantcontract", map[string]any{"owner_address": addr, "contract_address": TronUSDTContract, "function_selector": "balanceOf(address)", "parameter": strings.Repeat("0", 24) + hex.EncodeToString(body), "visible": true}, &result); err != nil {
		return "", err
	}
	if result.Error != "" || !result.Result.Result || len(result.ConstantResult) != 1 {
		return "", ErrInvalidResponse
	}
	return uint256Result(result.ConstantResult[0])
}

var _ Adapter = (*TronAdapter)(nil)
