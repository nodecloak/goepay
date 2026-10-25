package channelconfig

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"reflect"
	"testing"
)

func TestFactoryExplicitPreauthorizationProductsFiveAndNine(t *testing.T) {
	key, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	public, _ := x509.MarshalPKIXPublicKey(&key.PublicKey)
	for _, product := range []string{"5", "9", "10"} {
		c := Channel{ID: 8, Plugin: "alipay", AppTypes: []string{product}, Config: map[string]string{"appid": "app", "appsecret": base64.StdEncoding.EncodeToString(x509.MarshalPKCS1PrivateKey(key)), "appkey": base64.StdEncoding.EncodeToString(public)}}
		clients, e := Build(Import{Version: 1, Channels: []Channel{c}}, false)
		if e != nil {
			t.Fatalf("explicit preauth product %s rejected: %v", product, e)
		}
		if !reflect.DeepEqual(clients[8].Capabilities().Methods, []string{"preauth"}) {
			t.Fatalf("product%s capabilities=%v", product, clients[8].Capabilities())
		}
	}
}
