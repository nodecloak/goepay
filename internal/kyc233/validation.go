package kyc233

import (
	"bytes"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"regexp"
	"strings"
	"time"
	"unicode"
)

var tokenID = regexp.MustCompile(`^[0-9a-f]{32}$`)
var otherDocument = regexp.MustCompile(`^[A-Z0-9()]{5,32}$`)
var companyNumber = regexp.MustCompile(`^[0-9A-Z]{18}$`)

func validName(v string, max int) bool {
	if strings.TrimSpace(v) != v || len([]rune(v)) < 2 || len([]rune(v)) > max {
		return false
	}
	for _, c := range v {
		if unicode.IsControl(c) || c == '<' || c == '>' {
			return false
		}
	}
	return true
}
func validID(v string) bool {
	if len(v) != 18 {
		return false
	}
	w := []int{7, 9, 10, 5, 8, 4, 2, 1, 6, 3, 7, 9, 10, 5, 8, 4, 2}
	sum := 0
	for i := 0; i < 17; i++ {
		if v[i] < '0' || v[i] > '9' {
			return false
		}
		sum += int(v[i]-'0') * w[i]
	}
	if "10X98765432"[sum%11] != v[17] || v[:6] == "000000" {
		return false
	}
	d, e := time.Parse("20060102", v[6:14])
	return e == nil && !d.After(time.Now()) && d.Year() >= 1850
}
func validSubmission(v Submission, mode int, corp bool) bool {
	if !v.Consent || (v.CertType != 0 && v.CertType != 1) || !validName(v.Name, 64) || v.CardType < 0 || v.CardType > 5 {
		return false
	}
	if v.CardType != 0 && mode != 1 {
		return false
	}
	if (v.CardType == 0 && !validID(v.Number)) || (v.CardType != 0 && !otherDocument.MatchString(v.Number)) {
		return false
	}
	if v.CertType == 1 {
		return corp && v.CardType == 0 && validName(v.CompanyName, 128) && companyNumber.MatchString(v.CompanyNumber)
	}
	return v.CompanyName == "" && v.CompanyNumber == ""
}
func validDocument(v string) bool {
	switch v {
	case "idcard", "idcard_back", "passport", "bank_card", "business_license", "bank_account_license":
		return true
	}
	return false
}

const MaxImageBytes = 4 << 20

func validateImage(raw []byte) (string, error) {
	if len(raw) == 0 || len(raw) > MaxImageBytes {
		return "", ErrInvalid
	}
	c, format, e := image.DecodeConfig(bytes.NewReader(raw))
	if e != nil || (format != "jpeg" && format != "png") || c.Width < 15 || c.Height < 15 || c.Width > 8192 || c.Height > 8192 || int64(c.Width)*int64(c.Height) > 32000000 {
		return "", ErrInvalid
	}
	// Decode the full payload so truncated/broken images never reach a billable provider.
	if _, _, e = image.Decode(bytes.NewReader(raw)); e != nil {
		return "", ErrInvalid
	}
	return format, nil
}
