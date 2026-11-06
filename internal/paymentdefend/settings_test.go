package paymentdefend

import (
	"reflect"
	"testing"
)

func TestSettings206PreservesOriginalTextAndExactDecimal(t *testing.T) {
	s := Defaults()
	if s["pay_verify"] != "0" || s["pay_verify_type"] != "0" || len(s) != 7 {
		t.Fatal("default policy is not explicitly off")
	}
	s["pay_verify"], s["pay_verify_type"] = "3", "3"
	s["pay_verify_check_second"], s["pay_verify_check_count"] = "00060", "0005"
	s["pay_verify_check_ip"], s["pay_verify_check_uid"] = "0002", "0001|9223372036854775807|0001"
	s["pay_verify_check_rate"] = "00099.999999999999999999999999999999999999"
	normal, err := Validate(s)
	if err != nil || !reflect.DeepEqual(normal, s) {
		t.Fatal("original policy text was changed or rejected", err)
	}
	rate, err := ParseRate(normal["pay_verify_check_rate"])
	other, otherErr := ParseRate("100")
	if err != nil || otherErr != nil || rate.Cmp(other) >= 0 {
		t.Fatal("decimal comparison rounded a value below 100 to 100", err, otherErr)
	}
	leading, err := ParseRate("00008")
	eight, otherErr := ParseRate("8")
	if err != nil || otherErr != nil || leading.Cmp(eight) != 0 {
		t.Fatal("leading zeros changed decimal integer interpretation", err, otherErr)
	}
	normal["pay_verify"] = "0"
	if s["pay_verify"] != "3" {
		t.Fatal("validated settings alias caller-owned mutable storage")
	}
}

func TestSettings206RejectsUnsupportedFieldsAndAmbiguousNumberSyntax(t *testing.T) {
	for _, change := range []func(Settings){
		func(s Settings) { s["unexpected"] = "x" },
		func(s Settings) { delete(s, "pay_verify_check_uid") },
		func(s Settings) { s["pay_verify"] = "4" },
		func(s Settings) { s["pay_verify_type"] = "03" },
		func(s Settings) { s["pay_verify_check_ip"] = "127.0.0.1" },
		func(s Settings) { s["pay_verify_check_second"] = "-1" },
		func(s Settings) { s["pay_verify_check_count"] = "1e3" },
		func(s Settings) { s["pay_verify_check_rate"] = "1e2" },
		func(s Settings) { s["pay_verify_check_rate"] = "NaN" },
		func(s Settings) { s["pay_verify_check_uid"] = "1,2" },
		func(s Settings) { s["pay_verify_check_uid"] = "1||2" },
		func(s Settings) { s["pay_verify_check_uid"] = "0" },
		func(s Settings) { s["pay_verify_check_uid"] = "1| 2" },
	} {
		s := Defaults()
		change(s)
		if _, err := Validate(s); err == nil {
			t.Fatal("ambiguous or unsupported policy input was accepted", s)
		}
	}
}
