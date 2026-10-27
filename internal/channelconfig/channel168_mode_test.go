package channelconfig

import (
	"encoding/json"
	"testing"
)

func TestChannel168ExplicitDirectSettlementImportNeverDefaultsToCollection(t *testing.T) {
	for _, body := range []string{
		`{"version":1,"channels":[{"id":7,"plugin":"wxpay","mode":1,"app_types":["1"],"config":{"appid":"local168-app","appmchid":"local168-merchant","appkey":"0123456789abcdef0123456789abcdef"}}]}`,
		`{"version":1,"channels":[{"id":7,"plugin":"wxpay","app_types":["1"],"config":{"mode":"1","appid":"local168-app","appmchid":"local168-merchant","appkey":"0123456789abcdef0123456789abcdef"}}]}`,
	} {
		var imp Import
		if e := json.Unmarshal([]byte(body), &imp); e != nil {
			t.Fatal(e)
		}
		if _, e := Build(imp, false); e == nil {
			t.Fatal("explicit channel mode1 silently became platform collection")
		}
	}
}
