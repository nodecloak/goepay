package channelconfig

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	adapter "goepay/internal/adapters/payment"
	"reflect"
	"testing"
)

func TestFactoryExplicitCommonProductsAndTrustedH5Scene(t *testing.T) {
	wx := Channel{ID: 7, Plugin: "wxpay", AppTypes: []string{"1", "2", "3", "5"}, Config: map[string]string{"appid": "app", "appmchid": "mch", "appkey": "0123456789abcdef0123456789abcdef", "h5_site_url": "https://pay.example.invalid/", "h5_site_name": "测试站点"}}
	clients, e := Build(Import{Version: 1, Channels: []Channel{wx}}, false)
	if e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(clients[7].Capabilities().Methods, []string{"native", "jsapi", "h5", "app"}) {
		t.Fatal(clients[7].Capabilities())
	}
	identity, ok := clients[7].(adapter.ApplicationIdentity)
	if !ok || identity.ApplicationID() != "app" {
		t.Fatal("configured application binding lost")
	}
	delete(wx.Config, "h5_site_url")
	if _, e = Build(Import{Version: 1, Channels: []Channel{wx}}, false); e == nil {
		t.Fatal("missing H5 site silently enabled")
	}
	key, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	public, e := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if e != nil {
		t.Fatal(e)
	}
	ali := Channel{ID: 8, Plugin: "alipay", AppTypes: []string{"1", "2", "3", "6", "4", "7"}, Config: map[string]string{"appid": "aliapp", "appsecret": base64.StdEncoding.EncodeToString(x509.MarshalPKCS1PrivateKey(key)), "appkey": base64.StdEncoding.EncodeToString(public)}}
	clients, e = Build(Import{Version: 1, Channels: []Channel{ali}}, false)
	if e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(clients[8].Capabilities().Methods, []string{"page", "wap", "native", "app", "jsapi", "mini"}) {
		t.Fatal(clients[8].Capabilities())
	}
}
