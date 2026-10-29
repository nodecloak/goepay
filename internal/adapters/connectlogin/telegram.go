package connectlogin

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ErrTelegram marks a Telegram login-widget payload that fails verification
// (bad hash, stale auth_date, missing id) — the caller must not treat it as an
// identity.
var ErrTelegram = errors.New("telegram login verification failed")

// Telegram ports the Telegram Login Widget check. There is no authorize/code
// exchange: the widget posts signed fields (id, auth_date, hash, ...) and the
// server verifies hash == HMAC_SHA256(data_check_string, SHA256(bot_token)),
// per https://core.telegram.org/widgets/login#checking-authorization.
type Telegram struct {
	BotToken string
	// MaxAge rejects a payload older than this (replay / staleness). Default 1h.
	MaxAge time.Duration
	Now    func() time.Time
}

// Verify checks the signed fields and returns the Telegram user id as a string.
// `fields` is the full set of widget params EXCEPT hash; `hashHex` is the hash.
func (t Telegram) Verify(fields map[string]string, hashHex string) (string, error) {
	if t.BotToken == "" || hashHex == "" {
		return "", ErrTelegram
	}
	id := fields["id"]
	if id == "" {
		return "", ErrTelegram
	}
	// auth_date freshness.
	authDate, e := strconv.ParseInt(fields["auth_date"], 10, 64)
	if e != nil {
		return "", ErrTelegram
	}
	now := time.Now
	if t.Now != nil {
		now = t.Now
	}
	maxAge := t.MaxAge
	if maxAge <= 0 {
		maxAge = time.Hour
	}
	age := now().Unix() - authDate
	if age < -60 || age > int64(maxAge/time.Second) {
		return "", ErrTelegram
	}
	// data_check_string: all fields except hash, sorted by key, "k=v" joined by \n.
	keys := make([]string, 0, len(fields))
	for k := range fields {
		if k == "hash" {
			continue
		}
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
	secret := sha256.Sum256([]byte(t.BotToken))
	mac := hmac.New(sha256.New, secret[:])
	mac.Write([]byte(b.String()))
	want := hex.EncodeToString(mac.Sum(nil))
	got := strings.ToLower(strings.TrimSpace(hashHex))
	if !hmac.Equal([]byte(want), []byte(got)) {
		return "", ErrTelegram
	}
	return id, nil
}
