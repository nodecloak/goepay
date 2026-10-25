package printing

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"time"
)

// Wire protocol shared by the fixed-gateway connector and local TLS tests.
// No configurable gateway URL is accepted from a printing setting or job.
const ExcelsecuPrintPath = "/openapi/v1/iot/printer/print"

func BuildExcelsecuRequest(c PrintCommand, at time.Time) ([]byte, error) {
	if c.RequestNo == "" || !safeText(c.RequestNo, 64) || c.Device == "" || !safeText(c.Device, 128) || c.Copies < 1 || c.Copies > 5 || c.Credential.AppID == "" || c.Credential.Secret == "" || at.UnixMilli() <= 0 {
		return nil, ErrInvalid
	}
	content, e := RenderReceipt(c.Receipt)
	if e != nil {
		return nil, e
	}
	biz := map[string]any{"sn": c.Device, "content": base64.StdEncoding.EncodeToString(content), "count": c.Copies}
	if c.Voice {
		biz["tts"] = fmt.Sprintf("%s收款%d.%02d元", c.Receipt.Type, c.Receipt.Amount/100, c.Receipt.Amount%100)
	}
	raw, e := json.Marshal(biz)
	if e != nil {
		return nil, e
	}
	ts := at.UnixMilli()
	m := hmac.New(sha256.New, []byte(c.Credential.Secret))
	m.Write([]byte(c.Credential.AppID + strconv.FormatInt(ts, 10) + string(raw) + "3" + c.RequestNo))
	return json.Marshal(map[string]any{"appid": c.Credential.AppID, "reqid": c.RequestNo, "timestamp": ts, "sign": base64.StdEncoding.EncodeToString(m.Sum(nil)), "biztype": 3, "biz": string(raw)})
}
func ParseExcelsecuResponse(status int, raw []byte) (Outcome, error) {
	if status != 200 || len(raw) > 4096 {
		return Outcome{}, ErrState
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	t, e := dec.Token()
	if e != nil || t != json.Delim('{') {
		return Outcome{}, ErrInvalid
	}
	seen := map[string]bool{}
	var code int64
	hasCode := false
	for dec.More() {
		key, e := dec.Token()
		if e != nil {
			return Outcome{}, ErrInvalid
		}
		name, ok := key.(string)
		if !ok || seen[name] {
			return Outcome{}, ErrInvalid
		}
		seen[name] = true
		var value json.RawMessage
		if dec.Decode(&value) != nil {
			return Outcome{}, ErrInvalid
		}
		if name == "code" {
			if json.Unmarshal(value, &code) != nil || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
				return Outcome{}, ErrInvalid
			}
			hasCode = true
		}
	}
	if _, e = dec.Token(); e != nil {
		return Outcome{}, ErrInvalid
	}
	var extra any
	if dec.Decode(&extra) != io.EOF || !hasCode {
		return Outcome{}, ErrInvalid
	}
	if code == 0 {
		return Outcome{State: "accepted"}, nil
	}
	return Outcome{State: "rejected"}, nil
}
