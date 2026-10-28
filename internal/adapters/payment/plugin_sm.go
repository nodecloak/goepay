package payment

// 国密 (Chinese national) cryptography primitives for the payment plugins that
// need them (dinpay 智付, helipay 合利宝, jlpay 嘉联支付). Go's standard library
// and the vendored modules provide none of SM2/SM3/SM4, so this file implements
// them from the GB/T standards:
//
//   SM3  — GB/T 32905-2016 cryptographic hash (256-bit).
//   SM4  — GB/T 32907-2016 block cipher (128-bit block/key), ECB and CBC with
//          PKCS#7 padding, as the plugin SDKs use it.
//   SM2  — GB/T 32918 public-key signature over the sm2p256v1 curve, using the
//          SM3-based Z value with the default user id "1234567812345678", plus
//          C1C3C2 public-key encryption used to wrap the per-request SM4 key.
//
// Everything here is self-contained in this file and every exported-to-package
// identifier is prefixed sm2/sm3/sm4 so parallel ports never collide. Keys are
// parsed in the formats the PHP SDKs use: SM2 keys are raw hex (private = 32-byte
// scalar, public = 04||X||Y or X||Y), and the yinyingtong PFX path is handled by
// its own plugin with golang.org/x/crypto/pkcs12.

import (
	"crypto/elliptic"
	"crypto/rand"
	"crypto/subtle"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"math/big"
	"sync"
)

// ---------------------------------------------------------------------------
// SM3 — GB/T 32905-2016
// ---------------------------------------------------------------------------

var sm3IV = [8]uint32{
	0x7380166f, 0x4914b2b9, 0x172442d7, 0xda8a0600,
	0xa96f30bc, 0x163138aa, 0xe38dee4d, 0xb0fb0e4e,
}

func sm3T(j int) uint32 {
	if j < 16 {
		return 0x79cc4519
	}
	return 0x7a879d8a
}

func sm3rotl(x uint32, n uint) uint32 { return (x << (n % 32)) | (x >> ((32 - n) % 32)) }

func sm3ff(x, y, z uint32, j int) uint32 {
	if j < 16 {
		return x ^ y ^ z
	}
	return (x & y) | (x & z) | (y & z)
}

func sm3gg(x, y, z uint32, j int) uint32 {
	if j < 16 {
		return x ^ y ^ z
	}
	return (x & y) | (^x & z)
}

func sm3p0(x uint32) uint32 { return x ^ sm3rotl(x, 9) ^ sm3rotl(x, 17) }
func sm3p1(x uint32) uint32 { return x ^ sm3rotl(x, 15) ^ sm3rotl(x, 23) }

// sm3Sum returns the 32-byte SM3 digest of msg.
func sm3Sum(msg []byte) [32]byte {
	// Padding: append 0x80, then zeros, then the 64-bit bit length.
	bitLen := uint64(len(msg)) * 8
	padded := make([]byte, len(msg))
	copy(padded, msg)
	padded = append(padded, 0x80)
	for len(padded)%64 != 56 {
		padded = append(padded, 0x00)
	}
	var lenBuf [8]byte
	binary.BigEndian.PutUint64(lenBuf[:], bitLen)
	padded = append(padded, lenBuf[:]...)

	v := sm3IV
	for off := 0; off < len(padded); off += 64 {
		block := padded[off : off+64]
		var w [68]uint32
		for i := 0; i < 16; i++ {
			w[i] = binary.BigEndian.Uint32(block[i*4 : i*4+4])
		}
		for i := 16; i < 68; i++ {
			w[i] = sm3p1(w[i-16]^w[i-9]^sm3rotl(w[i-3], 15)) ^ sm3rotl(w[i-13], 7) ^ w[i-6]
		}
		var w1 [64]uint32
		for i := 0; i < 64; i++ {
			w1[i] = w[i] ^ w[i+4]
		}
		a, b, c, d := v[0], v[1], v[2], v[3]
		e, f, g, h := v[4], v[5], v[6], v[7]
		for j := 0; j < 64; j++ {
			ss1 := sm3rotl(sm3rotl(a, 12)+e+sm3rotl(sm3T(j), uint(j)), 7)
			ss2 := ss1 ^ sm3rotl(a, 12)
			tt1 := sm3ff(a, b, c, j) + d + ss2 + w1[j]
			tt2 := sm3gg(e, f, g, j) + h + ss1 + w[j]
			d = c
			c = sm3rotl(b, 9)
			b = a
			a = tt1
			h = g
			g = sm3rotl(f, 19)
			f = e
			e = sm3p0(tt2)
		}
		v[0] ^= a
		v[1] ^= b
		v[2] ^= c
		v[3] ^= d
		v[4] ^= e
		v[5] ^= f
		v[6] ^= g
		v[7] ^= h
	}
	var out [32]byte
	for i := 0; i < 8; i++ {
		binary.BigEndian.PutUint32(out[i*4:i*4+4], v[i])
	}
	return out
}

func sm3HexString(msg []byte) string {
	d := sm3Sum(msg)
	return hex.EncodeToString(d[:])
}

// ---------------------------------------------------------------------------
// SM4 — GB/T 32907-2016
// ---------------------------------------------------------------------------

const sm4BlockSize = 16

var sm4SBox = [256]byte{
	0xd6, 0x90, 0xe9, 0xfe, 0xcc, 0xe1, 0x3d, 0xb7, 0x16, 0xb6, 0x14, 0xc2, 0x28, 0xfb, 0x2c, 0x05,
	0x2b, 0x67, 0x9a, 0x76, 0x2a, 0xbe, 0x04, 0xc3, 0xaa, 0x44, 0x13, 0x26, 0x49, 0x86, 0x06, 0x99,
	0x9c, 0x42, 0x50, 0xf4, 0x91, 0xef, 0x98, 0x7a, 0x33, 0x54, 0x0b, 0x43, 0xed, 0xcf, 0xac, 0x62,
	0xe4, 0xb3, 0x1c, 0xa9, 0xc9, 0x08, 0xe8, 0x95, 0x80, 0xdf, 0x94, 0xfa, 0x75, 0x8f, 0x3f, 0xa6,
	0x47, 0x07, 0xa7, 0xfc, 0xf3, 0x73, 0x17, 0xba, 0x83, 0x59, 0x3c, 0x19, 0xe6, 0x85, 0x4f, 0xa8,
	0x68, 0x6b, 0x81, 0xb2, 0x71, 0x64, 0xda, 0x8b, 0xf8, 0xeb, 0x0f, 0x4b, 0x70, 0x56, 0x9d, 0x35,
	0x1e, 0x24, 0x0e, 0x5e, 0x63, 0x58, 0xd1, 0xa2, 0x25, 0x22, 0x7c, 0x3b, 0x01, 0x21, 0x78, 0x87,
	0xd4, 0x00, 0x46, 0x57, 0x9f, 0xd3, 0x27, 0x52, 0x4c, 0x36, 0x02, 0xe7, 0xa0, 0xc4, 0xc8, 0x9e,
	0xea, 0xbf, 0x8a, 0xd2, 0x40, 0xc7, 0x38, 0xb5, 0xa3, 0xf7, 0xf2, 0xce, 0xf9, 0x61, 0x15, 0xa1,
	0xe0, 0xae, 0x5d, 0xa4, 0x9b, 0x34, 0x1a, 0x55, 0xad, 0x93, 0x32, 0x30, 0xf5, 0x8c, 0xb1, 0xe3,
	0x1d, 0xf6, 0xe2, 0x2e, 0x82, 0x66, 0xca, 0x60, 0xc0, 0x29, 0x23, 0xab, 0x0d, 0x53, 0x4e, 0x6f,
	0xd5, 0xdb, 0x37, 0x45, 0xde, 0xfd, 0x8e, 0x2f, 0x03, 0xff, 0x6a, 0x72, 0x6d, 0x6c, 0x5b, 0x51,
	0x8d, 0x1b, 0xaf, 0x92, 0xbb, 0xdd, 0xbc, 0x7f, 0x11, 0xd9, 0x5c, 0x41, 0x1f, 0x10, 0x5a, 0xd8,
	0x0a, 0xc1, 0x31, 0x88, 0xa5, 0xcd, 0x7b, 0xbd, 0x2d, 0x74, 0xd0, 0x12, 0xb8, 0xe5, 0xb4, 0xb0,
	0x89, 0x69, 0x97, 0x4a, 0x0c, 0x96, 0x77, 0x7e, 0x65, 0xb9, 0xf1, 0x09, 0xc5, 0x6e, 0xc6, 0x84,
	0x18, 0xf0, 0x7d, 0xec, 0x3a, 0xdc, 0x4d, 0x20, 0x79, 0xee, 0x5f, 0x3e, 0xd7, 0xcb, 0x39, 0x48,
}

var sm4FK = [4]uint32{0xa3b1bac6, 0x56aa3350, 0x677d9197, 0xb27022dc}

var sm4CK = [32]uint32{
	0x00070e15, 0x1c232a31, 0x383f464d, 0x545b6269,
	0x70777e85, 0x8c939aa1, 0xa8afb6bd, 0xc4cbd2d9,
	0xe0e7eef5, 0xfc030a11, 0x181f262d, 0x343b4249,
	0x50575e65, 0x6c737a81, 0x888f969d, 0xa4abb2b9,
	0xc0c7ced5, 0xdce3eaf1, 0xf8ff060d, 0x141b2229,
	0x30373e45, 0x4c535a61, 0x686f767d, 0x848b9299,
	0xa0a7aeb5, 0xbcc3cad1, 0xd8dfe6ed, 0xf4fb0209,
	0x10171e25, 0x2c333a41, 0x484f565d, 0x646b7279,
}

func sm4rotl(x uint32, n uint) uint32 { return (x << n) | (x >> (32 - n)) }

func sm4tau(a uint32) uint32 {
	return uint32(sm4SBox[a>>24])<<24 |
		uint32(sm4SBox[(a>>16)&0xff])<<16 |
		uint32(sm4SBox[(a>>8)&0xff])<<8 |
		uint32(sm4SBox[a&0xff])
}

// L transform for encryption rounds.
func sm4lEnc(b uint32) uint32 {
	return b ^ sm4rotl(b, 2) ^ sm4rotl(b, 10) ^ sm4rotl(b, 18) ^ sm4rotl(b, 24)
}

// L' transform for key expansion.
func sm4lKey(b uint32) uint32 { return b ^ sm4rotl(b, 13) ^ sm4rotl(b, 23) }

// sm4Cipher holds an expanded SM4 round-key schedule.
type sm4Cipher struct{ rk [32]uint32 }

func sm4NewCipher(key []byte) (*sm4Cipher, error) {
	if len(key) != 16 {
		return nil, errors.New("sm4: key must be 16 bytes")
	}
	var k [4]uint32
	for i := 0; i < 4; i++ {
		k[i] = binary.BigEndian.Uint32(key[i*4:i*4+4]) ^ sm4FK[i]
	}
	c := &sm4Cipher{}
	for i := 0; i < 32; i++ {
		t := k[1] ^ k[2] ^ k[3] ^ sm4CK[i]
		k0 := k[0] ^ sm4lKey(sm4tau(t))
		c.rk[i] = k0
		k[0], k[1], k[2], k[3] = k[1], k[2], k[3], k0
	}
	return c, nil
}

func (c *sm4Cipher) crypt(dst, src []byte, decrypt bool) {
	var x [4]uint32
	for i := 0; i < 4; i++ {
		x[i] = binary.BigEndian.Uint32(src[i*4 : i*4+4])
	}
	for i := 0; i < 32; i++ {
		rk := c.rk[i]
		if decrypt {
			rk = c.rk[31-i]
		}
		t := x[1] ^ x[2] ^ x[3] ^ rk
		x0 := x[0] ^ sm4lEnc(sm4tau(t))
		x[0], x[1], x[2], x[3] = x[1], x[2], x[3], x0
	}
	// Reverse the final state (R transform).
	out := [4]uint32{x[3], x[2], x[1], x[0]}
	for i := 0; i < 4; i++ {
		binary.BigEndian.PutUint32(dst[i*4:i*4+4], out[i])
	}
}

func (c *sm4Cipher) encryptBlock(dst, src []byte) { c.crypt(dst, src, false) }
func (c *sm4Cipher) decryptBlock(dst, src []byte) { c.crypt(dst, src, true) }

func sm4Pad(data []byte) []byte {
	pad := sm4BlockSize - len(data)%sm4BlockSize
	out := make([]byte, len(data)+pad)
	copy(out, data)
	for i := len(data); i < len(out); i++ {
		out[i] = byte(pad)
	}
	return out
}

func sm4Unpad(data []byte) ([]byte, error) {
	if len(data) == 0 || len(data)%sm4BlockSize != 0 {
		return nil, errors.New("sm4: invalid padded length")
	}
	pad := int(data[len(data)-1])
	if pad < 1 || pad > sm4BlockSize || pad > len(data) {
		return nil, errors.New("sm4: invalid padding")
	}
	for _, b := range data[len(data)-pad:] {
		if int(b) != pad {
			return nil, errors.New("sm4: invalid padding")
		}
	}
	return data[:len(data)-pad], nil
}

// sm4EncryptECB encrypts plaintext under PKCS#7 padding in ECB mode.
func sm4EncryptECB(key, plaintext []byte) ([]byte, error) {
	c, err := sm4NewCipher(key)
	if err != nil {
		return nil, err
	}
	src := sm4Pad(plaintext)
	out := make([]byte, len(src))
	for off := 0; off < len(src); off += sm4BlockSize {
		c.encryptBlock(out[off:off+sm4BlockSize], src[off:off+sm4BlockSize])
	}
	return out, nil
}

// sm4DecryptECB decrypts ciphertext in ECB mode and removes PKCS#7 padding.
func sm4DecryptECB(key, ciphertext []byte) ([]byte, error) {
	c, err := sm4NewCipher(key)
	if err != nil {
		return nil, err
	}
	if len(ciphertext) == 0 || len(ciphertext)%sm4BlockSize != 0 {
		return nil, errors.New("sm4: invalid ciphertext length")
	}
	out := make([]byte, len(ciphertext))
	for off := 0; off < len(ciphertext); off += sm4BlockSize {
		c.decryptBlock(out[off:off+sm4BlockSize], ciphertext[off:off+sm4BlockSize])
	}
	return sm4Unpad(out)
}

// sm4EncryptCBC encrypts plaintext under PKCS#7 padding in CBC mode.
func sm4EncryptCBC(key, iv, plaintext []byte) ([]byte, error) {
	if len(iv) != sm4BlockSize {
		return nil, errors.New("sm4: iv must be 16 bytes")
	}
	c, err := sm4NewCipher(key)
	if err != nil {
		return nil, err
	}
	src := sm4Pad(plaintext)
	out := make([]byte, len(src))
	prev := make([]byte, sm4BlockSize)
	copy(prev, iv)
	block := make([]byte, sm4BlockSize)
	for off := 0; off < len(src); off += sm4BlockSize {
		for i := 0; i < sm4BlockSize; i++ {
			block[i] = src[off+i] ^ prev[i]
		}
		c.encryptBlock(out[off:off+sm4BlockSize], block)
		copy(prev, out[off:off+sm4BlockSize])
	}
	return out, nil
}

// sm4DecryptCBC decrypts ciphertext in CBC mode and removes PKCS#7 padding.
func sm4DecryptCBC(key, iv, ciphertext []byte) ([]byte, error) {
	if len(iv) != sm4BlockSize {
		return nil, errors.New("sm4: iv must be 16 bytes")
	}
	c, err := sm4NewCipher(key)
	if err != nil {
		return nil, err
	}
	if len(ciphertext) == 0 || len(ciphertext)%sm4BlockSize != 0 {
		return nil, errors.New("sm4: invalid ciphertext length")
	}
	out := make([]byte, len(ciphertext))
	prev := make([]byte, sm4BlockSize)
	copy(prev, iv)
	tmp := make([]byte, sm4BlockSize)
	for off := 0; off < len(ciphertext); off += sm4BlockSize {
		c.decryptBlock(tmp, ciphertext[off:off+sm4BlockSize])
		for i := 0; i < sm4BlockSize; i++ {
			out[off+i] = tmp[i] ^ prev[i]
		}
		copy(prev, ciphertext[off:off+sm4BlockSize])
	}
	return sm4Unpad(out)
}

// ---------------------------------------------------------------------------
// SM2 — GB/T 32918 over the sm2p256v1 curve
// ---------------------------------------------------------------------------

// sm2DefaultUserID is the default signing user id (GB/T 32918) used by every
// plugin SDK here.
const sm2DefaultUserID = "1234567812345678"

var (
	sm2Once  sync.Once
	sm2Curve *sm2CurveParams
)

// sm2CurveParams reuses the generic short-Weierstrass implementation in
// crypto/elliptic via CurveParams. a = p-3 for this curve, which the standard
// add/double formulas assume.
type sm2CurveParams struct{ *elliptic.CurveParams }

func sm2P256() *sm2CurveParams {
	sm2Once.Do(func() {
		cp := &elliptic.CurveParams{Name: "sm2p256v1"}
		cp.P, _ = new(big.Int).SetString("FFFFFFFEFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFF00000000FFFFFFFFFFFFFFFF", 16)
		cp.N, _ = new(big.Int).SetString("FFFFFFFEFFFFFFFFFFFFFFFFFFFFFFFF7203DF6B21C6052B53BBF40939D54123", 16)
		cp.B, _ = new(big.Int).SetString("28E9FA9E9D9F5E344D5A9E4BCF6509A7F39789F515AB8F92DDBCBD414D940E93", 16)
		cp.Gx, _ = new(big.Int).SetString("32C4AE2C1F1981195F9904466A39C9948FE30BBFF2660BE1715A4589334C74C7", 16)
		cp.Gy, _ = new(big.Int).SetString("BC3736A2F4F6779C59BDCEE36B692153D0A9877CC62A474002DF32E52139F0A0", 16)
		cp.BitSize = 256
		sm2Curve = &sm2CurveParams{cp}
	})
	return sm2Curve
}

// sm2PrivateKey is an SM2 private key (scalar d) with its public point.
type sm2PrivateKey struct {
	d    *big.Int
	x, y *big.Int
}

// sm2PublicKey is an SM2 public key point.
type sm2PublicKey struct{ x, y *big.Int }

func (k *sm2PrivateKey) public() *sm2PublicKey { return &sm2PublicKey{x: k.x, y: k.y} }

// sm2ParsePrivateHex parses a raw-hex SM2 private key (32-byte scalar). It
// derives and stores the public point.
func sm2ParsePrivateHex(s string) (*sm2PrivateKey, error) {
	raw, err := hex.DecodeString(sm2CleanHex(s))
	if err != nil {
		return nil, errors.New("sm2: private key is not hex")
	}
	if len(raw) != 32 {
		return nil, errors.New("sm2: private key must be 32 bytes")
	}
	curve := sm2P256()
	d := new(big.Int).SetBytes(raw)
	if d.Sign() == 0 || d.Cmp(curve.N) >= 0 {
		return nil, errors.New("sm2: private scalar out of range")
	}
	x, y := curve.ScalarBaseMult(raw)
	return &sm2PrivateKey{d: d, x: x, y: y}, nil
}

// sm2ParsePublicHex parses a raw-hex SM2 public key: 04||X||Y (65 bytes) or the
// bare X||Y (64 bytes). Compressed points are rejected (none of the SDKs use
// them).
func sm2ParsePublicHex(s string) (*sm2PublicKey, error) {
	raw, err := hex.DecodeString(sm2CleanHex(s))
	if err != nil {
		return nil, errors.New("sm2: public key is not hex")
	}
	switch len(raw) {
	case 65:
		if raw[0] != 0x04 {
			return nil, errors.New("sm2: unsupported public key prefix")
		}
		raw = raw[1:]
	case 64:
		// bare X||Y
	default:
		return nil, errors.New("sm2: public key must be 64 or 65 bytes")
	}
	curve := sm2P256()
	x := new(big.Int).SetBytes(raw[:32])
	y := new(big.Int).SetBytes(raw[32:])
	if !curve.IsOnCurve(x, y) {
		return nil, errors.New("sm2: public key point not on curve")
	}
	return &sm2PublicKey{x: x, y: y}, nil
}

func sm2CleanHex(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= '0' && c <= '9', c >= 'a' && c <= 'f', c >= 'A' && c <= 'F':
			out = append(out, c)
		case c == ' ' || c == '\t' || c == '\r' || c == '\n':
			// skip whitespace
		default:
			return string(out) + "?" // force a hex-decode error downstream
		}
	}
	return string(out)
}

// sm2ZValue computes Z = SM3(ENTL || userID || a || b || Gx || Gy || Px || Py)
// per GB/T 32918.
func sm2ZValue(userID string, px, py *big.Int) [32]byte {
	curve := sm2P256()
	entl := uint16(len(userID) * 8)
	buf := make([]byte, 0, 2+len(userID)+32*6)
	buf = append(buf, byte(entl>>8), byte(entl))
	buf = append(buf, []byte(userID)...)
	// a = p - 3 for this curve.
	a := new(big.Int).Sub(curve.P, big.NewInt(3))
	buf = append(buf, sm2FieldBytes(a)...)
	buf = append(buf, sm2FieldBytes(curve.B)...)
	buf = append(buf, sm2FieldBytes(curve.Gx)...)
	buf = append(buf, sm2FieldBytes(curve.Gy)...)
	buf = append(buf, sm2FieldBytes(px)...)
	buf = append(buf, sm2FieldBytes(py)...)
	return sm3Sum(buf)
}

// sm2FieldBytes left-pads a field element to 32 bytes.
func sm2FieldBytes(v *big.Int) []byte {
	b := v.Bytes()
	if len(b) >= 32 {
		return b[len(b)-32:]
	}
	out := make([]byte, 32)
	copy(out[32-len(b):], b)
	return out
}

// sm2Digest computes e = SM3(Z || M) as a big.Int, the value signed/verified.
func sm2Digest(userID string, px, py *big.Int, msg []byte) *big.Int {
	z := sm2ZValue(userID, px, py)
	full := make([]byte, 0, 32+len(msg))
	full = append(full, z[:]...)
	full = append(full, msg...)
	d := sm3Sum(full)
	return new(big.Int).SetBytes(d[:])
}

// sm2SignASN1 signs msg and returns the (r,s) signature DER-encoded (the Der
// form the SDKs base64-encode). userID defaults to sm2DefaultUserID when empty.
func sm2SignASN1(key *sm2PrivateKey, userID string, msg []byte) ([]byte, error) {
	r, s, err := sm2Sign(key, userID, msg)
	if err != nil {
		return nil, err
	}
	return sm2EncodeSignature(r, s), nil
}

// sm2Sign produces the raw (r,s) SM2 signature.
func sm2Sign(key *sm2PrivateKey, userID string, msg []byte) (*big.Int, *big.Int, error) {
	if key == nil {
		return nil, nil, errors.New("sm2: nil private key")
	}
	if userID == "" {
		userID = sm2DefaultUserID
	}
	curve := sm2P256()
	n := curve.N
	e := sm2Digest(userID, key.x, key.y, msg)
	for {
		k, err := rand.Int(rand.Reader, n)
		if err != nil {
			return nil, nil, err
		}
		if k.Sign() == 0 {
			continue
		}
		x1, _ := curve.ScalarBaseMult(sm2FieldBytes(k))
		// r = (e + x1) mod n
		r := new(big.Int).Add(e, x1)
		r.Mod(r, n)
		if r.Sign() == 0 {
			continue
		}
		if new(big.Int).Add(r, k).Cmp(n) == 0 {
			continue
		}
		// s = ((1 + d)^-1 * (k - r*d)) mod n
		d1 := new(big.Int).Add(big.NewInt(1), key.d)
		d1inv := new(big.Int).ModInverse(d1, n)
		if d1inv == nil {
			continue
		}
		rd := new(big.Int).Mul(r, key.d)
		kmrd := new(big.Int).Sub(k, rd)
		s := new(big.Int).Mul(d1inv, kmrd)
		s.Mod(s, n)
		if s.Sign() == 0 {
			continue
		}
		return r, s, nil
	}
}

// sm2VerifyASN1 verifies a DER-encoded (r,s) signature.
func sm2VerifyASN1(pub *sm2PublicKey, userID string, msg, der []byte) bool {
	r, s, ok := sm2DecodeSignature(der)
	if !ok {
		return false
	}
	return sm2Verify(pub, userID, msg, r, s)
}

// sm2Verify checks a raw (r,s) SM2 signature.
func sm2Verify(pub *sm2PublicKey, userID string, msg []byte, r, s *big.Int) bool {
	if pub == nil || r == nil || s == nil {
		return false
	}
	if userID == "" {
		userID = sm2DefaultUserID
	}
	curve := sm2P256()
	n := curve.N
	if r.Sign() <= 0 || r.Cmp(n) >= 0 || s.Sign() <= 0 || s.Cmp(n) >= 0 {
		return false
	}
	e := sm2Digest(userID, pub.x, pub.y, msg)
	t := new(big.Int).Add(r, s)
	t.Mod(t, n)
	if t.Sign() == 0 {
		return false
	}
	// (x1,y1) = s*G + t*P
	x1, y1 := curve.ScalarBaseMult(sm2FieldBytes(s))
	x2, y2 := curve.ScalarMult(pub.x, pub.y, sm2FieldBytes(t))
	px, _ := curve.Add(x1, y1, x2, y2)
	// R = (e + x) mod n
	rCalc := new(big.Int).Add(e, px)
	rCalc.Mod(rCalc, n)
	return subtle.ConstantTimeCompare(sm2FieldBytes(rCalc), sm2FieldBytes(r)) == 1
}

// ---- minimal DER for the SM2 ECDSA-style (r,s) SEQUENCE of two INTEGERs ----

func sm2EncodeSignature(r, s *big.Int) []byte {
	rb := sm2derInt(r)
	sb := sm2derInt(s)
	body := make([]byte, 0, len(rb)+len(sb))
	body = append(body, rb...)
	body = append(body, sb...)
	out := make([]byte, 0, len(body)+2)
	out = append(out, 0x30)
	out = append(out, sm2derLen(len(body))...)
	out = append(out, body...)
	return out
}

func sm2derInt(v *big.Int) []byte {
	b := v.Bytes()
	if len(b) == 0 {
		b = []byte{0x00}
	}
	if b[0]&0x80 != 0 {
		b = append([]byte{0x00}, b...)
	}
	out := make([]byte, 0, len(b)+2)
	out = append(out, 0x02)
	out = append(out, sm2derLen(len(b))...)
	out = append(out, b...)
	return out
}

func sm2derLen(n int) []byte {
	if n < 0x80 {
		return []byte{byte(n)}
	}
	// Lengths here never exceed one length byte in practice, but encode fully.
	var tmp []byte
	for n > 0 {
		tmp = append([]byte{byte(n & 0xff)}, tmp...)
		n >>= 8
	}
	return append([]byte{byte(0x80 | len(tmp))}, tmp...)
}

func sm2DecodeSignature(der []byte) (*big.Int, *big.Int, bool) {
	p := der
	if len(p) < 2 || p[0] != 0x30 {
		return nil, nil, false
	}
	bodyLen, rest, ok := sm2readLen(p[1:])
	if !ok || bodyLen != len(rest) {
		return nil, nil, false
	}
	r, rest, ok := sm2readInt(rest)
	if !ok {
		return nil, nil, false
	}
	s, rest, ok := sm2readInt(rest)
	if !ok || len(rest) != 0 {
		return nil, nil, false
	}
	return r, s, true
}

func sm2readLen(p []byte) (int, []byte, bool) {
	if len(p) == 0 {
		return 0, nil, false
	}
	b := p[0]
	if b < 0x80 {
		return int(b), p[1:], true
	}
	num := int(b & 0x7f)
	if num == 0 || num > 4 || len(p) < 1+num {
		return 0, nil, false
	}
	n := 0
	for i := 0; i < num; i++ {
		n = n<<8 | int(p[1+i])
	}
	return n, p[1+num:], true
}

func sm2readInt(p []byte) (*big.Int, []byte, bool) {
	if len(p) < 2 || p[0] != 0x02 {
		return nil, nil, false
	}
	n, rest, ok := sm2readLen(p[1:])
	if !ok || n <= 0 || n > len(rest) {
		return nil, nil, false
	}
	v := new(big.Int).SetBytes(rest[:n])
	return v, rest[n:], true
}

// ---------------------------------------------------------------------------
// SM2 public-key encryption (C1C3C2), used to wrap the per-request SM4 key.
// Output is hex of C1(64 bytes, bare X||Y) || C3(32) || C2(len(msg)); the SDKs
// prepend "04" to the X||Y of C1 separately when they need the 65-byte form.
// ---------------------------------------------------------------------------

func sm2kdf(z []byte, kLen int) []byte {
	out := make([]byte, 0, kLen)
	var ct uint32 = 1
	for len(out) < kLen {
		buf := make([]byte, 0, len(z)+4)
		buf = append(buf, z...)
		var ctb [4]byte
		binary.BigEndian.PutUint32(ctb[:], ct)
		buf = append(buf, ctb[:]...)
		d := sm3Sum(buf)
		out = append(out, d[:]...)
		ct++
	}
	return out[:kLen]
}

// sm2EncryptC1C3C2 encrypts msg to the public key and returns the raw bytes
// C1(X||Y, 64) || C3(32) || C2.
func sm2EncryptC1C3C2(pub *sm2PublicKey, msg []byte) ([]byte, error) {
	if pub == nil || len(msg) == 0 {
		return nil, errors.New("sm2: bad encrypt input")
	}
	curve := sm2P256()
	n := curve.N
	for {
		k, err := rand.Int(rand.Reader, n)
		if err != nil {
			return nil, err
		}
		if k.Sign() == 0 {
			continue
		}
		c1x, c1y := curve.ScalarBaseMult(sm2FieldBytes(k))
		x2, y2 := curve.ScalarMult(pub.x, pub.y, sm2FieldBytes(k))
		zb := make([]byte, 0, 64)
		zb = append(zb, sm2FieldBytes(x2)...)
		zb = append(zb, sm2FieldBytes(y2)...)
		t := sm2kdf(zb, len(msg))
		allZero := true
		for _, b := range t {
			if b != 0 {
				allZero = false
				break
			}
		}
		if allZero {
			continue
		}
		c2 := make([]byte, len(msg))
		for i := range msg {
			c2[i] = msg[i] ^ t[i]
		}
		hashIn := make([]byte, 0, 64+len(msg))
		hashIn = append(hashIn, sm2FieldBytes(x2)...)
		hashIn = append(hashIn, msg...)
		hashIn = append(hashIn, sm2FieldBytes(y2)...)
		c3 := sm3Sum(hashIn)
		out := make([]byte, 0, 64+32+len(msg))
		out = append(out, sm2FieldBytes(c1x)...)
		out = append(out, sm2FieldBytes(c1y)...)
		out = append(out, c3[:]...)
		out = append(out, c2...)
		return out, nil
	}
}

// sm2DecryptC1C3C2 reverses sm2EncryptC1C3C2 (used only in tests to round-trip).
func sm2DecryptC1C3C2(key *sm2PrivateKey, in []byte) ([]byte, error) {
	if key == nil || len(in) < 96 {
		return nil, errors.New("sm2: bad ciphertext")
	}
	curve := sm2P256()
	c1x := new(big.Int).SetBytes(in[:32])
	c1y := new(big.Int).SetBytes(in[32:64])
	if !curve.IsOnCurve(c1x, c1y) {
		return nil, errors.New("sm2: C1 not on curve")
	}
	c3 := in[64:96]
	c2 := in[96:]
	x2, y2 := curve.ScalarMult(c1x, c1y, sm2FieldBytes(key.d))
	zb := make([]byte, 0, 64)
	zb = append(zb, sm2FieldBytes(x2)...)
	zb = append(zb, sm2FieldBytes(y2)...)
	t := sm2kdf(zb, len(c2))
	msg := make([]byte, len(c2))
	for i := range c2 {
		msg[i] = c2[i] ^ t[i]
	}
	hashIn := make([]byte, 0, 64+len(msg))
	hashIn = append(hashIn, sm2FieldBytes(x2)...)
	hashIn = append(hashIn, msg...)
	hashIn = append(hashIn, sm2FieldBytes(y2)...)
	check := sm3Sum(hashIn)
	if subtle.ConstantTimeCompare(check[:], c3) != 1 {
		return nil, errors.New("sm2: C3 mismatch")
	}
	return msg, nil
}
