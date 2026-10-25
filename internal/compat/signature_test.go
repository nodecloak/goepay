package compat

import (
	"crypto/rand"
	"crypto/rsa"
	"net/url"
	"testing"
	"time"
)

func TestCanonicalPreservesWireValues(t *testing.T) {
	p := Params{"pid": "10", "money": "1.0", "name": " 中文😀 +% ", "empty": "", "space": " \t", "zero": "0", "sign": "ignored", "sign_type": "RSA", "extension": "yes"}
	want := "extension=yes&money=1.0&name= 中文😀 +% &pid=10&space= \t&zero=0"
	if got := Canonical(p); got != want {
		t.Fatalf("canonical = %q, want %q", got, want)
	}
	wantSDK := "extension=yes&money=1.0&name= 中文😀 +% &pid=10&zero=0"
	if got := CanonicalSDK(p); got != wantSDK {
		t.Fatalf("SDK canonical = %q", got)
	}
}
func TestParseRejectsAmbiguity(t *testing.T) {
	for _, raw := range []string{"pid=1&pid=2", "pid=1&pid%5B%5D=2", "a.b=x", "a+b=x", "=x", "name%00hidden=x"} {
		v, err := url.ParseQuery(raw)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = Parse(v); err == nil {
			t.Errorf("accepted ambiguous form %q", raw)
		}
	}
	v, _ := url.ParseQuery("pid=1&name=%E4%B8%AD%E6%96%87%2B%25&money=1.0&zero=0")
	p, err := Parse(v)
	if err != nil || p["name"] != "中文+%" || p["money"] != "1.0" {
		t.Fatalf("parse=%v %v", p, err)
	}
}
func TestMD5FixedVector(t *testing.T) {
	p := Params{"pid": "1000", "money": "1.00", "out_trade_no": "test"}
	if got := SignMD5(p, "secret"); got != "762597569c498f3a4c1fe52c067aa43c" {
		t.Fatalf("digest=%s", got)
	}
	if VerifyMD5(p, "secret", "762597569C498F3A4C1FE52C067AA43C") {
		t.Fatal("accepted uppercase digest; PHP compares strict lowercase")
	}
	if VerifyMD5(p, "wrong", SignMD5(p, "secret")) {
		t.Fatal("wrong key accepted")
	}
}
func TestRSAAndPolicy(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	p := Params{"pid": "1", "timestamp": "1700000000", "money": "1.00", "sign_type": "RSA"}
	signature, err := SignRSA(p, key)
	if err != nil {
		t.Fatal(err)
	}
	if !VerifyRSA(p, signature, &key.PublicKey) {
		t.Fatal("valid RSA rejected")
	}
	p["money"] = "1.01"
	if VerifyRSA(p, signature, &key.PublicKey) {
		t.Fatal("tampering accepted")
	}
	p["money"] = "1.00"
	p["sign"] = signature
	c := Credentials{Algorithm: "RSA", PublicKey: &key.PublicKey}
	if err := Verify(p, c, time.Unix(1700000000, 0)); err != nil {
		t.Fatal(err)
	}
	p["sign_type"] = "MD5"
	p["sign"] = SignMD5(p, "")
	if err := Verify(p, c, time.Unix(1700000000, 0)); err == nil {
		t.Fatal("RSA downgrade accepted")
	}
}
func TestTimestampBoundary(t *testing.T) {
	now := time.Unix(1700000000, 0)
	for _, s := range []string{"1699999700", "1700000300", "1700000000"} {
		if err := ValidateTimestamp(s, now, 300*time.Second); err != nil {
			t.Errorf("%s: %v", s, err)
		}
	}
	for _, s := range []string{"", "0", "-1", "1700000301", "1699999699", "1700000000.0", "+1700000000", "9223372036854775807", " 1700000000"} {
		if err := ValidateTimestamp(s, now, 300*time.Second); err == nil {
			t.Errorf("accepted %q", s)
		}
	}
}
