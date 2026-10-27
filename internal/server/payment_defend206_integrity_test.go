package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"sync"
	"testing"

	"goepay/internal/payment"
	"goepay/internal/paymentdefend206"
	"goepay/internal/riskpolicy"
)

func TestPaymentDefend206ConcurrentCASHasOneOriginalCommit(t *testing.T) {
	f := paymentDefend206Fixture(t)
	before := paymentDefend206Read(t, f)
	beforeBusiness := paymentDefend206Effects(t, f)
	start := make(chan struct{})
	results := make(chan int, 2)
	var done sync.WaitGroup
	for _, body := range []string{paymentDefend206Body(t, before, true), paymentDefend206Body(t, before, false)} {
		done.Add(1)
		go func(body string) {
			defer done.Done()
			<-start
			results <- paymentDefend206Request(f, http.MethodPatch, "", body, f.token, f.csrf).Code
		}(body)
	}
	close(start)
	done.Wait()
	close(results)
	counts := map[int]int{}
	for status := range results {
		counts[status]++
	}
	if counts[http.StatusOK] != 1 || counts[http.StatusConflict] != 1 {
		t.Fatal("same original fingerprint did not have exactly one winner", counts)
	}
	after := paymentDefend206Read(t, f)
	if !after.Configured || after.Revision != "1" || after.Fingerprint == before.Fingerprint {
		t.Fatal("concurrent first save did not publish one original revision", after)
	}
	var heads, commits, audits int
	if err := f.p.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM payment_defend206_policy),(SELECT count(*) FROM payment_defend206_policy_commits),(SELECT count(*) FROM backoffice_audit WHERE resource='payment-defend206' AND operation='configure')`).Scan(&heads, &commits, &audits); err != nil || heads != 1 || commits != 1 || audits != 1 {
		t.Fatal("CAS winner and its immutable witness/audit were not atomic", heads, commits, audits, err)
	}
	if paymentDefend206Effects(t, f) != beforeBusiness {
		t.Fatal("policy CAS created ordinary payment or routing work")
	}
}

func TestPaymentDefend206CorruptOrMissingPolicyCannotBecomeAnUnconfiguredDefault(t *testing.T) {
	for _, damage := range []string{"settings", "null-empty-text", "head-delete", "commit-mac", "old-commit-mac"} {
		t.Run(damage, func(t *testing.T) {
			f := paymentDefend206Fixture(t)
			first := paymentDefend206Enable(t, f)
			if damage == "old-commit-mac" {
				if w := paymentDefend206Request(f, http.MethodPatch, "", paymentDefend206Body(t, first, false), f.token, f.csrf); w.Code != http.StatusOK {
					t.Fatal("second original witness fixture was not committed", w.Code, w.Body.String())
				}
			}
			before := paymentDefend206Effects(t, f)
			tx, err := f.p.Begin(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(t.Context())
			table := "payment_defend206_policy"
			if damage == "commit-mac" || damage == "old-commit-mac" {
				table = "payment_defend206_policy_commits"
			}
			if _, err = tx.Exec(t.Context(), "ALTER TABLE "+table+" DISABLE TRIGGER USER"); err != nil {
				t.Fatal(err)
			}
			statements := map[string]string{
				"settings":        `UPDATE payment_defend206_policy SET facts=jsonb_set(facts,'{Settings,pay_verify}','"0"'::jsonb)`,
				"null-empty-text": `UPDATE payment_defend206_policy SET facts=jsonb_set(facts,'{Settings,pay_verify_check_uid}','null'::jsonb)`,
				"head-delete":     `DELETE FROM payment_defend206_policy`,
				"commit-mac":      `UPDATE payment_defend206_policy_commits SET proof_hash=decode(repeat('00',32),'hex')`,
				"old-commit-mac":  `UPDATE payment_defend206_policy_commits SET proof_hash=decode(repeat('00',32),'hex') WHERE revision=1`,
			}
			if _, err = tx.Exec(t.Context(), statements[damage]); err != nil {
				t.Fatal(err)
			}
			if _, err = tx.Exec(t.Context(), "ALTER TABLE "+table+" ENABLE TRIGGER USER"); err != nil {
				t.Fatal(err)
			}
			if err = tx.Commit(t.Context()); err != nil {
				t.Fatal(err)
			}
			if w := paymentDefend206Request(f, http.MethodGet, "", "", f.token, f.csrf); w.Code != http.StatusConflict {
				t.Fatalf("damaged original policy was returned as usable/default: HTTP=%d body=%s", w.Code, w.Body.String())
			}
			if paymentDefend206Effects(t, f) != before {
				t.Fatal("reading damaged policy created ordinary payment work")
			}
		})
	}
}

func TestPaymentDefend206OldValidHeadAndWrongMasterCannotRestorePolicyAuthority(t *testing.T) {
	f := paymentDefend206Fixture(t)
	first := paymentDefend206Enable(t, f)
	var oldHead string
	if err := f.p.QueryRow(t.Context(), `SELECT to_jsonb(p)::text FROM payment_defend206_policy p`).Scan(&oldHead); err != nil {
		t.Fatal(err)
	}
	wrong, err := paymentdefend206.NewStore(f.p, bytes.Repeat([]byte{2}, 32))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = wrong.Current(t.Context()); !errors.Is(err, paymentdefend206.ErrConflict) {
		t.Fatal("another master key accepted the original configured policy", err)
	}
	first.Settings["pay_verify"], first.Settings["pay_verify_type"] = "0", "0"
	w := paymentDefend206Request(f, http.MethodPatch, "", paymentDefend206Body(t, first, false), f.token, f.csrf)
	if w.Code != http.StatusOK {
		t.Fatal("second legitimate original policy revision was not saved", w.Code, w.Body.String())
	}
	if current := paymentDefend206Read(t, f); current.Revision != "2" || current.Settings["pay_verify"] != "0" {
		t.Fatal("second policy revision was not observed", current)
	}
	tx, err := f.p.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(t.Context())
	if _, err = tx.Exec(t.Context(), `ALTER TABLE payment_defend206_policy DISABLE TRIGGER USER`); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(t.Context(), `UPDATE payment_defend206_policy SET (revision,facts,proof_hash)=(SELECT revision,facts,proof_hash FROM jsonb_populate_record(NULL::payment_defend206_policy,$1::jsonb))`, oldHead); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(t.Context(), `ALTER TABLE payment_defend206_policy ENABLE TRIGGER USER`); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(t.Context()); err != nil {
		t.Fatal(err)
	}
	if w = paymentDefend206Request(f, http.MethodGet, "", "", f.token, f.csrf); w.Code != http.StatusConflict {
		t.Fatal("whole earlier valid policy row rolled back the original authority", w.Code, w.Body.String())
	}
}

func TestPaymentDefend206AuditFailureRollsBackHeadWitnessAndConfiguration(t *testing.T) {
	f := paymentDefend206Fixture(t)
	before := paymentDefend206Read(t, f)
	if _, err := f.p.Exec(t.Context(), `CREATE FUNCTION payment_defend206_audit_failure() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.resource='payment-defend206' THEN RAISE EXCEPTION 'synthetic payment defend audit failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER payment_defend206_audit_failure BEFORE INSERT ON backoffice_audit FOR EACH ROW EXECUTE FUNCTION payment_defend206_audit_failure()`); err != nil {
		t.Fatal(err)
	}
	if w := paymentDefend206Request(f, http.MethodPatch, "", paymentDefend206Body(t, before, true), f.token, f.csrf); w.Code != http.StatusServiceUnavailable {
		t.Fatal("audit failure was reported as a saved policy", w.Code, w.Body.String())
	}
	if current := paymentDefend206Read(t, f); !reflect.DeepEqual(current, before) {
		t.Fatal("failed policy save changed original defaults or revision")
	}
	var heads, commits, audits int
	if err := f.p.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM payment_defend206_policy),(SELECT count(*) FROM payment_defend206_policy_commits),(SELECT count(*) FROM backoffice_audit WHERE resource='payment-defend206')`).Scan(&heads, &commits, &audits); err != nil || heads != 0 || commits != 0 || audits != 0 {
		t.Fatal("failed policy save left partial authority", heads, commits, audits, err)
	}
}

func TestPaymentDefend206LegacyMetadataAndOldRiskProofRemainIndependent(t *testing.T) {
	f := paymentDefend206Fixture(t)
	if _, err := f.p.Exec(t.Context(), `INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"pay_verify","value":"3"}'),('settings','{"key":"pay_verify_type","value":"3"}')`); err != nil {
		t.Fatal(err)
	}
	session, ok := f.s.Sessions.Get(f.token, "admin")
	if !ok {
		t.Fatal("fixture lost the persistent super session")
	}
	actor := payment.MerchantCredentialActor{User: session.User, SessionToken: f.token}
	oldRisk, err := f.s.Store.CoreRiskPolicy(t.Context(), actor)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.Store.UpdateCoreRiskPolicy(t.Context(), actor, riskpolicy.Defaults(), oldRisk.Fingerprint); err != nil {
		t.Fatal(err)
	}
	oldData := func() string {
		t.Helper()
		var data string
		if err := f.p.QueryRow(t.Context(), `SELECT jsonb_build_object('risk',(SELECT jsonb_agg(to_jsonb(p) ORDER BY id) FROM core_payment_risk_policy p),'audit',(SELECT jsonb_agg(to_jsonb(a) ORDER BY id) FROM backoffice_audit a WHERE resource='core-payment-risk'),'metadata',(SELECT jsonb_agg(to_jsonb(r) ORDER BY id) FROM backoffice_records r WHERE resource='settings' AND body->>'key' IN('pay_verify','pay_verify_type')))::text`).Scan(&data); err != nil {
			t.Fatal(err)
		}
		return data
	}
	before := oldData()
	unconfigured := paymentDefend206Read(t, f)
	if unconfigured.Configured || unconfigured.Settings["pay_verify"] != "0" || unconfigured.Settings["pay_verify_type"] != "0" {
		t.Fatal("descriptive old settings activated a protected payment defend policy")
	}
	paymentDefend206Enable(t, f)
	if after := oldData(); after != before {
		t.Fatal("new policy changed legacy metadata, old risk data, or its original MAC/audit")
	}
	if _, err = f.s.Store.CoreRiskPolicy(t.Context(), actor); err != nil {
		t.Fatal("new policy broke validation of the original risk proof", err)
	}
	var public map[string]any
	w := paymentDefend206Request(f, http.MethodGet, "", "", f.token, f.csrf)
	if json.Unmarshal(w.Body.Bytes(), &public) != nil {
		t.Fatal("policy response stopped being JSON")
	}
}

func TestPaymentDefend206KeylessReadOnlyProofRequiresAllAuthoritySourcesAbsent(t *testing.T) {
	for _, source := range []string{"empty", "configured", "witness-without-head", "audit-only", "missing-table", "database-error"} {
		t.Run(source, func(t *testing.T) {
			f := paymentDefend206Fixture(t)
			switch source {
			case "configured", "witness-without-head":
				paymentDefend206Enable(t, f)
				if source == "witness-without-head" {
					if _, err := f.p.Exec(t.Context(), `ALTER TABLE payment_defend206_policy DISABLE TRIGGER USER; DELETE FROM payment_defend206_policy; ALTER TABLE payment_defend206_policy ENABLE TRIGGER USER`); err != nil {
						t.Fatal(err)
					}
				}
			case "audit-only":
				if _, err := f.p.Exec(t.Context(), `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes) VALUES('admin',168,'synthetic206','payment-defend206',1,'configure','{"revision":"1"}')`); err != nil {
					t.Fatal(err)
				}
			case "missing-table":
				if _, err := f.p.Exec(t.Context(), `ALTER TABLE payment_defend206_policy RENAME TO payment_defend206_policy_temporarily_missing`); err != nil {
					t.Fatal(err)
				}
			}
			tx, err := f.p.Begin(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(t.Context())
			ctx := t.Context()
			if source == "empty" {
				zero := &paymentdefend206.Store{}
				if _, err := zero.CurrentTx(ctx, tx); !errors.Is(err, paymentdefend206.ErrUnavailable) {
					t.Fatal("zero-value store fabricated policy authority without a master key", err)
				}
			}
			if source == "database-error" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			err = paymentdefend206.ConfirmUnconfiguredTx(ctx, tx)
			if source == "empty" {
				if err != nil {
					t.Fatal("real empty policy authority was not explicitly confirmed off", err)
				}
			} else if err == nil {
				t.Fatal("keyless policy proof accepted non-empty, missing, or unavailable authority", source)
			}
		})
	}
}
