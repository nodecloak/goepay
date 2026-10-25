package channelconfig

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	provider "goepay/internal/adapters/payment"
	"goepay/internal/platform/httpclient"
	"io"
	"strconv"
	"strings"
)

// IdentityProfileSpec is accepted only inside the explicitly encrypted import.
// Enabled defaults false. Missing account mode never means direct merchant.
type IdentityProfileSpec struct {
	ID           int64               `json:"id,string"`
	Revision     int64               `json:"revision,string"`
	ChannelID    int64               `json:"channel_id,string"`
	Enabled      bool                `json:"enabled"`
	Provider     string              `json:"provider"`
	Kind         string              `json:"kind"`
	AppID        string              `json:"app_id"`
	AccountMode  string              `json:"account_mode"`
	Path         string              `json:"path"`
	MerchantIDs  IdentityMerchantIDs `json:"merchant_ids"`
	Config       map[string]string   `json:"config"`
	Certificates map[string]string   `json:"certificates,omitempty"`
	// Omission preserves existing serialized profile/fingerprint bytes. This
	// flag authorizes only a standalone WeChat mini identity link attempt.
	EnableURLLink bool `json:"enable_url_link,omitempty"`
}

// Runtime profiles deliberately never serialize credentials or adapter objects.
type IdentityProfile struct {
	ID          int64                             `json:"id,string"`
	Revision    int64                             `json:"revision,string"`
	ChannelID   int64                             `json:"channel_id,string"`
	Provider    string                            `json:"provider"`
	Kind        string                            `json:"kind"`
	AppID       string                            `json:"app_id"`
	Path        string                            `json:"path"`
	MerchantIDs []int64                           `json:"-"`
	Fingerprint string                            `json:"-"`
	Web         provider.WebOAuthProvider         `json:"-"`
	Mini        provider.MiniOAuthProvider        `json:"-"`
	MiniLink    provider.MiniIdentityLinkProvider `json:"-"`
	// Installed client availability is not vendor permission, a created link,
	// or a claim about a later link worker's health.
	URLLinkAvailable bool `json:"url_link_available,omitempty"`
}

func (p IdentityProfile) AllowsMerchant(mid int64) bool {
	for _, id := range p.MerchantIDs {
		if id == mid && mid > 0 {
			return true
		}
	}
	return false
}

func directIdentityChannel(c Channel) bool {
	if mode := c.Config["account_mode"]; mode != "" && mode != "direct" {
		return false
	}
	for _, key := range []string{"sub_appid", "sub_mchid", "sub_mch_id", "sp_appid", "sp_mchid", "app_auth_token"} {
		if c.Config[key] != "" {
			return false
		}
	}
	return c.Config["ecommerce"] == "" || c.Config["ecommerce"] == "false"
}

// BuildIdentityProfiles performs no I/O. It has an independent default-off gate
// and does not require payment products, mini payment launch, or financial writes.
func BuildIdentityProfiles(imp Import, enabled bool) (map[int64]IdentityProfile, error) {
	out := map[int64]IdentityProfile{}
	if !enabled {
		return out, nil
	}
	if imp.Version != 1 || len(imp.IdentityProfiles) > 256 {
		return nil, ErrImport
	}
	channels := map[int64]Channel{}
	for _, c := range imp.Channels {
		if c.ID <= 0 || channels[c.ID].ID != 0 {
			return nil, ErrImport
		}
		channels[c.ID] = c
	}
	seen := map[int64]bool{}
	for _, spec := range imp.IdentityProfiles {
		if spec.ID <= 0 || seen[spec.ID] {
			return nil, ErrImport
		}
		seen[spec.ID] = true
		if !spec.Enabled {
			continue
		}
		c, ok := channels[spec.ChannelID]
		if !ok || !directIdentityChannel(c) || c.Plugin != spec.Provider || spec.Revision <= 0 || spec.AccountMode != "direct" || (spec.Kind != "web" && spec.Kind != "mini") || len(spec.MerchantIDs) < 1 || len(spec.MerchantIDs) > 1000 {
			return nil, ErrImport
		}
		if spec.EnableURLLink && (spec.Provider != "wxpay" || spec.Kind != "mini") {
			return nil, ErrImport
		}
		ids := map[int64]bool{}
		for _, id := range spec.MerchantIDs {
			if id <= 0 || ids[id] {
				return nil, ErrImport
			}
			ids[id] = true
		}
		if spec.Kind == "web" && spec.Path != "" || spec.Kind == "mini" && (len(spec.Path) > 256 || !miniOAuthPath.MatchString(spec.Path)) {
			return nil, ErrImport
		}
		if spec.Kind == "mini" {
			for _, part := range strings.Split(spec.Path, "/") {
				if part == "." || part == ".." {
					return nil, ErrImport
				}
			}
		}
		p := IdentityProfile{ID: spec.ID, Revision: spec.Revision, ChannelID: spec.ChannelID, Provider: spec.Provider, Kind: spec.Kind, AppID: spec.AppID, Path: spec.Path, MerchantIDs: append([]int64(nil), spec.MerchantIDs...)}
		switch spec.Provider {
		case "wxpay":
			if len(spec.Config) != 1 || spec.Config["appsecret"] == "" || len(spec.Certificates) != 0 {
				return nil, ErrImport
			}
			cfg := provider.WechatOAuthConfig{AppID: spec.AppID, AppSecret: spec.Config["appsecret"], HTTPClient: httpclient.New()}
			var e error
			if spec.Kind == "web" {
				p.Web, e = provider.NewWechatOAuth(cfg)
			} else {
				p.Mini, e = provider.NewWechatMiniOAuth(cfg)
			}
			if e != nil {
				return nil, ErrImport
			}
			if spec.EnableURLLink {
				// Nil HTTPClient selects the URL Link adapter's owned secure
				// transport, not the unrelated platform guardedTransport.
				p.MiniLink, e = provider.NewWechatMiniLink(provider.WechatMiniLinkConfig{AppID: spec.AppID, AppSecret: spec.Config["appsecret"], IdentityPath: spec.Path})
				if e != nil {
					return nil, ErrImport
				}
				p.URLLinkAvailable = true
			}
		case "alipay":
			for key := range spec.Config {
				if key != "appsecret" && key != "appkey" && key != "sign_type" {
					return nil, ErrImport
				}
			}
			for key := range spec.Certificates {
				if key != "app_cert_path" && key != "alipay_cert_path" && key != "root_cert_path" {
					return nil, ErrImport
				}
			}
			clone := Channel{ID: spec.ID, Plugin: "alipay", AppTypes: []string{"4"}, Config: map[string]string{"appid": spec.AppID, "appsecret": spec.Config["appsecret"], "appkey": spec.Config["appkey"], "sign_type": spec.Config["sign_type"], "enable_web_oauth": "true"}, Certificates: spec.Certificates}
			adapters, e := BuildOAuth(Import{Version: 1, Channels: []Channel{clone}})
			if e != nil {
				return nil, ErrImport
			}
			if spec.Kind == "web" {
				p.Web = adapters[spec.ID]
			} else {
				var valid bool
				p.Mini, valid = adapters[spec.ID].(provider.MiniOAuthProvider)
				if !valid {
					return nil, ErrImport
				}
			}
		default:
			return nil, ErrImport
		}
		raw, e := json.Marshal(struct {
			Profile IdentityProfileSpec
			Channel Channel
		}{spec, c})
		if e != nil {
			return nil, ErrImport
		}
		h := sha256.Sum256(raw)
		p.Fingerprint = hex.EncodeToString(h[:])
		out[p.ID] = p
	}
	return out, nil
}

// Slice elements need their own canonical decoder; encoding/json's ,string
// option applies to scalars and cannot protect []int64 elements.
type IdentityMerchantIDs []int64

func (ids IdentityMerchantIDs) MarshalJSON() ([]byte, error) {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = strconv.FormatInt(id, 10)
	}
	return json.Marshal(out)
}
func (ids *IdentityMerchantIDs) UnmarshalJSON(raw []byte) error {
	var values []string
	if bytes.Equal(raw, []byte("null")) || json.Unmarshal(raw, &values) != nil || len(values) > 1000 {
		return ErrImport
	}
	out := make(IdentityMerchantIDs, len(values))
	for i, v := range values {
		id, e := strconv.ParseInt(v, 10, 64)
		if e != nil || id <= 0 || strconv.FormatInt(id, 10) != v {
			return ErrImport
		}
		out[i] = id
	}
	*ids = out
	return nil
}
func (p *IdentityProfileSpec) UnmarshalJSON(raw []byte) error {
	if len(raw) > 65536 {
		return ErrImport
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	first, e := d.Token()
	if e != nil || first != json.Delim('{') {
		return ErrImport
	}
	seen := map[string]bool{}
	for d.More() {
		key, e := d.Token()
		name, ok := key.(string)
		if e != nil || !ok || seen[name] {
			return ErrImport
		}
		seen[name] = true
		// Field names are exact; encoding/json's case folding must not turn a
		// differently named flag (or identifier alias) into trusted config.
		switch name {
		case "id", "revision", "channel_id", "enabled", "provider", "kind", "app_id", "account_mode", "path", "merchant_ids", "config", "certificates", "enable_url_link":
		default:
			return ErrImport
		}
		var value json.RawMessage
		if d.Decode(&value) != nil || bytes.Equal(value, []byte("null")) {
			return ErrImport
		}
		if name == "id" || name == "revision" || name == "channel_id" {
			var text string
			if json.Unmarshal(value, &text) != nil {
				return ErrImport
			}
			id, err := strconv.ParseInt(text, 10, 64)
			if err != nil || id <= 0 || strconv.FormatInt(id, 10) != text {
				return ErrImport
			}
		}
		if (name == "config" || name == "certificates") && !identityProfileStringMap(value) {
			return ErrImport
		}
	}
	if _, e = d.Token(); e != nil {
		return ErrImport
	}
	if _, e = d.Token(); e != io.EOF {
		return ErrImport
	}
	type plain IdentityProfileSpec
	var value plain
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&value) != nil {
		return ErrImport
	}
	*p = IdentityProfileSpec(value)
	return nil
}

func identityProfileStringMap(raw []byte) bool {
	d := json.NewDecoder(bytes.NewReader(raw))
	first, err := d.Token()
	if err != nil || first != json.Delim('{') {
		return false
	}
	seen := map[string]bool{}
	for d.More() {
		key, err := d.Token()
		name, ok := key.(string)
		if err != nil || !ok || seen[name] {
			return false
		}
		seen[name] = true
		var value json.RawMessage
		var text string
		if d.Decode(&value) != nil || bytes.Equal(value, []byte("null")) || json.Unmarshal(value, &text) != nil {
			return false
		}
	}
	_, err = d.Token()
	if err != nil {
		return false
	}
	_, err = d.Token()
	return err == io.EOF
}
