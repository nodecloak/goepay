package payment

import (
	"bytes"
	"encoding/hex"
	"strings"
	"testing"
)

// SM3 standard test vectors from GB/T 32905-2016 Appendix A.
func TestPluginSm3StandardVectors(t *testing.T) {
	// Example 1: message "abc".
	if got := sm3HexString([]byte("abc")); got != "66c7f0f462eeedd9d1f2d46bdc10e4e24167c4875cf2f7a2297da02b8f4ba8e0" {
		t.Fatalf("sm3(abc) = %s", got)
	}
	// Example 2: 64-byte message "abcd" repeated 16 times.
	msg2 := strings.Repeat("abcd", 16)
	if len(msg2) != 64 {
		t.Fatalf("vector length %d", len(msg2))
	}
	if got := sm3HexString([]byte(msg2)); got != "debe9ff92275b8a138604889c18e5a4d6fdb70e5387e5765293dcba39c0c5732" {
		t.Fatalf("sm3(abcd*16) = %s", got)
	}
	// Empty input is well defined.
	if got := sm3HexString(nil); got != "1ab21d8355cfa17f8e61194831e81a8f22bec8c728fefb747ed035eb5082aa2b" {
		t.Fatalf("sm3() = %s", got)
	}
}

// SM4 standard test vector from GB/T 32907-2016 Appendix A: one-block ECB of a
// single 16-byte block equals the published ciphertext.
func TestPluginSm4StandardVector(t *testing.T) {
	key, _ := hex.DecodeString("0123456789abcdeffedcba9876543210")
	plain, _ := hex.DecodeString("0123456789abcdeffedcba9876543210")
	wantCT, _ := hex.DecodeString("681edf34d206965e86b3e94f536e4246")

	// Raw single-block encryption (no padding) must match the standard vector.
	c, err := sm4NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	block := make([]byte, 16)
	c.encryptBlock(block, plain)
	if !bytes.Equal(block, wantCT) {
		t.Fatalf("sm4 block = %x want %x", block, wantCT)
	}
	dec := make([]byte, 16)
	c.decryptBlock(dec, wantCT)
	if !bytes.Equal(dec, plain) {
		t.Fatalf("sm4 decrypt block = %x", dec)
	}

	// The GB/T 32907 "1,000,000 iterations" vector: encrypt the block 1e6 times.
	state := make([]byte, 16)
	copy(state, plain)
	tmp := make([]byte, 16)
	for i := 0; i < 1000000; i++ {
		c.encryptBlock(tmp, state)
		copy(state, tmp)
	}
	wantMillion, _ := hex.DecodeString("595298c7c6fd271f0402f804c33d3f66")
	if !bytes.Equal(state, wantMillion) {
		t.Fatalf("sm4 1e6 = %x want %x", state, wantMillion)
	}
}

func TestPluginSm4ECBCBCRoundTrip(t *testing.T) {
	key, _ := hex.DecodeString("0123456789abcdeffedcba9876543210")
	iv, _ := hex.DecodeString("00112233445566778899aabbccddeeff")
	for _, msg := range [][]byte{[]byte(""), []byte("A"), []byte("国密SM4 padding test — 一二三"), bytes.Repeat([]byte{0x7f}, 48)} {
		ct, err := sm4EncryptECB(key, msg)
		if err != nil {
			t.Fatal(err)
		}
		if len(ct)%16 != 0 {
			t.Fatalf("ecb ct not block aligned: %d", len(ct))
		}
		pt, err := sm4DecryptECB(key, ct)
		if err != nil || !bytes.Equal(pt, msg) {
			t.Fatalf("ecb round trip %q -> %q (%v)", msg, pt, err)
		}
		ct2, err := sm4EncryptCBC(key, iv, msg)
		if err != nil {
			t.Fatal(err)
		}
		pt2, err := sm4DecryptCBC(key, iv, ct2)
		if err != nil || !bytes.Equal(pt2, msg) {
			t.Fatalf("cbc round trip %q -> %q (%v)", msg, pt2, err)
		}
	}
	// Tampered ECB/CBC ciphertext is rejected by padding (usually) — at minimum
	// a truncated ciphertext must error.
	if _, err := sm4DecryptECB(key, []byte("short")); err == nil {
		t.Fatal("short ecb accepted")
	}
	if _, err := sm4DecryptCBC(key, iv, []byte("short")); err == nil {
		t.Fatal("short cbc accepted")
	}
}

// A fixed test key pair derived independently: private scalar d (hex) with the
// matching public point computed by sm2ParsePrivateHex (ScalarBaseMult on the
// sm2p256v1 curve). We cross-check the public point is on the curve and that a
// sign→verify round trip holds, a tampered message is rejected, and a tampered
// signature is rejected.
const smTestPrivHex = "128B2FA8BD433C6C068C8D803DFF79792A519A55171B1B650C23661D15897263"

func smTestKeys(t *testing.T) (*sm2PrivateKey, *sm2PublicKey) {
	t.Helper()
	priv, err := sm2ParsePrivateHex(smTestPrivHex)
	if err != nil {
		t.Fatalf("parse priv: %v", err)
	}
	// Re-export the public key as 04||X||Y hex and re-parse it, proving the
	// public-key hex parser agrees with the derived point.
	pubHex := "04" + hex.EncodeToString(sm2FieldBytes(priv.x)) + hex.EncodeToString(sm2FieldBytes(priv.y))
	pub, err := sm2ParsePublicHex(pubHex)
	if err != nil {
		t.Fatalf("parse pub: %v", err)
	}
	if pub.x.Cmp(priv.x) != 0 || pub.y.Cmp(priv.y) != 0 {
		t.Fatal("public key mismatch")
	}
	return priv, pub
}

func TestPluginSm2SignVerifyRoundTrip(t *testing.T) {
	priv, pub := smTestKeys(t)
	msg := []byte("message digest")

	der, err := sm2SignASN1(priv, "", msg)
	if err != nil {
		t.Fatal(err)
	}
	if !sm2VerifyASN1(pub, "", msg, der) {
		t.Fatal("round-trip DER verify failed")
	}
	// Default user id is applied consistently to both sign and verify.
	if !sm2VerifyASN1(pub, sm2DefaultUserID, msg, der) {
		t.Fatal("explicit default user id verify failed")
	}
	// A verify under a DIFFERENT user id must fail (Z value changes).
	if sm2VerifyASN1(pub, "0000000000000000", msg, der) {
		t.Fatal("verify under wrong user id succeeded")
	}
	// Tampered message rejected.
	if sm2VerifyASN1(pub, "", []byte("message digesT"), der) {
		t.Fatal("tampered message verified")
	}
	// Tampered signature rejected.
	bad := append([]byte(nil), der...)
	bad[len(bad)-1] ^= 0x01
	if sm2VerifyASN1(pub, "", msg, bad) {
		t.Fatal("tampered signature verified")
	}
	// A signature produced by a different key does not verify against pub.
	other, _ := sm2ParsePrivateHex("0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF")
	otherSig, _ := sm2SignASN1(other, "", msg)
	if sm2VerifyASN1(pub, "", msg, otherSig) {
		t.Fatal("foreign key signature verified")
	}
}

// Independently verify a known-good signature: sign the same message many times
// and confirm each distinct (randomised) signature verifies, and that the raw
// (r,s) path and the DER path agree. This derives correctness of verify from
// the sign side independently of any single fixed blob.
func TestPluginSm2SignatureSelfConsistent(t *testing.T) {
	priv, pub := smTestKeys(t)
	msg := []byte("合利宝国密验签 payload 12345")
	seen := map[string]bool{}
	for i := 0; i < 8; i++ {
		r, s, err := sm2Sign(priv, "", msg)
		if err != nil {
			t.Fatal(err)
		}
		if !sm2Verify(pub, "", msg, r, s) {
			t.Fatalf("raw verify failed on iteration %d", i)
		}
		der := sm2EncodeSignature(r, s)
		if !sm2VerifyASN1(pub, "", msg, der) {
			t.Fatalf("der verify failed on iteration %d", i)
		}
		dr, ds, ok := sm2DecodeSignature(der)
		if !ok || dr.Cmp(r) != 0 || ds.Cmp(s) != 0 {
			t.Fatalf("der round trip mismatch")
		}
		seen[r.String()] = true
	}
	if len(seen) < 2 {
		t.Fatal("signatures were not randomised")
	}
}

func TestPluginSm2EncryptRoundTrip(t *testing.T) {
	priv, pub := smTestKeys(t)
	msg := []byte("0123456789abcdef") // a 16-byte SM4 key, the real use case
	ct, err := sm2EncryptC1C3C2(pub, msg)
	if err != nil {
		t.Fatal(err)
	}
	if len(ct) != 64+32+len(msg) {
		t.Fatalf("ciphertext length %d", len(ct))
	}
	pt, err := sm2DecryptC1C3C2(priv, ct)
	if err != nil || !bytes.Equal(pt, msg) {
		t.Fatalf("sm2 decrypt = %q (%v)", pt, err)
	}
	// Tampered C3 rejected.
	bad := append([]byte(nil), ct...)
	bad[70] ^= 0x01
	if _, err := sm2DecryptC1C3C2(priv, bad); err == nil {
		t.Fatal("tampered ciphertext decrypted")
	}
}

func TestPluginSm2KeyParsingErrors(t *testing.T) {
	bad := []string{"", "zz", "04", strings.Repeat("00", 32), strings.Repeat("ff", 32)}
	for _, s := range bad {
		if _, err := sm2ParsePrivateHex(s); err == nil {
			t.Fatalf("accepted bad private key %q", s)
		}
	}
	// A 64-byte all-zero "public key" is not on the curve.
	if _, err := sm2ParsePublicHex(strings.Repeat("00", 64)); err == nil {
		t.Fatal("accepted off-curve public key")
	}
	if _, err := sm2ParsePublicHex("05" + strings.Repeat("00", 64)); err == nil {
		t.Fatal("accepted bad prefix")
	}
}
