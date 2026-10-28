package paymentdefend206

import "strings"

// Trigger is the original checkPayVerifyOpen decision for one merchant.
// Known=false means the decision depends on rules not implemented here
// (pay_verify=1 conditional windows); callers must fail closed.
type Trigger struct {
	Triggered bool
	Known     bool
}

// UIDListed matches PHP in_array($pid, explode('|', $uid)) for validated
// settings: entries are positive decimal integers, compared numerically.
func UIDListed(raw string, merchantID int64) bool {
	if raw == "" || merchantID <= 0 {
		return false
	}
	for _, entry := range strings.Split(raw, "|") {
		if n, ok := parsePositive(entry); ok && n == merchantID {
			return true
		}
	}
	return false
}

func parsePositive(s string) (int64, bool) {
	if s == "" || len(s) > 19 {
		return 0, false
	}
	var n int64
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, false
		}
		d := int64(c - '0')
		if n > (1<<63-1-d)/10 {
			return 0, false
		}
		n = n*10 + d
	}
	return n, n > 0
}

// EvaluateStatic decides modes 0, 2 and 3 and the uid pre-filter of mode 1.
// mode is the effective pay_verify (global or a group override).
func EvaluateStatic(mode string, s Settings, merchantID int64) Trigger {
	switch mode {
	case "0":
		return Trigger{Known: true}
	case "3":
		return Trigger{Triggered: true, Known: true}
	case "2":
		return Trigger{Triggered: UIDListed(s["pay_verify_check_uid"], merchantID), Known: true}
	case "1":
		if s["pay_verify_check_uid"] != "" && !UIDListed(s["pay_verify_check_uid"], merchantID) {
			return Trigger{Known: true}
		}
		return Trigger{} // conditional rules: decided by the windowed evaluator
	}
	return Trigger{}
}
