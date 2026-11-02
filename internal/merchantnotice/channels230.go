package merchantnotice

// An absent legacy setting preserves the deployed behavior. Once explicitly
// configured, a channel switch is checked again before an unclaimed send.
func noticeChannels230(values map[string]bool) map[Transport]bool {
	out := map[Transport]bool{}
	for key, kind := range map[string]Transport{"wxnotice": Wechat, "mailnotice": SMTP, "robotnotice": Robot, "smsnotice": SMS} {
		if on, ok := values[key]; ok {
			out[kind] = on
		}
	}
	return out
}
