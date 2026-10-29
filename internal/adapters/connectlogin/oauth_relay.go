package connectlogin

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// OauthRelay ports epay_release/includes/lib/Oauth.php: a self-hosted quick-login
// relay (login_apiurl + login_appid/login_appkey) used for WeChat and Alipay
// (login_wx==-1 / login_alipay==-1 in the original). `type` selects the social
// network at the relay ("wx" / "alipay").
//
//	login(type)     GET apiurl?act=login&...&state=STATE -> {code:0, url:"..."}
//	callback(code)  GET apiurl?act=callback&...&code=CODE -> {code:0, social_uid, access_token}
//
// The relay owns the provider handshake; GOEPAY only passes state through and
// reads social_uid. Fails closed: any non-{code:0} / missing social_uid /
// non-2xx / unparseable reply yields ErrUpstream, so an unknown or unconfirmed
// identity is never a login.
type OauthRelay struct {
	APIBase     string // login_apiurl; the relay endpoint is APIBase+"connect.php"
	AppID       string
	AppKey      string
	Type        string // "wx" | "alipay"
	CallbackURL string
	Client      *http.Client
}

func (o OauthRelay) endpoint() string {
	return strings.TrimRight(o.APIBase, "/") + "/connect.php"
}

func (o OauthRelay) configured() bool {
	return o.APIBase != "" && o.AppID != "" && o.AppKey != "" && o.Type != "" && o.CallbackURL != ""
}

// AuthorizeURL asks the relay for the provider authorize URL for this state.
func (o OauthRelay) AuthorizeURL(ctx context.Context, state string) (string, error) {
	if !o.configured() || state == "" {
		return "", ErrConfig
	}
	v := url.Values{}
	v.Set("act", "login")
	v.Set("appid", o.AppID)
	v.Set("appkey", o.AppKey)
	v.Set("type", o.Type)
	v.Set("redirect_uri", o.CallbackURL)
	v.Set("state", state)
	var out struct {
		Code int    `json:"code"`
		URL  string `json:"url"`
		Msg  string `json:"msg"`
	}
	if e := o.get(ctx, v, &out); e != nil {
		return "", e
	}
	if out.Code != 0 || out.URL == "" {
		return "", ErrUpstream
	}
	return out.URL, nil
}

// Exchange trades the callback code for the relay's social_uid.
func (o OauthRelay) Exchange(ctx context.Context, code string) (string, error) {
	if !o.configured() {
		return "", ErrConfig
	}
	if code == "" {
		return "", ErrUpstream
	}
	v := url.Values{}
	v.Set("act", "callback")
	v.Set("appid", o.AppID)
	v.Set("appkey", o.AppKey)
	v.Set("code", code)
	var out struct {
		Code      int    `json:"code"`
		SocialUID string `json:"social_uid"`
		Token     string `json:"access_token"`
		Msg       string `json:"msg"`
	}
	if e := o.get(ctx, v, &out); e != nil {
		return "", e
	}
	if out.Code != 0 || out.SocialUID == "" {
		return "", ErrUpstream
	}
	return out.SocialUID, nil
}

func (o OauthRelay) get(ctx context.Context, v url.Values, out any) error {
	req, e := http.NewRequestWithContext(ctx, http.MethodGet, o.endpoint()+"?"+v.Encode(), nil)
	if e != nil {
		return ErrUpstream
	}
	resp, e := client(o.Client).Do(req)
	if e != nil {
		return ErrUpstream
	}
	defer resp.Body.Close()
	body, e := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if e != nil || resp.StatusCode != 200 {
		return ErrUpstream
	}
	if json.Unmarshal(body, out) != nil {
		return ErrUpstream
	}
	return nil
}
