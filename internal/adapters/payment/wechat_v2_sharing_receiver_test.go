package payment

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"
)

type sharingReceiverAPI79 interface {
	ChannelSharingCapabilities() ChannelSharingCapabilities
	ValidateChannelSharingReceiver(SharingReceiver, bool) error
	BindChannelSharingReceiver(context.Context, string, SharingReceiver) error
	UnbindChannelSharingReceiver(context.Context, string, SharingReceiver) error
}

func enableWechatV2Sharing79(t *testing.T, cfg *WechatV2Config) {
	t.Helper()
	field := reflect.ValueOf(cfg).Elem().FieldByName("EnableChannelSharing")
	if !field.IsValid() || !field.CanSet() || field.Kind() != reflect.Bool {
		t.Fatal("original V2 sharing has no separate default-off capability")
	}
	field.SetBool(true)
}

func TestWechatV2Sharing79ReceiverUsesHMACAndExactTLSAccountEcho(t *testing.T) {
	const key = "0123456789abcdef0123456789abcdef"
	for _, mode := range []string{"bind", "unbind", "wrong-mch", "wrong-receiver", "bad-sign", "md5", "no-nonce", "duplicate-json", "http", "insecure", "disabled", "invalid-account", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			var calls atomic.Int32
			up := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				raw, e := io.ReadAll(r.Body)
				if e != nil {
					t.Error(e)
					return
				}
				fields, e := decodeXML(raw)
				if e != nil || fields["sign_type"] != "HMAC-SHA256" || !VerifyWechatV2(fields, key, "HMAC-SHA256", fields["sign"]) || fields["appid"] != "app79" || fields["mch_id"] != "mch79" {
					t.Error("original V2 receiver request was not HMAC signed for its account")
					return
				}
				var receiver map[string]string
				if json.Unmarshal([]byte(fields["receiver"]), &receiver) != nil || receiver["type"] != "MERCHANT_ID" || receiver["account"] != "receiver79" {
					t.Error("wrong original receiver")
					return
				}
				if mode == "unbind" {
					if r.URL.Path != "/pay/profitsharingremovereceiver" || len(receiver) != 2 {
						t.Error("unbind sent changed request or unnecessary identity fields")
					}
				} else if r.URL.Path != "/pay/profitsharingaddreceiver" || receiver["name"] != "Synthetic Receiver" || receiver["relation_type"] != "SERVICE_PROVIDER" {
					t.Error("bind lost original receiver relation")
				}
				echo := `{"type":"MERCHANT_ID","account":"receiver79"}`
				if mode == "wrong-receiver" {
					echo = `{"type":"MERCHANT_ID","account":"other79"}`
				}
				if mode == "duplicate-json" {
					echo = `{"type":"MERCHANT_ID","account":"other79","account":"receiver79"}`
				}
				out := map[string]string{"return_code": "SUCCESS", "result_code": "SUCCESS", "appid": "app79", "mch_id": "mch79", "nonce_str": "receiver79nonce", "receiver": echo}
				if mode == "wrong-mch" {
					out["mch_id"] = "other79"
				}
				if mode == "no-nonce" {
					delete(out, "nonce_str")
				}
				signType := "HMAC-SHA256"
				if mode == "md5" {
					signType = "MD5"
				}
				out["sign"], _ = SignWechatV2(out, key, signType)
				if mode == "bad-sign" {
					out["sign"] = "wrong79"
				}
				w.Write(encodeXML(out))
			}))
			defer up.Close()
			client := up.Client()
			endpoint := up.URL
			if mode == "http" {
				endpoint = "http://127.0.0.1:1"
			}
			if mode == "insecure" {
				transport := client.Transport.(*http.Transport).Clone()
				transport.TLSClientConfig = transport.TLSClientConfig.Clone()
				transport.TLSClientConfig.InsecureSkipVerify = true
				defer transport.CloseIdleConnections()
				client = &http.Client{Transport: transport}
			}
			cfg := WechatV2Config{AppID: "app79", MerchantID: "mch79", APIKey: key, BaseURL: endpoint, HTTPClient: client}
			if mode != "disabled" {
				enableWechatV2Sharing79(t, &cfg)
			}
			base, e := NewWechatV2(cfg)
			if e != nil {
				t.Fatal(e)
			}
			p, ok := any(base).(sharingReceiverAPI79)
			if !ok {
				t.Fatal("original V2 receiver protocol missing")
			}
			if mode == "disabled" && p.ChannelSharingCapabilities().Product != "" {
				t.Fatal("V2 sharing enabled by ordinary payment configuration")
			}
			r := SharingReceiver{Type: "MERCHANT_ID", Account: "receiver79", Name: "Synthetic Receiver", RelationType: "SERVICE_PROVIDER"}
			if mode == "invalid-account" {
				r.Account = "bad\r\n79"
			}
			ctx := context.Background()
			if mode == "cancelled" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			if mode == "unbind" {
				e = p.UnbindChannelSharingReceiver(ctx, "PU79_ORIGINAL", r)
			} else {
				e = p.BindChannelSharingReceiver(ctx, "PB79_ORIGINAL", r)
			}
			valid := mode == "bind" || mode == "unbind"
			if valid && e != nil || !valid && e == nil {
				t.Fatal("wrong original V2 receiver authentication outcome", mode, e)
			}
			noIO := mode == "http" || mode == "insecure" || mode == "disabled" || mode == "invalid-account" || mode == "cancelled"
			want := int32(1)
			if noIO {
				want = 0
			}
			if calls.Load() != want {
				t.Fatal("disabled/invalid receiver performed I/O or operation implicitly retried", mode, calls.Load())
			}
		})
	}
}
