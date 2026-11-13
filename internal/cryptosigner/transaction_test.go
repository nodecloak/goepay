package cryptosigner

import (
	"bytes"
	"encoding/hex"
	"testing"
)

func TestStrictProtoRejectsNonCanonicalAndSemanticallyDangerousForms(t *testing.T) {
	for _, raw := range [][]byte{
		{0x08, 0x81, 0x00},       // overlong varint
		{0x08, 0x00},             // explicit proto3 default changes when JSON is reencoded
		{0x12, 0x80, 0x00},       // overlong bytes length
		{0x12, 0x00},             // explicit empty bytes
		{0x12, 0x01, 1, 0x08, 1}, // noncanonical field order
	} {
		if _, err := parseProto(raw, map[uint64]uint64{1: 0, 2: 2}); err == nil {
			t.Fatalf("ambiguous protobuf accepted: %x", raw)
		}
	}
}

// Hand-encoded canonical unsigned EIP-155 USDT transfer. A codec that silently
// changes integer lengths or RLP list length must fail before producing a txid.
func TestUnsignedEthereumRLPAgainstHandEncodedFixture(t *testing.T) {
	const raw = "f869808504a817c80082fde894dac17f958d2ee523a2206206994597c13d831ec780b844a9059cbb000000000000000000000000222222222222222222222222222222222222222200000000000000000000000000000000000000000000000000000000000186a0018080"
	got := ethereumPrepared("fixture", 0, "100000")
	if got.UnsignedPayload != raw {
		t.Fatalf("RLP differs from independent fixture: %s", got.UnsignedPayload)
	}
	b, _ := hex.DecodeString(raw)
	fields, err := decodeList(b)
	if err != nil {
		t.Fatal(err)
	}
	if len(fields) != 9 || len(fields[0]) != 0 || !bytes.Equal(fields[2], []byte{0xfd, 0xe8}) || len(fields[5]) != 68 {
		t.Fatalf("wrong transaction decode: %x", fields)
	}
}
