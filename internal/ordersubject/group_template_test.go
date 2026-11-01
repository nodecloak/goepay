package ordersubject

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestGroupSubjectTemplateParsingReusesPolicyValidation(t *testing.T) {
	for _, tc := range []struct{ body, want string }{
		{`{}`, ""}, {`{"ordername":null}`, ""}, {`{"ordername":""}`, ""}, {`{"ordername":"0"}`, "0"}, {`{"ordername":"_"}`, "_"}, {`{"ordername":"one-[name]|two-[outorder]"}`, "one-[name]|two-[outorder]"},
	} {
		if got, e := GroupTemplate(json.RawMessage(tc.body)); e != nil || got != tc.want {
			t.Error(tc.body, got, e)
		}
	}
	for _, body := range []string{`{"ordername":0}`, `{"ordername":true}`, `{"ordername":"one|"}`, `{"ordername":"one\n"}`, `{"ordername":` + string(mustGroupTemplateJSON(strings.Repeat("x", 1025))) + `}`} {
		if _, e := GroupTemplate(json.RawMessage(body)); e == nil {
			t.Error("invalid group template accepted", body)
		}
	}
}

func mustGroupTemplateJSON(value string) []byte { raw, _ := json.Marshal(value); return raw }

func TestGroupSubjectTemplatePrecedenceRulesAndFrozenHistory(t *testing.T) {
	p, s := subjectFixture(t)
	ctx := t.Context()
	r := New(p)
	set := func(value string) {
		t.Helper()
		if _, e := p.Exec(ctx, `UPDATE backoffice_records SET body=body||jsonb_build_object('ordername',$1::text) WHERE resource='groups' AND id=0`, value); e != nil {
			t.Fatal(e)
		}
	}
	putPolicy(t, p, "global", "0", "global-[name]", `[{"name":"amount-[outorder]","min":"0","max":"1.00"}]`)
	set("group-[name]")
	a := newSubjectOrder(t, s, "group-title", "raw", 200)
	if title, e := r.Resolve(ctx, a, 7); e != nil || title != "group-raw" {
		t.Error("group did not override global template", title, e)
	}
	if _, e := p.Exec(ctx, `UPDATE backoffice_records SET body=body||'{"ordername":"bad|","enabled":false}'::jsonb WHERE resource='groups' AND id=0`); e != nil {
		t.Fatal(e)
	}
	if title, e := r.Resolve(ctx, a, 7); e != nil || title != "group-raw" {
		t.Fatal("current invalid group changed captured original title", title, e)
	}
	if _, e := p.Exec(ctx, `UPDATE backoffice_records SET body=body||'{"enabled":true}'::jsonb WHERE resource='groups' AND id=0`); e != nil {
		t.Fatal(e)
	}
	set("group-[name]")
	b := newSubjectOrder(t, s, "group-amount", "raw", 100)
	if title, e := r.Resolve(ctx, b, 7); e != nil || title != "amount-group-amount" {
		t.Error("group template discarded inherited amount rules", title, e)
	}
	putPolicy(t, p, "merchant", "101", "merchant-[name]", "")
	c := newSubjectOrder(t, s, "merchant-title", "raw", 200)
	if title, e := r.Resolve(ctx, c, 7); e != nil || title != "merchant-raw" {
		t.Error("merchant template did not override group", title, e)
	}
	putPolicy(t, p, "channel", "7", "channel-[name]", "")
	d := newSubjectOrder(t, s, "channel-title", "raw", 200)
	if title, e := r.Resolve(ctx, d, 7); e != nil || title != "channel-raw" {
		t.Error("channel template did not override merchant", title, e)
	}
	var snapshot json.RawMessage
	if e := p.QueryRow(ctx, `SELECT policy_snapshot FROM provider_subject_snapshots WHERE order_id=$1`, a.ID).Scan(&snapshot); e != nil {
		t.Fatal(e)
	}
	if !json.Valid(snapshot) {
		t.Fatal("invalid subject snapshot")
	}
}

func TestGroupSubjectTemplateRejectsInvalidAndUsesCurrentMembership(t *testing.T) {
	p, s := subjectFixture(t)
	ctx := t.Context()
	r := New(p)
	if _, e := p.Exec(ctx, `UPDATE backoffice_records SET body=body||'{"ordername":"bad|"}'::jsonb WHERE resource='groups' AND id=0`); e != nil {
		t.Fatal(e)
	}
	a := newSubjectOrder(t, s, "invalid-group-title", "raw", 100)
	if _, e := r.Resolve(ctx, a, 7); e == nil {
		t.Error("invalid group template produced a durable snapshot")
	}
	var count int
	if e := p.QueryRow(ctx, `SELECT count(*) FROM provider_subject_snapshots WHERE order_id=$1`, a.ID).Scan(&count); e != nil || count != 0 {
		t.Error("invalid group template was persisted", count, e)
	}
	if _, e := p.Exec(ctx, `UPDATE backoffice_records SET body=body||'{"ordername":"base-[name]"}'::jsonb WHERE resource='groups' AND id=0`); e != nil {
		t.Fatal(e)
	}
	var gid int64
	if e := p.QueryRow(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('groups','{"name":"synthetic title VIP","enabled":true,"ordername":"vip-[name]"}') RETURNING id`).Scan(&gid); e != nil {
		t.Fatal(e)
	}
	if _, e := p.Exec(ctx, `INSERT INTO memberships(merchant_id,group_id,expires_at) VALUES(101,$1,clock_timestamp()+interval '1 day')`, gid); e != nil {
		t.Fatal(e)
	}
	b := newSubjectOrder(t, s, "vip-title", "raw", 100)
	if title, e := r.Resolve(ctx, b, 7); e != nil || title != "vip-raw" {
		t.Error("active member group template not selected", title, e)
	}
	if _, e := p.Exec(ctx, `UPDATE memberships SET expires_at=clock_timestamp()-interval '1 second' WHERE merchant_id=101`); e != nil {
		t.Fatal(e)
	}
	c := newSubjectOrder(t, s, "expired-vip-title", "raw", 100)
	if title, e := r.Resolve(ctx, c, 7); e != nil || title != "base-raw" {
		t.Error("expired membership did not fall back to base group", title, e)
	}
	if title, e := r.Resolve(ctx, b, 7); e != nil || title != "vip-raw" {
		t.Error("expired membership rewrote captured VIP title", title, e)
	}
	if _, e := p.Exec(ctx, `DELETE FROM backoffice_records WHERE resource='order-subject-policies'; UPDATE backoffice_records SET body=body||'{"ordername":"0"}'::jsonb WHERE resource='groups' AND id=0`); e != nil {
		t.Fatal(e)
	}
	d := newSubjectOrder(t, s, "literal-zero-title", "raw", 100)
	if title, e := r.Resolve(ctx, d, 7); e != nil || title != "0" {
		t.Error("literal zero group title was treated as inheritance", title, e)
	}
	historical := newSubjectOrder(t, s, "group-historical-attempt", "raw-before-snapshot", 100)
	if _, _, e := s.BeginProviderAttempt(ctx, historical.TradeNo, 7, "native"); e != nil {
		t.Fatal(e)
	}
	if _, e := p.Exec(ctx, `UPDATE backoffice_records SET body=body||'{"ordername":"bad|","enabled":false}'::jsonb WHERE resource='groups' AND id=0`); e != nil {
		t.Fatal(e)
	}
	if title, e := r.Resolve(ctx, historical, 7); e != nil || title != historical.Name {
		t.Error("current group reinterpreted historical provider request", title, e)
	}
}

func TestGroupSubjectTemplateExpiryDuringSnapshotPersistenceRollsBack(t *testing.T) {
	p, s := subjectFixture(t)
	ctx := t.Context()
	var gid int64
	if e := p.QueryRow(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('groups','{"name":"Synthetic title expiry","enabled":true,"ordername":"vip-[name]"}') RETURNING id`).Scan(&gid); e != nil {
		t.Fatal(e)
	}
	if _, e := p.Exec(ctx, `INSERT INTO memberships(merchant_id,group_id,expires_at) VALUES(101,$1,clock_timestamp()+interval '1 day')`, gid); e != nil {
		t.Fatal(e)
	}
	o := newSubjectOrder(t, s, "title-expiry-persistence", "raw", 100)
	if _, e := p.Exec(ctx, `CREATE FUNCTION title_expiry_barrier() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN PERFORM pg_sleep(GREATEST(0,EXTRACT(EPOCH FROM (SELECT expires_at FROM memberships WHERE merchant_id=101)-clock_timestamp()))+0.05); RETURN NEW; END $$; CREATE TRIGGER title_expiry_barrier BEFORE INSERT ON provider_subject_snapshots FOR EACH ROW EXECUTE FUNCTION title_expiry_barrier(); UPDATE memberships SET expires_at=clock_timestamp()+interval '1 second' WHERE merchant_id=101`); e != nil {
		t.Fatal(e)
	}
	if _, e := New(p).Resolve(ctx, o, 7); e == nil {
		t.Error("expired group title committed after insert wait")
	}
	var count int
	if e := p.QueryRow(ctx, `SELECT count(*) FROM provider_subject_snapshots WHERE order_id=$1`, o.ID).Scan(&count); e != nil || count != 0 {
		t.Error("expired title snapshot survived rollback", count, e)
	}
	if _, e := p.Exec(ctx, `DROP TRIGGER title_expiry_barrier ON provider_subject_snapshots`); e != nil {
		t.Fatal(e)
	}
	if title, e := New(p).Resolve(ctx, o, 7); e != nil || title != "raw" {
		t.Error("new base-group title recovery failed", title, e)
	}
	if saved, e := s.Order(ctx, 101, o.OutTradeNo); e != nil || saved.Name != o.Name || saved.Status != "created" {
		t.Error("subject rollback changed merchant intent", saved, e)
	}
}
