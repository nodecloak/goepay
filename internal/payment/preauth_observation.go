package payment

import (
	"context"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	adapter "goepay/internal/adapters/payment"
	"strings"
	"time"
)

var ErrPreauthObservation = errors.New("authorization observation is unavailable or no longer authorized")

type PreauthObservationInput struct {
	MerchantID      int64  `json:"merchant_id,string"`
	TradeNo         string `json:"trade_no"`
	ClientRequestNo string `json:"client_request_no"`
}
type PreauthorizationObservation struct {
	ID                                    string     `json:"request_id"`
	ClientRequestNo                       string     `json:"client_request_no"`
	MerchantID                            int64      `json:"merchant_id,string"`
	OrderID                               int64      `json:"order_id,string"`
	AttemptID                             int64      `json:"attempt_id,string"`
	ChannelID                             int64      `json:"channel_id,string"`
	TradeNo                               string     `json:"trade_no"`
	AppID                                 string     `json:"app_id"`
	AuthNo                                string     `json:"auth_no"`
	BuyerID                               string     `json:"buyer_id"`
	Amount                                int64      `json:"amount,string"`
	Status                                string     `json:"status"`
	RestKnown                             bool       `json:"rest_known"`
	RestAmount                            *int64     `json:"rest_amount,string,omitempty"`
	ProviderStatus                        string     `json:"provider_status,omitempty"`
	OrderStatus                           string     `json:"order_status,omitempty"`
	OperationType                         string     `json:"operation_type,omitempty"`
	ObservedAt                            *time.Time `json:"observed_at,omitempty"`
	SourceVerified                        bool       `json:"source_verified"`
	original                              IdentityLookup
	merchantRevision                      int64
	expiresAt                             time.Time
	accountHash, sourceProof, resultProof []byte
}

const preauthObservationColumns = `id,client_request_no,merchant_id,order_id,attempt_id,channel_id,trade_no,app_id,auth_no,buyer_id,amount,account_hash,merchant_revision,session_hash,actor_username,actor_role,actor_id,actor_auth_version,expires_at,source_proof,status,rest_amount,provider_status,order_status,operation_type,observed_at,result_proof`

func observationID68(v string) bool {
	if len(v) != 64 || strings.ToLower(v) != v {
		return false
	}
	_, e := hex.DecodeString(v)
	return e == nil
}
func (s *Store) observationSource(q PreauthorizationObservation) []byte {
	raw, _ := json.Marshal([]any{q.ID, q.ClientRequestNo, q.MerchantID, q.OrderID, q.AttemptID, q.ChannelID, q.TradeNo, q.AppID, q.AuthNo, q.BuyerID, q.Amount, q.accountHash, q.merchantRevision, q.original.sessionHash, q.original.actorUsername, q.original.actorRole, q.original.actorID, q.original.actorVersion, q.expiresAt.UTC().Format(time.RFC3339Nano)})
	return s.oauthHash("preauth-observation-source-v1", string(raw))
}
func (s *Store) observationResult(q PreauthorizationObservation) []byte {
	var at string
	if q.ObservedAt != nil {
		at = q.ObservedAt.UTC().Format(time.RFC3339Nano)
	}
	raw, _ := json.Marshal([]any{q.ID, q.sourceProof, q.Status, q.RestAmount, q.ProviderStatus, q.OrderStatus, q.OperationType, at})
	return s.oauthHash("preauth-observation-result-v1", string(raw))
}
func (s *Store) scanObservation(row pgx.Row) (PreauthorizationObservation, error) {
	var q PreauthorizationObservation
	e := row.Scan(&q.ID, &q.ClientRequestNo, &q.MerchantID, &q.OrderID, &q.AttemptID, &q.ChannelID, &q.TradeNo, &q.AppID, &q.AuthNo, &q.BuyerID, &q.Amount, &q.accountHash, &q.merchantRevision, &q.original.sessionHash, &q.original.actorUsername, &q.original.actorRole, &q.original.actorID, &q.original.actorVersion, &q.expiresAt, &q.sourceProof, &q.Status, &q.RestAmount, &q.ProviderStatus, &q.OrderStatus, &q.OperationType, &q.ObservedAt, &q.resultProof)
	if e != nil || len(q.sourceProof) != 32 || subtle.ConstantTimeCompare(q.sourceProof, s.observationSource(q)) != 1 {
		return q, ErrPreauthObservation
	}
	if q.Status != "processing" && (len(q.resultProof) != 32 || subtle.ConstantTimeCompare(q.resultProof, s.observationResult(q)) != 1) {
		return q, ErrPreauthObservation
	}
	q.RestKnown = q.Status == "verified"
	q.SourceVerified = q.RestKnown
	q.original.MerchantID = q.MerchantID
	return q, nil
}
func (s *Store) observationRow(ctx context.Context, tx pgx.Tx, where string, args ...any) (PreauthorizationObservation, error) {
	return s.scanObservation(tx.QueryRow(ctx, "SELECT "+preauthObservationColumns+" FROM preauth_observations WHERE "+where+" FOR UPDATE", args...))
}
func (s *Store) observationEvent(ctx context.Context, tx pgx.Tx, q PreauthorizationObservation, op string) error {
	proof := s.oauthHash("preauth-observation-event-v1", q.ID+"/"+hex.EncodeToString(q.sourceProof)+"/"+op)
	_, e := tx.Exec(ctx, `INSERT INTO preauth_observation_events(observation_id,operation,proof)VALUES($1,$2,$3)`, q.ID, op, proof)
	return e
}
func (s *Store) observationAuditValid(ctx context.Context, tx remainingEvidenceReader, q PreauthorizationObservation) error {
	rows, e := tx.Query(ctx, `SELECT operation,proof FROM preauth_observation_events WHERE observation_id=$1`, q.ID)
	if e != nil {
		return e
	}
	defer rows.Close()
	seen := map[string]bool{}
	for rows.Next() {
		var op string
		var proof []byte
		if rows.Scan(&op, &proof) != nil || seen[op] || subtle.ConstantTimeCompare(proof, s.oauthHash("preauth-observation-event-v1", q.ID+"/"+hex.EncodeToString(q.sourceProof)+"/"+op)) != 1 {
			return ErrPreauthObservation
		}
		seen[op] = true
	}
	if rows.Err() != nil || !seen["claimed"] || q.Status == "processing" && len(seen) != 1 || q.Status != "processing" && (!seen[q.Status] || len(seen) != 2) {
		return ErrPreauthObservation
	}
	return nil
}
func observationSession(ctx context.Context, tx pgx.Tx, token, role string, mid int64) (IdentityLookup, error) {
	q := IdentityLookup{IdentityLookupBinding: IdentityLookupBinding{MerchantID: mid}, sessionHash: lookupSessionHash(token)}
	if len(token) != 43 || lookupSession(ctx, tx, &q) != nil || q.actorRole != role {
		return q, ErrPreauthObservation
	}
	return q, nil
}
func (s *Store) observationOrigin(ctx context.Context, tx remainingEvidenceReader, q PreauthorizationObservation) error {
	var ok bool
	e := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM orders o JOIN provider_attempts p ON p.order_id=o.id JOIN payment_authorizations a ON a.order_id=o.id JOIN provider_attempt_accounts ac ON ac.attempt_id=p.id WHERE o.id=$1 AND o.merchant_id=$2 AND o.trade_no=$3 AND o.amount=$4 AND o.type='alipay' AND p.id=$5 AND p.channel_id=$6 AND p.method='preauth' AND a.channel_id=$6 AND a.amount=$4 AND a.auth_no=$7 AND a.buyer_id=$8 AND ac.account_hash=$9)`, q.OrderID, q.MerchantID, q.TradeNo, q.Amount, q.AttemptID, q.ChannelID, q.AuthNo, q.BuyerID, q.accountHash).Scan(&ok)
	if e != nil || !ok {
		return ErrPreauthObservation
	}
	return nil
}
func (s *Store) observationAlive(ctx context.Context, tx pgx.Tx, q PreauthorizationObservation) error {
	original := q.original
	if lookupSession(ctx, tx, &original) != nil {
		return ErrPreauthObservation
	}
	var revision int64
	var alive bool
	if e := tx.QueryRow(ctx, `SELECT config_revision,$2::timestamptz>clock_timestamp() FROM merchants WHERE id=$1`, q.MerchantID, q.expiresAt).Scan(&revision, &alive); e != nil || revision != q.merchantRevision || !alive {
		return ErrPreauthObservation
	}
	return s.observationOrigin(ctx, tx, q)
}
func observationProvider(p adapter.Provider) (adapter.PreauthorizationProvider, string, string, error) {
	if p == nil || p.ID() != "alipay" || !p.Capabilities().Query {
		return nil, "", "", ErrPreauthObservation
	}
	v, ok := p.(adapter.PreauthorizationProvider)
	app, aok := p.(adapter.ApplicationIdentity)
	account, cok := p.(adapter.TransferAccountIdentity)
	if !ok || !aok || !cok || app.ApplicationID() == "" || !strings.HasPrefix(account.TransferAccountIdentity(), "seller:") || len(account.TransferAccountIdentity()) <= 7 {
		return nil, "", "", ErrPreauthObservation
	}
	capable := false
	for _, m := range p.Capabilities().Methods {
		capable = capable || m == "preauth"
	}
	if !capable {
		return nil, "", "", ErrPreauthObservation
	}
	scope, e := adapter.TransferAccountScope(p)
	return v, app.ApplicationID(), scope, e
}
func (s *Store) beginObservation(ctx context.Context, in PreauthObservationInput, token, role, app, scope string) (PreauthorizationObservation, bool, error) {
	var q PreauthorizationObservation
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return q, false, e
	}
	defer tx.Rollback(ctx)
	actor, e := observationSession(ctx, tx, token, role, in.MerchantID)
	if e != nil {
		return q, false, e
	}
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,68))`, in.TradeNo+"/"+in.ClientRequestNo); e != nil {
		return q, false, e
	}
	var exists bool
	if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM preauth_observations WHERE merchant_id=$1 AND trade_no=$2 AND client_request_no=$3)`, in.MerchantID, in.TradeNo, in.ClientRequestNo).Scan(&exists); e != nil {
		return q, false, e
	}
	if exists {
		q, e = s.observationRow(ctx, tx, "merchant_id=$1 AND trade_no=$2 AND client_request_no=$3", in.MerchantID, in.TradeNo, in.ClientRequestNo)
		if e != nil {
			return q, false, e
		}
		if e = s.observationAuditValid(ctx, tx, q); e != nil {
			return q, false, e
		}
		return q, false, tx.Commit(ctx)
	}
	q.MerchantID = in.MerchantID
	q.TradeNo = in.TradeNo
	q.ClientRequestNo = in.ClientRequestNo
	q.AppID = app
	q.original = actor
	q.Status = "processing"
	q.ID, e = lookupNonce()
	if e != nil {
		return q, false, e
	}
	e = tx.QueryRow(ctx, `SELECT o.id,p.id,p.channel_id,a.auth_no,a.buyer_id,a.amount,ac.account_hash,m.config_revision,LEAST($3::timestamptz,statement_timestamp()+interval '5 minutes') FROM orders o JOIN merchants m ON m.id=o.merchant_id JOIN provider_attempts p ON p.order_id=o.id JOIN payment_authorizations a ON a.order_id=o.id JOIN provider_attempt_accounts ac ON ac.attempt_id=p.id WHERE o.merchant_id=$1 AND o.trade_no=$2 AND o.amount=a.amount AND o.type='alipay' AND p.method='preauth' AND p.channel_id=a.channel_id FOR SHARE OF o,p,a,ac,m`, in.MerchantID, in.TradeNo, actor.ExpiresAt).Scan(&q.OrderID, &q.AttemptID, &q.ChannelID, &q.AuthNo, &q.BuyerID, &q.Amount, &q.accountHash, &q.merchantRevision, &q.expiresAt)
	if e != nil {
		return q, false, ErrPreauthObservation
	}
	expected, e := s.providerAccountHash(scope)
	if e != nil || subtle.ConstantTimeCompare(expected, q.accountHash) != 1 {
		return q, false, ErrPreauthObservation
	}
	q.sourceProof = s.observationSource(q)
	_, e = tx.Exec(ctx, `INSERT INTO preauth_observations(id,client_request_no,merchant_id,order_id,attempt_id,channel_id,trade_no,app_id,auth_no,buyer_id,amount,account_hash,merchant_revision,session_hash,actor_username,actor_role,actor_id,actor_auth_version,expires_at,source_proof)VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20)`, q.ID, q.ClientRequestNo, q.MerchantID, q.OrderID, q.AttemptID, q.ChannelID, q.TradeNo, q.AppID, q.AuthNo, q.BuyerID, q.Amount, q.accountHash, q.merchantRevision, q.original.sessionHash, q.original.actorUsername, q.original.actorRole, q.original.actorID, q.original.actorVersion, q.expiresAt, q.sourceProof)
	if e != nil {
		return q, false, e
	}
	if e = s.observationEvent(ctx, tx, q, "claimed"); e != nil {
		return q, false, e
	}
	if e = s.observationAlive(ctx, tx, q); e != nil {
		return q, false, e
	}
	return q, true, tx.Commit(ctx)
}

// A sealed observation has no public constructor or JSON ingestion path. Only
// this trusted provider-query flow constructs it after authenticated validation.
type verifiedPreauthObservation struct{ event adapter.AuthorizationEvent }

func sealPreauthObservation(q PreauthorizationObservation, e adapter.AuthorizationEvent) (verifiedPreauthObservation, error) {
	if e.TradeNo != q.TradeNo || e.RequestNo != q.TradeNo || e.AuthNo != q.AuthNo || e.BuyerID != q.BuyerID || e.Amount != q.Amount || !e.RestAmountKnown || e.RestAmount < 0 || e.RestAmount > q.Amount || e.OperationType != "FREEZE" || e.State != "SUCCESS" || (e.OrderStatus != "AUTHORIZED" && e.OrderStatus != "FINISH") {
		return verifiedPreauthObservation{}, adapter.ErrAuthentication
	}
	return verifiedPreauthObservation{e}, nil
}
func (s *Store) finishObservation(ctx context.Context, id string, sealed *verifiedPreauthObservation, check func(context.Context) error) error {
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	q, e := s.observationRow(ctx, tx, "id=$1", id)
	if e != nil {
		return e
	}
	if e = s.observationAuditValid(ctx, tx, q); e != nil {
		return e
	}
	if q.Status != "processing" {
		return tx.Commit(ctx)
	}
	if sealed != nil {
		if check == nil || check(ctx) != nil || s.observationAlive(ctx, tx, q) != nil {
			return ErrPreauthObservation
		}
		if _, e = sealPreauthObservation(q, sealed.event); e != nil {
			return e
		}
		remaining := sealed.event.RestAmount
		q.RestAmount = &remaining
		q.ProviderStatus = sealed.event.State
		q.OrderStatus = sealed.event.OrderStatus
		q.OperationType = sealed.event.OperationType
		q.Status = "verified"
	} else {
		q.Status = "unknown"
	}
	var at time.Time
	if e = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&at); e != nil {
		return e
	}
	q.ObservedAt = &at
	q.resultProof = s.observationResult(q)
	if _, e = tx.Exec(ctx, `UPDATE preauth_observations SET status=$2,rest_amount=$3,provider_status=$4,order_status=$5,operation_type=$6,observed_at=$7,result_proof=$8 WHERE id=$1`, q.ID, q.Status, q.RestAmount, q.ProviderStatus, q.OrderStatus, q.OperationType, q.ObservedAt, q.resultProof); e != nil {
		return e
	}
	if e = s.observationEvent(ctx, tx, q, q.Status); e != nil {
		return e
	}
	if sealed != nil && (check(ctx) != nil || s.observationAlive(ctx, tx, q) != nil) {
		return ErrPreauthObservation
	}
	return tx.Commit(ctx)
}
func (g ProviderFlow) observationGuard(ctx context.Context, q PreauthorizationObservation, check func(context.Context) error) error {
	if check == nil || check(ctx) != nil {
		return ErrPreauthObservation
	}
	_, a, p, e := g.owned(ctx, q.MerchantID, q.TradeNo)
	if e != nil || a.ID != q.AttemptID || a.ChannelID != q.ChannelID {
		return ErrPreauthObservation
	}
	_, app, scope, e := observationProvider(p)
	if e != nil || app != q.AppID {
		return ErrPreauthObservation
	}
	hash, e := g.Store.providerAccountHash(scope)
	if e != nil || subtle.ConstantTimeCompare(hash, q.accountHash) != 1 {
		return ErrPreauthObservation
	}
	tx, e := g.Store.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if e = g.Store.observationAlive(ctx, tx, q); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func (g ProviderFlow) observationUnknown(id string) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = g.Store.finishObservation(ctx, id, nil, nil)
}
func (g ProviderFlow) ObservePreauthorization(ctx context.Context, in PreauthObservationInput, token, role string, check func(context.Context) error) (PreauthorizationObservation, error) {
	var empty PreauthorizationObservation
	if g.Store == nil || check == nil || in.MerchantID <= 0 || in.TradeNo == "" || len(in.TradeNo) > 128 || !observationID68(in.ClientRequestNo) || len(token) != 43 {
		return empty, ErrPreauthObservation
	}
	if e := check(ctx); e != nil {
		return empty, e
	}
	_, attempt, p, e := g.owned(ctx, in.MerchantID, in.TradeNo)
	if e != nil || attempt.Method != "preauth" {
		return empty, ErrPreauthObservation
	}
	v, app, scope, e := observationProvider(p)
	if e != nil {
		return empty, e
	}
	q, owner, e := g.Store.beginObservation(ctx, in, token, role, app, scope)
	if e != nil {
		return empty, e
	}
	if !owner {
		return g.ReadPreauthorizationObservation(ctx, q.ID, token, role, check)
	}
	if e = g.observationGuard(ctx, q, check); e != nil {
		g.observationUnknown(q.ID)
		return empty, e
	}
	event, queryErr := v.QueryAuthorization(ctx, adapter.AuthorizationRequest{TradeNo: q.TradeNo, RequestNo: q.TradeNo, AuthNo: q.AuthNo, Amount: q.Amount})
	if e = g.observationGuard(ctx, q, check); e != nil {
		g.observationUnknown(q.ID)
		return empty, e
	}
	sealed, sealErr := sealPreauthObservation(q, event)
	if queryErr != nil || sealErr != nil {
		g.observationUnknown(q.ID)
		return g.ReadPreauthorizationObservation(ctx, q.ID, token, role, check)
	}
	if e = g.Store.finishObservation(ctx, q.ID, &sealed, check); e != nil {
		g.observationUnknown(q.ID)
		return empty, e
	}
	return g.ReadPreauthorizationObservation(ctx, q.ID, token, role, check)
}
func (g ProviderFlow) ReadPreauthorizationObservation(ctx context.Context, id, token, role string, check func(context.Context) error) (PreauthorizationObservation, error) {
	var empty PreauthorizationObservation
	if g.Store == nil || check == nil || !observationID68(id) || len(token) != 43 {
		return empty, ErrPreauthObservation
	}
	if e := check(ctx); e != nil {
		return empty, e
	}
	tx, e := g.Store.pool.Begin(ctx)
	if e != nil {
		return empty, e
	}
	defer tx.Rollback(ctx)
	q, e := g.Store.observationRow(ctx, tx, "id=$1", id)
	if e != nil {
		return empty, e
	}
	if _, e = observationSession(ctx, tx, token, role, q.MerchantID); e != nil {
		return empty, e
	}
	if e = g.Store.observationOrigin(ctx, tx, q); e != nil {
		return empty, e
	}
	if e = g.Store.observationAuditValid(ctx, tx, q); e != nil {
		return empty, e
	}
	_, a, p, e := g.owned(ctx, q.MerchantID, q.TradeNo)
	if e != nil || a.ID != q.AttemptID {
		return empty, ErrPreauthObservation
	}
	_, app, scope, e := observationProvider(p)
	if e != nil || app != q.AppID {
		return empty, ErrPreauthObservation
	}
	hash, e := g.Store.providerAccountHash(scope)
	if e != nil || subtle.ConstantTimeCompare(hash, q.accountHash) != 1 {
		return empty, ErrPreauthObservation
	}
	if e = check(ctx); e != nil {
		return empty, e
	}
	if e = tx.Commit(ctx); e != nil {
		return empty, e
	}
	return q, nil
}
