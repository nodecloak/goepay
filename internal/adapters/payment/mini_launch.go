package payment

import (
	"encoding/json"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// MiniProgramConfig is trusted startup metadata. It cannot select a different
// payment AppID or take a caller-supplied page/redirect. No identity is implied.
type MiniProgramConfig struct{ AppID, Path, OriginalID string }
type MiniLaunchRequest struct {
	TradeNo, CompleteURL string
	Amount               int64
}
type MiniLaunchProvider interface {
	MiniProgram() (MiniProgramConfig, bool)
	MiniLaunch(MiniLaunchRequest) (CreateResult, error)
}

var miniPathPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+(?:/[A-Za-z0-9_.-]+)*$`)

func configureMiniProgram(c *MiniProgramConfig, appID, kind string, methods []string) (*MiniProgramConfig, []string, error) {
	if c == nil {
		if enabled("applet", methods) {
			return nil, nil, ErrInvalid
		}
		return nil, methods, nil
	}
	copy := *c
	if copy.AppID == "" {
		copy.AppID = appID
	}
	if copy.AppID != appID || copy.Path == "" || len(copy.Path) > 256 || !miniPathPattern.MatchString(copy.Path) {
		return nil, nil, ErrInvalid
	}
	for _, segment := range strings.Split(copy.Path, "/") {
		if segment == "." || segment == ".." {
			return nil, nil, ErrInvalid
		}
	}
	underlying := "jsapi"
	if kind == "alipay" {
		underlying = "mini"
		if copy.OriginalID != "" {
			return nil, nil, ErrInvalid
		}
	}
	if !enabled(underlying, methods) {
		return nil, nil, ErrUnsupported
	}
	if copy.OriginalID != "" && (!strings.HasPrefix(copy.OriginalID, "gh_") || len(copy.OriginalID) > 128 || !validOAuthValue(copy.OriginalID, 128)) {
		return nil, nil, ErrInvalid
	}
	if !enabled("applet", methods) {
		methods = append(methods, "applet")
	}
	return &copy, methods, nil
}

func miniLaunch(kind string, c *MiniProgramConfig, r MiniLaunchRequest) (CreateResult, error) {
	if c == nil {
		return CreateResult{}, ErrUnsupported
	}
	if r.TradeNo == "" || len(r.TradeNo) > 64 || r.Amount <= 0 || r.Amount > 999999999999 {
		return CreateResult{}, ErrInvalid
	}
	u, e := url.Parse(r.CompleteURL)
	if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Fragment != "" || u.Opaque != "" || u.RawPath != "" {
		return CreateResult{}, ErrInvalid
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) != 6 || parts[0] != "oauth" || parts[1] != "mini" || parts[2] != "complete" || parts[3] != kind || parts[5] != r.TradeNo || u.Path != "/"+strings.Join(parts, "/")+"/" {
		return CreateResult{}, ErrInvalid
	}
	if channel, e := strconv.ParseInt(parts[4], 10, 64); e != nil || channel <= 0 || strconv.FormatInt(channel, 10) != parts[4] {
		return CreateResult{}, ErrInvalid
	}
	query, e := url.ParseQuery(u.RawQuery)
	if e != nil || len(query) != 1 || len(query["state"]) != 1 || len(query.Get("state")) < 32 || len(query.Get("state")) > 128 {
		return CreateResult{}, ErrInvalid
	}
	for _, character := range query.Get("state") {
		if !(character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == '-' || character == '_') {
			return CreateResult{}, ErrInvalid
		}
	}
	page := c.Path + "?" + url.Values{"money": {amountString(r.Amount)}, "url": {u.String()}}.Encode()
	if kind == "wxpay" {
		material, e := json.Marshal(map[string]string{"appId": c.AppID, "miniProgramId": c.OriginalID, "path": page})
		if e != nil {
			return CreateResult{}, e
		}
		return CreateResult{MaterialType: "wxapp", Material: string(material)}, nil
	}
	if kind == "alipay" {
		return CreateResult{MaterialType: "urlscheme", Material: "alipays://platformapi/startapp?" + url.Values{"appId": {c.AppID}, "page": {page}}.Encode()}, nil
	}
	return CreateResult{}, ErrUnsupported
}
func (w *WechatV2) MiniProgram() (MiniProgramConfig, bool) {
	if w.config.MiniProgram == nil {
		return MiniProgramConfig{}, false
	}
	return *w.config.MiniProgram, true
}
func (w *WechatV2) MiniLaunch(r MiniLaunchRequest) (CreateResult, error) {
	return miniLaunch(w.ID(), w.config.MiniProgram, r)
}
func (a *Alipay) MiniProgram() (MiniProgramConfig, bool) {
	if a.config.MiniProgram == nil {
		return MiniProgramConfig{}, false
	}
	return *a.config.MiniProgram, true
}
func (a *Alipay) MiniLaunch(r MiniLaunchRequest) (CreateResult, error) {
	if e := a.validateCertificates(); e != nil {
		return CreateResult{}, e
	}
	return miniLaunch(a.ID(), a.config.MiniProgram, r)
}
