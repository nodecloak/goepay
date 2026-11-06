// Package wxtransferlink signs only the fixed local confirmation route.
package wxtransferlink

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"strconv"
	"time"
)

func Sign(key []byte, n, t string) string {
	k := hmac.New(sha256.New, key)
	k.Write([]byte("goepay/wxtrans/confirm-link/v1"))
	m := hmac.New(sha256.New, k.Sum(nil))
	m.Write([]byte("link\x00" + n + "\x00" + t))
	return hex.EncodeToString(m.Sum(nil))
}
func Path(key []byte, id int64, created time.Time) string {
	n, t := strconv.FormatInt(id, 10), strconv.FormatInt(created.Unix(), 10)
	return "/pay/wxtrans?" + url.Values{"n": {n}, "t": {t}, "s": {Sign(key, n, t)}}.Encode()
}
