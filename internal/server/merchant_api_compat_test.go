package server

import "testing"

// A-03: out_trade_no must match the original Pay.php charset ^[a-zA-Z0-9.\_\-|]+$.
func TestOutTradeNoPatternMatchesOriginalCharset(t *testing.T) {
	ok := []string{"ok_123.1-2|3", "ABCabc099", "a", "a.b_c-d|e"}
	bad := []string{"", "a b", "a/b", "a#b", "订单", "a@b", "a+b"}
	for _, s := range ok {
		if !outTradeNoPattern.MatchString(s) {
			t.Fatalf("original-valid out_trade_no rejected: %q", s)
		}
	}
	for _, s := range bad {
		if outTradeNoPattern.MatchString(s) {
			t.Fatalf("original-invalid out_trade_no accepted: %q", s)
		}
	}
}

// A-05: name over 127 bytes is truncated at a UTF-8 boundary (mb_strcut), never split.
func TestTruncateUTF8BytesMatchesMbStrcut(t *testing.T) {
	if got := truncateUTF8Bytes("short", 127); got != "short" {
		t.Fatalf("short name altered: %q", got)
	}
	ascii := make([]byte, 200)
	for i := range ascii {
		ascii[i] = 'a'
	}
	if got := truncateUTF8Bytes(string(ascii), 127); len(got) != 127 {
		t.Fatalf("ascii name not truncated to 127 bytes: len=%d", len(got))
	}
	// 42 three-byte runes = 126 bytes; a 43rd rune would cross 127 and must be
	// dropped whole rather than split mid-rune.
	runes := ""
	for i := 0; i < 60; i++ {
		runes += "中"
	}
	got := truncateUTF8Bytes(runes, 127)
	if len(got) != 126 {
		t.Fatalf("multi-byte truncation split a rune or wrong length: len=%d", len(got))
	}
	for _, r := range got {
		if r == '\uFFFD' {
			t.Fatal("truncation produced a replacement char (split rune)")
		}
	}
}
