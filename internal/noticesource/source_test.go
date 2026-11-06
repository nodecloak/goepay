package noticesource

import (
	"bytes"
	"testing"
)

func TestSourceHashBindsAllDeliveryFacts231(t *testing.T) {
	original := Facts{Kind: "wxtransfer", MerchantID: 1, ReferenceID: 2, ChannelID: 3, Recipient: "synthetic-openid", AppID: "wx-original", PackageHash: "original", Params: map[string]string{"money": "0.01"}}
	h := Hash(original)
	for _, change := range []func(*Facts){func(f *Facts) { f.Recipient = "another" }, func(f *Facts) { f.AppID = "wx-new" }, func(f *Facts) { f.ReferenceID++ }, func(f *Facts) { f.PackageHash = "new" }, func(f *Facts) { f.ChannelID++ }} {
		copy := original
		change(&copy)
		if bytes.Equal(h, Hash(copy)) {
			t.Fatal("changed delivery authority preserved hash")
		}
	}
}
