package payment

import (
	"context"
	"errors"
	"testing"

	adapter "goepay/internal/adapters/payment"
)

// mchTransferDouble is a 商家转账（新） channel: bills wait for the payee.
type mchTransferDouble struct {
	outgoingProvider
	beforeSend func()
}

func (*mchTransferDouble) UsesMerchantTransfer() bool { return true }
func (*mchTransferDouble) MerchantTransferConfirmation(pkg string) (string, string, string) {
	return "local-mch", "local-app", pkg
}
func (p *mchTransferDouble) ValidateTransfer(adapter.TransferRequest) error {
	if p.beforeSend != nil {
		p.beforeSend()
	}
	return nil
}
func (p *mchTransferDouble) Transfer(ctx context.Context, r adapter.TransferRequest) (adapter.TransferResult, error) {
	res, e := p.outgoingProvider.Transfer(ctx, r)
	if e == nil {
		res.PackageInfo = "affffddafdfafddffda=="
	}
	return res, e
}

func mchTransferSetting(t *testing.T, s *Store, key, value string) {
	t.Helper()
	if e := upsertMchTransferSetting(s, key, value); e != nil {
		t.Fatal(e)
	}
}

func upsertMchTransferSetting(s *Store, key, value string) error {
	ctx := context.Background()
	tag, e := s.pool.Exec(ctx, "UPDATE backoffice_records SET body=jsonb_build_object('key',$1::text,'value',$2::text) WHERE resource='settings' AND body->>'key'=$1", key, value)
	if e != nil || tag.RowsAffected() > 0 {
		return e
	}
	_, e = s.pool.Exec(ctx, "INSERT INTO backoffice_records(resource,body) VALUES('settings',jsonb_build_object('key',$1::text,'value',$2::text))", key, value)
	return e
}

func TestMerchantTransferNeedsOriginalSettingsBeforeReservation(t *testing.T) {
	s, flow, _ := transferFixture(t)
	ctx := context.Background()
	p := &mchTransferDouble{}
	flow.Providers[7] = p
	req := adapter.TransferRequest{Type: "wxpay", Account: "payee-openid", Amount: 100, Remark: "佣金报酬"}
	steps := []struct {
		key, value string
		want       error
	}{
		{"", "", ErrMerchantTransferOff},
		{"transfer_wxpay_type", "0", ErrMerchantTransferOff},
		{"transfer_wxpay_type", "1", ErrMerchantTransferScene},
		{"transfer_wxpay_scene_id", "1005", ErrMerchantTransferReport},
		{"transfer_wxpay_info_type", "岗位类型|报酬说明", ErrMerchantTransferReport},
	}
	for _, step := range steps {
		if step.key != "" {
			mchTransferSetting(t, s, step.key, step.value)
		}
		if _, e := flow.Transfer(ctx, 501, 7, "mch-setup", req); !errors.Is(e, step.want) {
			t.Fatalf("%s=%s: %v", step.key, step.value, e)
		}
		if msg, ok := MerchantTransferSetupMessage(step.want); !ok || msg == "" {
			t.Fatal("no original wording", step.want)
		}
	}
	if p.transfers.Load() != 0 {
		t.Fatal("bill sent without settings")
	}
	if _, e := s.Transfer(ctx, 501, "mch-setup"); !errors.Is(e, ErrNotFound) {
		t.Fatal("transfer reserved without settings", e)
	}
	if b, _ := s.Balance(ctx, 501); b.Available != 1000 || b.Reserved != 0 {
		t.Fatalf("balance moved %+v", b)
	}
}

func TestMerchantTransferSendsSceneAndKeepsPackageForPayee(t *testing.T) {
	s, flow, _ := transferFixture(t)
	ctx := context.Background()
	p := &mchTransferDouble{}
	flow.Providers[7] = p
	for key, value := range map[string]string{"transfer_wxpay_type": "1", "transfer_wxpay_scene_id": "1005", "transfer_wxpay_info_type": "岗位类型|报酬说明", "transfer_wxpay_info_content": "外卖员|7月份配送费"} {
		mchTransferSetting(t, s, key, value)
	}
	req := adapter.TransferRequest{Type: "wxpay", Account: "payee-openid", Amount: 100, Remark: "佣金报酬"}
	r, e := flow.Transfer(ctx, 501, 7, "mch-bill", req)
	if e != nil || r.Status != "pending" {
		t.Fatalf("bill %+v %v", r, e)
	}
	p.seenMu.Lock()
	sent := p.seen[0]
	p.seenMu.Unlock()
	if sent.SceneID != "1005" || sent.ReportTypes != "岗位类型|报酬说明" || sent.ReportContents != "外卖员|7月份配送费" {
		t.Fatalf("scene not sent %+v", sent)
	}
	if pkg, e := s.TransferPackage(ctx, r.ID); e != nil || pkg != "affffddafdfafddffda==" {
		t.Fatalf("package %q %v", pkg, e)
	}
	// An identical retry neither resends nor conflicts on the stored request.
	if again, e := flow.Transfer(ctx, 501, 7, "mch-bill", req); e != nil || again.ID != r.ID || p.transfers.Load() != 1 {
		t.Fatalf("retry %+v %v sends=%d", again, e, p.transfers.Load())
	}
	done, e := flow.QueryTransfer(ctx, 501, "mch-bill")
	if e != nil || done.Status != "succeeded" {
		t.Fatalf("confirmed bill %+v %v", done, e)
	}
	if pkg, _ := s.TransferPackage(ctx, r.ID); pkg != "affffddafdfafddffda==" {
		t.Fatal("package changed")
	}
	if _, e = s.pool.Exec(ctx, "UPDATE wechat_transfer_packages SET package_info='x' WHERE transfer_id=$1", r.ID); e == nil {
		t.Fatal("package is mutable")
	}
}

func TestMerchantTransferSettingsRemovedBeforeSendFailsWithoutSending(t *testing.T) {
	s, flow, _ := transferFixture(t)
	ctx := context.Background()
	p := &mchTransferDouble{}
	flow.Providers[7] = p
	for key, value := range map[string]string{"transfer_wxpay_type": "1", "transfer_wxpay_scene_id": "1005", "transfer_wxpay_info_type": "岗位类型", "transfer_wxpay_info_content": "外卖员"} {
		mchTransferSetting(t, s, key, value)
	}
	// The setting changes after the pre-check but before the bill is sent.
	p.beforeSend = func() {
		if e := upsertMchTransferSetting(s, "transfer_wxpay_type", "0"); e != nil {
			t.Error(e)
		}
	}
	r, e := flow.Transfer(ctx, 501, 7, "mch-race", adapter.TransferRequest{Type: "wxpay", Account: "payee-openid", Amount: 100, Remark: "佣金报酬"})
	if !errors.Is(e, ErrMerchantTransferOff) || r.Status != "failed" {
		t.Fatalf("race %+v %v", r, e)
	}
	if p.transfers.Load() != 0 {
		t.Fatal("bill sent after switch-off")
	}
	if b, _ := s.Balance(ctx, 501); b.Available != 1000 || b.Reserved != 0 {
		t.Fatalf("reservation kept %+v", b)
	}
}
