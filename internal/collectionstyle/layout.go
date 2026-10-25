// Package collectionstyle validates presentation only. It cannot select payment
// accounts, create orders, or change the signed collection-link lifetime.
package collectionstyle

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

var ErrInvalid = errors.New("invalid collection style")
var color = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
var name = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{0,99}$`)
var font = regexp.MustCompile(`^([1-9][0-9]{0,2})px ('[\p{L}\p{N} _-]{1,64}'|"[\p{L}\p{N} _-]{1,64}"|[\p{L}\p{N} _-]{1,64})$`)
var assetPath = regexp.MustCompile(`^/collection-style-assets/([0-9a-f]{64})$`)

type Layout struct {
	Name       string `json:"name"`
	Label      string `json:"label"`
	Enabled    bool   `json:"enabled"`
	Sort       int    `json:"sort"`
	URL        string `json:"url"`
	Width      int    `json:"img_width"`
	Height     int    `json:"img_height"`
	QRWidth    int    `json:"qr_width"`
	QRHeight   int    `json:"qr_height"`
	QRLeft     int    `json:"qr_left"`
	QRTop      int    `json:"qr_top"`
	Foreground string `json:"foreground"`
	Background string `json:"background"`
	Font       string `json:"font"`
	FontColor  string `json:"font_color"`
	TextLeft   *int   `json:"rec_name_left"`
	TextTop    int    `json:"rec_name_top"`
	Word       int    `json:"word"`
}

func AssetID(address string) string {
	p := assetPath.FindStringSubmatch(address)
	if len(p) == 2 {
		return p[1]
	}
	return ""
}
func ValidImageURL(address string) bool {
	if AssetID(address) != "" {
		return true
	}
	u, e := url.Parse(address)
	return e == nil && u.Scheme == "https" && u.Host != "" && u.User == nil && u.Fragment == "" && len(address) <= 2048 && !strings.ContainsAny(address, "\x00\r\n")
}
func Revision(body map[string]any) string {
	raw, _ := json.Marshal(body)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func IsLayout(body map[string]any) bool {
	for _, key := range []string{"label", "url", "img_width", "img_height", "qr_width", "qr_height", "qr_left", "qr_top", "foreground", "background", "font", "font_color", "rec_name_left", "rec_name_top", "word"} {
		if _, ok := body[key]; ok {
			return true
		}
	}
	return false
}

// A nil layout is an old simple-color style, retained without inventing canvas
// geometry. New layouts require every coordinate consumed by the renderer.
func Parse(body map[string]any) (*Layout, error) {
	if !IsLayout(body) {
		return nil, nil
	}
	text := func(key string) string { s, _ := body[key].(string); return s }
	l := &Layout{Name: text("name"), Label: text("label"), URL: text("url"), Foreground: text("foreground"), Background: text("background"), Font: text("font"), FontColor: text("font_color"), Enabled: body["enabled"] == true}
	if !name.MatchString(l.Name) || l.Label == "" || len(l.Label) > 100 || !ValidImageURL(l.URL) || !color.MatchString(l.Foreground) || l.Background != "transparent" && !color.MatchString(l.Background) || l.FontColor != "transparent" && !color.MatchString(l.FontColor) {
		return nil, ErrInvalid
	}
	fp := font.FindStringSubmatch(l.Font)
	if len(fp) != 3 || strings.Trim(fp[2], "'\" ") == "" {
		return nil, ErrInvalid
	}
	size, e := strconv.Atoi(fp[1])
	if e != nil || size > 256 {
		return nil, ErrInvalid
	}
	number := func(key string, dest *int, min, max int) error {
		raw, ok := body[key]
		if !ok {
			return ErrInvalid
		}
		n, e := strconv.Atoi(fmt.Sprint(raw))
		if e != nil || n < min || n > max {
			return ErrInvalid
		}
		*dest = n
		return nil
	}
	for _, f := range []struct {
		key      string
		dest     *int
		min, max int
	}{{"sort", &l.Sort, 0, 1000000}, {"img_width", &l.Width, 64, 4096}, {"img_height", &l.Height, 64, 4096}, {"qr_width", &l.QRWidth, 32, 4096}, {"qr_height", &l.QRHeight, 32, 4096}, {"qr_left", &l.QRLeft, 0, 4096}, {"qr_top", &l.QRTop, 0, 4096}, {"rec_name_top", &l.TextTop, 0, 4096}, {"word", &l.Word, 0, 2}} {
		if e = number(f.key, f.dest, f.min, f.max); e != nil {
			return nil, e
		}
	}
	if l.Width*l.Height > 4194304 || l.QRLeft+l.QRWidth > l.Width || l.QRTop+l.QRHeight > l.Height || l.TextTop > l.Height {
		return nil, ErrInvalid
	}
	if v, ok := body["rec_name_left"]; ok && v != nil && fmt.Sprint(v) != "" {
		n, e := strconv.Atoi(fmt.Sprint(v))
		if e != nil || n < 0 || n > l.Width {
			return nil, ErrInvalid
		}
		l.TextLeft = &n
	}
	return l, nil
}
