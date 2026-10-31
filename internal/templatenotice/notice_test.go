package templatenotice

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"goepay/db"
	"goepay/internal/templatechange"
	"os"
)

// fixture spins up a private schema on the shared synthetic test database,
// migrated to head, and returns a pool bound to it.
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

func randomKey(t *testing.T) string {
	t.Helper()
	var b [16]byte
	if _, e := rand.Read(b[:]); e != nil {
		t.Fatal(e)
	}
	return hex.EncodeToString(b[:])
}

// seedEvent inserts an upload, one version and one event of operation for the
// given version/restoredFrom, and returns the event id. page_template_heads
// already carries the 'home' kind from db.Migrate (FK target). restoredFrom>0
// requires a lower prior version to satisfy the version FK, so restore events
// also seed that source version.
func seedEvent(t *testing.T, pool *pgxpool.Pool, name, operation string, version, restoredFrom int64, actor string) int64 {
	t.Helper()
	ctx := context.Background()
	var uploadID int64
	e := pool.QueryRow(ctx, `INSERT INTO page_template_uploads(kind,name)VALUES('home',$1) RETURNING id`, name).Scan(&uploadID)
	if e != nil {
		t.Fatal(e)
	}
	digest := strings.Repeat("a", 64)
	seedVersion := func(v, rf int64) {
		var restored any
		if rf > 0 {
			restored = rf
		}
		_, e := pool.Exec(ctx, `INSERT INTO page_template_upload_versions(upload_id,version,file_key,title,author,description,package_version,digest,files,size_bytes,restored_from,actor_id,actor_name)VALUES($1,$2,$3,'极光','','','','`+digest+`',2,1024,$4,1,$5)`, uploadID, v, randomKey(t), restored, actor)
		if e != nil {
			t.Fatal(e)
		}
	}
	if restoredFrom > 0 && restoredFrom < version {
		seedVersion(restoredFrom, 0)
	}
	seedVersion(version, restoredFrom)
	var restored any
	if restoredFrom > 0 {
		restored = restoredFrom
	}
	var eventID int64
	e = pool.QueryRow(ctx, `INSERT INTO page_template_upload_events(upload_id,operation,version,restored_from,actor_id,actor_name)VALUES($1,$2,$3,$4,1,$5) RETURNING id`, uploadID, operation, version, restored, actor).Scan(&eventID)
	if e != nil {
		t.Fatal(e)
	}
	return eventID
}

// seedJob inserts a job claiming the event, with a lease in the future.
func seedJob(t *testing.T, pool *pgxpool.Pool, eventID int64, lease string, running bool, kind string) int64 {
	t.Helper()
	ctx := context.Background()
	state := "running"
	if !running {
		state = "pending"
	}
	eventKey := "page-template-event:" + strconv.FormatInt(eventID, 10)
	eventText := strconv.FormatInt(eventID, 10)
	var id int64
	e := pool.QueryRow(ctx, `INSERT INTO jobs(event_key,kind,payload,state,lease_token,lease_until,run_at)VALUES($1,$2,jsonb_build_object('event_id',$3::text),$4,$5,clock_timestamp()+interval '1 minute',clock_timestamp()) RETURNING id`, eventKey, kind, eventText, state, lease).Scan(&id)
	if e != nil {
		t.Fatal(e)
	}
	return id
}

type stubSender struct {
	calls atomic.Int32
	fail  bool
	gotTo string
	gotN  templatechange.Notice
}

func (s *stubSender) SendPageTemplateNotice(_ context.Context, to string, n templatechange.Notice) error {
	s.calls.Add(1)
	s.gotTo, s.gotN = to, n
	if s.fail {
		return ErrNotice
	}
	return nil
}

func noticeResult(t *testing.T, pool *pgxpool.Pool, eventID int64) (string, bool) {
	t.Helper()
	var state string
	e := pool.QueryRow(context.Background(), `SELECT state FROM page_template_notice_results WHERE event_id=$1`, eventID).Scan(&state)
	if e == pgx.ErrNoRows {
		return "", false
	}
	if e != nil {
		t.Fatal(e)
	}
	return state, true
}

func auditCount(t *testing.T, pool *pgxpool.Pool, eventID int64, state string) int {
	t.Helper()
	var n int
	e := pool.QueryRow(context.Background(), `SELECT count(*) FROM backoffice_audit WHERE resource='page-template-notices' AND resource_id=$1 AND operation=$2 AND (actor_role,actor_id,actor_name)=('system',0,'page-template-notice')`, eventID, state).Scan(&n)
	if e != nil {
		t.Fatal(e)
	}
	return n
}

func auditChanges(t *testing.T, pool *pgxpool.Pool, eventID int64) string {
	t.Helper()
	var changes string
	e := pool.QueryRow(context.Background(), `SELECT changes::text FROM backoffice_audit WHERE resource='page-template-notices' AND resource_id=$1`, eventID).Scan(&changes)
	if e != nil {
		t.Fatal(e)
	}
	return changes
}

func TestDeliverSendsOnceAndRecordsAccepted(t *testing.T) {
	pool := fixture(t)
	ctx := context.Background()
	eventID := seedEvent(t, pool, "aurora", "upload", 4, 0, "admin")
	lease := randomLease(t)
	jobID := seedJob(t, pool, eventID, lease, true, "admin.page-template-notice")
	n := New(pool)
	sender := &stubSender{}

	if e := n.Deliver(ctx, jobID, lease, "admin@example.test", sender); e != nil {
		t.Fatal(e)
	}
	if sender.calls.Load() != 1 || sender.gotTo != "admin@example.test" {
		t.Fatal("wrong send args", sender)
	}
	got := sender.gotN
	if got.Kind != "home" || got.Name != "aurora" || got.Title != "极光" || got.Operation != "upload" || got.Version != 4 || got.RestoredFrom != 0 || got.Actor != "admin" || got.At.IsZero() {
		t.Fatal("wrong notice fields", got)
	}
	if state, ok := noticeResult(t, pool, eventID); !ok || state != "accepted" {
		t.Fatal("result not accepted", state, ok)
	}
	if auditCount(t, pool, eventID, "accepted") != 1 {
		t.Fatal("missing accepted audit row")
	}
	changes := auditChanges(t, pool, eventID)
	for _, want := range []string{`"kind": "home"`, `"name": "aurora"`, `"operation": "upload"`, `"version": 4`, `"restored_from": 0`, `"state": "accepted"`} {
		if !strings.Contains(changes, want) {
			t.Fatal("audit changes missing", want, changes)
		}
	}

	// A second call for the same job/recipient is a no-op: no resend.
	if e := n.Deliver(ctx, jobID, lease, "admin@example.test", sender); e != nil {
		t.Fatal(e)
	}
	if sender.calls.Load() != 1 {
		t.Fatal("notice resent on second call", sender.calls.Load())
	}
}

// The notice names the administrator who made the change: selecting a
// version that someone else uploaded names the one who selected it.
func TestDeliverNamesTheActorOfTheEvent(t *testing.T) {
	pool := fixture(t)
	ctx := context.Background()
	uploaded := seedEvent(t, pool, "aurora", "upload", 4, 0, "alice")
	var selected int64
	if e := pool.QueryRow(ctx, `INSERT INTO page_template_upload_events(upload_id,operation,version,actor_id,actor_name) SELECT upload_id,'select',version,2,'bob' FROM page_template_upload_events WHERE id=$1 RETURNING id`, uploaded).Scan(&selected); e != nil {
		t.Fatal(e)
	}
	lease := randomLease(t)
	jobID := seedJob(t, pool, selected, lease, true, "admin.page-template-notice")
	sender := &stubSender{}
	if e := New(pool).Deliver(ctx, jobID, lease, "admin@example.test", sender); e != nil {
		t.Fatal(e)
	}
	if got := sender.gotN; got.Operation != "select" || got.Actor != "bob" || got.Version != 4 || got.Name != "aurora" {
		t.Fatal("the notice must name who selected the template", got)
	}
}

func TestDeliverRestoreForwardsRestoredFrom(t *testing.T) {
	pool := fixture(t)
	ctx := context.Background()
	eventID := seedEvent(t, pool, "aurora", "restore", 5, 2, "root.1")
	lease := randomLease(t)
	jobID := seedJob(t, pool, eventID, lease, true, "admin.page-template-notice")
	sender := &stubSender{}
	if e := New(pool).Deliver(ctx, jobID, lease, "admin@example.test", sender); e != nil {
		t.Fatal(e)
	}
	got := sender.gotN
	if got.Operation != "restore" || got.Version != 5 || got.RestoredFrom != 2 || got.Actor != "root.1" {
		t.Fatal("wrong restore args", got)
	}
}

func TestDeliverRecordsUnknownOnSendFailure(t *testing.T) {
	pool := fixture(t)
	ctx := context.Background()
	eventID := seedEvent(t, pool, "aurora", "upload", 4, 0, "admin")
	lease := randomLease(t)
	jobID := seedJob(t, pool, eventID, lease, true, "admin.page-template-notice")
	sender := &stubSender{fail: true}
	// A send failure still completes the job (claim committed, result recorded).
	if e := New(pool).Deliver(ctx, jobID, lease, "admin@example.test", sender); e != nil {
		t.Fatal(e)
	}
	if state, ok := noticeResult(t, pool, eventID); !ok || state != "unknown" {
		t.Fatal("result not unknown", state, ok)
	}
	if auditCount(t, pool, eventID, "unknown") != 1 {
		t.Fatal("missing unknown audit row")
	}
}

func TestDeliverRefusesWrongLeaseOrKind(t *testing.T) {
	pool := fixture(t)
	ctx := context.Background()
	sender := &stubSender{}

	// Wrong lease token.
	eventID := seedEvent(t, pool, "aurora", "upload", 4, 0, "admin")
	lease := randomLease(t)
	jobID := seedJob(t, pool, eventID, lease, true, "admin.page-template-notice")
	if e := New(pool).Deliver(ctx, jobID, randomLease(t), "admin@example.test", sender); e != ErrNotice {
		t.Fatal("wrong lease accepted", e)
	}

	// Wrong job kind (the retired kind is no longer claimed).
	eventID2 := seedEvent(t, pool, "nebula", "upload", 7, 0, "admin")
	lease2 := randomLease(t)
	jobID2 := seedJob(t, pool, eventID2, lease2, true, "admin.page-template-changed")
	if e := New(pool).Deliver(ctx, jobID2, lease2, "admin@example.test", sender); e != ErrNotice {
		t.Fatal("retired kind accepted", e)
	}

	// Non-running job (pending).
	eventID3 := seedEvent(t, pool, "comet", "upload", 9, 0, "admin")
	lease3 := randomLease(t)
	jobID3 := seedJob(t, pool, eventID3, lease3, false, "admin.page-template-notice")
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
	eventID := seedEvent(t, pool, "aurora", "upload", 4, 0, "admin")
	lease := randomLease(t)
	jobID := seedJob(t, pool, eventID, lease, true, "admin.page-template-notice")
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
	eventID := seedEvent(t, pool, "aurora", "upload", 4, 0, "admin")
	lease := randomLease(t)
	jobID := seedJob(t, pool, eventID, lease, true, "admin.page-template-notice")
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
