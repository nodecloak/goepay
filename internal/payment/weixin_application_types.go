package payment

import (
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

type WeixinApplicationSpec struct {
	ID              int64   `json:"id,string"`
	Revision        int64   `json:"revision,string"`
	Name            string  `json:"name"`
	Kind            string  `json:"kind"`
	AppID           string  `json:"app_id"`
	AppSecret       string  `json:"-"`
	Fingerprint     string  `json:"-"`
	Enabled         bool    `json:"enabled"`
	Archived        bool    `json:"archived"`
	SourceChannelID int64   `json:"source_channel_id,string"`
	MerchantIDs     []int64 `json:"-"`
	Path            string  `json:"path"`
}

type WeixinApplicationView struct {
	ID               int64    `json:"id,string"`
	Revision         int64    `json:"revision,string"`
	Name             string   `json:"name"`
	Kind             string   `json:"kind"`
	AppID            string   `json:"app_id"`
	Enabled          bool     `json:"enabled"`
	Archived         bool     `json:"archived"`
	SourceChannelID  int64    `json:"source_channel_id,string"`
	MerchantIDs      []string `json:"merchant_ids"`
	Path             string   `json:"path"`
	Fingerprint      string   `json:"fingerprint"`
	SecretConfigured bool     `json:"secret_configured"`
	Draft            bool     `json:"draft"`
	ExecutionReady   bool     `json:"execution_ready"`
	Origin           string   `json:"origin"`
	ReadOnly         bool     `json:"read_only"`
}

type WeixinApplicationInput struct {
	RequestID                 string  `json:"request_id"`
	ExpectedRevision          int64   `json:"expected_revision,string"`
	ExpectedFingerprint       string  `json:"expected_fingerprint"`
	ExpectedSourceFingerprint string  `json:"expected_source_fingerprint"`
	Name                      string  `json:"name"`
	Kind                      string  `json:"kind"`
	AppID                     string  `json:"app_id"`
	AppSecret                 string  `json:"-"`
	Enabled                   bool    `json:"enabled"`
	SourceChannelID           int64   `json:"source_channel_id,string"`
	MerchantIDs               []int64 `json:"-"`
	Path                      string  `json:"path"`
}

// Trusted process-local capabilities, never decoded from a request. Callbacks
// retain the root's current authorization and source identity after waits.
type WeixinApplicationScope struct {
	ReservedIDs        map[int64]bool
	SourceFingerprints map[int64]string
	Allows             func(merchant, source int64) bool
	CurrentSource      func(source int64) string
	ValidateProfiles   func([]WeixinApplicationSpec) error
}

func (p WeixinApplicationSpec) ExecutionReady() bool { return p.Enabled && !p.Archived && !p.Draft() }
func (p WeixinApplicationSpec) Draft() bool {
	return p.SourceChannelID <= 0 || len(p.MerchantIDs) == 0 || p.Kind == "mini_program" && p.Path == ""
}

var weixinApplicationIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

func validWeixinApplicationSecret(value string) bool {
	return len(value) > 0 && len(value) <= 512 && !strings.HasPrefix(value, "@") && utf8.ValidString(value) && !strings.ContainsAny(value, "\x00\r\n") && strings.IndexFunc(value, unicode.IsSpace) < 0
}

func validateWeixinApplication(p WeixinApplicationSpec) error {
	if p.ID <= 0 || p.Revision <= 0 || len(p.Name) == 0 || len(p.Name) > 100 || strings.TrimSpace(p.Name) != p.Name || !utf8.ValidString(p.Name) || strings.ContainsAny(p.Name, "\x00\r\n") || !weixinApplicationIDPattern.MatchString(p.AppID) || !validWeixinApplicationSecret(p.AppSecret) || p.SourceChannelID < 0 || len(p.MerchantIDs) > 1000 || p.Kind != "official_account" && p.Kind != "mini_program" {
		return ErrInvalid
	}
	if p.Kind == "official_account" && p.Path != "" || len(p.Path) > 256 || p.SourceChannelID == 0 && len(p.MerchantIDs) > 0 {
		return ErrInvalid
	}
	if p.Path != "" {
		for _, part := range strings.Split(p.Path, "/") {
			if part == "" || part == "." || part == ".." {
				return ErrInvalid
			}
			for _, c := range part {
				if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("_.-", c)) {
					return ErrInvalid
				}
			}
		}
	}
	last := int64(0)
	for _, id := range p.MerchantIDs {
		if id <= last {
			return ErrInvalid
		}
		last = id
	}
	return nil
}

func canonicalWeixinApplicationMerchants(ids []int64) []int64 {
	out := append([]int64{}, ids...)
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
