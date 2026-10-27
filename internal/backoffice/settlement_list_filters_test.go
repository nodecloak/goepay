package backoffice

import (
	"net/http/httptest"
	"testing"
	"time"
)

func TestOriginalSettlementFilterBeijingCalendarBoundaries(t *testing.T) {
	f, e := parseSettlementListFilter(httptest.NewRequest("GET", "/?starttime=2026-10-01&endtime=2026-10-01&dstatus=0", nil), Principal{Role: "admin", SubjectID: 1})
	if e != nil {
		t.Fatal(e)
	}
	if f.From.UTC().Format(time.RFC3339) != "2026-09-30T16:00:00Z" || f.Until.UTC().Format(time.RFC3339) != "2026-10-01T16:00:00Z" || f.State != 0 {
		t.Fatal("wrong inclusive Beijing calendar boundaries", f)
	}
	for _, q := range []string{"from=1969-12-31&to=1970-01-01", "from=2026-02-29&to=2026-03-01", "from=2020-01-01&to=2026-01-01", "uid=1&merchant_id=1"} {
		if _, e := parseSettlementListFilter(httptest.NewRequest("GET", "/?"+q, nil), Principal{Role: "admin", SubjectID: 1}); e == nil {
			t.Fatal("invalid date or ambiguous alias accepted", q)
		}
	}
}
