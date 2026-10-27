package backoffice

import (
	"net/url"
	"testing"
)

func profile198Directory(t *testing.T) *Service {
	t.Helper()
	s := setup(t)
	if _, e := s.pool.Exec(t.Context(), `INSERT INTO merchants(id,name,api_key,created_at,registration_time_known,profile) SELECT 301,'Registration198',api_key,'2020-01-02T03:04:05Z',true,'{}' FROM merchants WHERE id=101; INSERT INTO merchants(id,name,api_key,created_at,registration_time_known,profile) SELECT 302,'Registration198',api_key,'2020-01-02T03:04:05Z',false,'{"legacy_source":"epay_pro","legacy_export":"offline198"}' FROM merchants WHERE id=101`); e != nil {
		t.Fatal(e)
	}
	return s
}
func TestProfile198AdminDirectoryDoesNotDisplayImportClockAsRegistration(t *testing.T) {
	s := profile198Directory(t)
	item, e := s.Get(t.Context(), admin, "merchants", 302)
	if e != nil {
		t.Fatal(e)
	}
	if item.Fields["created_at"] != "" || item.Fields["registration_time_known"] != false {
		t.Fatal("admin directory invented original registration timestamp", item.Fields["created_at"], item.Fields["registration_time_known"])
	}
}
func TestProfile198RegistrationDateFilterExcludesUnrecordedOriginalTimes(t *testing.T) {
	s := profile198Directory(t)
	values, _ := url.ParseQuery("column=name&q=Registration198&from=2020-01-02&to=2020-01-02")
	filter, search, e := ParseMerchantDirectoryQuery(values)
	if e != nil {
		t.Fatal(e)
	}
	page, e := s.List(t.Context(), admin, "merchants", Query{Page: 1, Limit: 20, Search: search, Merchant: filter})
	if e != nil || page.Total != 1 || len(page.Items) != 1 || page.Items[0].ID != 301 {
		t.Fatal("registration filter matched the unproven import clock", page.Total, e)
	}
}
