package main

func mailTestGate195(getenv func(string) string) (bool, error) {
	if getenv == nil {
		return false, errVerificationSMTPConfiguration
	}
	switch getenv("GOEPAY_ENABLE_SMTP_TEST") {
	case "", "false":
		return false, nil
	case "true":
		return true, nil
	default:
		return false, errVerificationSMTPConfiguration
	}
}
