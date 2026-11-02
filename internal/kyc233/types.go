// Package kyc233 owns real-name certification and OCR configuration and state.
package kyc233

import (
	"context"
	"errors"
	"goepay/internal/channelconfig"
	"goepay/internal/identity"
)

var (
	ErrInvalid      = errors.New("invalid certification request")
	ErrConflict     = errors.New("certification state changed")
	ErrUnavailable  = errors.New("certification provider unavailable")
	ErrUnknown      = errors.New("certification result unknown")
	ErrInsufficient = errors.New("insufficient certification balance")
)

type Actor struct {
	User        identity.User
	Token, CSRF string
	Delegated   bool
}

// Credentials and Snapshot never form public HTTP responses.
type Credentials struct {
	AppCode        string `json:"cert_appcode"`
	CompanyAppCode string `json:"cert_appcode2"`
	TencentID      string `json:"cert_qcloudid"`
	TencentKey     string `json:"cert_qcloudkey"`
	TencentRuleID  string `json:"cert_qcloudruleid"`
	AliyunID       string `json:"cert_aliyunid"`
	AliyunKey      string `json:"cert_aliyunkey"`
	AliyunScene    string `json:"cert_aliyunsceneid"`
	OCRAliyunID    string `json:"ocr_aliyunid"`
	OCRAliyunKey   string `json:"ocr_aliyunkey"`
	OCRBaiduID     string `json:"ocr_baiduid"`
	OCRBaiduKey    string `json:"ocr_baidukey"`
}

func (Credentials) String() string   { return "kyc233.Credentials(redacted)" }
func (Credentials) GoString() string { return "kyc233.Credentials(redacted)" }

type Snapshot struct {
	Revision    int64                  `json:"revision"`
	Mode        int                    `json:"cert_open"`
	ChannelID   int64                  `json:"cert_channel"`
	Corporate   bool                   `json:"cert_corpopen"`
	Force       bool                   `json:"cert_force"`
	Fee         int64                  `json:"fee_cents"`
	OCRType     string                 `json:"ocr_type"`
	Credentials Credentials            `json:"credentials"`
	Alipay      *channelconfig.Channel `json:"alipay,omitempty"`
}

func (Snapshot) String() string   { return "kyc233.Snapshot(redacted)" }
func (Snapshot) GoString() string { return "kyc233.Snapshot(redacted)" }

type Submission struct {
	ExpectedRevision int64  `json:"expected_revision,string"`
	CertType         int    `json:"certtype"`
	CardType         int    `json:"certcardtype"`
	Name             string `json:"certname"`
	Number           string `json:"certno"`
	CompanyName      string `json:"certcorpname"`
	CompanyNumber    string `json:"certcorpno"`
	Consent          bool   `json:"consent"`
	Phone            string `json:"phone"`
}

func (Submission) String() string   { return "kyc233.Submission(redacted)" }
func (Submission) GoString() string { return "kyc233.Submission(redacted)" }

// Attempt is an immutable private provider request, not a browser response.
type Attempt struct {
	ID          string `json:"id"`
	Token       string `json:"token"`
	State       string `json:"state"`
	CallbackURL string `json:"callback_url"`
	URL         string `json:"url"`
	AuthCode    string `json:"auth_code,omitempty"`
}

func (Attempt) String() string   { return "kyc233.Attempt(redacted)" }
func (Attempt) GoString() string { return "kyc233.Attempt(redacted)" }

type InitResult struct {
	Token  string
	URL    string
	Scan   string
	Status string // pending, passed or rejected; passed only from provider proof.
}
type QueryResult struct{ Status string }
type OCRResult struct {
	Fields map[string]string `json:"fields"`
}

type Provider interface {
	Initialize(context.Context, Snapshot, Submission, Attempt) (InitResult, error)
	Query(context.Context, Snapshot, Attempt) (QueryResult, error)
	Recognize(context.Context, Snapshot, string, []byte) (OCRResult, error)
}

type CredentialEdit struct {
	Action string `json:"action"` // keep, replace, clear
	Value  string `json:"value"`
}
type ConfigInput struct {
	ExpectedRevision int64                     `json:"expected_revision,string"`
	Mode             int                       `json:"cert_open"`
	ChannelID        string                    `json:"cert_channel"`
	Corporate        bool                      `json:"cert_corpopen"`
	Force            bool                      `json:"cert_force"`
	Money            string                    `json:"cert_money"`
	OCRType          string                    `json:"ocr_type"`
	Credentials      map[string]CredentialEdit `json:"credentials"`
}
type CredentialView struct {
	Present bool   `json:"present"`
	Masked  string `json:"masked"`
}
type ChannelView struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
type ConfigView struct {
	Revision    int64                     `json:"revision,string"`
	Mode        int                       `json:"cert_open"`
	ChannelID   string                    `json:"cert_channel"`
	Corporate   bool                      `json:"cert_corpopen"`
	Force       bool                      `json:"cert_force"`
	Money       string                    `json:"cert_money"`
	OCRType     string                    `json:"ocr_type"`
	Credentials map[string]CredentialView `json:"credentials"`
	Channels    []ChannelView             `json:"channels"`
}
type Info struct {
	AuthCodeReady        bool   `json:"auth_code_ready"`
	AuthCodeUsed         bool   `json:"auth_code_used"`
	QueryAfter           string `json:"query_after"`
	Revision             int64  `json:"revision,string"`
	OCRConfigRevision    int64  `json:"ocr_config_revision,string"`
	ConfigurationChanged bool   `json:"configuration_changed"`
	Cert                 int    `json:"cert"`
	CertType             int    `json:"certtype"`
	CardType             int    `json:"certcardtype"`
	Method               string `json:"certmethod_text"`
	TypeText             string `json:"certtype_text"`
	Name                 string `json:"certname"`
	Number               string `json:"certno"`
	CompanyName          string `json:"certcorpname"`
	CompanyNumber        string `json:"certcorpno"`
	Time                 string `json:"certtime"`
	Mode                 int    `json:"cert_open"`
	Corporate            bool   `json:"cert_corpopen"`
	Money                string `json:"cert_money"`
	Phone                string `json:"phone_masked"`
	OCRType              string `json:"ocr_type"`
	AttemptID            string `json:"attempt_id"`
	Status               string `json:"status"` // none, ready, initializing, pending, unknown, verified (awaiting fee), rejected, passed, expired, cancelled
	Scan                 string `json:"scan"`
	URL                  string `json:"url"`
	ExpiresAt            string `json:"expires_at"`
}
