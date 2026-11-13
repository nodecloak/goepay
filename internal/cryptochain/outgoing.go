package cryptochain

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
	"time"
)

const tronTriggerTypeURL = "type.googleapis.com/protocol.TriggerSmartContract"

type tronUnsigned struct {
	TxID       string   `json:"txID"`
	RawDataHex string   `json:"raw_data_hex"`
	Signature  []string `json:"signature,omitempty"`
	RawData    struct {
		RefBlockBytes string `json:"ref_block_bytes"`
		RefBlockNum   uint64 `json:"ref_block_num"`
		RefBlockHash  string `json:"ref_block_hash"`
		Expiration    uint64 `json:"expiration"`
		Timestamp     uint64 `json:"timestamp"`
		FeeLimit      uint64 `json:"fee_limit"`
		Contract      []struct {
			Type         string `json:"type"`
			PermissionID uint64 `json:"Permission_id"`
			Parameter    struct {
				TypeURL string `json:"type_url"`
				Value   struct {
					OwnerAddress    string `json:"owner_address"`
					ContractAddress string `json:"contract_address"`
					Data            string `json:"data"`
					CallValue       uint64 `json:"call_value"`
					CallTokenValue  uint64 `json:"call_token_value"`
					TokenID         uint64 `json:"token_id"`
				} `json:"value"`
			} `json:"parameter"`
		} `json:"contract"`
	} `json:"raw_data"`
}

func protoBytes(out []byte, field uint64, data []byte) []byte {
	out = binary.AppendUvarint(out, field<<3|2)
	out = binary.AppendUvarint(out, uint64(len(data)))
	return append(out, data...)
}
func protoUint(out []byte, field, value uint64) []byte {
	out = binary.AppendUvarint(out, field<<3)
	return binary.AppendUvarint(out, value)
}

// Validate both JSON and its protobuf serialization. The independent signer
// still redecodes and authorizes raw protobuf; this boundary never signs bytes.
func validateTronPayload(tx tronUnsigned, signed bool) (from, to, amount string, err error) {
	if len(tx.RawDataHex) > 32768 || len(tx.RawData.Contract) != 1 {
		return "", "", "", ErrInvalidResponse
	}
	raw, err := decodeHex(tx.RawDataHex, -1)
	if err != nil {
		return "", "", "", err
	}
	digest := sha256.Sum256(raw)
	if !strings.EqualFold(tx.TxID, hex.EncodeToString(digest[:])) {
		return "", "", "", fmt.Errorf("%w: transaction digest", ErrInvalidResponse)
	}
	if signed {
		if len(tx.Signature) != 1 {
			return "", "", "", ErrInvalidResponse
		}
		if _, err = decodeHex(tx.Signature[0], 65); err != nil {
			return "", "", "", err
		}
	} else if len(tx.Signature) != 0 {
		return "", "", "", ErrInvalidResponse
	}
	r := tx.RawData
	c := r.Contract[0]
	v := c.Parameter.Value
	if c.Type != "TriggerSmartContract" || c.Parameter.TypeURL != tronTriggerTypeURL || c.PermissionID != 0 || v.CallValue != 0 || v.CallTokenValue != 0 || v.TokenID != 0 {
		return "", "", "", ErrInvalidResponse
	}
	from, err = NormalizeAddress(Tron, v.OwnerAddress)
	if err != nil {
		return "", "", "", err
	}
	contract, err := NormalizeAddress(Tron, v.ContractAddress)
	if err != nil || contract != TronUSDTContract {
		return "", "", "", ErrInvalidResponse
	}
	data, err := decodeHex(v.Data, 68)
	if err != nil || !bytes.Equal(data[:4], []byte{0xa9, 0x05, 0x9c, 0xbb}) {
		return "", "", "", ErrInvalidResponse
	}
	if !bytes.Equal(data[4:16], make([]byte, 12)) {
		return "", "", "", ErrInvalidResponse
	}
	to = formatAddress(Tron, data[16:36])
	if _, err = NormalizeAddress(Tron, to); err != nil {
		return "", "", "", err
	}
	amount = new(big.Int).SetBytes(data[36:]).String()
	if _, err = ParseAtomicAmount(amount); err != nil {
		return "", "", "", err
	}
	now := uint64(time.Now().UnixMilli())
	if r.Timestamp == 0 || r.Timestamp > now+30000 || r.Expiration <= now || r.Expiration <= r.Timestamp || r.Expiration-r.Timestamp > 86400000 || r.FeeLimit == 0 || r.FeeLimit > 15000000000 {
		return "", "", "", fmt.Errorf("%w: transaction time or fee budget", ErrInvalidResponse)
	}
	refBytes, err := decodeHex(r.RefBlockBytes, 2)
	if err != nil {
		return "", "", "", err
	}
	refHash, err := decodeHex(r.RefBlockHash, 8)
	if err != nil {
		return "", "", "", err
	}
	owner, _ := addressBytes(Tron, from)
	token, _ := addressBytes(Tron, TronUSDTContract)
	trigger := protoBytes(nil, 1, append([]byte{0x41}, owner...))
	trigger = protoBytes(trigger, 2, append([]byte{0x41}, token...))
	trigger = protoBytes(trigger, 4, data)
	anyValue := protoBytes(nil, 1, []byte(tronTriggerTypeURL))
	anyValue = protoBytes(anyValue, 2, trigger)
	call := protoUint(nil, 1, 31)
	call = protoBytes(call, 2, anyValue)
	expected := protoBytes(nil, 1, refBytes)
	if r.RefBlockNum != 0 {
		expected = protoUint(expected, 3, r.RefBlockNum)
	}
	expected = protoBytes(expected, 4, refHash)
	expected = protoUint(expected, 8, r.Expiration)
	expected = protoBytes(expected, 11, call)
	expected = protoUint(expected, 14, r.Timestamp)
	expected = protoUint(expected, 18, r.FeeLimit)
	if !bytes.Equal(expected, raw) {
		return "", "", "", fmt.Errorf("%w: JSON/protobuf mismatch or unsupported transaction fields", ErrInvalidResponse)
	}
	return from, to, amount, nil
}

func (a *TronAdapter) Prepare(ctx context.Context, request TransferRequest) (PreparedTransaction, error) {
	r, err := normalizedRequest(Tron, request)
	if err != nil {
		return PreparedTransaction{}, err
	}
	if r.Nonce != nil {
		return PreparedTransaction{}, fmt.Errorf("%w: TRON does not use account nonces", ErrInvalidResponse)
	}
	if r.FeeLimit == 0 {
		r.FeeLimit = 100000000
	}
	if r.FeeLimit > 15000000000 {
		return PreparedTransaction{}, ErrInvalidResponse
	}
	if err := a.VerifyNetwork(ctx); err != nil {
		return PreparedTransaction{}, err
	}
	data, _ := TransferCallData(Tron, r.To, r.AtomicAmount)
	owner, _ := addressBytes(Tron, r.From)
	contract, _ := addressBytes(Tron, TronUSDTContract)
	var result struct {
		Result struct {
			Result bool `json:"result"`
		} `json:"result"`
		Transaction json.RawMessage `json:"transaction"`
		Error       string          `json:"Error"`
	}
	input := map[string]any{"owner_address": "41" + hex.EncodeToString(owner), "contract_address": "41" + hex.EncodeToString(contract), "function_selector": "transfer(address,uint256)", "parameter": data[8:], "fee_limit": r.FeeLimit, "call_value": 0, "visible": false}
	if err := a.node.post(ctx, "/wallet/triggersmartcontract", input, &result); err != nil {
		return PreparedTransaction{}, err
	}
	if !result.Result.Result || result.Error != "" {
		return PreparedTransaction{}, ErrInvalidResponse
	}
	var tx tronUnsigned
	if err := json.Unmarshal(result.Transaction, &tx); err != nil {
		return PreparedTransaction{}, ErrInvalidResponse
	}
	from, to, amount, err := validateTronPayload(tx, false)
	if err != nil {
		return PreparedTransaction{}, err
	}
	if from != r.From || to != r.To || amount != r.AtomicAmount || tx.RawData.FeeLimit != r.FeeLimit {
		return PreparedTransaction{}, fmt.Errorf("%w: builder changed requested transfer", ErrInvalidResponse)
	}
	return PreparedTransaction{Chain: Tron, Network: Mainnet, ChainID: "728126428", RequestID: r.RequestID, From: r.From, To: r.To, Contract: TronUSDTContract, AtomicAmount: r.AtomicAmount, UnsignedPayload: string(result.Transaction), SigningPayload: strings.ToLower(tx.RawDataHex), TxID: strings.ToLower(tx.TxID), FeeLimit: r.FeeLimit, Expiration: tx.RawData.Expiration}, nil
}

func (a *TronAdapter) Broadcast(ctx context.Context, tx SignedTransaction) (BroadcastResult, error) {
	if !a.node.allowBroadcast {
		return BroadcastResult{}, ErrBroadcastDisabled
	}
	if tx.Chain != Tron || len(tx.RawTransaction) > 65536 {
		return BroadcastResult{}, ErrInvalidResponse
	}
	var payload tronUnsigned
	if err := json.Unmarshal([]byte(tx.RawTransaction), &payload); err != nil {
		return BroadcastResult{}, ErrInvalidResponse
	}
	if _, _, _, err := validateTronPayload(payload, true); err != nil {
		return BroadcastResult{}, err
	}
	id, err := normalizeHash(Tron, tx.TxID)
	if err != nil || !strings.EqualFold(id, payload.TxID) {
		return BroadcastResult{}, ErrInvalidResponse
	}
	if err := a.VerifyNetwork(ctx); err != nil {
		return BroadcastResult{}, err
	}
	var response struct {
		Result bool   `json:"result"`
		TxID   string `json:"txid"`
		Code   string `json:"code"`
	}
	if err := a.node.post(ctx, "/wallet/broadcasttransaction", json.RawMessage(tx.RawTransaction), &response); err != nil {
		return BroadcastResult{TxID: id}, err
	}
	if !response.Result {
		return BroadcastResult{TxID: id}, fmt.Errorf("cryptochain: broadcast rejected")
	}
	if !strings.EqualFold(response.TxID, id) {
		return BroadcastResult{TxID: id}, ErrInvalidResponse
	}
	return BroadcastResult{TxID: id, Accepted: true}, nil
}

func (a *EthereumAdapter) Prepare(ctx context.Context, request TransferRequest) (PreparedTransaction, error) {
	r, err := normalizedRequest(Ethereum, request)
	if err != nil {
		return PreparedTransaction{}, err
	}
	if err := a.VerifyNetwork(ctx); err != nil {
		return PreparedTransaction{}, err
	}
	var nonce uint64
	if r.Nonce != nil {
		nonce = *r.Nonce
	} else {
		var result string
		if err := a.node.rpc(ctx, "eth_getTransactionCount", []any{r.From, "pending"}, &result); err != nil {
			return PreparedTransaction{}, err
		}
		nonce, err = hexQuantity(result)
		if err != nil {
			return PreparedTransaction{}, err
		}
	}
	var priceHex string
	if err := a.node.rpc(ctx, "eth_gasPrice", []any{}, &priceHex); err != nil {
		return PreparedTransaction{}, err
	}
	price, err := hexBigQuantity(priceHex)
	if err != nil || price.Sign() <= 0 {
		return PreparedTransaction{}, ErrInvalidResponse
	}
	data, _ := TransferCallData(Ethereum, r.To, r.AtomicAmount)
	var gasHex string
	if err := a.node.rpc(ctx, "eth_estimateGas", []any{map[string]any{"from": r.From, "to": EthereumUSDTContract, "data": "0x" + data, "value": "0x0"}}, &gasHex); err != nil {
		return PreparedTransaction{}, err
	}
	estimated, err := hexQuantity(gasHex)
	if err != nil || estimated < 21000 || estimated > 833333 {
		return PreparedTransaction{}, ErrInvalidResponse
	}
	gas := estimated + estimated/5
	fee := new(big.Int).Mul(price, new(big.Int).SetUint64(gas))
	if !fee.IsUint64() || r.FeeLimit > 0 && fee.Uint64() > r.FeeLimit {
		return PreparedTransaction{}, fmt.Errorf("%w: fee budget exceeded", ErrInvalidResponse)
	}
	if r.FeeLimit == 0 {
		r.FeeLimit = fee.Uint64()
	}
	to, _ := addressBytes(Ethereum, EthereumUSDTContract)
	dataBytes, _ := hex.DecodeString(data)
	unsigned := encodeRLPList([][]byte{new(big.Int).SetUint64(nonce).Bytes(), price.Bytes(), new(big.Int).SetUint64(gas).Bytes(), to, {}, dataBytes, {1}, {}, {}})
	return PreparedTransaction{Chain: Ethereum, Network: Mainnet, ChainID: "1", RequestID: r.RequestID, From: r.From, To: r.To, Contract: EthereumUSDTContract, AtomicAmount: r.AtomicAmount, UnsignedPayload: hex.EncodeToString(unsigned), SigningPayload: hex.EncodeToString(keccak(unsigned)), Nonce: nonce, GasLimit: gas, GasPrice: price.String(), FeeLimit: r.FeeLimit}, nil
}

func hexBigQuantity(s string) (*big.Int, error) {
	if !strings.HasPrefix(s, "0x") || len(s) < 3 || len(s) > 66 || len(s) > 3 && s[2] == '0' {
		return nil, ErrInvalidResponse
	}
	for _, c := range s[2:] {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
			return nil, ErrInvalidResponse
		}
	}
	v, ok := new(big.Int).SetString(s[2:], 16)
	if !ok || v.Sign() < 0 || v.BitLen() > 256 {
		return nil, ErrInvalidResponse
	}
	return v, nil
}

func validateEthereumSigned(raw []byte) error {
	fields, err := decodeRLPList(raw)
	if err != nil || len(fields) != 9 {
		return ErrInvalidResponse
	}
	for _, i := range []int{0, 1, 2, 4, 6, 7, 8} {
		if len(fields[i]) > 32 || len(fields[i]) > 0 && fields[i][0] == 0 {
			return ErrInvalidResponse
		}
	}
	if len(fields[0]) > 8 || len(fields[2]) > 8 || len(fields[1]) == 0 || len(fields[2]) == 0 || len(fields[4]) != 0 || len(fields[6]) != 1 || (fields[6][0] != 37 && fields[6][0] != 38) || len(fields[7]) == 0 || len(fields[8]) == 0 {
		return ErrInvalidResponse
	}
	if formatAddress(Ethereum, fields[3]) != EthereumUSDTContract {
		return ErrInvalidResponse
	}
	data := fields[5]
	if len(data) != 68 || !bytes.Equal(data[:4], []byte{0xa9, 0x05, 0x9c, 0xbb}) || !bytes.Equal(data[4:16], make([]byte, 12)) {
		return ErrInvalidResponse
	}
	if _, err := NormalizeAddress(Ethereum, formatAddress(Ethereum, data[16:36])); err != nil {
		return err
	}
	_, err = ParseAtomicAmount(new(big.Int).SetBytes(data[36:]).String())
	return err
}

func (a *EthereumAdapter) Broadcast(ctx context.Context, tx SignedTransaction) (BroadcastResult, error) {
	if !a.node.allowBroadcast {
		return BroadcastResult{}, ErrBroadcastDisabled
	}
	if tx.Chain != Ethereum {
		return BroadcastResult{}, ErrInvalidResponse
	}
	if len(tx.RawTransaction) > 32768 {
		return BroadcastResult{}, ErrInvalidResponse
	}
	raw, err := decodeHex(tx.RawTransaction, -1)
	if err != nil {
		return BroadcastResult{}, err
	}
	if err := validateEthereumSigned(raw); err != nil {
		return BroadcastResult{}, err
	}
	id := "0x" + hex.EncodeToString(keccak(raw))
	expected, err := normalizeHash(Ethereum, tx.TxID)
	if err != nil || id != expected {
		return BroadcastResult{}, ErrInvalidResponse
	}
	if err := a.VerifyNetwork(ctx); err != nil {
		return BroadcastResult{TxID: id}, err
	}
	var returned string
	if err := a.node.rpc(ctx, "eth_sendRawTransaction", []any{"0x" + hex.EncodeToString(raw)}, &returned); err != nil {
		return BroadcastResult{TxID: id}, err
	}
	if !strings.EqualFold(returned, id) {
		return BroadcastResult{TxID: id}, ErrInvalidResponse
	}
	return BroadcastResult{TxID: id, Accepted: true}, nil
}
