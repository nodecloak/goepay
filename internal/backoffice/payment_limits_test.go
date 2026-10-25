package backoffice

import (
	"context"
	"fmt"
	"testing"
)

func TestOriginalGroupPaymentLimitsSchemaAndValidation(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	schema, e := s.Schema(admin, "groups")
	if e != nil {
		t.Fatal(e)
	}
	fields := map[string]Field{}
	for _, f := range schema.Fields {
		fields[f.Name] = f
	}
	ids := map[string]int64{}
	for _, key := range []string{"pay_minmoney", "pay_maxmoney"} {
		if _, ok := fields[key]; !ok {
			t.Fatalf("original field %s missing", key)
		}
	}
	for i, body := range []string{`{"pay_minmoney":"","pay_maxmoney":""}`, `{"pay_minmoney":"0","pay_maxmoney":"0"}`, `{"pay_minmoney":"1.23","pay_maxmoney":"90071992547409.93"}`} {
		raw := fmt.Sprintf(`{"name":"limits-%d",%s}`, i, body[1:len(body)-1])
		if _, e = s.Create(ctx, admin, "groups", input(raw)); e != nil {
			t.Fatal(raw, e)
		}
	}
	for _, body := range []string{`{"pay_minmoney":"2","pay_maxmoney":"1"}`, `{"pay_minmoney":"-1"}`, `{"pay_maxmoney":"1e2"}`, `{"pay_maxmoney":"0.001"}`, `{"pay_maxmoney":"92233720368547758.08"}`} {
		raw := fmt.Sprintf(`{"name":"invalid",%s}`, body[1:len(body)-1])
		if _, e = s.Create(ctx, admin, "groups", input(raw)); e == nil {
			t.Fatal("invalid limits accepted", body)
		}
	}
	for _, key := range []string{"pay_minmoney", "pay_maxmoney"} {
		item, e := s.Create(ctx, admin, "settings", input(fmt.Sprintf(`{"key":%q,"value":"1.23"}`, key)))
		if e != nil {
			t.Fatal(key, e)
		}
		ids[key] = item.ID
		if _, e = s.Update(ctx, admin, "settings", item.ID, input(fmt.Sprintf(`{"key":%q,"value":"-1"}`, key))); e == nil {
			t.Fatal("negative original global limit accepted")
		}
	}
	if _, e = s.Update(ctx, admin, "settings", ids["pay_maxmoney"], input(`{"key":"pay_maxmoney","value":"1.00"}`)); e == nil {
		t.Fatal("contradictory global bounds were saved")
	}
	for _, pair := range [][2]string{{"pay_minmoney", "1.00"}, {"pay_maxmoney", "3.00"}} {
		if _, e = s.Update(ctx, admin, "settings", ids[pair[0]], input(fmt.Sprintf(`{"key":%q,"value":%q}`, pair[0], pair[1]))); e != nil {
			t.Fatal(e)
		}
	}
	start, results := make(chan struct{}), make(chan error, 2)
	for _, pair := range [][2]string{{"pay_minmoney", "2.00"}, {"pay_maxmoney", "1.50"}} {
		go func(pair [2]string) {
			<-start
			_, err := s.Update(ctx, admin, "settings", ids[pair[0]], input(fmt.Sprintf(`{"key":%q,"value":%q}`, pair[0], pair[1])))
			results <- err
		}(pair)
	}
	close(start)
	accepted := 0
	for i := 0; i < 2; i++ {
		if <-results == nil {
			accepted++
		}
	}
	if accepted != 1 {
		t.Fatalf("concurrent contradictory bounds: accepted %d writes", accepted)
	}
}
