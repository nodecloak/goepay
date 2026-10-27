package server

import (
	"goepay/internal/payment"
	"testing"
)

func TestParentChild189NestedViewsRetainOriginalRootPolicy(t *testing.T) {
	root := &Server{ProviderFlow: &payment.ProviderFlow{AllowLive: true}}
	parent := root.executionView()
	child := parent.executionView()
	if child.subchannelPolicySource != root {
		t.Fatal("nested view lost canonical policy source")
	}
	if !child.paymentExecutionWriteAllowed() {
		t.Fatal("enabled original source was rejected")
	}
	root.ProviderFlow.AllowLive = false
	if child.paymentExecutionWriteAllowed() {
		t.Fatal("nested payment view bypassed revoked root write gate")
	}
}
