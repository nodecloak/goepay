package server

import (
	"goepay/internal/identity"
	"testing"
)

func TestParentChannel180WeixinCatalogAndBindingsRequireCurrentOriginalSuper(t *testing.T) {
	for _, scenario := range []string{"operator", "expired_session", "disabled_admin"} {
		t.Run(scenario, func(t *testing.T) {
			setup, _, _, _, _ := binding180Ready(t)
			f := setup.f
			switch scenario {
			case "operator":
				if _, err := f.p.Exec(t.Context(), `UPDATE console_users SET admin_class='operator' WHERE username='admin168'`); err != nil {
					t.Fatal(err)
				}
				var version int64
				if err := f.p.QueryRow(t.Context(), `SELECT auth_version FROM console_users WHERE username='admin168'`).Scan(&version); err != nil {
					t.Fatal(err)
				}
				var err error
				f.token, f.csrf, err = f.s.Sessions.Create(identity.User{ID: 168, Role: "admin", Username: "admin168", AdminClass: "operator", AuthVersion: version})
				if err != nil {
					t.Fatal(err)
				}
			case "expired_session":
				if _, err := f.p.Exec(t.Context(), `UPDATE console_sessions SET expires_at=clock_timestamp()-interval '1 second'`); err != nil {
					t.Fatal(err)
				}
			case "disabled_admin":
				if _, err := f.p.Exec(t.Context(), `UPDATE console_users SET enabled=false WHERE username='admin168'`); err != nil {
					t.Fatal(err)
				}
			}
			for _, path := range []string{"/8/weixin-options", "/" + setup.id + "/weixin-options"} {
				w := parent180HTTP(f, "GET", path, "")
				if w.Code != 401 && w.Code != 403 {
					t.Fatal("non-super reached original catalog", scenario, w.Code)
				}
			}
			w := parent180HTTP(f, "POST", "/bind-weixin", `{"type":0,"weixin":"0","items":[]}`)
			if w.Code != 401 && w.Code != 403 {
				t.Fatal("non-super reached binding write", scenario, w.Code)
			}
			if setup.creates.Load() != 0 {
				t.Fatal("permission probe sent a payment")
			}
		})
	}
}
