package cryptochain

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"math/big"
	"strings"

	"golang.org/x/crypto/sha3"
)

const base58Alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"
const transferTopic = "ddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef"

// NormalizeAddress returns a Base58Check TRON address or lowercase Ethereum
// address. Mixed-case Ethereum inputs must have a valid EIP-55 checksum.
func NormalizeAddress(chain Chain, s string) (string, error) {
	b, err := addressBytes(chain, s)
	if err != nil || bytes.Equal(b, make([]byte, 20)) {
		return "", ErrInvalidAddress
	}
	return formatAddress(chain, b), nil
}

func addressBytes(chain Chain, s string) ([]byte, error) {
	switch chain {
	case Ethereum:
		if !strings.HasPrefix(s, "0x") || len(s) != 42 {
			return nil, ErrInvalidAddress
		}
		body := s[2:]
		b, err := hex.DecodeString(body)
		if err != nil {
			return nil, ErrInvalidAddress
		}
		lower := strings.ToLower(body)
		if body != lower && body != strings.ToUpper(body) {
			digest := hex.EncodeToString(keccak([]byte(lower)))
			for i, c := range body {
				if c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F' {
					wantUpper := digest[i] >= '8'
					isUpper := c >= 'A' && c <= 'F'
					if wantUpper != isUpper {
						return nil, ErrInvalidAddress
					}
				}
			}
		}
		return b, nil
	case Tron:
		if len(s) == 42 && strings.HasPrefix(s, "41") {
			b, err := hex.DecodeString(s)
			if err != nil || len(b) != 21 || b[0] != 0x41 {
				return nil, ErrInvalidAddress
			}
			return b[1:], nil
		}
		if len(s) != 34 {
			return nil, ErrInvalidAddress
		}
		v := new(big.Int)
		for _, c := range s {
			i := strings.IndexRune(base58Alphabet, c)
			if i < 0 {
				return nil, ErrInvalidAddress
			}
			v.Mul(v, big.NewInt(58))
			v.Add(v, big.NewInt(int64(i)))
		}
		b := v.Bytes()
		if len(b) != 25 || b[0] != 0x41 {
			return nil, ErrInvalidAddress
		}
		a := sha256.Sum256(b[:21])
		check := sha256.Sum256(a[:])
		if !bytes.Equal(b[21:], check[:4]) {
			return nil, ErrInvalidAddress
		}
		if formatAddress(Tron, b[1:21]) != s {
			return nil, ErrInvalidAddress
		}
		return b[1:21], nil
	default:
		return nil, ErrInvalidAddress
	}
}

func formatAddress(chain Chain, b []byte) string {
	if chain == Ethereum {
		return "0x" + hex.EncodeToString(b)
	}
	payload := append([]byte{0x41}, b...)
	a := sha256.Sum256(payload)
	check := sha256.Sum256(a[:])
	payload = append(payload, check[:4]...)
	v := new(big.Int).SetBytes(payload)
	var out []byte
	rem := new(big.Int)
	for v.Sign() > 0 {
		v.QuoRem(v, big.NewInt(58), rem)
		out = append(out, base58Alphabet[rem.Int64()])
	}
	for _, c := range payload {
		if c != 0 {
			break
		}
		out = append(out, '1')
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return string(out)
}

// ParseAtomicAmount accepts canonical, positive base-10 uint256 values only.
// No floating-point conversion or decimal scaling occurs at the RPC boundary.
func ParseAtomicAmount(s string) (*big.Int, error) {
	if s == "" || s[0] == '0' || len(s) > 78 {
		return nil, ErrInvalidAmount
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return nil, ErrInvalidAmount
		}
	}
	v, ok := new(big.Int).SetString(s, 10)
	if !ok || v.Sign() <= 0 || v.BitLen() > 256 {
		return nil, ErrInvalidAmount
	}
	return v, nil
}

func keccak(b []byte) []byte { h := sha3.NewLegacyKeccak256(); _, _ = h.Write(b); return h.Sum(nil) }

// TransferCallData encodes the sole supported payment operation,
// transfer(address,uint256). The returned hex does not have a 0x prefix.
func TransferCallData(chain Chain, to, amount string) (string, error) {
	if _, err := NormalizeAddress(chain, to); err != nil {
		return "", err
	}
	b, err := addressBytes(chain, to)
	if err != nil {
		return "", err
	}
	v, err := ParseAtomicAmount(amount)
	if err != nil {
		return "", err
	}
	data := make([]byte, 68)
	copy(data, []byte{0xa9, 0x05, 0x9c, 0xbb})
	copy(data[16:36], b)
	v.FillBytes(data[36:])
	return hex.EncodeToString(data), nil
}
