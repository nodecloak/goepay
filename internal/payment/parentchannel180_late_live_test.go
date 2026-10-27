package payment

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	adapter "goepay/internal/adapters/payment"
	"testing"
)

type parent180ReleaseGateProvider struct {
	releaseFlowProvider
	beforeAuthorization func()
}

func TestParentChannel180CaptureLastWriteGuardRollsBackSendBarrierAndJob(t *testing.T) {
	s, o := releaseTestOrder(t)
	calls := 0
	_, owner, e := s.BeginPreauthorizationCapture(t.Context(), o.TradeNo, func(context.Context, pgx.Tx) error {
		calls++
		if calls == 2 {
			return ErrLiveDisabled
		}
		return nil
	})
	if !errors.Is(e, ErrLiveDisabled) || owner || calls != 2 {
		t.Fatal("last write guard ignored", owner, calls, e)
	}
	a, e := s.Preauthorization(t.Context(), o.TradeNo)
	if e != nil || a.CaptureState != "none" {
		t.Fatal("late failure left capture barrier", e)
	}
	var jobs int
	if e = s.pool.QueryRow(t.Context(), `SELECT count(*) FROM jobs WHERE event_key=$1`, "preauth-capture-reconcile:"+o.TradeNo).Scan(&jobs); e != nil || jobs != 0 {
		t.Fatal("late failure left capture reconciliation", jobs, e)
	}
}
func (p *parent180ReleaseGateProvider) QueryAuthorization(ctx context.Context, r adapter.AuthorizationRequest) (adapter.AuthorizationEvent, error) {
	out, e := p.releaseFlowProvider.QueryAuthorization(ctx, r)
	if p.beforeAuthorization != nil {
		p.beforeAuthorization()
	}
	return out, e
}
func TestParentChannel180ReleaseRechecksRootWriteGateAfterOriginalSignedQuery(t *testing.T) {
	s, o := releaseTestOrder(t)
	p := &parent180ReleaseGateProvider{releaseFlowProvider: releaseFlowProvider{remainingKnown: true}}
	g := ProviderFlow{Store: s, Providers: map[int64]adapter.Provider{7: p}, AllowLive: true}
	if e := g.InstallParentAccountFactories(nil); e != nil {
		t.Fatal(e)
	}
	prepared := g
	p.beforeAuthorization = func() { g.AllowLive = false }
	r, e := prepared.ReleaseAuthorization(t.Context(), o.MerchantID, o.TradeNo)
	if !errors.Is(e, ErrLiveDisabled) || p.unfreezes != 0 || r.Sent {
		t.Fatal("late root revoke sent an unfreeze", p.unfreezes, r.Sent, e)
	}
}
