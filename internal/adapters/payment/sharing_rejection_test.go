package payment

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAlipaySharingRelationFailureRetainsAuthenticatedProvenance(t *testing.T) {
	key := testKey(t)
	for _, secure := range []bool{false, true} {
		up := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if err := r.ParseForm(); err != nil {
				t.Error(err)
				return
			}
			node := []byte(`{"code":"10000","result_code":"FAIL"}`)
			digest := sha256.Sum256(node)
			signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
			if err != nil {
				t.Error(err)
				return
			}
			json.NewEncoder(w).Encode(map[string]any{strings.ReplaceAll(r.Form.Get("method"), ".", "_") + "_response": json.RawMessage(node), "sign": base64.StdEncoding.EncodeToString(signature)})
		}))
		if secure {
			up.StartTLS()
		} else {
			up.Start()
		}
		p, err := NewAlipay(AlipayConfig{AppID: "local-sharing", SellerID: "2088000000000099", PrivateKey: key, PublicKey: &key.PublicKey, EnableChannelSharing: true, GatewayURL: up.URL, HTTPClient: up.Client()})
		if err != nil {
			up.Close()
			t.Fatal(err)
		}
		for _, action := range []string{"bind", "unbind"} {
			var err error
			receiver := SharingReceiver{Type: "userId", Account: "2088000000000001"}
			if action == "bind" {
				err = p.BindChannelSharingReceiver(context.Background(), "PB1", receiver)
			} else {
				err = p.UnbindChannelSharingReceiver(context.Background(), "PU1", receiver)
			}
			proof, ok := VerifiedProviderRejection(err)
			if ok != secure || secure && (proof.Provider != "alipay" || proof.Operation != "alipay.trade.royalty.relation."+action || proof.Code != "SHARING_RELATION_FAIL") {
				t.Errorf("wrong relation refusal provenance secure=%v action=%s proof=%+v err=%v", secure, action, proof, err)
			}
		}
		up.Close()
	}
}
