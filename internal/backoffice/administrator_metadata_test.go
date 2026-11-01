package backoffice

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestAdministratorDirectoryMetadataAndExactIDSearch(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	root := Principal{Role: "admin", SubjectID: 1, Username: "root", AdminClass: "super"}
	created, err := s.Create(ctx, root, "admin-users", input(`{"username":"metadata-admin","display_name":"Metadata","password":"synthetic-test-password","admin_class":"operator"}`))
	if err != nil {
		t.Fatal(err)
	}
	created, err = s.Get(ctx, root, "admin-users", created.ID)
	if err != nil {
		t.Fatal(err)
	}
	var recorded time.Time
	if err = s.pool.QueryRow(ctx, `SELECT created_at FROM backoffice_audit WHERE resource='admin-users' AND resource_id=$1 AND operation='create'`, created.ID).Scan(&recorded); err != nil {
		t.Fatal(err)
	}
	actual, ok := created.Fields["created_at"].(string)
	if !ok {
		t.Fatalf("created time unavailable: %+v", created.Fields)
	}
	stamp, err := time.Parse(time.RFC3339Nano, actual)
	if err != nil || !stamp.Equal(recorded) {
		t.Fatalf("created time=%q want audit %v", actual, recorded)
	}
	const legacyID = int64(9007199254740993)
	_, err = s.pool.Exec(ctx, `INSERT INTO console_users(id,username,role,subject_id,display_name,password_hash,admin_class) VALUES($1,'legacy-metadata','admin',$1,'Imported','synthetic-hash','operator')`, legacyID)
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := s.Get(ctx, root, "admin-users", legacyID)
	if err != nil || legacy.Fields["created_at"] != nil || legacy.Fields["last_login_at"] != nil || legacy.Fields["totp_enabled"] != false {
		t.Fatalf("missing history must stay unknown: %+v %v", legacy, err)
	}
	_, err = s.pool.Exec(ctx, `INSERT INTO console_login_audit(username,role,ip,success,created_at) VALUES
 ('legacy-metadata','admin','127.0.0.1',true,'2026-01-02T03:04:05Z'),
 ('legacy-metadata','admin','127.0.0.1',false,'2026-01-03T03:04:05Z'),
 ('legacy-metadata','merchant','127.0.0.1',true,'2026-01-04T03:04:05Z')`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.pool.Exec(ctx, `INSERT INTO console_mfa(username,role,pending_secret,pending_until) VALUES('legacy-metadata','admin',$1,now()+interval '5 minutes')`, []byte("synthetic-pending-secret"))
	if err != nil {
		t.Fatal(err)
	}
	page, err := s.List(ctx, root, "admin-users", Query{Search: strconv.FormatInt(legacyID, 10)})
	if err != nil || page.Total != 1 || len(page.Items) != 1 || page.Items[0].ID != legacyID {
		t.Fatalf("exact large UID search: %+v %v", page, err)
	}
	fields := page.Items[0].Fields
	if fields["last_login_at"] != "2026-01-02T03:04:05Z" || fields["totp_enabled"] != false {
		t.Fatalf("only successful admin login/active MFA count: %+v", fields)
	}
	_, err = s.pool.Exec(ctx, `UPDATE console_mfa SET secret=$1,pending_secret=NULL WHERE username='legacy-metadata' AND role='admin'`, []byte("synthetic-active-secret"))
	if err != nil {
		t.Fatal(err)
	}
	legacy, err = s.Get(ctx, root, "admin-users", legacyID)
	if err != nil || legacy.Fields["totp_enabled"] != true {
		t.Fatalf("active MFA: %+v %v", legacy, err)
	}
	raw, _ := json.Marshal(legacy.Fields)
	for _, secret := range []string{"password_hash", "synthetic-active-secret", "synthetic-hash", "pending_secret"} {
		if strings.Contains(string(raw), secret) {
			t.Fatalf("private credential exposed: %s", secret)
		}
	}
	schema, err := s.Schema(root, "admin-users")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"created_at", "last_login_at", "totp_enabled"} {
		found := false
		for _, field := range schema.Fields {
			if field.Name == name {
				found = field.ReadOnly
			}
		}
		if !found {
			t.Errorf("%s must be read-only", name)
		}
	}
}

func TestRiskRecordsExposeActualTimeAndOptionalURL(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	item, err := s.Create(ctx, admin, "risk-records", input(`{"merchant_id":"101","decision":"deny","reason":"synthetic risk","source":"manual","url":"https://shop.example.invalid/pay"}`))
	if err != nil {
		t.Fatal(err)
	}
	page, err := s.List(ctx, admin, "risk-records", Query{})
	if err != nil || len(page.Items) != 1 || page.Items[0].Fields["url"] != "https://shop.example.invalid/pay" {
		t.Fatalf("risk list: %+v %v", page, err)
	}
	if _, ok := page.Items[0].Fields["created_at"].(string); !ok {
		t.Fatal("actual record creation time missing")
	}
	detail, err := s.Get(ctx, admin, "risk-records", item.ID)
	if err != nil || detail.Fields["created_at"] != page.Items[0].Fields["created_at"] {
		t.Fatalf("list and detail timestamps differ: %+v %v", detail, err)
	}
	if _, err = s.Update(ctx, admin, "risk-records", item.ID, input(`{"created_at":"2000-01-01T00:00:00Z"}`)); err != ErrForbidden {
		t.Fatalf("cannot forge timestamp: %v", err)
	}
}
