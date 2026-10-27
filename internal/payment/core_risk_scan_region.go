package payment

import (
	"context"
	"github.com/jackc/pgx/v5"
	"goepay/internal/riskpolicy"
)

// CoreRiskScanRegionResult deliberately contains no raw customer or transport IP.
type CoreRiskScanRegionResult struct {
	Mode                 string
	Applicable, Mismatch bool
	BaselineSource       string
}

// CoreRiskScanRegion reads the authenticated policy frozen at the first send.
// It neither submits a payment nor changes an existing payment material. An empty
// scanIP only asks whether outgoing material needs a platform intermediary.
func (s *Store) CoreRiskScanRegion(ctx context.Context, trade, scanIP string) (CoreRiskScanRegionResult, error) {
	out := CoreRiskScanRegionResult{Mode: "0"}
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if e != nil {
		return out, e
	}
	defer tx.Rollback(ctx)
	ready, e := s.coreRiskSchema(ctx, tx)
	if e != nil {
		return out, e
	}
	if !ready {
		return out, tx.Commit(ctx)
	}
	var id int64
	var required bool
	var method, kind string
	e = tx.QueryRow(ctx, `SELECT o.id,a.core_risk_required,a.method,o.type FROM orders o JOIN provider_attempts a ON a.order_id=o.id WHERE o.trade_no=$1`, trade).Scan(&id, &required, &method, &kind)
	if e != nil {
		return out, e
	}
	if !required {
		var anchored bool
		if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM core_risk_origins WHERE order_id=$1)OR EXISTS(SELECT 1 FROM backoffice_audit WHERE resource='core-risk-origins' AND resource_id=$1 AND operation='admit')`, id).Scan(&anchored); e != nil {
			return out, e
		}
		if anchored {
			return out, ErrConflict
		}
		return out, tx.Commit(ctx)
	}
	if method != "native" && method != "wap" && method != "h5" || kind != "alipay" && kind != "wxpay" {
		return out, tx.Commit(ctx)
	}
	f, _, e := s.readCoreRiskOrigin(ctx, tx, id)
	if e != nil {
		return out, e
	}
	if f.ScanRegion == "" || f.ScanRegion == "0" {
		return out, tx.Commit(ctx)
	}
	if f.ScanRegion != "1" && f.ScanRegion != "2" {
		return out, ErrConflict
	}
	out.Mode, out.Applicable, out.BaselineSource = f.ScanRegion, true, f.ScanBaselineSource
	if scanIP == "" {
		return out, tx.Commit(ctx)
	}
	if !s.coreRiskGeoAvailable() {
		return out, ErrState
	}
	original, e := riskpolicy.CanonicalIP(f.ScanBaselineIP)
	if e != nil {
		return out, ErrState
	}
	current, e := riskpolicy.CanonicalIP(scanIP)
	if e != nil {
		return out, ErrInvalid
	}
	a, found, e := s.CoreRiskGeo.Lookup(original)
	if e != nil || !found || a.Province == "" || a.City == "" {
		return out, ErrState
	}
	b, found, e := s.CoreRiskGeo.Lookup(current)
	if e != nil || !found || b.Province == "" || b.City == "" {
		return out, ErrState
	}
	out.Mismatch = a.Province != b.Province || a.City != b.City
	return out, tx.Commit(ctx)
}
