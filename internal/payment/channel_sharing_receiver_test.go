package payment

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"fmt"
	adapter "goepay/internal/adapters/payment"
	"net/http"
	"strings"
	"sync"
	"testing"
)

func TestChannelSharingReceiverEncryptsBindingsAndConcurrentReplayIsOneDraft(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	if e := s.CreateMerchant(ctx, Merchant{ID: 751, Name: "sharing-owner", APIKey: "local-only"}); e != nil {
		t.Fatal(e)
	}
	actor := TransferBatchActor{Role: "merchant", ID: 751, Name: "sharing-owner"}
	binding := channelSharingBinding{MerchantID: 751, ChannelID: 7, Provider: "wxpay", AppID: "local-app", Product: "wxpay_v3_direct", AccountScope: "public-original-account", RelationScope: "public-mch"}
	input := adapter.SharingReceiver{Type: "MERCHANT_ID", Account: "private-receiver-account-1234", Name: "Private Receiver Legal Name", RelationType: "SUPPLIER"}
	const parallel = 8
	ids := make(chan int64, parallel)
	failures := make(chan error, parallel)
	var wg sync.WaitGroup
	for i := 0; i < parallel; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			view, e := s.createChannelSharingReceiver(ctx, binding, "local-receiver-original", input, actor)
			if e != nil {
				failures <- e
				return
			}
			ids <- view.ID
		}()
	}
	wg.Wait()
	close(ids)
	close(failures)
	for e := range failures {
		t.Fatal(e)
	}
	id := int64(0)
	for candidate := range ids {
		if id == 0 {
			id = candidate
		}
		if candidate != id {
			t.Fatal("same receiver intent created multiple drafts")
		}
	}
	view, e := s.ChannelSharingReceiver(ctx, 751, id, actor)
	if e != nil || view.State != "draft" {
		t.Fatal(view, e)
	}
	encoded, _ := json.Marshal(view)
	if bytes.Contains(encoded, []byte(input.Account)) || bytes.Contains(encoded, []byte(input.Name)) {
		t.Fatal("receiver view disclosed plaintext")
	}
	var ciphertext []byte
	var audit string
	var count int
	if e = s.pool.QueryRow(ctx, "SELECT receiver_cipher FROM channel_sharing_receivers WHERE id=$1", id).Scan(&ciphertext); e != nil {
		t.Fatal(e)
	}
	if bytes.Contains(ciphertext, []byte(input.Account)) || bytes.Contains(ciphertext, []byte(input.Name)) {
		t.Fatal("receiver stored plaintext")
	}
	if e = s.pool.QueryRow(ctx, "SELECT count(*),COALESCE(string_agg(changes::text,''),'') FROM backoffice_audit WHERE resource='channel-sharing-receivers'").Scan(&count, &audit); e != nil || count != 1 || strings.Contains(audit, input.Account) || strings.Contains(audit, input.Name) {
		t.Fatal("create audit missing, duplicated or sensitive", count, e)
	}
	if _, e = s.openChannelSharingReceiver(id+1, binding, ciphertext); e == nil {
		t.Fatal("ciphertext transplanted into another receiver")
	}
	changed := binding
	changed.AccountScope = "changed-public-account"
	if _, e = s.openChannelSharingReceiver(id, changed, ciphertext); e == nil {
		t.Fatal("ciphertext rebound to another payer")
	}
	input.Name = "Changed Legal Name"
	if _, e = s.createChannelSharingReceiver(ctx, binding, "local-receiver-original", input, actor); !errors.Is(e, ErrConflict) {
		t.Fatal("same receiver intent changed its legal identity", e)
	}
	if _, e = s.ChannelSharingReceiver(ctx, 752, id, TransferBatchActor{Role: "merchant", ID: 752, Name: "another"}); e == nil {
		t.Fatal("another merchant read receiver")
	}
	if _, e = s.pool.Exec(ctx, "UPDATE channel_sharing_receivers SET channel_id=8 WHERE id=$1", id); e == nil {
		t.Fatal("receiver channel was mutable")
	}
	if _, e = s.pool.Exec(ctx, "UPDATE channel_sharing_receivers SET state='bound' WHERE id=$1", id); e == nil {
		t.Fatal("draft receiver became bound without operation proof")
	}
}

func TestChannelSharingReceiverPhysicalReferencesCannotDeleteAnotherOwnersActiveRelation(t *testing.T) {
	s, g, calls, _ := sharingReceiverFlowFixture(t)
	ctx := context.Background()
	original, e := g.CreateChannelSharingReceiver(ctx, 751, 7, "physical-original", channelSharingReceiverInput, channelSharingActor)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = g.BindChannelSharingReceiver(ctx, 751, original.ID, "physical-bind", channelSharingActor); e != nil {
		t.Fatal(e)
	}
	if _, e = g.SetChannelSharingRule(ctx, 751, 7, ChannelSharingRuleInput{Enabled: true, Methods: []string{"native"}, Items: []ChannelSharingRuleItem{{ReceiverID: original.ID, Mode: "rate", RateBPS: 1000}}}, 0, channelSharingActor); e != nil {
		t.Fatal(e)
	}
	alias, e := g.CreateChannelSharingReceiver(ctx, 751, 7, "physical-alias", channelSharingReceiverInput, channelSharingActor)
	if e != nil {
		t.Fatal(e)
	}
	bound, e := g.BindChannelSharingReceiver(ctx, 751, alias.ID, "physical-alias-bind", channelSharingActor)
	if e != nil || bound.State != "bound" || bound.Operation == nil || bound.Operation.Source != "reference" || calls.Load() != 1 {
		t.Fatal("alias did not use original verified reference", bound, e, calls.Load())
	}
	if _, e = g.UnbindChannelSharingReceiver(ctx, 751, alias.ID, "physical-alias-unbind", channelSharingActor); e != nil || calls.Load() != 1 {
		t.Fatal("alias deleted the enabled original relation", e, calls.Load())
	}
	stillBound, e := s.ChannelSharingReceiver(ctx, 751, original.ID, channelSharingActor)
	if e != nil || stillBound.State != "bound" {
		t.Fatal(stillBound, e)
	}
	g.Providers[8] = g.Providers[7]
	channelAlias, e := g.CreateChannelSharingReceiver(ctx, 751, 8, "physical-channel-alias", channelSharingReceiverInput, channelSharingActor)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = g.BindChannelSharingReceiver(ctx, 751, channelAlias.ID, "physical-channel-bind", channelSharingActor); e != nil || calls.Load() != 1 {
		t.Fatal("same-account channel reference performed another remote bind", e, calls.Load())
	}
	if e = s.CreateMerchant(ctx, Merchant{ID: 752, Name: "other", APIKey: "local-only"}); e != nil {
		t.Fatal(e)
	}
	otherActor := TransferBatchActor{Role: "merchant", ID: 752, Name: "other"}
	tenantAlias, e := g.CreateChannelSharingReceiver(ctx, 752, 7, "physical-tenant-alias", channelSharingReceiverInput, otherActor)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = g.BindChannelSharingReceiver(ctx, 752, tenantAlias.ID, "physical-tenant-bind", otherActor); e != nil || calls.Load() != 1 {
		t.Fatal("same-account tenant reference performed another remote bind", e, calls.Load())
	}
	if _, e = g.UnbindChannelSharingReceiver(ctx, 752, tenantAlias.ID, "physical-tenant-unbind", otherActor); e != nil || calls.Load() != 1 {
		t.Fatal("another tenant deleted an active original relationship", e, calls.Load())
	}
	if calls.Load() != 1 {
		t.Fatal("alias registration performed remote mutation", calls.Load())
	}
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	blocked := &forbidSharingReceiverTransport{}
	otherApp, e := adapter.NewWechatV3(adapter.WechatV3Config{AppID: "another-app", MerchantID: "original-mch", MerchantSerial: "OTHER_SERIAL", PrivateKey: key, APIv3Key: []byte("0123456789abcdef0123456789abcdef"), VerificationKeys: map[string]*rsa.PublicKey{"PUB_KEY_ID_OTHER": &key.PublicKey}, EnableChannelSharing: true, HTTPClient: &http.Client{Transport: blocked}})
	if e != nil {
		t.Fatal(e)
	}
	g.Providers[9] = otherApp
	appAlias, e := g.CreateChannelSharingReceiver(ctx, 751, 9, "physical-app-alias", channelSharingReceiverInput, channelSharingActor)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = g.BindChannelSharingReceiver(ctx, 751, appAlias.ID, "physical-app-bind", channelSharingActor); !errors.Is(e, ErrConflict) || blocked.calls.Load() != 0 {
		t.Fatal("unverified another-App binding inherited the original proof", e, blocked.calls.Load())
	}
}

func TestChannelSharingConcurrentDraftAliasesDoNotOccupyRegistryAndAlipayRequiresCanonicalUID(t *testing.T) {
	s, g, calls, _ := sharingReceiverFlowFixture(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	results := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, e := g.CreateChannelSharingReceiver(ctx, 751, 7, fmt.Sprintf("concurrent-physical-%d", i), channelSharingReceiverInput, channelSharingActor)
			results <- e
		}(i)
	}
	wg.Wait()
	close(results)
	success, conflicts := 0, 0
	for e := range results {
		if e == nil {
			success++
		} else if errors.Is(e, ErrConflict) {
			conflicts++
		} else {
			t.Fatal(e)
		}
	}
	if success != 8 || conflicts != 0 || calls.Load() != 0 {
		t.Fatal(success, conflicts, calls.Load())
	}
	var occupied int
	if e := s.pool.QueryRow(ctx, "SELECT count(*) FROM channel_sharing_relations").Scan(&occupied); e != nil || occupied != 0 {
		t.Fatal("unbound drafts occupied a global upstream relationship", occupied, e)
	}
	binding := channelSharingBinding{MerchantID: 751, ChannelID: 11, Provider: "alipay", AppID: "app-one", Product: "alipay_direct", AccountScope: "original-app-one-seller", RelationScope: `["alipay","2088000000000099"]`}
	for _, kind := range []string{"loginName", "openId"} {
		if _, e := s.createChannelSharingReceiver(ctx, binding, "unresolved-"+kind, adapter.SharingReceiver{Type: kind, Account: "unresolved-alias"}, channelSharingActor); !errors.Is(e, adapter.ErrUnsupported) {
			t.Fatal("unverified canonical alias entered persistent relationship", kind, e)
		}
	}
	input := adapter.SharingReceiver{Type: "userId", Account: "2088000000000001"}
	if _, e := s.createChannelSharingReceiver(ctx, binding, "canonical-original", input, channelSharingActor); e != nil {
		t.Fatal(e)
	}
	changedSeller := binding
	changedSeller.RelationScope = `["alipay","2088000000000088"]`
	changedSeller.AccountScope = "same-app-configured-other-seller"
	if _, e := s.createChannelSharingReceiver(ctx, changedSeller, "canonical-fake-seller", input, channelSharingActor); e != nil {
		t.Fatal("draft creation incorrectly claimed an upstream account", e)
	}
	binding.AppID = "app-two"
	binding.ChannelID = 12
	binding.AccountScope = "original-app-two-same-seller"
	if _, e := s.createChannelSharingReceiver(ctx, binding, "canonical-other-app", input, channelSharingActor); e != nil {
		t.Fatal("a different application draft occupied a physical relationship", e)
	}
}
