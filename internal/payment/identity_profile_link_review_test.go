package payment

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestIdentityProfileLink67ReadyCipherSQLConstraint(t *testing.T) {
	for _, tc := range []struct {
		name   string
		cipher []byte
	}{{"null", nil}, {"empty", []byte{}}, {"one_byte", []byte{1}}, {"nonce_and_tag_only", make([]byte, 28)}} {
		t.Run(tc.name, func(t *testing.T) {
			s, q, b, token := link67Fixture(t)
			ctx := context.Background()
			link67Begin(t, s, q, b, token)
			tx, err := s.pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(ctx)
			_, err = tx.Exec(ctx, `UPDATE profile_identity_links SET status='ready',completed_at=clock_timestamp(),link_cipher=$2 WHERE request_id=$1`, q.ID, tc.cipher)
			if err == nil {
				_, err = tx.Exec(ctx, `INSERT INTO profile_identity_link_events(request_id,operation) VALUES($1,'ready')`, q.ID)
			}
			if err == nil {
				err = tx.Commit(ctx)
			}
			if err == nil {
				t.Fatal("ready intent without a nonempty ciphertext committed")
			}
			var check *pgconn.PgError
			if !errors.As(err, &check) || check.Code != "23514" {
				t.Fatalf("expected SQL CHECK violation, got %v", err)
			}
			tx.Rollback(ctx)
			got, err := s.ReadProfileIdentityLink(ctx, q.ID, token, "merchant", b)
			if err != nil || got.Status != "creating" {
				t.Fatalf("rejected transaction damaged creating intent: %s, %v", got.Status, err)
			}
			if err = s.CompleteProfileIdentityLink(ctx, q.ID, token, "merchant", b, link67Result(q)); err != nil {
				t.Fatal("valid ready completion failed after rejected SQL", err)
			}
			got, err = s.ReadProfileIdentityLink(ctx, q.ID, token, "merchant", b)
			if err != nil || got.Status != "ready" || got.URLLink != link67Result(q).URLLink {
				t.Fatalf("normal ready result unavailable: %s, %v", got.Status, err)
			}
		})
	}
}
