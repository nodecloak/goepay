package migration

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestProfile198ImportAcceptsExplicitOriginalRegistrationTime(t *testing.T) {
	raw := strings.Replace(snapshot, `"name":"Imported demo"`, `"name":"Imported demo","created_at":"2018-07-08T09:10:11+08:00"`, 1)
	if _, _, e := Validate([]byte(raw)); e != nil {
		t.Fatal("explicit historical merchant registration timestamp not supported", e)
	}
	p := migrationPool(t)
	if _, e := Apply(t.Context(), p, bytes.Repeat([]byte{3}, 32), []byte(raw)); e != nil {
		t.Fatal(e)
	}
	var created time.Time
	var known *bool
	if e := p.QueryRow(t.Context(), `SELECT created_at,(to_jsonb(m)->>'registration_time_known')::boolean FROM merchants m WHERE id=9001`).Scan(&created, &known); e != nil || created.UTC().Format(time.RFC3339) != "2018-07-08T01:10:11Z" || known == nil || !*known {
		t.Fatal("import replaced original registration with import clock", created, known, e)
	}
}
func TestProfile198LegacySnapshotWithoutTimestampDoesNotInventRegistrationTime(t *testing.T) {
	p := migrationPool(t)
	if _, e := Apply(t.Context(), p, bytes.Repeat([]byte{3}, 32), []byte(snapshot)); e != nil {
		t.Fatal(e)
	}
	var known *bool
	if e := p.QueryRow(t.Context(), `SELECT (to_jsonb(m)->>'registration_time_known')::boolean FROM merchants m WHERE id=9001`).Scan(&known); e != nil || known == nil || *known {
		t.Fatal("missing original timestamp has no explicit unknown state", known, e)
	}
}
func TestProfile198RegistrationTimeRejectsInvalidOrLaterThanExport(t *testing.T) {
	for _, value := range []string{"0000-00-00 00:00:00", "0001-01-01T00:00:00Z", "2020-01-02T03:04:05", "2018-07-08T09:10:11-00:00", "2018-07-08T09:10:11+24:00", "2018-07-08T09:10:11+01:60", "2027-01-01T00:00:00Z"} {
		raw := strings.Replace(snapshot, `"name":"Imported demo"`, `"name":"Imported demo","created_at":"`+value+`"`, 1)
		if _, _, e := Validate([]byte(raw)); e == nil {
			t.Fatal("invalid/future registration accepted", value)
		}
	}
}
