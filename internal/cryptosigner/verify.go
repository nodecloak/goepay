package cryptosigner

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"time"

	"github.com/decred/dcrd/dcrec/secp256k1/v4/ecdsa"
	"goepay/internal/cryptochain"
)

// VerifySigned independently binds the returned transaction to the exact
// prepared transfer and recovers its source account. No network or key access
// is required, so the API can verify before durably storing and broadcasting.
func VerifySigned(p cryptochain.PreparedTransaction, signed cryptochain.SignedTransaction) error {
	if signed.Chain != p.Chain || p.Network != cryptochain.Mainnet || !validPositive(p.AtomicAmount) {
		return fmt.Errorf("invalid signed transaction network or amount")
	}
	var sig, digest []byte
	if p.Chain == cryptochain.Ethereum {
		if p.ChainID != "1" || !sameAddress(p.Chain, p.Contract, cryptochain.EthereumUSDTContract) {
			return fmt.Errorf("wrong Ethereum network or token")
		}
		v, err := validateEthereum(p, Rule{MaxNonce: ^uint64(0), MaxGasLimit: p.GasLimit, MaxGasPrice: p.GasPrice})
		if err != nil {
			return err
		}
		digest = v.digest
		raw, err := decodeHex(signed.RawTransaction)
		if err != nil {
			return err
		}
		fields, err := decodeList(raw)
		if err != nil || len(fields) != 9 {
			return fmt.Errorf("invalid signed Ethereum RLP")
		}
		original, _ := decodeHex(p.UnsignedPayload)
		unsigned, _ := decodeList(original)
		for i := 0; i < 6; i++ {
			if !bytes.Equal(fields[i], unsigned[i]) {
				return fmt.Errorf("signed transaction substituted field %d", i)
			}
		}
		if len(fields[6]) != 1 || (fields[6][0] != 37 && fields[6][0] != 38) || len(fields[7]) == 0 || len(fields[7]) > 32 || len(fields[8]) == 0 || len(fields[8]) > 32 || fields[7][0] == 0 || fields[8][0] == 0 {
			return fmt.Errorf("invalid Ethereum signature fields")
		}
		sig = make([]byte, 65)
		copy(sig[32-len(fields[7]):32], fields[7])
		copy(sig[64-len(fields[8]):64], fields[8])
		sig[64] = fields[6][0] - 37
		if signed.TxID != "0x"+hex.EncodeToString(keccak(raw)) {
			return fmt.Errorf("Ethereum transaction ID mismatch")
		}
	} else if p.Chain == cryptochain.Tron {
		if p.ChainID != "728126428" || !sameAddress(p.Chain, p.Contract, cryptochain.TronUSDTContract) || p.Expiration == 0 || p.Expiration > uint64(^uint64(0)>>1) {
			return fmt.Errorf("wrong TRON network or token")
		}
		// Verify identity and exact contents without interpreting expiry as proof
		// of non-broadcast. Only the service applies current-time/RPC policy.
		v, err := validateTRONContents(p, Rule{MaxFeeLimit: p.FeeLimit, MaxTTLSeconds: 86400}, time.UnixMilli(int64(p.Expiration)-1))
		if err != nil {
			return err
		}
		digest = v.digest
		var envelope struct {
			Signature []string `json:"signature"`
		}
		if err = json.Unmarshal([]byte(signed.RawTransaction), &envelope); err != nil || len(envelope.Signature) != 1 {
			return fmt.Errorf("invalid TRON signature envelope")
		}
		sig, err = hex.DecodeString(envelope.Signature[0])
		if err != nil || len(sig) != 65 {
			return fmt.Errorf("invalid TRON signature")
		}
		want, err := v.finish(sig)
		if err != nil {
			return err
		}
		if signed.TxID != want.TxID || !sameJSON(signed.RawTransaction, want.RawTransaction) {
			return fmt.Errorf("TRON signed transaction differs from prepared transfer")
		}
	} else {
		return fmt.Errorf("unsupported signed transaction chain")
	}
	if len(sig) != 65 || sig[64] > 1 {
		return fmt.Errorf("invalid recoverable signature")
	}
	compact := append([]byte{27 + sig[64]}, sig[:64]...)
	pub, _, err := ecdsa.RecoverCompact(compact, digest)
	if err != nil {
		return err
	}
	if !publicKeyMatches(pub.SerializeUncompressed(), p.Chain, p.From) {
		return fmt.Errorf("signed source differs from prepared source")
	}
	return verifySignature(pub.SerializeUncompressed(), digest, sig)
}

func sameJSON(a, b string) bool {
	decode := func(s string) (any, error) {
		d := json.NewDecoder(bytes.NewBufferString(s))
		d.UseNumber()
		var v any
		err := d.Decode(&v)
		return v, err
	}
	x, e := decode(a)
	y, f := decode(b)
	return e == nil && f == nil && reflect.DeepEqual(x, y)
}
