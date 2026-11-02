package noticeconfig231

import (
	"crypto/ed25519"
	"encoding/hex"
	"strings"
	"testing"
	"time"
)

func TestQQSignature231(t *testing.T) {
	now := time.Unix(1793600000, 0)
	raw := []byte(`{"op":0,"t":"C2C_MESSAGE_CREATE"}`)
	key := qqKey("test-secret")
	stamp := "1793600000"
	sig := hex.EncodeToString(ed25519.Sign(key, append([]byte(stamp), raw...)))
	if !verifyQQ("test-secret", stamp, sig, raw, now) {
		t.Fatal("signed current body rejected")
	}
	for _, v := range []struct {
		secret, stamp, sig string
		body               []byte
		at                 time.Time
	}{
		{"other", stamp, sig, raw, now}, {"test-secret", stamp, sig, append(raw, ' '), now},
		{"test-secret", stamp, sig, raw, now.Add(301 * time.Second)},
		{"test-secret", "01793600000", sig, raw, now}, {"test-secret", stamp, "zz", raw, now},
	} {
		if verifyQQ(v.secret, v.stamp, v.sig, v.body, v.at) {
			t.Fatal("invalid signature accepted")
		}
	}
}

func TestCredentialValidation231(t *testing.T) {
	cases := []struct {
		k     string
		c     Credential
		valid bool
	}{
		{"robot", Credential{AppID: "http://127.0.0.1/a"}, false},
		{"robot", Credential{AppID: "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=test"}, true},
		{"robot", Credential{AppID: "https://qyapi.weixin.qq.com:443/cgi-bin/webhook/send?key=test"}, true},
		{"robot", Credential{AppID: "https://oapi.dingtalk.com/robot/send?access_token=test&timestamp=123&sign=abc"}, true},
		{"robot", Credential{AppID: "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=test&key=other"}, false},
		{"qqbot", Credential{AppID: "123", Secret: "secret"}, true},
		{"qqbot", Credential{AppID: "123", Secret: "secret\n"}, false},
		{"telegram", Credential{AppID: "123", Secret: "123:abcdefghijk"}, true},
		{"telegram", Credential{AppID: "", Secret: "123:abcdefghijk"}, false},
		{"telegram", Credential{AppID: "99999999999999999999", Secret: "123:abcdefghijk"}, false},
	}
	for _, c := range cases {
		if validCredential(c.k, c.c) != c.valid {
			t.Fatalf("kind %s validation mismatch", c.k)
		}
	}
}

func TestQQValidationCannotSignAnEventBody231(t *testing.T) {
	now := time.Unix(1793600000, 0)
	if !validQQChallenge231("opaque_validation_token_123", "1793600000", now) {
		t.Fatal("opaque validation rejected")
	}
	for _, token := range []string{`{"op":0,"t":"C2C_MESSAGE_CREATE"}`, "token\n", "", strings.Repeat("a", 129)} {
		if validQQChallenge231(token, "1793600000", now) {
			t.Fatal("unsafe signing input accepted")
		}
	}
	if validQQChallenge231("token", "1793500000", now) {
		t.Fatal("stale challenge accepted")
	}
}
