package server

import (
	"net/http/httptest"
	"strings"
	"testing"

	"goepay/internal/identity"
)

func TestAdminSharingOverviewConsoleBoundaries(t *testing.T) {
	super := identity.User{Role: "admin", ID: 1, Username: "root", AdminClass: "super"}
	cases := []struct {
		name, role, method, path, query string
		user                            identity.User
		want                            int
	}{
		{"merchant orders", "merchant", "GET", "order-overview", "", identity.User{Role: "merchant", ID: 751, Username: "merchant"}, 403},
		{"merchant rules", "merchant", "GET", "rule-overview", "", identity.User{Role: "merchant", ID: 751, Username: "merchant"}, 403},
		{"no writes", "admin", "POST", "order-overview", "", super, 405},
		{"no rule writes", "admin", "DELETE", "rule-overview", "", super, 405},
		{"duplicate owner", "admin", "GET", "order-overview", "?merchant_id=1&merchant_id=2", super, 400},
		{"empty owner is not all", "admin", "GET", "order-overview", "?merchant_id=", super, 400},
		{"noncanonical owner", "admin", "GET", "rule-overview", "?merchant_id=01", super, 400},
		{"oversized owner", "admin", "GET", "order-overview", "?merchant_id=9223372036854775808", super, 400},
		{"unknown filter", "admin", "GET", "order-overview", "?unexpected=true", super, 400},
		{"invalid status", "admin", "GET", "rule-overview", "?enabled=0", super, 400},
		{"bad amount", "admin", "GET", "order-overview", "?field=share_amount&keyword=not-money", super, 400},
		{"reversed dates", "admin", "GET", "order-overview", "?from=2026-11-02&to=2026-11-01", super, 400},
		{"bad child", "admin", "GET", "order-overview", "?subchannel_id=0", super, 400},
		{"bad limit", "admin", "GET", "rule-overview", "?limit=101", super, 400},
		{"empty rule", "admin", "GET", "order-overview", "?rule_id=", super, 400},
		{"empty limit", "admin", "GET", "order-overview", "?limit=", super, 400},
		{"statistics has no paging", "admin", "GET", "order-overview/statistics", "?limit=20", super, 400},
		{"malformed query", "admin", "GET", "order-overview", "?merchant_id=%GG", super, 400},
		{"unknown suffix", "admin", "GET", "order-overview/unexpected", "", super, 404},
		{"partial status", "admin", "GET", "order-overview", "?status=partial", super, 503},
		{"unknown status", "admin", "GET", "order-overview", "?status=unknown", super, 503},
		{"global orders", "admin", "GET", "order-overview", "", super, 503},
		{"global rules", "admin", "GET", "rule-overview", "", super, 503},
		{"statistics", "admin", "GET", "order-overview/statistics", "?merchant_id=9007199254740993", super, 503},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := &Server{}
			req := httptest.NewRequest(tc.method, "/console/v1/"+tc.role+"/channel-sharing/"+tc.path+tc.query, nil)
			rec := httptest.NewRecorder()
			parts := append([]string{tc.role, "channel-sharing"}, strings.Split(tc.path, "/")...)
			if !s.channelSharingConsole(rec, req, tc.role, identity.Session{User: tc.user}, parts) || rec.Code != tc.want {
				t.Fatalf("want %d got %d: %s", tc.want, rec.Code, rec.Body.String())
			}
		})
	}
	for _, tc := range []struct {
		grant, path string
		want        bool
	}{{"PsReceiver", "rule-overview", true}, {"PsReceiver", "order-overview", false}, {"PsOrder", "order-overview", true}, {"PsOrder", "rule-overview", false}} {
		user := adminUser("operator", []string{tc.grant})
		if got := adminRequestAllowed(user, []string{"admin", "channel-sharing", tc.path}); got != tc.want {
			t.Fatalf("%s access %s = %v", tc.grant, tc.path, got)
		}
	}
}

func TestSharingOrderDateIncludesFinalFractionalSecond(t *testing.T) {
	q, ok := sharingOrderQueryFromRequest(httptest.NewRequest("GET", "/?from=2026-11-01&to=2026-11-01", nil), 751)
	if !ok || q.To.Sub(q.From).Nanoseconds() != 24*60*60*1e9-1 {
		t.Fatalf("calendar day lost final fractional second: %+v", q)
	}
}
