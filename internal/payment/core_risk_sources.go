package payment

import (
	"context"
	"crypto/hmac"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"

	"goepay/internal/riskpolicy"
)

type CoreRiskRequest struct {
	PeerIP, BuyerIP, TransportIP, Source string
	Forwarded                            bool
}
type coreRiskContextKey struct{}
type coreRiskBuyerContextKey struct{}

func withCoreRiskBuyer(ctx context.Context, buyer string) context.Context {
	return context.WithValue(ctx, coreRiskBuyerContextKey{}, buyer)
}
func (s *Store) coreRiskBuyerKey(kind, buyer string) (string, error) {
	if buyer == "" || kind != "alipay" && kind != "wxpay" {
		return "", nil
	}
	if len(buyer) > 256 {
		return "", ErrInvalid
	}
	p, e := s.referralDigest("risk102/buyer", []string{kind, buyer})
	return hex.EncodeToString(p), e
}

func WithCoreRiskRequest(ctx context.Context, in CoreRiskRequest) context.Context {
	return context.WithValue(ctx, coreRiskContextKey{}, in)
}

// WithCoreRiskCustomerIP preserves the separately trusted network peer while
// recording whether the original customer address was direct or merchant-declared.
func WithCoreRiskCustomerIP(ctx context.Context, ip, source string) context.Context {
	in, _ := ctx.Value(coreRiskContextKey{}).(CoreRiskRequest)
	in.BuyerIP = ip
	in.Source = source
	return WithCoreRiskRequest(ctx, in)
}

type coreRiskOrigin struct {
	CustomerCharge104                                                 *orderChargeCommitment104 `json:",omitempty"`
	ScanRegion, ScanBaselineIP, ScanBaselineSource                    string
	Group                                                             *coreRiskGroupSource
	OrderID, AttemptID, MerchantID, ChannelID, Amount, PolicyRevision int64
	Type, Method, Transaction                                         string
	BuyerKey                                                          string
	PeerIP, BuyerIP, TransportIP, Source                              string
	Forwarded                                                         bool
	IPs                                                               []string
	AdmittedAt                                                        time.Time
}

func (s *Store) coreRiskOriginProof(f coreRiskOrigin) ([]byte, error) {
	return s.referralDigest("risk102/origin", f)
}
func (s *Store) coreRiskIPKey(ip string) (string, error) {
	p, e := s.referralDigest("risk102/ip", ip)
	return hex.EncodeToString(p), e
}
func coreRiskDBNow(ctx context.Context, q referralReader) (time.Time, error) {
	var t time.Time
	e := q.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&t)
	return t.UTC(), e
}
func (s *Store) coreRiskSchema(ctx context.Context, tx pgx.Tx) (bool, error) {
	var migrations, audits, marker bool
	var tables int
	if e := tx.QueryRow(ctx, `SELECT to_regclass(current_schema()||'.schema_migrations')IS NOT NULL,to_regclass(current_schema()||'.backoffice_audit')IS NOT NULL,EXISTS(SELECT 1 FROM information_schema.columns WHERE table_schema=current_schema() AND table_name='provider_attempts' AND column_name='core_risk_required')`).Scan(&migrations, &audits, &marker); e != nil {
		return false, e
	}
	if e := tx.QueryRow(ctx, `SELECT count(*)FROM unnest(ARRAY['core_payment_risk_policy','core_risk_origins','core_risk_successes','core_risk_actions','core_risk_check_runs','core_risk_notices','core_risk_notice_claims','core_risk_notice_results','core_payment_risk_groups'])name WHERE to_regclass(current_schema()||'.'||name)IS NOT NULL`).Scan(&tables); e != nil {
		return false, e
	}
	if tables == 9 && marker {
		return true, nil
	}
	if tables > 0 || marker {
		return false, ErrConflict
	}
	if audits {
		var audit bool
		if e := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM backoffice_audit WHERE resource IN('core-payment-risk','core-payment-risk-groups','core-risk-origins','core-risk-success','core-risk-actions','core-risk-check','core-risk-notices'))`).Scan(&audit); e != nil {
			return false, e
		}
		if audit {
			return false, ErrConflict
		}
	}
	if migrations {
		var registered bool
		if e := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE name='000102_core_payment_risk.up.sql')`).Scan(&registered); e != nil {
			return false, e
		}
		if registered {
			return false, ErrConflict
		}
	}
	return false, nil
}
func (s *Store) readCoreRiskOrigin(ctx context.Context, q referralReader, id int64) (coreRiskOrigin, []byte, error) {
	var raw, proof []byte
	var attempt int64
	var ips []string
	var at time.Time
	if e := q.QueryRow(ctx, `SELECT attempt_id,ips,admitted_at,facts,proof_hash FROM core_risk_origins WHERE order_id=$1`, id).Scan(&attempt, &ips, &at, &raw, &proof); e != nil {
		return coreRiskOrigin{}, nil, ErrConflict
	}
	f, e := s.decodeCoreRiskOriginFact(raw, proof, id, attempt, ips, at)
	if e != nil {
		return f, nil, e
	}
	var original bool
	if e = q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM backoffice_audit WHERE resource='core-risk-origins' AND resource_id=$1 AND operation='admit' AND actor_role='system' AND actor_id=0 AND actor_name='core-risk' AND changes->>'attempt_id'=$2 AND changes->>'source_hash'=$3 AND changes->>'admitted_at'=$4)`, id, fmt.Sprint(attempt), hex.EncodeToString(proof), coreRiskTimeKey(f.AdmittedAt)).Scan(&original); e != nil || !original {
		return f, nil, ErrConflict
	}
	if e = q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM provider_attempts a JOIN orders o ON o.id=a.order_id WHERE a.id=$1 AND a.order_id=$2 AND a.channel_id=$3 AND a.method=$4 AND a.core_risk_required AND o.merchant_id=$5 AND o.type=$6 AND o.amount=$7)`, f.AttemptID, id, f.ChannelID, f.Method, f.MerchantID, f.Type, f.Amount).Scan(&original); e != nil || !original {
		return f, nil, ErrConflict
	}
	o, e := scanOrder(q.QueryRow(ctx, "SELECT "+orderColumns+" FROM orders WHERE id=$1", id))
	if e != nil {
		return f, nil, e
	}
	if e = s.authenticateFrozenCharge104(ctx, q, o, f.CustomerCharge104); e != nil {
		return f, nil, e
	}
	return f, proof, nil
}
func (s *Store) bindCoreRiskOrigin(ctx context.Context, tx pgx.Tx, f *coreRiskOrigin, id int64) error {
	if f == nil {
		return nil
	}
	f.AttemptID = id
	proof, e := s.coreRiskOriginProof(*f)
	if e != nil {
		return e
	}
	raw, e := json.Marshal(f)
	if e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO core_risk_origins(order_id,attempt_id,ips,admitted_at,facts,proof_hash)VALUES($1,$2,$3,$4,$5::jsonb,$6)`, f.OrderID, id, f.IPs, f.AdmittedAt, string(raw), proof); e != nil {
		return e
	}
	keys := []string{}
	for _, ip := range f.IPs {
		key, e := s.coreRiskIPKey(ip)
		if e != nil {
			return e
		}
		keys = append(keys, key)
	}
	keyJSON, _ := json.Marshal(keys)
	if _, e = tx.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes)VALUES('system',0,'core-risk','core-risk-origins',$1,'admit',jsonb_build_object('attempt_id',$2::text,'source_hash',$3::text,'subject_keys',$4::jsonb,'buyer_key',$5::text,'admitted_at',$6::text))`, f.OrderID, fmt.Sprint(id), hex.EncodeToString(proof), string(keyJSON), f.BuyerKey, coreRiskTimeKey(f.AdmittedAt)); e != nil {
		return e
	}
	_, e = tx.Exec(ctx, `UPDATE provider_attempts SET core_risk_required=true WHERE id=$1`, id)
	return e
}
func (s *Store) prepareCoreRiskOrigin(ctx context.Context, tx pgx.Tx, o Order, channelID int64, method string) (*coreRiskOrigin, error) {
	ready, e := s.coreRiskSchema(ctx, tx)
	if e != nil || !ready {
		return nil, e
	}
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock_shared(hashtextextended('goepay/core-payment-risk-policy',0))`); e != nil {
		return nil, e
	}
	p, configured, e := s.coreRiskPolicyTx(ctx, tx, false)
	if e != nil {
		return nil, e
	}
	request, present := ctx.Value(coreRiskContextKey{}).(CoreRiskRequest)
	effective, group, e := s.coreRiskGroupEffectiveTx(ctx, tx, o.MerchantID, p.Settings, configured)
	if e != nil {
		return nil, e
	}
	if !present && !configured && group == nil {
		return nil, nil
	}
	if request.BuyerIP == "" && request.Source != "signed_api_clientip" {
		_ = tx.QueryRow(ctx, `SELECT client_ip FROM order_reporting WHERE order_id=$1`, o.ID).Scan(&request.BuyerIP)
	}
	if request.PeerIP == "" {
		request.PeerIP = request.BuyerIP
	}
	seen := map[string]bool{}
	ips := []string{}
	for index, raw := range []string{request.PeerIP, request.BuyerIP} {
		if raw == "" {
			continue
		}
		ip, e := riskpolicy.CanonicalIP(raw)
		if e != nil {
			return nil, ErrInvalid
		}
		if index == 0 {
			request.PeerIP = ip.String()
		} else {
			request.BuyerIP = ip.String()
		}
		if !seen[ip.String()] {
			seen[ip.String()] = true
			ips = append(ips, ip.String())
		}
	}
	sort.Strings(ips)
	if len(ips) == 0 && (p.Settings["pay_iplimit"] != "" || p.Settings["pay_ip_interval"] != "") {
		return nil, ErrState
	}
	for _, ip := range ips {
		if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "goepay/core-risk-ip/"+ip); e != nil {
			return nil, e
		}
	}
	buyer, _ := ctx.Value(coreRiskBuyerContextKey{}).(string)
	buyerKey, e := s.coreRiskBuyerKey(o.Type, buyer)
	if e != nil {
		return nil, e
	}
	if buyerKey != "" {
		if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "goepay/core-risk-buyer/"+buyerKey); e != nil {
			return nil, e
		}
	}
	if e = s.lockCoreRiskAdmissionActions(ctx, tx, ips, buyerKey, o.MerchantID, channelID); e != nil {
		return nil, e
	}
	now, e := coreRiskDBNow(ctx, tx)
	if e != nil {
		return nil, e
	}
	if e = s.checkCoreRiskActions(ctx, tx, ips, buyerKey, o.MerchantID, channelID, now); e != nil {
		return nil, e
	}
	for _, ip := range ips {
		if riskpolicy.IPWhitelisted(p.Settings, ip) {
			continue
		}
		if e = s.checkCoreRiskIP(ctx, tx, p.Settings, ip, now); e != nil {
			return nil, e
		}
	}
	if e = s.checkCoreRiskBuyer(ctx, tx, p.Settings, buyerKey, now); e != nil {
		return nil, e
	}
	if e = s.checkCoreRiskGeo(effective, ips); e != nil {
		return nil, e
	}
	f := &coreRiskOrigin{OrderID: o.ID, MerchantID: o.MerchantID, ChannelID: channelID, Amount: o.Amount, PolicyRevision: p.Revision, Type: o.Type, Method: method, IPs: ips, AdmittedAt: now}
	f.BuyerKey = buyerKey
	f.ScanRegion = effective["check_pay_regoin"]
	f.ScanBaselineIP, f.ScanBaselineSource = request.BuyerIP, request.Source
	if (method == "native" || method == "wap" || method == "h5") && f.ScanRegion != "0" && (f.ScanBaselineIP == "" || !s.coreRiskGeoAvailable()) {
		return nil, ErrState
	}
	charge, chargeProof, e := s.authenticatedOrderCharge104(ctx, tx, o)
	if e != nil {
		return nil, e
	}
	if len(chargeProof) != 0 {
		f.CustomerCharge104 = &orderChargeCommitment104{Charge: charge, SourceProof: chargeProof}
	}
	f.Group = group
	f.PeerIP, f.BuyerIP, f.Source, f.Forwarded = request.PeerIP, request.BuyerIP, request.Source, request.Forwarded
	if f.Source == "" {
		f.Source = "internal_order_reporting"
	}
	if request.TransportIP != "" {
		transport, e := riskpolicy.CanonicalIP(request.TransportIP)
		if e != nil {
			return nil, ErrInvalid
		}
		f.TransportIP = transport.String()
	}
	if e = tx.QueryRow(ctx, `SELECT pg_current_xact_id()::text`).Scan(&f.Transaction); e != nil {
		return nil, e
	}
	return f, nil
}

func coreRiskTimeKey(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000000000Z") }
func (s *Store) decodeCoreRiskOriginFact(raw, proof []byte, id, attempt int64, ips []string, at time.Time) (coreRiskOrigin, error) {
	var f coreRiskOrigin
	if decodeCoreRiskFacts(raw, &f) != nil || f.OrderID != id || f.AttemptID != attempt || !f.AdmittedAt.Equal(at) {
		return f, ErrConflict
	}
	a, _ := json.Marshal(ips)
	b, _ := json.Marshal(f.IPs)
	expected, e := s.coreRiskOriginProof(f)
	if e != nil || !hmac.Equal(a, b) || !hmac.Equal(expected, proof) {
		return f, ErrConflict
	}
	return f, nil
}
