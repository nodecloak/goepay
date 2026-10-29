package pagetemplate

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strings"
	"unicode/utf8"
)

var ErrInvalid = errors.New("invalid page template input")
var (
	ErrConflict  = errors.New("page template revision conflict")
	ErrForbidden = errors.New("page template operation forbidden")
	ErrNotFound  = errors.New("page template not found")
)

type Config struct {
	BrandName       string `json:"brand_name"`
	Notice          string `json:"notice"`
	AccentColor     string `json:"accent_color"`
	BackgroundColor string `json:"background_color"`
	LogoAssetID     string `json:"logo_asset_id"`
	Density         string `json:"density"`
	ShowOrderName   bool   `json:"show_order_name,omitempty"`
}
type Template struct {
	Type        string `json:"type"`
	Name        string `json:"name"`
	Title       string `json:"title"`
	Author      string `json:"author"`
	Version     string `json:"version"`
	Description string `json:"description"`
	PreviewURL  string `json:"preview_url"`
	HasConfig   bool   `json:"has_config"`
}
type Option struct {
	Label string `json:"label"`
	Value string `json:"value"`
}
type Field struct {
	Name      string   `json:"name"`
	Label     string   `json:"label"`
	Type      string   `json:"type"`
	Required  bool     `json:"required"`
	MaxLength int      `json:"max_length,omitempty"`
	Options   []Option `json:"options,omitempty"`
}
type Resolved struct {
	Type, Name string
	Revision   int64
	Config     Config
	Fallback   string
}
type Page struct {
	SiteLogo                                        bool
	Title, TradeNo, Amount, OrderName, MerchantName string
	BrandName, Keywords, Description                string
	ForegroundColor                                 string
	Sandbox, Preview                                bool
}

func validKind(kind string) bool { return kind == "home" || kind == "pay" || kind == "qrpay" }
func Templates(kind string) ([]Template, error) {
	if !validKind(kind) {
		return nil, ErrInvalid
	}
	if kind == "home" {
		return HomeTemplates(), nil
	}
	title := map[string]string{"pay": "支付页", "qrpay": "聚合收款页"}[kind]
	return []Template{{Type: kind, Name: "default", Title: "标准" + title, Author: "GOEPAY", Version: "1", Description: "本地内置标准布局", PreviewURL: "/console/v1/admin/templates/" + kind + "/default/preview", HasConfig: true}, {Type: kind, Name: "compact", Title: "紧凑" + title, Author: "GOEPAY", Version: "1", Description: "本地内置紧凑布局", PreviewURL: "/console/v1/admin/templates/" + kind + "/compact/preview", HasConfig: true}}, nil
}
func Lookup(kind, name string) (Template, bool) {
	list, e := Templates(kind)
	if e != nil {
		return Template{}, false
	}
	for _, v := range list {
		if v.Name == name {
			return v, true
		}
	}
	return Template{}, false
}
func DefaultConfig(kind, name string) Config {
	return Config{BrandName: "GOEPAY", AccentColor: "#007c91", BackgroundColor: "#f4f8fa", Density: "comfortable", ShowOrderName: kind == "pay"}
}
func (c Config) Public(kind string) map[string]any {
	m := map[string]any{"brand_name": c.BrandName, "notice": c.Notice, "accent_color": c.AccentColor, "background_color": c.BackgroundColor, "logo_asset_id": c.LogoAssetID, "density": c.Density}
	if kind == "pay" {
		m["show_order_name"] = c.ShowOrderName
	}
	return m
}
func Fields(kind string) []Field {
	out := []Field{{Name: "brand_name", Label: "品牌名称", Type: "text", Required: true, MaxLength: 64}, {Name: "notice", Label: "说明文字", Type: "textarea", MaxLength: 240}, {Name: "accent_color", Label: "主题颜色", Type: "color", Required: true}, {Name: "background_color", Label: "背景颜色", Type: "color", Required: true}, {Name: "logo_asset_id", Label: "品牌图片", Type: "image"}, {Name: "density", Label: "内容间距", Type: "select", Required: true, Options: []Option{{Label: "舒适", Value: "comfortable"}, {Label: "紧凑", Value: "compact"}}}}
	if kind == "pay" {
		out = append(out, Field{Name: "show_order_name", Label: "显示订单名称", Type: "switch"})
	}
	return out
}

var colorPattern = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
var assetPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

func object(raw []byte) (map[string]json.RawMessage, error) {
	if len(raw) == 0 || len(raw) > 8192 || !utf8.Valid(raw) {
		return nil, ErrInvalid
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	v, e := d.Token()
	if e != nil || v != json.Delim('{') {
		return nil, ErrInvalid
	}
	m := map[string]json.RawMessage{}
	for d.More() {
		key, e := d.Token()
		s, ok := key.(string)
		if e != nil || !ok {
			return nil, ErrInvalid
		}
		if _, ok = m[s]; ok {
			return nil, ErrInvalid
		}
		var value json.RawMessage
		if e = d.Decode(&value); e != nil || bytes.Equal(value, []byte("null")) {
			return nil, ErrInvalid
		}
		m[s] = value
		if len(m) > 8 {
			return nil, ErrInvalid
		}
	}
	v, e = d.Token()
	if e != nil || v != json.Delim('}') {
		return nil, ErrInvalid
	}
	if _, e = d.Token(); e != io.EOF {
		return nil, ErrInvalid
	}
	return m, nil
}
func ParseConfig(kind, name string, raw json.RawMessage) (Config, error) {
	var c Config
	if _, ok := Lookup(kind, name); !ok {
		return c, ErrInvalid
	}
	m, e := object(raw)
	if e != nil || len(m) != len(Fields(kind)) {
		return c, ErrInvalid
	}
	for _, f := range Fields(kind) {
		value, ok := m[f.Name]
		if !ok {
			return c, ErrInvalid
		}
		if f.Type == "switch" {
			if e = json.Unmarshal(value, &c.ShowOrderName); e != nil {
				return c, ErrInvalid
			}
			continue
		}
		var text string
		if e = json.Unmarshal(value, &text); e != nil {
			return c, ErrInvalid
		}
		for _, r := range text {
			if r < 32 && !(f.Name == "notice" && (r == '\n' || r == '\r' || r == '\t')) || r == 127 {
				return c, ErrInvalid
			}
		}
		switch f.Name {
		case "brand_name":
			if strings.TrimSpace(text) == "" || utf8.RuneCountInString(text) > 64 {
				return c, ErrInvalid
			}
			c.BrandName = text
		case "notice":
			if utf8.RuneCountInString(text) > 240 {
				return c, ErrInvalid
			}
			c.Notice = text
		case "accent_color":
			if !colorPattern.MatchString(text) {
				return c, ErrInvalid
			}
			c.AccentColor = text
		case "background_color":
			if !colorPattern.MatchString(text) {
				return c, ErrInvalid
			}
			c.BackgroundColor = text
		case "logo_asset_id":
			if text != "" && !assetPattern.MatchString(text) {
				return c, ErrInvalid
			}
			c.LogoAssetID = text
		case "density":
			if text != "comfortable" && text != "compact" {
				return c, ErrInvalid
			}
			c.Density = text
		default:
			return c, ErrInvalid
		}
	}
	return c, nil
}
