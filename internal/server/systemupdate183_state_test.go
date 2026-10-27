package server

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"goepay/internal/identity"
)

func preparedUpdate183(t *testing.T) (update183Fixture, string, map[string]any) {
	t.Helper()
	f := update183Local(t)
	view := update183Data(t, f.update("GET", "", ""))
	fp := view["fingerprint"].(string)
	raw, _ := json.Marshal(map[string]string{"intent": fp, "expected_fingerprint": fp})
	if w := f.update("POST", "/prepare", string(raw)); w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	return f, fp, waitUpdate183(t, f, fp, "prepared")
}
func TestSystemUpdate183MissingCurrentSourceStillShowsOriginalVerifiedPreparation(t *testing.T) {
	f, fp, _ := preparedUpdate183(t)
	if e := os.Rename(f.cfg.ManifestPath, f.cfg.ManifestPath+".unavailable"); e != nil {
		t.Fatal(e)
	}
	view := update183Data(t, f.update("GET", "", ""))
	prep, ok := view["preparation"].(map[string]any)
	if !ok || prep["intent"] != fp || prep["state"] != "prepared" || view["check_status"] != "source_unavailable" {
		t.Fatal("current source outage erased authenticated existing preparation", view)
	}
}
func TestSystemUpdate183LostPreparedFilesRequireExplicitRepairAndNoBlindRestart(t *testing.T) {
	f, fp, _ := preparedUpdate183(t)
	if e := os.RemoveAll(filepath.Join(f.cfg.StagingDir, fp)); e != nil {
		t.Fatal(e)
	}
	unknown := update183Data(t, f.update("GET", "/preparations/"+fp, ""))
	if unknown["state"] != "unknown" || unknown["can_retry"] != true {
		t.Fatal("verified source loss created an unrecoverable dead control", unknown)
	}
	original, _ := json.Marshal(map[string]string{"intent": fp, "expected_fingerprint": fp})
	if v := update183Data(t, f.update("POST", "/prepare", string(original))); v["state"] != "unknown" {
		t.Fatal("prepare replay silently repaired unknown outcome", v)
	}
	retry, _ := json.Marshal(map[string]string{"expected_fingerprint": fp, "expected_state": unknown["state_fingerprint"].(string)})
	if w := f.update("POST", "/preparations/"+fp+"/retry", string(retry)); w.Code != 202 {
		t.Fatal("explicit lost-artifact repair absent", w.Code, w.Body.String())
	}
	waitUpdate183(t, f, fp, "prepared")
}
func TestSystemUpdate183PreparedPackageCanBeWithdrawnBeforeApplication(t *testing.T) {
	f, fp, status := preparedUpdate183(t)
	raw, _ := json.Marshal(map[string]string{"expected_state": status["state_fingerprint"].(string)})
	w := f.update("POST", "/preparations/"+fp+"/cancel", string(raw))
	if w.Code != http.StatusOK {
		t.Fatal("prepared but unapplied package cannot be withdrawn", w.Code, w.Body.String())
	}
	v := update183Data(t, w)
	if v["state"] != "cancelled" || v["applied"] != false {
		t.Fatal(v)
	}
}
func TestSystemUpdate183OrdinaryAdminCannotReceiveWriteCapabilities(t *testing.T) {
	f := update183Local(t)
	raw, e := os.ReadFile(f.cfg.PackagePath)
	if e != nil {
		t.Fatal(e)
	}
	raw[0] ^= 1
	if e = os.WriteFile(f.cfg.PackagePath, raw, 0600); e != nil {
		t.Fatal(e)
	}
	view := update183Data(t, f.update("GET", "", ""))
	fp := view["fingerprint"].(string)
	body, _ := json.Marshal(map[string]string{"intent": fp, "expected_fingerprint": fp})
	if w := f.update("POST", "/prepare", string(body)); w.Code != 202 {
		t.Fatal(w.Code)
	}
	waitUpdate183(t, f, fp, "failed")
	if _, e = f.p.Exec(t.Context(), `INSERT INTO console_users(username,role,subject_id,display_name,password_hash,admin_class) VALUES('operator183','admin',1830,'Synthetic operator','unused','operator')`); e != nil {
		t.Fatal(e)
	}
	f.token, f.csrf, e = f.s.Sessions.Create(identity.User{ID: 1830, Username: "operator183", Role: "admin", AdminClass: "operator", AuthVersion: 1})
	if e != nil {
		t.Fatal(e)
	}
	status := update183Data(t, f.update("GET", "/preparations/"+fp, ""))
	if status["can_retry"] != false || status["can_cancel"] != false {
		t.Fatal("read-only operator received write capability", status)
	}
}
