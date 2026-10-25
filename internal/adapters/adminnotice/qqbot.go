package adminnotice

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"strconv"
	"sync"
	"time"
)

type QQBot struct {
	appID, secret string
	client        *http.Client
	mu            sync.Mutex
	token         string
	tokenUntil    time.Time
}

func (*QQBot) String() string { return "adminnotice.QQBot(configured)" }

var qqIdentity = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

func validCredential(value string, max int) bool {
	if len(value) == 0 || len(value) > max {
		return false
	}
	for _, r := range value {
		if r < 33 || r > 126 {
			return false
		}
	}
	return true
}

func NewQQBot(appID, secret string, client *http.Client) (*QQBot, error) {
	if !qqIdentity.MatchString(appID) || !validCredential(secret, 1024) {
		return nil, ErrDelivery
	}
	configured, e := newHTTPClient(client)
	if e != nil {
		return nil, e
	}
	return &QQBot{appID: appID, secret: secret, client: configured}, nil
}
func (q *QQBot) SendChannelStopped(ctx context.Context, to string, channel, event int64, code string) error {
	if q == nil || ctx == nil || ctx.Err() != nil || !qqIdentity.MatchString(to) {
		return ErrDelivery
	}
	title, text, e := channelStoppedMessage(channel, event, code)
	if e != nil {
		return e
	}
	work, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	token, e := q.accessToken(work)
	if e != nil {
		return ErrDelivery
	}
	body, e := json.Marshal(map[string]any{"msg_type": 2, "markdown": map[string]string{"content": "# " + title + "\n\n" + text}})
	if e != nil {
		return ErrDelivery
	}
	result, e := requestJSON(work, q.client, "https://api.bot.qq.com/v2/users/"+to+"/messages", body, "application/json", "QQBot "+token)
	if e != nil {
		return ErrDelivery
	}
	var id string
	if code, ok := result["code"]; ok && string(code) != "0" {
		return ErrDelivery
	}
	if json.Unmarshal(result["id"], &id) != nil || !validCredential(id, 512) {
		return ErrDelivery
	}
	return nil
}

func (q *QQBot) accessToken(ctx context.Context) (string, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if ctx.Err() != nil {
		return "", ErrDelivery
	}
	if q.token != "" && time.Now().Before(q.tokenUntil) {
		return q.token, nil
	}
	q.token, q.tokenUntil = "", time.Time{}
	// Measure TTL from before the request, so network time cannot extend a
	// provider's actual validity period. Each profile owns its private cache.
	started := time.Now()
	credentials, e := postJSON(ctx, q.client, "https://api.bot.qq.com/app/getAppAccessToken", map[string]string{"appId": q.appID, "clientSecret": q.secret})
	if e != nil {
		return "", ErrDelivery
	}
	if code, ok := credentials["code"]; ok && string(code) != "0" {
		return "", ErrDelivery
	}
	var token string
	if json.Unmarshal(credentials["access_token"], &token) != nil || !validCredential(token, 4096) {
		return "", ErrDelivery
	}
	seconds := int64(7200) // Legacy default only when the field is absent.
	if raw, ok := credentials["expires_in"]; ok {
		value := string(raw)
		if len(value) > 0 && value[0] == '"' {
			if json.Unmarshal(raw, &value) != nil {
				return "", ErrDelivery
			}
		}
		seconds, e = strconv.ParseInt(value, 10, 64)
		if e != nil || strconv.FormatInt(seconds, 10) != value || seconds < 1 || seconds > 86400 {
			return "", ErrDelivery
		}
	}
	margin := int64(120)
	if seconds <= 240 {
		margin = seconds / 2
	}
	until := started.Add(time.Duration(seconds-margin) * time.Second)
	if !time.Now().Before(until) {
		return "", ErrDelivery
	}
	q.token, q.tokenUntil = token, until
	return token, nil
}
