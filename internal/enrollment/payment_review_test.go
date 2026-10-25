package enrollment

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"goepay/internal/identity"
)

func TestMerchantPaymentReview82RegistrationKeepsLoginButWaitsForPayApproval(t *testing.T) {
	s, sender := enrollmentFixture(t)
	ctx := context.Background()
	if _, err := s.pool.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"user_review","value":"1"}')`); err != nil {
		t.Fatal(err)
	}
	registered := registerVerified(t, s, sender, "review82@example.test", "review82", "192.0.2.82")
	var pay string
	var enabled bool
	var hash string
	if err := s.pool.QueryRow(ctx, `SELECT COALESCE(to_jsonb(m)->>'pay','missing'),m.enabled,u.password_hash FROM merchants m JOIN console_users u ON u.subject_id=m.id AND u.role='merchant' WHERE m.id=$1`, registered.MerchantID).Scan(&pay, &enabled, &hash); err != nil {
		t.Fatal(err)
	}
	if pay != "2" || !enabled || !identity.CheckPassword(hash, "correct-test-password") {
		t.Fatalf("review state must not disable login: pay=%s enabled=%v", pay, enabled)
	}
	raw, _ := json.Marshal(registered)
	var value map[string]any
	json.Unmarshal(raw, &value)
	if value["pay"] != "2" {
		t.Fatal("registration result omits original pending pay status")
	}
	capabilities, err := s.Capabilities(ctx)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ = json.Marshal(capabilities)
	json.Unmarshal(raw, &value)
	if value["review_required"] != true {
		t.Fatal("public capability omits required review")
	}
}

func TestMerchantPaymentReview82RejectsMalformedPolicyBeforeCreatingIdentity(t *testing.T) {
	for _, raw := range []string{`true`, `1`, `null`, `"2"`, `" 1"`} {
		t.Run(raw, func(t *testing.T) {
			s, sender := enrollmentFixture(t)
			ctx := context.Background()
			challenge, err := s.Issue(ctx, VerificationRequest{Purpose: "register", Email: "invalid82@example.test", Source: "192.0.2.82"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = s.pool.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('settings',jsonb_build_object('key','user_review','value',$1::jsonb))`, raw); err != nil {
				t.Fatal(err)
			}
			_, err = s.Register(ctx, RegisterInput{ChallengeID: challenge.ID, Code: sender.last(t).code, Email: "invalid82@example.test", Source: "192.0.2.82", Username: "invalid82", Name: "Invalid", Password: "correct-test-password"})
			if !errors.Is(err, ErrInvalid) {
				t.Fatal("malformed review policy accepted", err)
			}
			if _, err = s.Capabilities(ctx); !errors.Is(err, ErrInvalid) {
				t.Fatal("malformed policy advertised usable capabilities", err)
			}
			var users int
			var consumed bool
			err = s.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM merchants),(SELECT consumed_at IS NOT NULL FROM enrollment_challenges WHERE id=$1)`, challenge.ID).Scan(&users, &consumed)
			if err != nil || users != 0 || consumed {
				t.Fatal("invalid policy created user or consumed email proof", users, consumed, err)
			}
		})
	}
}

func TestMerchantPaymentReview82SettingChangesDoNotBackfillExistingMerchants(t *testing.T) {
	s, sender := enrollmentFixture(t)
	ctx := context.Background()
	first := registerVerified(t, s, sender, "normal82@example.test", "normal82", "192.0.2.81")
	if _, err := s.pool.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"user_review","value":"1"}')`); err != nil {
		t.Fatal(err)
	}
	second := registerVerified(t, s, sender, "pending82@example.test", "pending82", "192.0.2.82")
	if _, err := s.pool.Exec(ctx, `UPDATE backoffice_records SET body=jsonb_set(body,'{value}','"0"') WHERE resource='settings' AND body->>'key'='user_review'`); err != nil {
		t.Fatal(err)
	}
	var old, next int
	if err := s.pool.QueryRow(ctx, `SELECT (SELECT pay FROM merchants WHERE id=$1),(SELECT pay FROM merchants WHERE id=$2)`, first.MerchantID, second.MerchantID).Scan(&old, &next); err != nil || old != 1 || next != 2 {
		t.Fatal("policy retroactively changed original payment admission", old, next, err)
	}
}

func TestMerchantPaymentReview82DuplicateImportedPolicyFailsClosed(t *testing.T) {
	s, _ := enrollmentFixture(t)
	ctx := context.Background()
	// Only this fixture's random schema is affected; model malformed imports
	// independently from the ordinary unique-settings index.
	if _, err := s.pool.Exec(ctx, `DROP INDEX backoffice_setting_unique`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"user_review","value":"0"}'),('settings','{"key":"user_review","value":"1"}')`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Capabilities(ctx); !errors.Is(err, ErrInvalid) {
		t.Fatal("ambiguous imported review policy accepted", err)
	}
}
