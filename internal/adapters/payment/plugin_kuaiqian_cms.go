package payment

// Minimal PKCS#7/CMS for the kuaiqian (快钱) 当面付/查单/退款 envelope, matching
// what the original inc/CryptoProcessor.php produces with OpenSSL:
//
//   seal   = { signedData, envelopedData }  (each the base64 DER of a PKCS#7
//            ContentInfo, with the PEM header stripped as the PHP substr does)
//     signedData    openssl_pkcs7_sign(..., PKCS7_BINARY) — a DETACHED SignedData
//                   with a SHA-256 digest and an RSA (PKCS#1 v1.5) signer that
//                   signs the DER of the authenticated attributes (contentType +
//                   messageDigest), the default OpenSSL layout.
//     envelopedData openssl_pkcs7_encrypt(..., OPENSSL_CIPHER_AES_128_CBC) — an
//                   EnvelopedData with one RSA key-transport recipient
//                   (rsaEncryption, PKCS#1 v1.5) and AES-128-CBC content.
//
//   unseal = getDecryptData(envelopedData) then getVerifyFlag(plaintext,
//            signedData): decrypt the EnvelopedData with the merchant key, then
//            verify the detached SignedData over that plaintext (PKCS7_NOVERIFY:
//            signature integrity only, no certificate-chain check).
//
// Only the structures the original uses are implemented (no CRLs, no multiple
// recipients/signers, no GCM). Everything is encoding/asn1 + crypto/* stdlib.

import (
	"bytes"
	"crypto"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/asn1"
	"errors"
	"math/big"
)

var (
	kuaiqianOIDData         = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 7, 1}
	kuaiqianOIDSignedData   = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 7, 2}
	kuaiqianOIDEnveloped    = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 7, 3}
	kuaiqianOIDSHA256       = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 2, 1}
	kuaiqianOIDRSA          = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 1, 1}
	kuaiqianOIDAES128CBC    = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 1, 2}
	kuaiqianOIDContentType  = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 3}
	kuaiqianOIDMessageDigst = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 4}
)

// ---- ASN.1 wire structures ----

type kuaiqianContentInfo struct {
	ContentType asn1.ObjectIdentifier
	Content     asn1.RawValue `asn1:"explicit,optional,tag:0"`
}

type kuaiqianAlgID struct {
	Algorithm  asn1.ObjectIdentifier
	Parameters asn1.RawValue `asn1:"optional"`
}

type kuaiqianIssuerAndSerial struct {
	Issuer       asn1.RawValue
	SerialNumber *big.Int
}

// SignedData (detached: EncapContentInfo has no eContent).
type kuaiqianSignedData struct {
	Version          int
	DigestAlgorithms []kuaiqianAlgID `asn1:"set"`
	EncapContentInfo kuaiqianEncapContentInfo
	Certificates     asn1.RawValue        `asn1:"optional,tag:0"`
	SignerInfos      []kuaiqianSignerInfo `asn1:"set"`
}

type kuaiqianEncapContentInfo struct {
	EContentType asn1.ObjectIdentifier
	EContent     asn1.RawValue `asn1:"explicit,optional,tag:0"`
}

type kuaiqianSignerInfo struct {
	Version            int
	SID                kuaiqianIssuerAndSerial
	DigestAlgorithm    kuaiqianAlgID
	AuthAttrs          asn1.RawValue `asn1:"optional,tag:0"`
	SignatureAlgorithm kuaiqianAlgID
	Signature          []byte
}

type kuaiqianAttribute struct {
	Type   asn1.ObjectIdentifier
	Values asn1.RawValue `asn1:"set"`
}

// EnvelopedData with one key-transport recipient.
type kuaiqianEnvelopedData struct {
	Version              int
	RecipientInfos       []kuaiqianRecipientInfo `asn1:"set"`
	EncryptedContentInfo kuaiqianEncryptedContentInfo
}

type kuaiqianRecipientInfo struct {
	Version                int
	IssuerAndSerial        kuaiqianIssuerAndSerial
	KeyEncryptionAlgorithm kuaiqianAlgID
	EncryptedKey           []byte
}

type kuaiqianEncryptedContentInfo struct {
	ContentType                asn1.ObjectIdentifier
	ContentEncryptionAlgorithm kuaiqianAlgID
	EncryptedContent           asn1.RawValue `asn1:"optional,tag:0"`
}

// ---- seal: build signedData + envelopedData ----

// kuaiqianSeal returns the base64-less DER of the detached SignedData and the
// EnvelopedData ContentInfos (callers base64 them). It mirrors seal().
func (p *kuaiqianPlugin) kuaiqianSeal(plaintext []byte) (signedDER, envelopedDER []byte, err error) {
	if p.merchantCert == nil || p.platformCert == nil {
		return nil, nil, ErrUnsupported
	}
	signedDER, err = kuaiqianBuildSignedData(plaintext, p.merchantCert, p.privateKey)
	if err != nil {
		return nil, nil, err
	}
	envelopedDER, err = kuaiqianBuildEnvelopedData(plaintext, p.platformCert)
	if err != nil {
		return nil, nil, err
	}
	return signedDER, envelopedDER, nil
}

// kuaiqianBuildSignedData produces a detached SignedData ContentInfo (DER) with
// SHA-256 and an RSA signer over the authenticated attributes.
func kuaiqianBuildSignedData(content []byte, cert *x509.Certificate, key *rsa.PrivateKey) ([]byte, error) {
	digest := sha256.Sum256(content)
	// Authenticated attributes: contentType=data, messageDigest=digest.
	ctVal, err := asn1.Marshal(kuaiqianOIDData)
	if err != nil {
		return nil, err
	}
	mdVal, err := asn1.Marshal(digest[:])
	if err != nil {
		return nil, err
	}
	attrs := []kuaiqianAttribute{
		{Type: kuaiqianOIDContentType, Values: asn1.RawValue{Class: 0, Tag: asn1.TagSet, IsCompound: true, Bytes: ctVal}},
		{Type: kuaiqianOIDMessageDigst, Values: asn1.RawValue{Class: 0, Tag: asn1.TagSet, IsCompound: true, Bytes: mdVal}},
	}
	// The signature is over the DER of the attributes as an explicit SET (tag
	// 0x31), per CMS. Marshal the slice, which yields a SEQUENCE OF; re-tag the
	// leading byte to SET for both the signature input and the stored field.
	signedAttrsSetDER, err := kuaiqianMarshalAttrsSET(attrs)
	if err != nil {
		return nil, err
	}
	h := sha256.Sum256(signedAttrsSetDER)
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, h[:])
	if err != nil {
		return nil, err
	}
	// AuthAttrs are stored tagged [0] IMPLICIT (0xA0) in the SignerInfo.
	authAttrsField := asn1.RawValue{Class: asn1.ClassContextSpecific, Tag: 0, IsCompound: true, Bytes: signedAttrsSetDER[kuaiqianSetHeaderLen(signedAttrsSetDER):]}

	issuer := asn1.RawValue{FullBytes: cert.RawIssuer}
	si := kuaiqianSignerInfo{
		Version:            1,
		SID:                kuaiqianIssuerAndSerial{Issuer: issuer, SerialNumber: cert.SerialNumber},
		DigestAlgorithm:    kuaiqianAlgID{Algorithm: kuaiqianOIDSHA256, Parameters: kuaiqianNull()},
		AuthAttrs:          authAttrsField,
		SignatureAlgorithm: kuaiqianAlgID{Algorithm: kuaiqianOIDRSA, Parameters: kuaiqianNull()},
		Signature:          sig,
	}
	sd := kuaiqianSignedData{
		Version:          1,
		DigestAlgorithms: []kuaiqianAlgID{{Algorithm: kuaiqianOIDSHA256, Parameters: kuaiqianNull()}},
		EncapContentInfo: kuaiqianEncapContentInfo{EContentType: kuaiqianOIDData}, // detached: no eContent
		Certificates:     asn1.RawValue{Class: asn1.ClassContextSpecific, Tag: 0, IsCompound: true, Bytes: cert.Raw},
		SignerInfos:      []kuaiqianSignerInfo{si},
	}
	sdDER, err := asn1.Marshal(sd)
	if err != nil {
		return nil, err
	}
	return kuaiqianWrapContentInfo(kuaiqianOIDSignedData, sdDER)
}

// kuaiqianBuildEnvelopedData produces an EnvelopedData ContentInfo (DER):
// AES-128-CBC content, RSA key-transport recipient.
func kuaiqianBuildEnvelopedData(content []byte, recipient *x509.Certificate) ([]byte, error) {
	pub, ok := recipient.PublicKey.(*rsa.PublicKey)
	if !ok {
		return nil, ErrInvalid
	}
	cek := make([]byte, 16)
	if _, err := rand.Read(cek); err != nil {
		return nil, err
	}
	iv := make([]byte, aes.BlockSize)
	if _, err := rand.Read(iv); err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(cek)
	if err != nil {
		return nil, err
	}
	padded := kuaiqianPKCS7Pad(content, aes.BlockSize)
	ct := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(ct, padded)

	encKey, err := rsa.EncryptPKCS1v15(rand.Reader, pub, cek)
	if err != nil {
		return nil, err
	}
	ivParam, err := asn1.Marshal(iv)
	if err != nil {
		return nil, err
	}
	ed := kuaiqianEnvelopedData{
		Version: 0,
		RecipientInfos: []kuaiqianRecipientInfo{{
			Version:                0,
			IssuerAndSerial:        kuaiqianIssuerAndSerial{Issuer: asn1.RawValue{FullBytes: recipient.RawIssuer}, SerialNumber: recipient.SerialNumber},
			KeyEncryptionAlgorithm: kuaiqianAlgID{Algorithm: kuaiqianOIDRSA, Parameters: kuaiqianNull()},
			EncryptedKey:           encKey,
		}},
		EncryptedContentInfo: kuaiqianEncryptedContentInfo{
			ContentType:                kuaiqianOIDData,
			ContentEncryptionAlgorithm: kuaiqianAlgID{Algorithm: kuaiqianOIDAES128CBC, Parameters: asn1.RawValue{FullBytes: ivParam}},
			EncryptedContent:           asn1.RawValue{Class: asn1.ClassContextSpecific, Tag: 0, IsCompound: false, Bytes: ct},
		},
	}
	edDER, err := asn1.Marshal(ed)
	if err != nil {
		return nil, err
	}
	return kuaiqianWrapContentInfo(kuaiqianOIDEnveloped, edDER)
}

// ---- unseal: decrypt envelopedData, verify detached signedData ----

// kuaiqianUnseal decrypts the EnvelopedData with the merchant key and verifies
// the detached SignedData over the recovered plaintext, mirroring unseal().
func (p *kuaiqianPlugin) kuaiqianUnseal(signedDER, envelopedDER []byte) ([]byte, error) {
	plaintext, err := kuaiqianOpenEnvelopedData(envelopedDER, p.privateKey)
	if err != nil {
		return nil, err
	}
	if !kuaiqianVerifySignedData(signedDER, plaintext) {
		return nil, ErrAuthentication
	}
	return plaintext, nil
}

func kuaiqianOpenEnvelopedData(der []byte, key *rsa.PrivateKey) ([]byte, error) {
	ci, inner, err := kuaiqianUnwrapContentInfo(der)
	if err != nil || !ci.Equal(kuaiqianOIDEnveloped) {
		return nil, ErrInvalid
	}
	var ed kuaiqianEnvelopedData
	if _, err := asn1.Unmarshal(inner, &ed); err != nil || len(ed.RecipientInfos) == 0 {
		return nil, ErrInvalid
	}
	ri := ed.RecipientInfos[0]
	if !ri.KeyEncryptionAlgorithm.Algorithm.Equal(kuaiqianOIDRSA) {
		return nil, ErrInvalid
	}
	cek, err := rsa.DecryptPKCS1v15(rand.Reader, key, ri.EncryptedKey)
	if err != nil {
		return nil, ErrAuthentication
	}
	eci := ed.EncryptedContentInfo
	if !eci.ContentEncryptionAlgorithm.Algorithm.Equal(kuaiqianOIDAES128CBC) {
		return nil, ErrInvalid
	}
	var iv []byte
	if _, err := asn1.Unmarshal(eci.ContentEncryptionAlgorithm.Parameters.FullBytes, &iv); err != nil || len(iv) != aes.BlockSize {
		return nil, ErrInvalid
	}
	ct := eci.EncryptedContent.Bytes
	block, err := aes.NewCipher(cek)
	if err != nil || len(ct) == 0 || len(ct)%aes.BlockSize != 0 {
		return nil, ErrInvalid
	}
	pt := make([]byte, len(ct))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(pt, ct)
	return kuaiqianPKCS7Unpad(pt, aes.BlockSize)
}

func kuaiqianVerifySignedData(der, content []byte) bool {
	ci, inner, err := kuaiqianUnwrapContentInfo(der)
	if err != nil || !ci.Equal(kuaiqianOIDSignedData) {
		return false
	}
	var sd kuaiqianSignedData
	if _, err := asn1.Unmarshal(inner, &sd); err != nil || len(sd.SignerInfos) == 0 {
		return false
	}
	// Signer certificate from the embedded certificates field.
	if len(sd.Certificates.Bytes) == 0 {
		return false
	}
	cert, err := x509.ParseCertificate(sd.Certificates.Bytes)
	if err != nil {
		return false
	}
	pub, ok := cert.PublicKey.(*rsa.PublicKey)
	if !ok {
		return false
	}
	si := sd.SignerInfos[0]
	if !si.DigestAlgorithm.Algorithm.Equal(kuaiqianOIDSHA256) {
		return false
	}
	digest := sha256.Sum256(content)
	if len(si.AuthAttrs.Bytes) == 0 {
		// No authenticated attributes: signature is over the content digest.
		h := sha256.Sum256(content)
		return rsa.VerifyPKCS1v15(pub, crypto.SHA256, h[:], si.Signature) == nil
	}
	// Parse the authenticated attributes and check messageDigest == digest.
	var attrs []kuaiqianAttribute
	// AuthAttrs.Bytes are the inner attribute elements (the [0] IMPLICIT
	// content). SET OF and SEQUENCE OF share element encoding, so parse the
	// slice from a SEQUENCE-tagged copy; the signature input uses the SET DER.
	setDER := kuaiqianReSET(si.AuthAttrs.Bytes)
	seqDER := kuaiqianTagWrap(0x30, si.AuthAttrs.Bytes)
	if _, err := asn1.Unmarshal(seqDER, &attrs); err != nil {
		return false
	}
	mdOK := false
	for _, a := range attrs {
		if a.Type.Equal(kuaiqianOIDMessageDigst) {
			var md []byte
			if _, err := asn1.Unmarshal(a.Values.Bytes, &md); err == nil && bytes.Equal(md, digest[:]) {
				mdOK = true
			}
		}
	}
	if !mdOK {
		return false
	}
	h := sha256.Sum256(setDER)
	return rsa.VerifyPKCS1v15(pub, crypto.SHA256, h[:], si.Signature) == nil
}

// ---- helpers ----

func kuaiqianNull() asn1.RawValue {
	return asn1.RawValue{Tag: asn1.TagNull, FullBytes: []byte{asn1.TagNull, 0x00}}
}

func kuaiqianWrapContentInfo(oid asn1.ObjectIdentifier, inner []byte) ([]byte, error) {
	ci := kuaiqianContentInfo{ContentType: oid, Content: asn1.RawValue{Class: asn1.ClassContextSpecific, Tag: 0, IsCompound: true, Bytes: inner}}
	return asn1.Marshal(ci)
}

func kuaiqianUnwrapContentInfo(der []byte) (asn1.ObjectIdentifier, []byte, error) {
	var ci kuaiqianContentInfo
	if _, err := asn1.Unmarshal(der, &ci); err != nil {
		return nil, nil, err
	}
	return ci.ContentType, ci.Content.Bytes, nil
}

// kuaiqianMarshalAttrsSET marshals attributes as a SET (tag 0x31). asn1.Marshal
// of a slice yields a SEQUENCE OF (0x30); we retag the outermost byte.
func kuaiqianMarshalAttrsSET(attrs []kuaiqianAttribute) ([]byte, error) {
	seq, err := asn1.Marshal(struct {
		A []kuaiqianAttribute
	}{attrs})
	if err != nil {
		return nil, err
	}
	// seq is SEQUENCE { SEQUENCE OF attribute }; unwrap the outer and retag the
	// inner SEQUENCE OF to a SET.
	var outer asn1.RawValue
	if _, err := asn1.Unmarshal(seq, &outer); err != nil {
		return nil, err
	}
	inner := outer.Bytes // this is the SEQUENCE-OF (0x30 ...)
	if len(inner) == 0 || inner[0] != 0x30 {
		return nil, errors.New("kuaiqian: unexpected attributes encoding")
	}
	setDER := make([]byte, len(inner))
	copy(setDER, inner)
	setDER[0] = 0x31 // SET
	return setDER, nil
}

// kuaiqianSetHeaderLen returns the length of the tag+length header of a DER SET.
func kuaiqianSetHeaderLen(der []byte) int {
	if len(der) < 2 {
		return 2
	}
	l := der[1]
	if l < 0x80 {
		return 2
	}
	return 2 + int(l&0x7f)
}

// kuaiqianReSET wraps raw attribute content bytes back into a SET DER (0x31).
func kuaiqianReSET(inner []byte) []byte {
	return kuaiqianTagWrap(0x31, inner)
}

// kuaiqianTagWrap builds a DER TLV with the given tag around content.
func kuaiqianTagWrap(tag byte, content []byte) []byte {
	var b bytes.Buffer
	b.WriteByte(tag)
	n := len(content)
	switch {
	case n < 0x80:
		b.WriteByte(byte(n))
	case n < 0x100:
		b.WriteByte(0x81)
		b.WriteByte(byte(n))
	case n < 0x10000:
		b.WriteByte(0x82)
		b.WriteByte(byte(n >> 8))
		b.WriteByte(byte(n))
	default:
		b.WriteByte(0x83)
		b.WriteByte(byte(n >> 16))
		b.WriteByte(byte(n >> 8))
		b.WriteByte(byte(n))
	}
	b.Write(content)
	return b.Bytes()
}

func kuaiqianPKCS7Pad(data []byte, blockSize int) []byte {
	pad := blockSize - len(data)%blockSize
	out := make([]byte, len(data)+pad)
	copy(out, data)
	for i := len(data); i < len(out); i++ {
		out[i] = byte(pad)
	}
	return out
}

func kuaiqianPKCS7Unpad(data []byte, blockSize int) ([]byte, error) {
	if len(data) == 0 || len(data)%blockSize != 0 {
		return nil, ErrInvalid
	}
	pad := int(data[len(data)-1])
	if pad == 0 || pad > blockSize || pad > len(data) {
		return nil, ErrInvalid
	}
	for _, b := range data[len(data)-pad:] {
		if int(b) != pad {
			return nil, ErrInvalid
		}
	}
	return data[:len(data)-pad], nil
}
