package channelconfig

// The source plugin prefers APP/mini authorization when 5 or 10 is configured;
// product9 otherwise creates an online authorization QR voucher.
func preauthVoucherOnly(products []string) bool {
	qr := false
	for _, p := range products {
		if p == "5" || p == "10" {
			return false
		}
		if p == "9" {
			qr = true
		}
	}
	return qr
}
