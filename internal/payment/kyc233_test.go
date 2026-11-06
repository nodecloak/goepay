package payment

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	adapter "goepay/internal/adapters/payment"
	"goepay/internal/channelconfig"
	"goepay/internal/kyc"
	"testing"
)

func TestKYC233ResolverAuthenticatesManagedAccountAndDeniesDisabledOrReplayedHead(t *testing.T) {
	s := testStore(t)
	ctx := t.Context()
	actor := reviewWeixinActor(t, s)
	key, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	c := channelconfig.Channel{ID: 7, Plugin: "alipay", AppTypes: []string{"1"}, Config: map[string]string{
		"appid": "2021000000000001", "appmchid": "2088000000000001",
		"appsecret": string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})),
		"appkey":    string(pem.EncodeToMemory(&pem.Block{Type: "RSA PUBLIC KEY", Bytes: x509.MarshalPKCS1PublicKey(&key.PublicKey)})),
	}}
	f, e := channelconfig.NewParentChannelFactory(c, channelconfig.ParentBuildOptions{})
	if e != nil {
		t.Fatal(e)
	}
	ex, _, e := f.BootstrapSnapshot(7)
	if e != nil {
		t.Fatal(e)
	}
	g := ProviderFlow{Store: s, Providers: map[int64]adapter.Provider{7: ex.Payment}, ParentFactories: map[int64]adapter.ParentAccountFactory{7: f}}
	template, e := g.ParentChannelTemplate(7, nil)
	if e != nil {
		t.Fatal(e)
	}
	view, e := s.AdoptParentChannel(ctx, actor, template, template.Public().Fingerprint)
	if e != nil {
		t.Fatal(e)
	}
	resolve := func(id int64) (channelconfig.Channel, error) {
		tx, err := s.pool.Begin(ctx)
		if err != nil {
			return channelconfig.Channel{}, err
		}
		defer tx.Rollback(ctx)
		return g.ResolveKYCChannel233(ctx, tx, id)
	}
	out, e := resolve(7)
	if e != nil || out.Config["appid"] != c.Config["appid"] {
		t.Fatal("original account unavailable", e)
	}
	if _, e = resolve(999); e == nil {
		t.Fatal("unknown channel accepted")
	}
	disabled := false
	rate := "100"
	if _, e = s.UpdateParentChannel(ctx, actor, 7, ParentChannelPatch{Expected: view.Fingerprint, Enabled: &disabled, Rate: &rate}, g.ParentFactories); e != nil {
		t.Fatal(e)
	}
	if _, e = resolve(7); e == nil {
		t.Fatal("disabled channel accepted")
	}
	if _, e = s.pool.Exec(ctx, `UPDATE runtime_parent_channels SET name='forged' WHERE id=7`); e != nil {
		t.Fatal(e)
	}
	if _, e = resolve(7); e == nil {
		t.Fatal("tampered current head accepted")
	}
}

func TestKYC233ForcedCertificationProtectsNewOrderAndFirstProviderSend(t *testing.T) {
	s := testStore(t)
	ctx := t.Context()
	admin := reviewWeixinActor(t, s)
	merchant := Merchant{ID: 233001, Name: "Synthetic certification gate", APIKey: "synthetic-only"}
	if err := s.CreateMerchant(ctx, merchant); err != nil {
		t.Fatal(err)
	}
	in := CreateOrderInput{MerchantID: merchant.ID, OutTradeNo: "before-force", Name: "synthetic", Type: "alipay", Amount: 100}
	old, err := s.CreateOrder(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	service, err := kyc.New(s.pool, s.confirmationKey231, nil, nil, "https://synthetic.invalid")
	if err != nil {
		t.Fatal(err)
	}
	edits := map[string]kyc.CredentialEdit{}
	for _, k := range kyc.CredentialKeys() {
		edits[k] = kyc.CredentialEdit{Action: "keep"}
	}
	edits["cert_appcode"] = kyc.CredentialEdit{Action: "replace", Value: "synthetic-appcode"}
	if _, err = service.UpdateConfig(ctx, kyc.Actor{User: admin.User, Token: admin.Token, CSRF: admin.CSRF}, kyc.ConfigInput{Mode: 2, Force: true, Money: "0.00", ChannelID: "0", Credentials: edits}); err != nil {
		t.Fatal(err)
	}
	// The old generic profile must never be an authenticated certification.
	if _, err = s.pool.Exec(ctx, `UPDATE merchants SET profile=profile||'{"cert":1,"certname":"forged"}'::jsonb WHERE id=$1`, merchant.ID); err != nil {
		t.Fatal(err)
	}
	in.OutTradeNo = "after-force"
	if _, err = s.CreateOrder(ctx, in); !errors.Is(err, ErrCertificationRequired) {
		t.Fatalf("new order must require the dedicated verified record: %v", err)
	}
	if _, _, err = s.BeginProviderAttempt(ctx, old.TradeNo, 7, "page"); !errors.Is(err, ErrCertificationRequired) {
		t.Fatalf("first external send must recheck certification: %v", err)
	}
	var orders, attempts int
	if err = s.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM orders WHERE merchant_id=$1),(SELECT count(*) FROM provider_attempts a JOIN orders o ON o.id=a.order_id WHERE o.merchant_id=$1)`, merchant.ID).Scan(&orders, &attempts); err != nil || orders != 1 || attempts != 0 {
		t.Fatal("denial wrote new financial intents", orders, attempts, err)
	}
}
