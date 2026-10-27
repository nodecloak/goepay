package siteinfo

import (
	"context"
	"encoding/json"
	"errors"
	"net/mail"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

var ErrInvalid = errors.New("invalid website settings")

var OriginalKeys = []string{"sitename", "title", "keywords", "description", "orgname", "email", "kfqq", "kfqq_url", "wework_url", "qqqun", "login_subtitle", "homepage", "homepage_url"}

type Config struct {
	Name          string `json:"sitename"`
	Title         string `json:"title"`
	Keywords      string `json:"keywords"`
	Description   string `json:"description"`
	Organisation  string `json:"orgname"`
	Email         string `json:"email"`
	QQ            string `json:"kfqq"`
	QQURL         string `json:"kfqq_url"`
	WeWorkURL     string `json:"wework_url"`
	QQGroupURL    string `json:"qqqun"`
	LoginSubtitle string `json:"login_subtitle"`
	Homepage      string `json:"homepage"`
	HomepageURL   string `json:"homepage_url"`
}

func Known(key string) bool {
	for _, k := range OriginalKeys {
		if k == key {
			return true
		}
	}
	return false
}
func Optional(key string) bool { return Known(key) && key != "sitename" && key != "homepage" }

func Validate(key, value string) (bool, error) {
	if !Known(key) {
		return false, nil
	}
	if !utf8.ValidString(value) || len(value) > 1000 || strings.IndexFunc(value, unicode.IsControl) >= 0 || strings.TrimSpace(value) != value {
		return true, ErrInvalid
	}
	if value == "" {
		if Optional(key) {
			return true, nil
		}
		return true, ErrInvalid
	}
	switch key {
	case "sitename":
		if utf8.RuneCountInString(value) > 64 {
			return true, ErrInvalid
		}
	case "homepage":
		if value != "0" && value != "1" && value != "2" && value != "3" {
			return true, ErrInvalid
		}
	case "email":
		a, err := mail.ParseAddress(value)
		if err != nil || a.Address != value || a.Name != "" {
			return true, ErrInvalid
		}
	case "kfqq_url", "wework_url", "qqqun", "homepage_url":
		u, err := url.Parse(value)
		if err != nil || !u.IsAbs() || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" || u.User != nil || strings.ContainsAny(value, "\\\"<>") {
			return true, ErrInvalid
		}
	}
	return true, nil
}

type Reader interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

// Read only the public allowlist; never materialise the rest of the settings
// table in a response or a cache shared with administrator configuration.
func Load(ctx context.Context, q Reader) (Config, error) {
	var out Config
	keys := append(append([]string(nil), OriginalKeys...), "site_name", "support_email")
	rows, err := q.Query(ctx, `SELECT body->>'key',body->'value' FROM backoffice_records WHERE resource='settings' AND body->>'key'=ANY($1)`, keys)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	values := map[string]string{}
	for rows.Next() {
		var key string
		var raw []byte
		if err = rows.Scan(&key, &raw); err != nil {
			return out, err
		}
		if _, exists := values[key]; exists {
			return out, ErrInvalid
		}
		var value string
		if len(raw) == 0 || raw[0] != '"' || json.Unmarshal(raw, &value) != nil {
			return out, ErrInvalid
		}
		validationKey := key
		if key == "site_name" {
			validationKey = "sitename"
		}
		if key == "support_email" {
			validationKey = "email"
		}
		if _, err = Validate(validationKey, value); err != nil {
			return out, err
		}
		values[key] = value
	}
	if err = rows.Err(); err != nil {
		return out, err
	}
	out = Config{Name: values["sitename"], Title: values["title"], Keywords: values["keywords"], Description: values["description"], Organisation: values["orgname"], Email: values["email"], QQ: values["kfqq"], QQURL: values["kfqq_url"], WeWorkURL: values["wework_url"], QQGroupURL: values["qqqun"], LoginSubtitle: values["login_subtitle"], Homepage: values["homepage"], HomepageURL: values["homepage_url"]}
	if out.Name == "" {
		out.Name = values["site_name"]
	}
	if out.Name == "" {
		out.Name = "GOEPAY"
	}
	if out.Title == "" {
		out.Title = out.Name
	}
	if _, exists := values["email"]; !exists {
		out.Email = values["support_email"]
	}
	if out.Homepage == "" {
		out.Homepage = "0"
	}
	if out.Homepage == "2" && out.HomepageURL == "" {
		return out, ErrInvalid
	}
	return out, nil
}
