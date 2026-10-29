package connectlogin

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

func tgSign(botToken string, fields map[string]string) string {
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for i, k := range keys {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(k + "=" + fields[k])
	}
	secret := sha256.Sum256([]byte(botToken))
	mac := hmac.New(sha256.New, secret[:])
	mac.Write([]byte(b.String()))
	return hex.EncodeToString(mac.Sum(nil))
}

func TestTelegramVerify(t *testing.T) {
	const bot = "123456:bot-token"
	now := time.Unix(1700000000, 0)
	fields := map[string]string{
		"id":         "987654321",
		"first_name": "Zoe",
		"username":   "zoe",
		"auth_date":  strconv.FormatInt(now.Unix(), 10),
	}
	good := tgSign(bot, fields)
	tg := Telegram{BotToken: bot, Now: func() time.Time { return now }}

	id, e := tg.Verify(fields, good)
	if e != nil || id != "987654321" {
		t.Fatal("valid payload should verify", id, e)
	}
	// Tampered field -> hash mismatch.
	tampered := map[string]string{}
	for k, v := range fields {
		tampered[k] = v
	}
	tampered["id"] = "111"
	if _, e := tg.Verify(tampered, good); e != ErrTelegram {
		t.Fatal("tampered payload must fail", e)
	}
	// Stale auth_date.
	stale := map[string]string{}
	for k, v := range fields {
		stale[k] = v
	}
	stale["auth_date"] = strconv.FormatInt(now.Unix()-7200, 10)
	if _, e := tg.Verify(stale, tgSign(bot, stale)); e != ErrTelegram {
		t.Fatal("stale payload must fail", e)
	}
	// Missing id.
	noID := map[string]string{"auth_date": strconv.FormatInt(now.Unix(), 10)}
	if _, e := tg.Verify(noID, tgSign(bot, noID)); e != ErrTelegram {
		t.Fatal("missing id must fail", e)
	}
	// Unconfigured.
	if _, e := (Telegram{}).Verify(fields, good); e != ErrTelegram {
		t.Fatal("no bot token must fail", e)
	}
}
