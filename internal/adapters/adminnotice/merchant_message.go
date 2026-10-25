package adminnotice

import (
	"context"
	"encoding/json"
	"errors"
	"goepay/internal/merchantnotice"
	"html"
	"net/url"
	"strings"
	"time"
)

var ErrMerchantDelivery = errors.New("merchant notice submission unavailable")

func (*Webhook) NoticeTransport() merchantnotice.Transport { return merchantnotice.Robot }
func (w *Webhook) NoticeScope() string                     { return w.ChannelNoticeScope() }
func (w *Webhook) ValidateNoticeRecipient(to string) error {
	if w.ValidateChannelNoticeRecipient(to) != nil {
		return ErrMerchantDelivery
	}
	return nil
}
func (*Telegram) NoticeTransport() merchantnotice.Transport { return merchantnotice.Telegram }
func (t *Telegram) NoticeScope() string                     { return t.ChannelNoticeScope() }
func (t *Telegram) ValidateNoticeRecipient(to string) error {
	if t.ValidateChannelNoticeRecipient(to) != nil {
		return ErrMerchantDelivery
	}
	return nil
}
func (*QQBot) NoticeTransport() merchantnotice.Transport { return merchantnotice.QQBot }
func (q *QQBot) NoticeAccountID() string {
	if q == nil {
		return ""
	}
	return q.appID
}
func (q *QQBot) NoticeScope() string { return q.ChannelNoticeScope() }
func (q *QQBot) ValidateNoticeRecipient(to string) error {
	if q.ValidateChannelNoticeRecipient(to) != nil {
		return ErrMerchantDelivery
	}
	return nil
}

func merchantMarkdown(value string) string {
	return strings.NewReplacer("\\", "\\\\", "`", "\\`", "*", "\\*", "_", "\\_", "[", "\\[", "]", "\\]", "(", "\\(", ")", "\\)", "~", "\\~", "#", "\\#", "|", "\\|", "!", "\\!", "-", "\\-").Replace(html.EscapeString(value))
}

func (w *Webhook) SendMerchantNotice(ctx context.Context, to string, m merchantnotice.Message) error {
	if ctx == nil || ctx.Err() != nil || !m.Valid() || w.ValidateNoticeRecipient(to) != nil {
		return ErrMerchantDelivery
	}
	title, text := merchantMarkdown(m.Title), merchantMarkdown(m.Text)
	var payload any
	switch w.kind {
	case "ding":
		payload = map[string]any{"msgtype": "markdown", "markdown": map[string]string{"title": title, "text": "### " + title + "  \n " + strings.ReplaceAll(text, "\n", "  \n ")}, "at": map[string]bool{"isAtAll": false}}
	case "work":
		payload = map[string]any{"msgtype": "markdown", "markdown": map[string]string{"content": "## " + title + "\n" + text}}
	default:
		// Text webhook protocols recognize mention-like tags too. Escaping
		// untrusted angle brackets prevents names from becoming group mentions.
		payload = map[string]any{"msg_type": "text", "content": map[string]string{"text": html.EscapeString(m.Title + "\n" + m.Text)}}
	}
	out, e := postJSON(ctx, w.client, w.endpoint, payload)
	if e != nil {
		return ErrMerchantDelivery
	}
	field := "code"
	if w.kind == "ding" || w.kind == "work" {
		field = "errcode"
	}
	if string(out[field]) != "0" {
		return ErrMerchantDelivery
	}
	return nil
}

func (t *Telegram) SendMerchantNotice(ctx context.Context, to string, m merchantnotice.Message) error {
	if ctx == nil || ctx.Err() != nil || !m.Valid() || t.ValidateNoticeRecipient(to) != nil {
		return ErrMerchantDelivery
	}
	form := url.Values{"chat_id": {to}, "text": {"<strong>" + html.EscapeString(m.Title) + "</strong>\n" + html.EscapeString(m.Text)}, "parse_mode": {"HTML"}, "disable_web_page_preview": {"true"}}
	out, e := requestJSON(ctx, t.client, t.endpoint, []byte(form.Encode()), "application/x-www-form-urlencoded", "")
	if e != nil {
		return ErrMerchantDelivery
	}
	var accepted bool
	if json.Unmarshal(out["ok"], &accepted) != nil || !accepted {
		return ErrMerchantDelivery
	}
	return nil
}

func (q *QQBot) SendMerchantNotice(ctx context.Context, to string, m merchantnotice.Message) error {
	if ctx == nil || ctx.Err() != nil || !m.Valid() || q.ValidateNoticeRecipient(to) != nil {
		return ErrMerchantDelivery
	}
	work, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	token, e := q.accessToken(work)
	if e != nil {
		return ErrMerchantDelivery
	}
	body, e := json.Marshal(map[string]any{"msg_type": 2, "markdown": map[string]string{"content": "# " + merchantMarkdown(m.Title) + "\n\n" + merchantMarkdown(m.Text)}})
	if e != nil {
		return ErrMerchantDelivery
	}
	out, e := requestJSON(work, q.client, "https://api.bot.qq.com/v2/users/"+to+"/messages", body, "application/json", "QQBot "+token)
	if e != nil {
		return ErrMerchantDelivery
	}
	if code, ok := out["code"]; ok && string(code) != "0" {
		return ErrMerchantDelivery
	}
	var id string
	if json.Unmarshal(out["id"], &id) != nil || !validCredential(id, 512) {
		return ErrMerchantDelivery
	}
	return nil
}

var _ merchantnotice.Sender = (*Webhook)(nil)
var _ merchantnotice.Sender = (*Telegram)(nil)
var _ merchantnotice.Sender = (*QQBot)(nil)
