package templatenotice

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"goepay/db"
	"os"
)

// fixture spins up a private schema on the shared synthetic test database,
// migrated to head, and returns a pool bound to it. Copied from the pattern in
// internal/pagetemplate/store_test.go.
func fixture(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("GOEPAY_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("GOEPAY_TEST_DATABASE_URL required")
	}
	ctx := context.Background()
	base, e := pgxpool.New(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	var nonce [8]byte
	if _, e = rand.Read(nonce[:]); e != nil {
		t.Fatal(e)
	}
	schema := "templatenotice_" + hex.EncodeToString(nonce[:])
	quoted := pgx.Identifier{schema}.Sanitize()
	if _, e = base.Exec(ctx, "CREATE SCHEMA "+quoted); e != nil {
		t.Fatal(e)
	}
	cfg, e := pgxpool.ParseConfig(dsn)
	if e != nil {
		t.Fatal(e)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	cfg.MaxConns = 4
	pool, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { pool.Close(); base.Exec(context.Background(), "DROP SCHEMA "+quoted+" CASCADE"); base.Close() })
	if e = db.Migrate(ctx, pool); e != nil {
		t.Fatal(e)
	}
	return pool
}

func randomLease(t *testing.T) string {
	t.Helper()
	var b [16]byte
	if _, e := rand.Read(b[:]); e != nil {
		t.Fatal(e)
	}
	return hex.EncodeToString(b[:])
}

// seedVersion inserts an immutable version row; page_template_heads must have
// the 'home' kind for the FK, which db.Migrate seeds. Returns the version id.
func seedVersion(t *testing.T, pool *pgxpool.Pool, version, restoredFrom int64, actor string) int64 {
	t.Helper()
	ctx := context.Background()
	var restored any
	if restoredFrom > 0 {
		restored = restoredFrom
	}
	digest := strings.Repeat("a", 64)
	var id int64
	e := pool.QueryRow(ctx, `INSERT INTO page_template_custom_versions(kind,version,html,css,frame,restored_from,digest,actor_id,actor_name)VALUES('home',$1,'<p>hi</p>','',true,$2,$3,1,$4) RETURNING id`, version, restored, digest, actor).Scan(&id)
	if e != nil {
		t.Fatal(e)
	}
	return id
}

// seedJob inserts a running job claiming the version, with a lease in the
// future. Returns the job id.
func seedJob(t *testing.T, pool *pgxpool.Pool, versionID int64, lease string, running bool, kind string) int64 {
	t.Helper()
	ctx := context.Background()
	state := "running"
	if !running {
		state = "pending"
	}
	eventKey := "page-template-custom:" + strconv.FormatInt(versionID, 10)
	versionText := strconv.FormatInt(versionID, 10)
	var id int64
	e := pool.QueryRow(ctx, `INSERT INTO jobs(event_key,kind,payload,state,lease_token,lease_until,run_at)VALUES($1,$2,jsonb_build_object('version_id',$3::text),$4,$5,clock_timestamp()+interval '1 minute',clock_timestamp()) RETURNING id`, eventKey, kind, versionText, state, lease).Scan(&id)
	if e != nil {
		t.Fatal(e)
	}
	return id
}

type stubSender struct {
	calls   atomic.Int32
	fail    bool
	gotVer  int64
	gotRest int64
	gotTo   string
	gotActr string
}

func (s *stubSender) SendPageTemplateChanged(_ context.Context, to string, version, restoredFrom int64, actor string, _ time.Time) error {
	s.calls.Add(1)
	s.gotTo, s.gotVer, s.gotRest, s.gotActr = to, version, restoredFrom, actor
	if s.fail {
		return ErrNotice
	}
	return nil
}

func noticeResult(t *testing.T, pool *pgxpool.Pool, versionID int64) (string, bool) {
	t.Helper()
	var state string
	e := pool.QueryRow(context.Background(), `SELECT state FROM page_template_custom_notice_results WHERE version_id=$1`, versionID).Scan(&state)
	if e == pgx.ErrNoRows {
		return "", false
	}
	if e != nil {
		t.Fatal(e)
	}
	return state, true
}

func auditCount(t *testing.T, pool *pgxpool.Pool, versionID int64, state string) int {
	t.Helper()
	var n int
	e := pool.QueryRow(context.Background(), `SELECT count(*) FROM backoffice_audit WHERE resource='page-template-notices' AND resource_id=$1 AND operation=$2 AND (actor_role,actor_id,actor_name)=('system',0,'page-template-notice')`, versionID, state).Scan(&n)
	if e != nil {
		t.Fatal(e)
	}
	return n
}

func TestDeliverSendsOnceAndRecordsAccepted(t *testing.T) {
	pool := fixture(t)
	ctx := context.Background()
	versionID := seedVersion(t, pool, 4, 0, "admin")
	lease := randomLease(t)
	jobID := seedJob(t, pool, versionID, lease, true, "admin.page-template-changed")
	n := New(pool)
	sender := &stubSender{}

	if e := n.Deliver(ctx, jobID, lease, "admin@example.test", sender); e != nil {
		t.Fatal(e)
	}
	if sender.calls.Load() != 1 || sender.gotVer != 4 || sender.gotRest != 0 || sender.gotTo != "admin@example.test" || sender.gotActr != "admin" {
		t.Fatal("wrong send args", sender)
	}
	if state, ok := noticeResult(t, pool, versionID); !ok || state != "accepted" {
		t.Fatal("result not accepted", state, ok)
	}
	if auditCount(t, pool, versionID, "accepted") != 1 {
		t.Fatal("missing accepted audit row")
	}

	// A second call for the same job/recipient is a no-op: no resend.
	if e := n.Deliver(ctx, jobID, lease, "admin@example.test", sender); e != nil {
		t.Fatal(e)
	}
	if sender.calls.Load() != 1 {
		t.Fatal("notice resent on second call", sender.calls.Load())
	}
}

func TestDeliverRestoreForwardsRestoredFrom(t *testing.T) {
	pool := fixture(t)
	ctx := context.Background()
	versionID := seedVersion(t, pool, 5, 2, "root.1")
	lease := randomLease(t)
	jobID := seedJob(t, pool, versionID, lease, true, "admin.page-template-changed")
	sender := &stubSender{}
	if e := New(pool).Deliver(ctx, jobID, lease, "admin@example.test", sender); e != nil {
		t.Fatal(e)
	}
	if sender.gotVer != 5 || sender.gotRest != 2 || sender.gotActr != "root.1" {
		t.Fatal("wrong restore args", sender)
	}
}

func TestDeliverRecordsUnknownOnSendFailure(t *testing.T) {
	pool := fixture(t)
	ctx := context.Background()
	versionID := seedVersion(t, pool, 4, 0, "admin")
	lease := randomLease(t)
	jobID := seedJob(t, pool, versionID, lease, true, "admin.page-template-changed")
	sender := &stubSender{fail: true}
	// A send failure still completes the job (claim committed, result recorded).
	if e := New(pool).Deliver(ctx, jobID, lease, "admin@example.test", sender); e != nil {
		t.Fatal(e)
	}
	if state, ok := noticeResult(t, pool, versionID); !ok || state != "unknown" {
		t.Fatal("result not unknown", state, ok)
	}
	if auditCount(t, pool, versionID, "unknown") != 1 {
		t.Fatal("missing unknown audit row")
	}
}

func TestDeliverRefusesWrongLeaseOrKind(t *testing.T) {
	pool := fixture(t)
	ctx := context.Background()

	// Wrong lease token.
	versionID := seedVersion(t, pool, 4, 0, "admin")
	lease := randomLease(t)
	jobID := seedJob(t, pool, versionID, lease, true, "admin.page-template-changed")
	sender := &stubSender{}
	if e := New(pool).Deliver(ctx, jobID, randomLease(t), "admin@example.test", sender); e != ErrNotice {
		t.Fatal("wrong lease accepted", e)
	}

	// Wrong job kind.
	versionID2 := seedVersion(t, pool, 7, 0, "admin")
	lease2 := randomLease(t)
	jobID2 := seedJob(t, pool, versionID2, lease2, true, "admin.channel-stopped")
	if e := New(pool).Deliver(ctx, jobID2, lease2, "admin@example.test", sender); e != ErrNotice {
		t.Fatal("wrong kind accepted", e)
	}

	// Non-running job (pending).
	versionID3 := seedVersion(t, pool, 9, 0, "admin")
	lease3 := randomLease(t)
	jobID3 := seedJob(t, pool, versionID3, lease3, false, "admin.page-template-changed")
	if e := New(pool).Deliver(ctx, jobID3, lease3, "admin@example.test", sender); e != ErrNotice {
		t.Fatal("non-running job accepted", e)
	}

	if sender.calls.Load() != 0 {
		t.Fatal("refused claim still sent", sender.calls.Load())
	}
}

func TestDeliverValidatesInputs(t *testing.T) {
	pool := fixture(t)
	ctx := context.Background()
	versionID := seedVersion(t, pool, 4, 0, "admin")
	lease := randomLease(t)
	jobID := seedJob(t, pool, versionID, lease, true, "admin.page-template-changed")
	n := New(pool)
	sender := &stubSender{}

	if e := n.Deliver(ctx, 0, lease, "admin@example.test", sender); e != ErrNotice {
		t.Fatal("non-positive job id accepted")
	}
	if e := n.Deliver(ctx, jobID, "short", "admin@example.test", sender); e != ErrNotice {
		t.Fatal("short lease accepted")
	}
	if e := n.Deliver(ctx, jobID, strings.Repeat("g", 32), "admin@example.test", sender); e != ErrNotice {
		t.Fatal("non-hex lease accepted")
	}
	if e := n.Deliver(ctx, jobID, lease, "", sender); e != ErrNotice {
		t.Fatal("empty recipient accepted")
	}
	if e := n.Deliver(ctx, jobID, lease, "admin@example.test", nil); e != ErrNotice {
		t.Fatal("nil sender accepted")
	}
	if sender.calls.Load() != 0 {
		t.Fatal("invalid input still sent")
	}
}

func TestDeliverDifferentRecipientAfterClaimConflicts(t *testing.T) {
	pool := fixture(t)
	ctx := context.Background()
	versionID := seedVersion(t, pool, 4, 0, "admin")
	lease := randomLease(t)
	jobID := seedJob(t, pool, versionID, lease, true, "admin.page-template-changed")
	n := New(pool)
	first := &stubSender{}
	if e := n.Deliver(ctx, jobID, lease, "admin@example.test", first); e != nil {
		t.Fatal(e)
	}
	// Same job, different recipient -> conflict, never resend.
	second := &stubSender{}
	if e := n.Deliver(ctx, jobID, lease, "other@example.test", second); e != ErrNotice {
		t.Fatal("different recipient reused claim", e)
	}
	if second.calls.Load() != 0 {
		t.Fatal("conflict still sent")
	}
}
