package printing

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"goepay/internal/payment"
	"regexp"
	"time"
)

type ManualInput struct {
	ExternalNo           string `json:"external_no"`
	PreviousJobID        int64  `json:"previous_job_id,string"`
	AcknowledgeDuplicate bool   `json:"acknowledge_duplicate"`
}
type JobView struct {
	ID            int64     `json:"id,string"`
	MerchantID    int64     `json:"merchant_id,string"`
	OrderID       int64     `json:"order_id,string"`
	PreviousJobID int64     `json:"previous_job_id,string"`
	TradeNo       string    `json:"trade_no"`
	Source        string    `json:"source"`
	ExternalNo    string    `json:"external_no"`
	RequestNo     string    `json:"request_no"`
	State         string    `json:"state"`
	Reason        string    `json:"reason"`
	Provider      string    `json:"provider"`
	DeviceMasked  string    `json:"device_masked"`
	Copies        int       `json:"copies"`
	Voice         bool      `json:"voice"`
	CreatedAt     time.Time `json:"created_at"`
	Receipt       Receipt   `json:"receipt"`
	TaskID        int64     `json:"task_id,string"`
	TaskState     string    `json:"task_state"`
	CanRecover    bool      `json:"can_recover"`
}
type intentBody struct {
	Receipt       Receipt `json:"receipt"`
	Copies        int     `json:"copies"`
	Voice         bool    `json:"voice"`
	Provider      string  `json:"provider"`
	DeviceMasked  string  `json:"device_masked"`
	PreviousJobID int64   `json:"previous_job_id,string"`
	ActorName     string  `json:"actor_name"`
}
type intentRecord struct {
	V                            JobView
	PlatformID, MerchantConfigID int64
	Body                         intentBody
	Proof                        []byte
}

func (s *Store) receipt(ctx context.Context, q reader, mid int64, trade string) (Receipt, payment.PrintingPaidSource, error) {
	var z Receipt
	var f payment.PrintingPaidSource
	var oid int64
	var name string
	e := q.QueryRow(ctx, "SELECT o.id,m.name FROM orders o JOIN merchants m ON m.id=o.merchant_id WHERE o.merchant_id=$1 AND o.trade_no=$2", mid, trade).Scan(&oid, &name)
	if errors.Is(e, pgx.ErrNoRows) {
		return z, f, ErrNotFound
	}
	if e != nil {
		return z, f, e
	}
	f, e = s.source.PrintingPaidSource(ctx, q, oid)
	if errors.Is(e, payment.ErrNotFound) {
		return z, f, ErrState
	}
	if e != nil {
		return z, f, ErrConflict
	}
	if f.MerchantID != mid {
		return z, f, ErrConflict
	}
	z = Receipt{TradeNo: f.TradeNo, MerchantName: name, Name: f.Name, Amount: f.Amount, Type: f.Type, PaidAt: f.PaidAt, Remark: f.Remark}
	if _, e = RenderReceipt(z); e != nil {
		return z, f, e
	}
	return z, f, nil
}
func (s *Store) Receipt(ctx context.Context, mid int64, trade string, a Actor) (Receipt, error) {
	if !owner(mid, a) || !safeText(trade, 64) || trade == "" {
		return Receipt{}, ErrInvalid
	}
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return Receipt{}, e
	}
	defer tx.Rollback(ctx)
	r, _, e := s.receipt(ctx, tx, mid, trade)
	if e != nil {
		return r, e
	}
	return r, tx.Commit(ctx)
}
func (s *Store) intentProof(r intentRecord) ([]byte, error) {
	return s.digest("intent", []any{r.V.ID, r.V.MerchantID, r.V.OrderID, r.V.Source, r.V.ExternalNo, r.V.RequestNo, r.PlatformID, r.MerchantConfigID, r.Body, r.V.CreatedAt.UTC().Format(time.RFC3339Nano)})
}
func (s *Store) readJob(ctx context.Context, q reader, mid, id int64) (intentRecord, error) {
	var r intentRecord
	var raw []byte
	var valid bool
	e := q.QueryRow(ctx, `SELECT i.id,i.merchant_id,i.order_id,i.source,i.external_no,i.request_no,i.platform_config_id,i.merchant_config_id,i.body,i.proof_hash,i.created_at,EXISTS(SELECT 1 FROM printing_intent_invariants v WHERE v.id=i.id AND v.valid IS TRUE) FROM printing_intents i WHERE i.id=$1 AND ($2::bigint=0 OR i.merchant_id=$2)`, id, mid).Scan(&r.V.ID, &r.V.MerchantID, &r.V.OrderID, &r.V.Source, &r.V.ExternalNo, &r.V.RequestNo, &r.PlatformID, &r.MerchantConfigID, &raw, &r.Proof, &r.V.CreatedAt, &valid)
	if errors.Is(e, pgx.ErrNoRows) {
		return r, ErrNotFound
	}
	if e != nil {
		return r, e
	}
	if json.Unmarshal(raw, &r.Body) != nil {
		return r, ErrConflict
	}
	want, e := s.intentProof(r)
	if e != nil || !hmac.Equal(want, r.Proof) || !valid {
		return r, ErrConflict
	}
	f, e := s.source.PrintingPaidSource(ctx, q, r.V.OrderID)
	if e != nil || f.MerchantID != r.V.MerchantID || f.TradeNo != r.Body.Receipt.TradeNo || f.Amount != r.Body.Receipt.Amount {
		return r, ErrConflict
	}
	p, e := s.readPlatform(ctx, q, r.PlatformID)
	if e != nil {
		return r, e
	}
	m, e := s.readMerchant(ctx, q, r.V.MerchantID, r.MerchantConfigID)
	if e != nil {
		return r, e
	}
	if p.V.Provider != r.Body.Provider || m.V.Copies != r.Body.Copies || m.V.Voice != r.Body.Voice || mask(m.Device) != r.Body.DeviceMasked {
		return r, ErrConflict
	}
	r.V.Receipt = r.Body.Receipt
	r.V.TradeNo = f.TradeNo
	r.V.Copies = r.Body.Copies
	r.V.Voice = r.Body.Voice
	r.V.Provider = r.Body.Provider
	r.V.DeviceMasked = r.Body.DeviceMasked
	r.V.PreviousJobID = r.Body.PreviousJobID
	r.V.State = "queued"
	if e = s.attachTask(ctx, q, &r); e != nil {
		return r, e
	}
	var token, proof []byte
	var at, now time.Time
	var audited bool
	e = q.QueryRow(ctx, `SELECT c.token,c.created_at,c.proof_hash,clock_timestamp(),EXISTS(SELECT 1 FROM backoffice_audit a WHERE a.resource='printing-intents' AND a.resource_id=c.intent_id AND a.operation='claim' AND a.actor_role='system' AND a.actor_id=0 AND a.actor_name='receipt-printer') FROM printing_claims c WHERE intent_id=$1`, id).Scan(&token, &at, &proof, &now, &audited)
	if errors.Is(e, pgx.ErrNoRows) {
		return r, nil
	}
	if e != nil {
		return r, e
	}
	want, e = s.digest("claim", []any{id, token, at.UTC().Format(time.RFC3339Nano)})
	if e != nil || !hmac.Equal(want, proof) || !audited {
		return r, ErrConflict
	}
	r.V.CanRecover = false
	r.V.State = "sending"
	if now.Sub(at) > 30*time.Second {
		r.V.State = "unknown"
		r.V.Reason = "claim_recovered"
	}
	var state, reason string
	e = q.QueryRow(ctx, `SELECT z.state,z.reason,z.created_at,z.proof_hash,EXISTS(SELECT 1 FROM backoffice_audit a WHERE a.resource='printing-intents' AND a.resource_id=z.intent_id AND a.operation='result' AND a.actor_role='system' AND a.actor_id=0 AND a.actor_name='receipt-printer') FROM printing_results z WHERE intent_id=$1`, id).Scan(&state, &reason, &at, &proof, &audited)
	if errors.Is(e, pgx.ErrNoRows) {
		return r, nil
	}
	if e != nil {
		return r, e
	}
	want, e = s.digest("result", []any{id, state, reason, at.UTC().Format(time.RFC3339Nano)})
	if e != nil || !hmac.Equal(want, proof) || !audited {
		return r, ErrConflict
	}
	r.V.State = state
	r.V.Reason = reason
	return r, nil
}
func (s *Store) Job(ctx context.Context, mid, id int64, a Actor) (JobView, error) {
	if !owner(mid, a) || id <= 0 {
		return JobView{}, ErrInvalid
	}
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return JobView{}, e
	}
	defer tx.Rollback(ctx)
	r, e := s.readJob(ctx, tx, mid, id)
	if e != nil {
		return r.V, e
	}
	return r.V, tx.Commit(ctx)
}
func (s *Store) Jobs(ctx context.Context, mid, after int64, limit int, a Actor) ([]JobView, error) {
	if !owner(mid, a) || after < 0 || limit < 1 || limit > 100 {
		return nil, ErrInvalid
	}
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	rows, e := tx.Query(ctx, "SELECT id FROM printing_intents WHERE merchant_id=$1 AND id>$2 ORDER BY id LIMIT $3", mid, after, limit)
	if e != nil {
		return nil, e
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return nil, e
		}
		ids = append(ids, id)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	out := make([]JobView, 0, len(ids))
	for _, id := range ids {
		r, e := s.readJob(ctx, tx, mid, id)
		if e != nil {
			return nil, e
		}
		out = append(out, r.V)
	}
	return out, tx.Commit(ctx)
}

var externalPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

func (s *Store) OrderJobs(ctx context.Context, mid int64, trade string, after int64, limit int, a Actor) ([]JobView, error) {
	if !owner(mid, a) || after < 0 || limit < 1 || limit > 100 {
		return nil, ErrInvalid
	}
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	_, f, e := s.receipt(ctx, tx, mid, trade)
	if e != nil {
		return nil, e
	}
	rows, e := tx.Query(ctx, "SELECT id FROM printing_intents WHERE merchant_id=$1 AND order_id=$2 AND id>$3 ORDER BY id LIMIT $4", mid, f.OrderID, after, limit)
	if e != nil {
		return nil, e
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return nil, e
		}
		ids = append(ids, id)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	out := make([]JobView, 0, len(ids))
	for _, id := range ids {
		r, e := s.readJob(ctx, tx, mid, id)
		if e != nil {
			return nil, e
		}
		out = append(out, r.V)
	}
	return out, tx.Commit(ctx)
}

func (s *Store) CreateManual(ctx context.Context, mid int64, trade string, in ManualInput, a Actor) (JobView, error) {
	var out JobView
	if !owner(mid, a) || !externalPattern.MatchString(in.ExternalNo) || in.PreviousJobID < 0 || in.AcknowledgeDuplicate != (in.PreviousJobID > 0) {
		return out, ErrInvalid
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(ctx)
	var active bool
	if e = tx.QueryRow(ctx, "SELECT enabled FROM merchants WHERE id=$1 FOR NO KEY UPDATE", mid).Scan(&active); e != nil {
		return out, e
	}
	if !active {
		return out, ErrState
	}
	var old int64
	e = tx.QueryRow(ctx, "SELECT id FROM printing_intents WHERE merchant_id=$1 AND external_no=$2", mid, in.ExternalNo).Scan(&old)
	if e == nil {
		r, e := s.readJob(ctx, tx, mid, old)
		if e != nil {
			return out, e
		}
		if r.V.TradeNo != trade || r.V.Source != "manual" || r.V.PreviousJobID != in.PreviousJobID {
			return out, ErrConflict
		}
		return r.V, tx.Commit(ctx)
	}
	if !errors.Is(e, pgx.ErrNoRows) {
		return out, e
	}
	receipt, f, e := s.receipt(ctx, tx, mid, trade)
	if e != nil {
		return out, e
	}
	var latestUnknown int64
	var before int64
	for {
		rows, e := tx.Query(ctx, "SELECT id FROM printing_intents WHERE order_id=$1 AND ($2::bigint=0 OR id<$2) ORDER BY id DESC LIMIT 100", f.OrderID, before)
		if e != nil {
			return out, e
		}
		var ids []int64
		for rows.Next() {
			var id int64
			if e = rows.Scan(&id); e != nil {
				rows.Close()
				return out, e
			}
			ids = append(ids, id)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return out, e
		}
		for _, id := range ids {
			r, e := s.readJob(ctx, tx, mid, id)
			if e != nil {
				return out, e
			}
			if r.V.State == "queued" || r.V.State == "blocked" {
				return out, ErrState
			}
			if latestUnknown == 0 && (r.V.State == "unknown" || r.V.State == "sending") {
				latestUnknown = id
			}
		}
		if len(ids) < 100 {
			break
		}
		before = ids[len(ids)-1]
	}
	if latestUnknown > 0 && (!in.AcknowledgeDuplicate || in.PreviousJobID != latestUnknown) {
		return out, ErrState
	}
	if in.PreviousJobID > 0 && in.PreviousJobID != latestUnknown {
		return out, ErrConflict
	}
	var count int
	var recent bool
	if e = tx.QueryRow(ctx, "SELECT count(*),COALESCE(bool_or(created_at>clock_timestamp()-interval '1 second'),false) FROM printing_intents WHERE merchant_id=$1 AND source='manual' AND created_at>clock_timestamp()-interval '1 day'", mid).Scan(&count, &recent); e != nil {
		return out, e
	}
	if count >= 100 || recent {
		return out, ErrState
	}
	p, e := s.readPlatform(ctx, tx, 0)
	if e != nil {
		return out, e
	}
	m, e := s.readMerchant(ctx, tx, mid, 0)
	if e != nil {
		return out, e
	}
	if !p.V.Enabled || m.V.Mode == 0 {
		return out, ErrState
	}
	r, e := s.insertIntent(ctx, tx, f, receipt, p, m, "manual", in.ExternalNo, in.PreviousJobID, a)
	if e != nil {
		return out, e
	}
	if e = tx.Commit(ctx); e != nil {
		return out, e
	}
	return r.V, nil
}
func (s *Store) insertIntent(ctx context.Context, tx pgx.Tx, f payment.PrintingPaidSource, receipt Receipt, p platformRecord, m merchantRecord, source, external string, previous int64, a Actor) (intentRecord, error) {
	r := intentRecord{V: JobView{MerchantID: f.MerchantID, OrderID: f.OrderID, Source: source, ExternalNo: external}, PlatformID: p.V.ID, MerchantConfigID: m.V.ID, Body: intentBody{Receipt: receipt, Copies: m.V.Copies, Voice: m.V.Voice, Provider: p.V.Provider, DeviceMasked: mask(m.Device), PreviousJobID: previous, ActorName: a.Name}}
	if e := tx.QueryRow(ctx, "SELECT nextval(pg_get_serial_sequence('printing_intents','id')),transaction_timestamp()").Scan(&r.V.ID, &r.V.CreatedAt); e != nil {
		return r, e
	}
	nonce := make([]byte, 15)
	if _, e := rand.Read(nonce); e != nil {
		return r, e
	}
	r.V.RequestNo = "PJ" + hex.EncodeToString(nonce)
	proof, e := s.intentProof(r)
	if e != nil {
		return r, e
	}
	raw, _ := json.Marshal(r.Body)
	if _, e = tx.Exec(ctx, "INSERT INTO printing_intents(id,merchant_id,order_id,source,external_no,request_no,platform_config_id,merchant_config_id,body,proof_hash,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)", r.V.ID, f.MerchantID, f.OrderID, source, external, r.V.RequestNo, p.V.ID, m.V.ID, raw, proof, r.V.CreatedAt); e != nil {
		return r, e
	}
	if e = audit(ctx, tx, a, "printing-intents", r.V.ID, "create", map[string]string{"source": source, "request_no": r.V.RequestNo}); e != nil {
		return r, e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO jobs(event_key,kind,payload) VALUES('receipt-intent:'||$1::bigint::text,'receipt-print.send',jsonb_build_object('intent_id',$1::bigint::text))`, r.V.ID); e != nil {
		return r, e
	}
	return s.readJob(ctx, tx, f.MerchantID, r.V.ID)
}
func (s *Store) PrepareAutomatic(ctx context.Context, oid int64) (*JobView, error) {
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	f, e := s.source.PrintingPaidSource(ctx, tx, oid)
	if e != nil {
		return nil, ErrConflict
	}
	var active bool
	if e = tx.QueryRow(ctx, "SELECT enabled FROM merchants WHERE id=$1 FOR NO KEY UPDATE", f.MerchantID).Scan(&active); e != nil {
		return nil, e
	}
	var old int64
	e = tx.QueryRow(ctx, "SELECT id FROM printing_intents WHERE order_id=$1 AND source='auto'", oid).Scan(&old)
	if e == nil {
		r, e := s.readJob(ctx, tx, f.MerchantID, old)
		if e != nil {
			return nil, e
		}
		return &r.V, tx.Commit(ctx)
	}
	if !errors.Is(e, pgx.ErrNoRows) {
		return nil, e
	}
	var manualExists bool
	if e = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM printing_intents WHERE order_id=$1 AND source='manual')", oid).Scan(&manualExists); e != nil {
		return nil, e
	}
	if manualExists {
		return nil, tx.Commit(ctx)
	}
	if !active || !f.Fresh || f.SnapshotState != "captured" || !f.PlatformEnabled || f.Mode == 0 || f.Mode == 1 && f.Origin != "collection" || f.Origin == "recharge" {
		return nil, tx.Commit(ctx)
	}
	p, e := s.readPlatform(ctx, tx, f.PlatformConfigID)
	if e != nil {
		return nil, e
	}
	m, e := s.readMerchant(ctx, tx, f.MerchantID, f.MerchantConfigID)
	if e != nil {
		return nil, e
	}
	if !p.V.Enabled || m.V.Mode != f.Mode {
		return nil, ErrConflict
	}
	receipt, _, e := s.receipt(ctx, tx, f.MerchantID, f.TradeNo)
	if e != nil {
		return nil, e
	}
	r, e := s.insertIntent(ctx, tx, f, receipt, p, m, "auto", fmt.Sprintf("auto:%d", oid), 0, Actor{Role: "system", Name: "receipt-printer"})
	if e != nil {
		return nil, e
	}
	if e = tx.Commit(ctx); e != nil {
		return nil, e
	}
	return &r.V, nil
}

type PrintCommand struct {
	RequestNo  string
	Receipt    Receipt
	Device     string
	Copies     int
	Voice      bool
	Credential Credential
}
type Outcome struct{ State string }
type Connector interface {
	LocalOnly() bool
	Send(context.Context, PrintCommand) (Outcome, error)
}
type LocalFake struct{ State string }

func (LocalFake) LocalOnly() bool { return true }
func (f LocalFake) Send(ctx context.Context, c PrintCommand) (Outcome, error) {
	if e := ctx.Err(); e != nil {
		return Outcome{}, e
	}
	if _, e := RenderReceipt(c.Receipt); e != nil {
		return Outcome{}, e
	}
	if f.State == "unknown" {
		return Outcome{}, errors.New("local simulated lost response")
	}
	if f.State == "rejected" {
		return Outcome{State: "rejected"}, nil
	}
	return Outcome{State: "accepted"}, nil
}
func (s *Store) recordResult(ctx context.Context, id int64, state, reason string) error {
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var at time.Time
	if e = tx.QueryRow(ctx, "SELECT transaction_timestamp()").Scan(&at); e != nil {
		return e
	}
	proof, e := s.digest("result", []any{id, state, reason, at.UTC().Format(time.RFC3339Nano)})
	if e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, "INSERT INTO printing_results(intent_id,state,reason,proof_hash,created_at) VALUES($1,$2,$3,$4,$5)", id, state, reason, proof, at); e != nil {
		return e
	}
	if e = audit(ctx, tx, Actor{Role: "system", Name: "receipt-printer"}, "printing-intents", id, "result", map[string]string{"state": state, "reason": reason}); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func (s *Store) Dispatch(ctx context.Context, id int64, connector Connector) (JobView, error) {
	var out JobView
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended('printing-platform',0))"); e != nil {
		return out, e
	}
	r, e := s.readJob(ctx, tx, 0, id)
	if e != nil {
		return out, e
	}
	if e = tx.QueryRow(ctx, "SELECT id FROM merchants WHERE id=$1 FOR NO KEY UPDATE", r.V.MerchantID).Scan(&r.V.MerchantID); e != nil {
		return out, e
	}
	if _, e = tx.Exec(ctx, "SELECT id FROM printing_intents WHERE id=$1 FOR UPDATE", id); e != nil {
		return out, e
	}
	r, e = s.readJob(ctx, tx, 0, id)
	if e != nil {
		return out, e
	}
	if r.V.State != "queued" {
		return r.V, tx.Commit(ctx)
	}
	p, e := s.readPlatform(ctx, tx, 0)
	if e != nil {
		return out, e
	}
	m, e := s.readMerchant(ctx, tx, r.V.MerchantID, 0)
	if e != nil {
		return out, e
	}
	scopeOK := p.V.ID == r.PlatformID && m.V.ID == r.MerchantConfigID && p.V.Enabled && m.V.Mode > 0
	if scopeOK && !printingConnectorMatches(p.V.Provider, connector) {
		// This is proven unsent. Leave the printing intent queued for its
		// correctly configured worker instead of consuming its one send claim.
		return r.V, ErrUnsupported
	}
	token := make([]byte, 32)
	if _, e = rand.Read(token); e != nil {
		return out, e
	}
	var at time.Time
	if e = tx.QueryRow(ctx, "SELECT transaction_timestamp()").Scan(&at); e != nil {
		return out, e
	}
	proof, e := s.digest("claim", []any{id, token, at.UTC().Format(time.RFC3339Nano)})
	if e != nil {
		return out, e
	}
	if _, e = tx.Exec(ctx, "INSERT INTO printing_claims(intent_id,token,proof_hash,created_at) VALUES($1,$2,$3,$4)", id, token, proof, at); e != nil {
		return out, e
	}
	if e = audit(ctx, tx, Actor{Role: "system", Name: "receipt-printer"}, "printing-intents", id, "claim", map[string]string{"state": "claimed"}); e != nil {
		return out, e
	}
	if e = tx.Commit(ctx); e != nil {
		return out, e
	}
	state, reason := "rejected", "scope_changed"
	if scopeOK {
		if !printingConnectorMatches(p.V.Provider, connector) {
			reason = "connector_unavailable"
		} else {
			outcome, sendErr := connector.Send(ctx, PrintCommand{RequestNo: r.V.RequestNo, Receipt: r.V.Receipt, Device: m.Device, Copies: r.V.Copies, Voice: r.V.Voice, Credential: p.Credential})
			state, reason = "unknown", "transport_unknown"
			if sendErr == nil && outcome.State == "accepted" {
				state, reason = "accepted", "gateway_accepted"
			} else if sendErr == nil && outcome.State == "rejected" {
				state, reason = "rejected", "gateway_rejected"
			}
		}
	}
	saveCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if e = s.recordResult(saveCtx, id, state, reason); e != nil {
		return out, e
	}
	read, e := s.readJob(saveCtx, s.pool, r.V.MerchantID, id)
	return read.V, e
}

func printingConnectorMatches(provider string, connector Connector) bool {
	if connector == nil {
		return false
	}
	if provider == "local_fake" {
		return connector.LocalOnly()
	}
	// The unexported method seals external provider selection to this package;
	// a generic Connector with LocalOnly=false cannot redirect a trusted intent.
	bound, ok := connector.(interface{ printerProvider() string })
	return provider == "excelsecu" && ok && bound.printerProvider() == provider && !connector.LocalOnly()
}
