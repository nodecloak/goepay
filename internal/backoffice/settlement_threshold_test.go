package backoffice

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestSettlementThreshold74OriginalSettingsAndGroupFieldsAreRegisteredAndValidated(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	ids := map[string]int64{}
	for _, pair := range [][2]string{{"auto_settle_money", "0"}, {"auto_settle_money", "12.34"}, {"transfer_alipay", "0"}, {"transfer_alipay", "7"}} {
		raw := input(fmt.Sprintf(`{"key":%q,"value":%q}`, pair[0], pair[1]))
		var item Item
		var e error
		if id := ids[pair[0]]; id > 0 {
			item, e = s.Update(ctx, admin, "settings", id, raw)
		} else {
			item, e = s.Create(ctx, admin, "settings", raw)
		}
		if e != nil {
			t.Fatal(pair, e)
		}
		ids[pair[0]] = item.ID
	}
	for _, pair := range [][2]string{{"auto_settle_money", "-1"}, {"auto_settle_money", "1.001"}, {"auto_settle_money", "1000000000.01"}, {"transfer_alipay", "07"}, {"transfer_alipay", "-1"}, {"transfer_alipay", "9223372036854775808"}} {
		if _, e := s.Update(ctx, admin, "settings", ids[pair[0]], input(fmt.Sprintf(`{"key":%q,"value":%q}`, pair[0], pair[1]))); !errors.Is(e, ErrInvalid) {
			t.Fatal("invalid original threshold setting accepted", pair)
		}
	}
	for _, body := range []string{`{"name":"inherit","settle_open":"0","settle_rate":""}`, `{"name":"zero","settle_open":"1","settle_rate":"0"}`, `{"name":"closed","settle_open":"2","settle_rate":"1.25"}`} {
		if _, e := s.Create(ctx, admin, "groups", input(body)); e != nil {
			t.Fatal(body, e)
		}
	}
	for _, body := range []string{`{"name":"invalid","settle_open":"4"}`, `{"name":"invalid","settle_rate":"100.01"}`, `{"name":"invalid","settle_rate":"1.001"}`} {
		if _, e := s.Create(ctx, admin, "groups", input(body)); e == nil {
			t.Fatal("invalid group threshold policy accepted", body)
		}
	}
}
