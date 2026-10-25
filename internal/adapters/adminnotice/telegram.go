package adminnotice

import (
	"context"
	"encoding/json"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
)

type Telegram struct {
	endpoint string
	client   *http.Client
}

func (*Telegram) String() string { return "adminnotice.Telegram(configured)" }

var telegramToken = regexp.MustCompile(`^[1-9][0-9]{0,18}:[A-Za-z0-9_-]{1,256}$`)
var telegramRecipientName = regexp.MustCompile(`^@[A-Za-z0-9_]{1,64}$`)

func NewTelegram(token string, client *http.Client) (*Telegram, error) {
	if !telegramToken.MatchString(token) {
		return nil, ErrDelivery
	}
	configured, e := newHTTPClient(client)
	if e != nil {
		return nil, e
	}
	return &Telegram{endpoint: "https://api.telegram.org/bot" + token + "/sendMessage", client: configured}, nil
}
func (t *Telegram) SendChannelStopped(ctx context.Context, to string, channel, event int64, code string) error {
	if t == nil || ctx == nil || ctx.Err() != nil {
		return ErrDelivery
	}
	id, e := strconv.ParseInt(to, 10, 64)
	if e != nil || id == 0 || strconv.FormatInt(id, 10) != to {
		if !telegramRecipientName.MatchString(to) {
			return ErrDelivery
		}
	}
	title, text, e := channelStoppedMessage(channel, event, code)
	if e != nil {
		return e
	}
	form := url.Values{"chat_id": {to}, "text": {"<strong>" + html.EscapeString(title) + "</strong>\n" + html.EscapeString(text)}, "parse_mode": {"HTML"}, "disable_web_page_preview": {"true"}}
	response, e := requestJSON(ctx, t.client, t.endpoint, []byte(form.Encode()), "application/x-www-form-urlencoded", "")
	if e != nil {
		return ErrDelivery
	}
	var accepted bool
	if json.Unmarshal(response["ok"], &accepted) != nil || !accepted {
		return ErrDelivery
	}
	return nil
}
