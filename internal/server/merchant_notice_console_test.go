package server

import (
	"net/http/httptest"
	"strings"
	"testing"

	"goepay/internal/identity"
)

const noticeBody80 = `{"expected_profile_id":"0","msgconfig":{"order":2,"settle":0,"login":0,"complain":0,"mchrisk":0,"balance":0,"group":0,"domain_audit":0,"daily_report":4,"order_money":"10.00","balance_money":"20.00"},"voice_order":false,"targets":{},"clear_fields":[]}`

func TestMerchantNotice80BodyPreservesProMoneyAndRejectsAmbiguousChanges(t *testing.T) {
	in, expected, err := parseMerchantNoticeBody([]byte(noticeBody80))
	if err != nil || expected != 0 || in.Preferences.Order != 2 || in.Preferences.DailyReport != 4 || in.Preferences.OrderMoney != 1000 || in.Preferences.BalanceMoney != 2000 {
		t.Fatalf("original fields not parsed: %+v %d %v", in, expected, err)
	}
	for _, raw := range []string{
		strings.Replace(noticeBody80, `"0"`, `0`, 1),
		strings.Replace(noticeBody80, `"0"`, `"00"`, 1),
		strings.Replace(noticeBody80, `"order":2`, `"order":2,"order":3`, 1),
		strings.Replace(noticeBody80, `"voice_order":false`, `"voice_order":null`, 1),
		strings.Replace(noticeBody80, `"targets":{}`, `"targets":{"merchant_id":"1001"}`, 1),
		strings.Replace(noticeBody80, `"targets":{}`, `"targets":{"qqbot_appsecret":null}`, 1),
		strings.Replace(noticeBody80, `"clear_fields":[]`, `"clear_fields":null`, 1),
		strings.Replace(noticeBody80, `"10.00"`, `10.00`, 1),
		strings.Replace(noticeBody80, `"10.00"`, `"0.001"`, 1),
		strings.Replace(noticeBody80, `"order":2`, `"order":7`, 1),
		strings.Replace(noticeBody80, `"balance_money":"20.00"`, `"balance_money":"20.00","evil":1`, 1),
		strings.Replace(noticeBody80, `"voice_order":false,`, ``, 1),
		noticeBody80 + `{}`,
	} {
		if _, _, err := parseMerchantNoticeBody([]byte(raw)); err == nil {
			t.Errorf("ambiguous body accepted: %s", raw)
		}
	}
}

func TestMerchantNotice80ConsoleChecksAuthorityBeforeServiceAndRejectsTenantOverrides(t *testing.T) {
	for _, tc := range []struct {
		role, class, path, method, query, csrf string
		delegated                              bool
		want                                   int
	}{
		{"merchant", "", "account/notice", "GET", "", "", false, 503},
		{"merchant", "", "account/notice/events", "GET", "", "", false, 503},
		{"admin", "operator", "merchants/1000/notice/events", "GET", "?limit=20&before_id=0", "", false, 503},
		{"merchant", "", "account/notice/events", "GET", "?merchant_id=1001", "", false, 400},
		{"merchant", "", "account/notice/events", "GET", "?limit=20&limit=30", "", false, 400},
		{"merchant", "", "account/notice/events", "GET", "?before_id=00", "", false, 400},
		{"merchant", "", "account/notice/events", "GET", "?limit=101", "", false, 400},
		{"merchant", "", "account/notice/events", "PUT", "", "csrf", false, 405},
		{"merchant", "", "account/notice", "GET", "?merchant_id=1001", "", false, 400},
		{"merchant", "", "account/notice", "PUT", "", "", false, 403},
		{"merchant", "", "account/notice", "GET", "", "", true, 403},
		{"merchant", "", "merchants/1001/notice", "GET", "", "", false, 403},
		{"admin", "super", "account/notice", "GET", "", "", false, 403},
		{"admin", "super", "merchants/1000/notice", "GET", "", "", false, 503},
		{"admin", "operator", "merchants/1000/notice", "GET", "", "", false, 503},
		{"admin", "auditor", "merchants/1000/notice", "PUT", "", "csrf", false, 403},
		{"admin", "super", "merchants/01000/notice", "GET", "", "", false, 400},
		{"admin", "super", "merchants/1000/notice", "GET", "", "", true, 403},
		{"merchant", "", "account/notice", "POST", "", "csrf", false, 405},
	} {
		t.Run(tc.role+tc.class+tc.path+tc.method+tc.query, func(t *testing.T) {
			session := identity.Session{User: identity.User{ID: 1000, Username: "notice-owner80", Role: tc.role, AdminClass: tc.class}, CSRF: "csrf"}
			if tc.delegated {
				session.Issuer = &identity.SessionIssuer{ID: 1}
			}
			req := httptest.NewRequest(tc.method, "/console/v1/"+tc.role+"/"+tc.path+tc.query, strings.NewReader(noticeBody80))
			req.Header.Set("X-CSRF-Token", tc.csrf)
			out := httptest.NewRecorder()
			if !(&Server{}).merchantNoticeConsole(out, req, tc.role, session, append([]string{tc.role}, strings.Split(tc.path, "/")...)) || out.Code != tc.want {
				t.Fatalf("authority/status mismatch: got %d want %d %s", out.Code, tc.want, out.Body.String())
			}
		})
	}
}
