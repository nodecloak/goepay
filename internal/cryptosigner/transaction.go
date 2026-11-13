package cryptosigner

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"time"

	"goepay/internal/cryptochain"
)

type validatedTransaction struct {
	digest      []byte
	reservation string
	finish      func([]byte) (cryptochain.SignedTransaction, error)
}

func validateTransaction(ctx context.Context, p cryptochain.PreparedTransaction, r Rule, now time.Time) (validatedTransaction, error) {
	if p.Chain == cryptochain.Ethereum {
		return validateEthereum(p, r)
	}
	return validateTRON(ctx, p, r, now)
}

func validateEthereum(p cryptochain.PreparedTransaction, r Rule) (validatedTransaction, error) {
	var z validatedTransaction
	raw, err := decodeHex(p.UnsignedPayload)
	if err != nil {
		return z, err
	}
	fields, err := decodeList(raw)
	if err != nil || len(fields) != 9 {
		return z, fmt.Errorf("only unsigned legacy EIP-155 transactions accepted")
	}
	for _, i := range []int{0, 1, 2, 4, 6, 7, 8} {
		if len(fields[i]) > 0 && fields[i][0] == 0 {
			return z, fmt.Errorf("noncanonical transaction integer")
		}
	}
	if p.ChainID != "1" || !bytes.Equal(fields[6], []byte{1}) || len(fields[7])+len(fields[8]) != 0 {
		return z, fmt.Errorf("wrong chain or pre-signed transaction")
	}
	if p.Nonce < r.MinNonce || p.Nonce > r.MaxNonce || !bytes.Equal(fields[0], uintBytes(p.Nonce)) {
		return z, fmt.Errorf("nonce denied")
	}
	if p.GasLimit == 0 || p.GasLimit > r.MaxGasLimit || !bytes.Equal(fields[2], uintBytes(p.GasLimit)) {
		return z, fmt.Errorf("gas limit denied")
	}
	if !validPositive(p.GasPrice) || mustBig(p.GasPrice).Cmp(mustBig(r.MaxGasPrice)) > 0 || !bytes.Equal(fields[1], mustBig(p.GasPrice).Bytes()) {
		return z, fmt.Errorf("gas price denied")
	}
	fee := new(big.Int).Mul(mustBig(p.GasPrice), new(big.Int).SetUint64(p.GasLimit))
	if p.FeeLimit > 0 && fee.Cmp(new(big.Int).SetUint64(p.FeeLimit)) > 0 {
		return z, fmt.Errorf("requested Ethereum fee budget exceeded")
	}
	if p.Expiration != 0 || p.TxID != "" || len(fields[4]) != 0 {
		return z, fmt.Errorf("unexpected native value or metadata")
	}
	contract, err := addressBytes(p.Chain, p.Contract)
	if err != nil || !bytes.Equal(fields[3], contract) {
		return z, fmt.Errorf("wrong token contract")
	}
	if err = checkCallData(p, fields[5]); err != nil {
		return z, err
	}
	digest := keccak(raw)
	claimed, err := decodeHex(p.SigningPayload)
	if err != nil || !bytes.Equal(digest, claimed) {
		return z, fmt.Errorf("signing digest mismatch")
	}
	return validatedTransaction{digest: digest, reservation: strconv.FormatUint(p.Nonce, 10), finish: func(sig []byte) (cryptochain.SignedTransaction, error) {
		out := make([][]byte, 9)
		copy(out, fields[:6])
		out[6] = uintBytes(37 + uint64(sig[64]))
		out[7] = bytes.TrimLeft(sig[:32], "\x00")
		out[8] = bytes.TrimLeft(sig[32:64], "\x00")
		signed := encodeList(out)
		return cryptochain.SignedTransaction{Chain: cryptochain.Ethereum, TxID: "0x" + hex.EncodeToString(keccak(signed)), RawTransaction: "0x" + hex.EncodeToString(signed)}, nil
	}}, nil
}

func checkCallData(p cryptochain.PreparedTransaction, data []byte) error {
	if len(data) != 68 || !bytes.Equal(data[:4], []byte{0xa9, 0x05, 0x9c, 0xbb}) || !bytes.Equal(data[4:16], make([]byte, 12)) {
		return fmt.Errorf("only transfer(address,uint256) accepted")
	}
	to, err := addressBytes(p.Chain, p.To)
	if err != nil {
		return err
	}
	if p.Chain == cryptochain.Tron {
		to = to[1:]
	}
	if !bytes.Equal(data[16:36], to) || new(big.Int).SetBytes(data[36:]).String() != p.AtomicAmount {
		return fmt.Errorf("actual transfer differs from approved request")
	}
	return nil
}

func addressBytes(chain cryptochain.Chain, s string) ([]byte, error) {
	canonical, err := cryptochain.NormalizeAddress(chain, s)
	if err != nil {
		return nil, err
	}
	if chain == cryptochain.Ethereum {
		return hex.DecodeString(canonical[2:])
	}
	const alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"
	n := new(big.Int)
	for _, c := range canonical {
		n.Mul(n, big.NewInt(58))
		n.Add(n, big.NewInt(int64(strings.IndexRune(alphabet, c))))
	}
	b := n.Bytes()
	if len(b) != 25 {
		return nil, fmt.Errorf("invalid TRON address")
	}
	return b[:21], nil
}

func decodeHex(s string) ([]byte, error) {
	if len(s) > 2<<20 {
		return nil, fmt.Errorf("transaction too large")
	}
	return hex.DecodeString(strings.TrimPrefix(s, "0x"))
}
func uintBytes(n uint64) []byte {
	if n == 0 {
		return nil
	}
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, n)
	return bytes.TrimLeft(b, "\x00")
}

// Limited canonical RLP codec: transaction fields are flat byte strings. Nested
// lists, oversized integers and non-minimal length forms are rejected.
func encodeList(fields [][]byte) []byte {
	var body []byte
	for _, b := range fields {
		if len(b) == 1 && b[0] < 128 {
			body = append(body, b...)
		} else {
			body = append(body, rlpPrefix(len(b), 0x80, 0xb7)...)
			body = append(body, b...)
		}
	}
	return append(rlpPrefix(len(body), 0xc0, 0xf7), body...)
}
func rlpPrefix(n int, short, long byte) []byte {
	if n < 56 {
		return []byte{short + byte(n)}
	}
	b := uintBytes(uint64(n))
	return append([]byte{long + byte(len(b))}, b...)
}
func decodeList(raw []byte) ([][]byte, error) {
	list, rest, isList, err := rlpItem(raw)
	if err != nil || !isList || len(rest) != 0 {
		return nil, fmt.Errorf("invalid transaction RLP")
	}
	var out [][]byte
	for len(list) > 0 {
		b, tail, nested, e := rlpItem(list)
		if e != nil || nested || len(out) > 9 {
			return nil, fmt.Errorf("invalid transaction field")
		}
		out = append(out, b)
		list = tail
	}
	return out, nil
}
func rlpItem(raw []byte) ([]byte, []byte, bool, error) {
	if len(raw) == 0 {
		return nil, nil, false, fmt.Errorf("truncated RLP")
	}
	p := raw[0]
	if p < 128 {
		return raw[:1], raw[1:], false, nil
	}
	list := p >= 0xc0
	start := byte(0x80)
	long := byte(0xb7)
	if list {
		start = 0xc0
		long = 0xf7
	}
	offset := 1
	n := int(p - start)
	if p > long {
		count := int(p - long)
		if count > 4 || len(raw) < 1+count || raw[1] == 0 {
			return nil, nil, false, fmt.Errorf("invalid RLP length")
		}
		n = 0
		for _, b := range raw[1 : 1+count] {
			n = n<<8 | int(b)
		}
		offset += count
		if n < 56 {
			return nil, nil, false, fmt.Errorf("noncanonical RLP length")
		}
	}
	if n > len(raw)-offset {
		return nil, nil, false, fmt.Errorf("truncated RLP data")
	}
	if !list && n == 1 && raw[offset] < 128 {
		return nil, nil, false, fmt.Errorf("noncanonical RLP byte")
	}
	return raw[offset : offset+n], raw[offset+n:], list, nil
}

type protoValue struct {
	wire uint64
	n    uint64
	b    []byte
}

func parseProto(b []byte, allowed map[uint64]uint64) (map[uint64]protoValue, error) {
	out := map[uint64]protoValue{}
	var previous uint64
	for len(b) > 0 {
		tag, n := binary.Uvarint(b)
		if n <= 0 || len(binary.AppendUvarint(nil, tag)) != n {
			return nil, fmt.Errorf("invalid protobuf tag")
		}
		b = b[n:]
		field, wire := tag>>3, tag&7
		if field <= previous {
			return nil, fmt.Errorf("noncanonical protobuf field order")
		}
		previous = field
		want, ok := allowed[field]
		if !ok || want != wire {
			return nil, fmt.Errorf("unexpected protobuf field %d", field)
		}
		if _, exists := out[field]; exists {
			return nil, fmt.Errorf("duplicate protobuf field %d", field)
		}
		v := protoValue{wire: wire}
		switch wire {
		case 0:
			v.n, n = binary.Uvarint(b)
			if n <= 0 || v.n == 0 || len(binary.AppendUvarint(nil, v.n)) != n {
				return nil, fmt.Errorf("invalid protobuf integer")
			}
			b = b[n:]
		case 2:
			var size uint64
			size, n = binary.Uvarint(b)
			if n <= 0 || size == 0 || len(binary.AppendUvarint(nil, size)) != n || size > uint64(len(b)-n) {
				return nil, fmt.Errorf("invalid protobuf bytes")
			}
			b = b[n:]
			v.b = b[:int(size)]
			b = b[int(size):]
		default:
			return nil, fmt.Errorf("unsupported protobuf wire")
		}
		out[field] = v
	}
	return out, nil
}

func validateTRON(ctx context.Context, p cryptochain.PreparedTransaction, r Rule, now time.Time) (validatedTransaction, error) {
	v, err := validateTRONContents(p, r, now)
	if err != nil {
		return validatedTransaction{}, err
	}
	raw, _ := decodeHex(p.SigningPayload)
	f, err := parseProto(raw, map[uint64]uint64{1: 2, 3: 0, 4: 2, 8: 0, 11: 2, 14: 0, 18: 0})
	if err != nil {
		return validatedTransaction{}, err
	}
	if err = verifyTRONReference(ctx, r, f[1].b, f[4].b, f[3].n); err != nil {
		return validatedTransaction{}, err
	}
	return v, nil
}

func validateTRONContents(p cryptochain.PreparedTransaction, r Rule, now time.Time) (validatedTransaction, error) {
	var z validatedTransaction
	if p.Nonce != 0 || p.GasLimit != 0 || p.GasPrice != "" || p.FeeLimit == 0 || p.FeeLimit > r.MaxFeeLimit {
		return z, fmt.Errorf("TRON fee metadata denied")
	}
	raw, err := decodeHex(p.SigningPayload)
	if err != nil {
		return z, err
	}
	f, err := parseProto(raw, map[uint64]uint64{1: 2, 3: 0, 4: 2, 8: 0, 11: 2, 14: 0, 18: 0})
	if err != nil {
		return z, err
	}
	if len(f[1].b) != 2 || len(f[4].b) != 8 || f[18].n != p.FeeLimit || f[8].n != p.Expiration {
		return z, fmt.Errorf("TRON reference, fee or expiration mismatch")
	}
	nowMS := uint64(now.UnixMilli())
	if p.Expiration <= nowMS || p.Expiration > nowMS+r.MaxTTLSeconds*1000 || f[14].n > nowMS+30000 || f[14].n+86400000 < nowMS || f[14].n >= p.Expiration {
		return z, fmt.Errorf("TRON expiration or timestamp denied")
	}
	c, err := parseProto(f[11].b, map[uint64]uint64{1: 0, 2: 2})
	if err != nil || c[1].n != 31 {
		return z, fmt.Errorf("only one TriggerSmartContract allowed")
	}
	a, err := parseProto(c[2].b, map[uint64]uint64{1: 2, 2: 2})
	if err != nil || string(a[1].b) != "type.googleapis.com/protocol.TriggerSmartContract" {
		return z, fmt.Errorf("invalid TRON contract type")
	}
	t, err := parseProto(a[2].b, map[uint64]uint64{1: 2, 2: 2, 3: 0, 4: 2, 5: 0, 6: 0})
	if err != nil || t[3].n != 0 || t[5].n != 0 || t[6].n != 0 {
		return z, fmt.Errorf("TRON native or token value denied")
	}
	from, _ := addressBytes(p.Chain, p.From)
	contract, _ := addressBytes(p.Chain, p.Contract)
	if !bytes.Equal(t[1].b, from) || !bytes.Equal(t[2].b, contract) {
		return z, fmt.Errorf("TRON owner or token mismatch")
	}
	if err = checkCallData(p, t[4].b); err != nil {
		return z, err
	}
	digest := sha256.Sum256(raw)
	txid := hex.EncodeToString(digest[:])
	if p.TxID != txid {
		return z, fmt.Errorf("TRON transaction ID mismatch")
	}
	// Ignore caller-provided JSON when emitting: regenerate raw_data from the
	// exact validated protobuf to prevent JSON/protobuf semantic substitution.
	var envelope struct {
		RawDataHex string   `json:"raw_data_hex"`
		TxID       string   `json:"txID"`
		Signature  []string `json:"signature"`
	}
	if err = json.Unmarshal([]byte(p.UnsignedPayload), &envelope); err != nil || !strings.EqualFold(envelope.RawDataHex, p.SigningPayload) || envelope.TxID != txid || len(envelope.Signature) != 0 {
		return z, fmt.Errorf("TRON unsigned envelope mismatch")
	}
	return validatedTransaction{digest: digest[:], reservation: txid, finish: func(sig []byte) (cryptochain.SignedTransaction, error) {
		value := map[string]any{"owner_address": hex.EncodeToString(from), "contract_address": hex.EncodeToString(contract), "data": hex.EncodeToString(t[4].b)}
		rawData := map[string]any{"ref_block_bytes": hex.EncodeToString(f[1].b), "ref_block_hash": hex.EncodeToString(f[4].b), "expiration": p.Expiration, "timestamp": f[14].n, "fee_limit": p.FeeLimit, "contract": []any{map[string]any{"type": "TriggerSmartContract", "parameter": map[string]any{"type_url": "type.googleapis.com/protocol.TriggerSmartContract", "value": value}}}}
		if f[3].n != 0 {
			rawData["ref_block_num"] = f[3].n
		}
		b, err := json.Marshal(map[string]any{"visible": false, "txID": txid, "raw_data": rawData, "raw_data_hex": hex.EncodeToString(raw), "signature": []string{hex.EncodeToString(sig)}})
		return cryptochain.SignedTransaction{Chain: cryptochain.Tron, TxID: txid, RawTransaction: string(b)}, err
	}}, nil
}
