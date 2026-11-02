// Package adminnotice submits bounded administrator notices through explicitly
// configured, authenticated services. Acceptance never proves final delivery.
package adminnotice

import (
	"context"
	"errors"
	"goepay/internal/merchantnotice"
	"goepay/internal/templatechange"
	"net/http"
	"regexp"
	"strings"
)

var ErrDelivery = errors.New("administrator notice submission unavailable")

type Webhook struct {
	target, endpoint, kind string
	mentionPhone           string
	client                 *http.Client
}

func (*Webhook) String() string { return "adminnotice.Webhook(configured)" }

func NewWebhook(endpoint string, client *http.Client) (*Webhook, error) {
	target, e := merchantnotice.ParseWebhookTarget(endpoint)
	if e != nil {
		return nil, ErrDelivery
	}
	configured, e := newHTTPClient(client)
	if e != nil {
		return nil, e
	}
	return &Webhook{target: target.Original, endpoint: target.Canonical, kind: target.Kind, client: configured}, nil
}

// Only explicit administrator configuration can mention a phone. Order text
// never becomes an @all or mention directive.
func NewWebhookMention(endpoint, phone string, client *http.Client) (*Webhook, error) {
	if phone != "" && !regexp.MustCompile(`^\+?[0-9]{5,20}$`).MatchString(phone) {
		return nil, ErrDelivery
	}
	w, e := NewWebhook(endpoint, client)
	if e != nil {
		return nil, e
	}
	w.mentionPhone = phone
	return w, nil
}
func (w *Webhook) mentionPayload(payload any, title, text string) any {
	if w.mentionPhone == "" {
		return payload
	}
	p, ok := payload.(map[string]any)
	if !ok {
		return payload
	}
	if w.kind == "ding" {
		p["at"] = map[string]any{"isAtAll": false, "atMobiles": []string{w.mentionPhone}}
	}
	if w.kind == "work" {
		return map[string]any{"msgtype": "text", "text": map[string]any{"content": title + "\n" + text, "mentioned_mobile_list": []string{w.mentionPhone}}}
	}
	return p
}

func (w *Webhook) SendChannelStopped(ctx context.Context, to string, channel, event int64, code string) error {
	if w == nil || ctx == nil || ctx.Err() != nil || to != w.target {
		return ErrDelivery
	}
	title, text, e := channelStoppedMessage(channel, event, code)
	if e != nil {
		return e
	}
	var payload any
	switch w.kind {
	case "ding":
		payload = map[string]any{"msgtype": "markdown", "markdown": map[string]string{"title": title, "text": "### " + title + "  \n " + strings.ReplaceAll(text, "\n", "  \n ")}}
	case "work":
		payload = map[string]any{"msgtype": "markdown", "markdown": map[string]string{"content": "## " + title + "\n" + text}}
	default:
		payload = map[string]any{"msg_type": "text", "content": map[string]string{"text": text}}
	}
	raw, e := postJSON(ctx, w.client, w.endpoint, w.mentionPayload(payload, title, text))
	if e != nil {
		return ErrDelivery
	}
	field := "code"
	if w.kind == "ding" || w.kind == "work" {
		field = "errcode"
	}
	if string(raw[field]) != "0" {
		return ErrDelivery
	}
	return nil
}

func (w *Webhook) SendPageTemplateNotice(ctx context.Context, to string, n templatechange.Notice) error {
	if w == nil || ctx == nil || ctx.Err() != nil || to != w.target {
		return ErrDelivery
	}
	title, text, e := n.Message()
	if e != nil {
		return ErrDelivery
	}
	var payload any
	switch w.kind {
	case "ding":
		payload = map[string]any{"msgtype": "markdown", "markdown": map[string]string{"title": title, "text": "### " + title + "  \n " + strings.ReplaceAll(text, "\n", "  \n ")}}
	case "work":
		payload = map[string]any{"msgtype": "markdown", "markdown": map[string]string{"content": "## " + title + "\n" + text}}
	default:
		payload = map[string]any{"msg_type": "text", "content": map[string]string{"text": title + "\n" + text}}
	}
	raw, e := postJSON(ctx, w.client, w.endpoint, w.mentionPayload(payload, title, text))
	if e != nil {
		return ErrDelivery
	}
	field := "code"
	if w.kind == "ding" || w.kind == "work" {
		field = "errcode"
	}
	if string(raw[field]) != "0" {
		return ErrDelivery
	}
	return nil
}
