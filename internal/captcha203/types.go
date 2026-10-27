package captcha203

import (
	"context"
	"errors"
)

var ErrUnavailable = errors.New("CAPTCHA verification unavailable")
var ErrInvalid = errors.New("invalid CAPTCHA configuration or proof")
var ErrConflict = errors.New("CAPTCHA configuration changed")

type Config struct {
	Version int
	ID, Key string
}
type Challenge struct {
	Version      int    `json:"version"`
	GT           string `json:"gt"`
	Challenge    string `json:"challenge"`
	NewCaptcha   bool   `json:"new_captcha"`
	Success      int    `json:"success"`
	CaptchaToken string `json:"captcha_token,omitempty"`
}
type Proof struct {
	CaptchaToken     string `json:"captcha_token,omitempty"`
	GeetestChallenge string `json:"geetest_challenge,omitempty"`
	GeetestValidate  string `json:"geetest_validate,omitempty"`
	GeetestSeccode   string `json:"geetest_seccode,omitempty"`
	CaptchaID        string `json:"captcha_id,omitempty"`
	LotNumber        string `json:"lot_number,omitempty"`
	PassToken        string `json:"pass_token,omitempty"`
	GenTime          string `json:"gen_time,omitempty"`
	CaptchaOutput    string `json:"captcha_output,omitempty"`
}
type Verifier interface {
	Init(context.Context, Config, string, string) (Challenge, error)
	Verify(context.Context, Config, Challenge, Proof, string) error
}
