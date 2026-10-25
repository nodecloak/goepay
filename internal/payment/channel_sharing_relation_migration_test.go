package payment

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func sharingStoreBeforeRelationReferences(t *testing.T) *Store {
	t.Helper()
	dsn := os.Getenv("GOEPAY_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("GOEPAY_TEST_DATABASE_URL required")
	}
	ctx := context.Background()
	admin, e := pgxpool.New(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	nonce := make([]byte, 8)
	if _, e = rand.Read(nonce); e != nil {
		t.Fatal(e)
	}
	schema := "sharing_upgrade_" + hex.EncodeToString(nonce)
	quoted := pgx.Identifier{schema}.Sanitize()
	if _, e = admin.Exec(ctx, "CREATE SCHEMA "+quoted); e != nil {
		t.Fatal(e)
	}
	cfg, e := pgxpool.ParseConfig(dsn)
	if e != nil {
		t.Fatal(e)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		pool.Close()
		admin.Exec(context.Background(), "DROP SCHEMA "+quoted+" CASCADE")
		admin.Close()
	})
	paths, e := filepath.Glob("../../db/migrations/*.up.sql")
	if e != nil {
		t.Fatal(e)
	}
	for _, path := range paths {
		if strings.Compare(filepath.Base(path), "000051_") >= 0 {
			continue
		}
		body, e := os.ReadFile(path)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = pool.Exec(ctx, string(body)); e != nil {
			t.Fatal(filepath.Base(path), e)
		}
	}
	s, e := NewWithKey(pool, make([]byte, 32))
	if e != nil {
		t.Fatal(e)
	}
	if e = s.CreateMerchant(ctx, Merchant{ID: 751, Name: "sharing-owner", APIKey: "local-upgrade"}); e != nil {
		t.Fatal(e)
	}
	return s
}

func seedOriginalSharingReceiverState(t *testing.T, s *Store, id int64, target string) {
	t.Helper()
	ctx := context.Background()
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(ctx)
	operation := func(action, result string) {
		t.Helper()
		var op int64
		if e = tx.QueryRow(ctx, "SELECT nextval(pg_get_serial_sequence('channel_sharing_receiver_operations','id'))").Scan(&op); e != nil {
			t.Fatal(e)
		}
		prefix := "PB"
		pending := "binding"
		if action == "unbind" {
			prefix = "PU"
			pending = "unbinding"
		}
		if _, e = tx.Exec(ctx, "INSERT INTO channel_sharing_receiver_operations(id,receiver_id,action,out_no,external_no,request_hash,actor_role,actor_id,actor_name) VALUES($1,$2,$3,$4,$5,decode(repeat('00',32),'hex'),'merchant',751,'sharing-owner')", op, id, action, fmt.Sprintf("%s%030x", prefix, op), fmt.Sprintf("legacy-%s-%d", action, id)); e != nil {
			t.Fatal(e)
		}
		if _, e = tx.Exec(ctx, "UPDATE channel_sharing_receiver_operations SET state='claimed',claimed_at=statement_timestamp() WHERE id=$1", op); e != nil {
			t.Fatal(e)
		}
		if _, e = tx.Exec(ctx, "UPDATE channel_sharing_receivers SET state=$2,last_operation_id=$3 WHERE id=$1", id, pending, op); e != nil {
			t.Fatal(e)
		}
		if e = sharingConfigurationAudit(ctx, tx, channelSharingActor, "channel-sharing-receiver-operations", op, action, "claimed"); e != nil {
			t.Fatal(e)
		}
		if result == "claimed" {
			return
		}
		next := "unknown"
		if result == "succeeded" {
			next = "bound"
			if action == "unbind" {
				next = "unbound"
			}
		}
		if result == "failed" {
			next = "draft"
			if action == "unbind" {
				next = "bound"
			}
		}
		if _, e = tx.Exec(ctx, "UPDATE channel_sharing_receiver_operations SET state=$2 WHERE id=$1", op, result); e != nil {
			t.Fatal(e)
		}
		if _, e = tx.Exec(ctx, "UPDATE channel_sharing_receivers SET state=$2 WHERE id=$1", id, next); e != nil {
			t.Fatal(e)
		}
		if e = sharingConfigurationAudit(ctx, tx, channelSharingActor, "channel-sharing-receiver-operations", op, action, result); e != nil {
			t.Fatal(e)
		}
	}
	bindResult := "succeeded"
	switch target {
	case "binding":
		bindResult = "claimed"
	case "unknown-bind":
		bindResult = "unknown"
	case "failed-bind":
		bindResult = "failed"
	}
	operation("bind", bindResult)
	switch target {
	case "unbound":
		operation("unbind", "succeeded")
	case "unbinding":
		operation("unbind", "claimed")
	case "unknown-unbind":
		operation("unbind", "unknown")
	case "failed-unbind":
		operation("unbind", "failed")
	}
	if e = tx.Commit(ctx); e != nil {
		t.Fatal(e)
	}
}

func TestChannelSharingRelationReferencesUpgradePreservesOriginalProofAndUncertainty(t *testing.T) {
	s := sharingStoreBeforeRelationReferences(t)
	ctx := context.Background()
	b := channelSharingBinding{MerchantID: 751, ChannelID: 7, Provider: "wxpay", AppID: "local-app", Product: "wxpay_v3_direct", AccountScope: "original-upgrade-account", RelationScope: `["wxpay","original-upgrade-mch"]`}
	type expectation struct {
		local, global string
		leases        int
	}
	cases := map[string]expectation{
		"bound": {"bound", "bound", 1}, "binding": {"binding", "binding", 0}, "unknown-bind": {"unknown", "unknown", 0}, "failed-bind": {"draft", "unbound", 0},
		"unbound": {"unbound", "unbound", 0}, "unbinding": {"unbinding", "unbinding", 1}, "unknown-unbind": {"unknown", "unknown", 1}, "failed-unbind": {"bound", "bound", 1},
	}
	ids := map[string]int64{}
	for state := range cases {
		input := channelSharingReceiverInput
		input.Account = "local-upgrade-" + state
		r, e := s.createChannelSharingReceiver(ctx, b, "upgrade-"+state, input, channelSharingActor)
		if e != nil {
			t.Fatal(e)
		}
		ids[state] = r.ID
		seedOriginalSharingReceiverState(t, s, r.ID, state)
	}
	draftInput := channelSharingReceiverInput
	draftInput.Account = "local-upgrade-draft-only"
	draft, e := s.createChannelSharingReceiver(ctx, b, "upgrade-draft-only", draftInput, channelSharingActor)
	if e != nil {
		t.Fatal(e)
	}
	body, e := os.ReadFile("../../db/migrations/000051_channel_sharing_relation_references.up.sql")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.pool.Exec(ctx, string(body)); e != nil {
		t.Fatal(e)
	}
	for state, want := range cases {
		var local, global string
		var leases int
		e = s.pool.QueryRow(ctx, "SELECT r.state,g.state,(SELECT count(*) FROM channel_sharing_relation_leases l WHERE l.relation_id=g.id AND l.released_by_operation_id IS NULL) FROM channel_sharing_receivers r JOIN channel_sharing_relations g ON g.relation_hash=r.relation_hash WHERE r.id=$1", ids[state]).Scan(&local, &global, &leases)
		if e != nil || local != want.local || global != want.global || leases != want.leases {
			t.Fatalf("%s migration got local=%s global=%s leases=%d err=%v", state, local, global, leases, e)
		}
	}
	var registered bool
	if e = s.pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM channel_sharing_relations g JOIN channel_sharing_receivers r ON r.relation_hash=g.relation_hash WHERE r.id=$1)", draft.ID).Scan(&registered); e != nil || registered {
		t.Fatal("upgrade registered an unclaimed draft", registered, e)
	}
	// Fire the new deferred invariants against the migrated rows, including
	// original actor audits and unknown operations whose send barrier must stay.
	if _, e = s.pool.Exec(ctx, "UPDATE channel_sharing_relations SET state=state; UPDATE channel_sharing_receivers SET state=state"); e != nil {
		t.Fatal("migrated relationship proofs fail new constraints", e)
	}
}
