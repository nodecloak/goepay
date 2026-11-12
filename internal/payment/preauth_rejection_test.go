package payment

import (
	"context"
	"errors"
	"testing"

	adapter "goepay/internal/adapters/payment"
)

func TestPreauthorizationCaptureUnverifiedRejectionCannotAuthorizeUnfreeze(t *testing.T) {
	plain := &adapter.ProviderError{Code: "40002:isv.invalid-parameter"}
	for name, failure := range map[string]error{
		"plain":     plain,
		"unknown":   &adapter.UnknownOutcome{Operation: "capture", Cause: plain},
		"cancelled": errors.Join(plain, context.Canceled),
	} {
		t.Run(name, func(t *testing.T) {
			s, order := releaseTestOrder(t)
			p := &releaseFlowProvider{preauthFlowProvider: preauthFlowProvider{captureErr: failure}, remainingKnown: true}
			g := ProviderFlow{Store: s, Providers: map[int64]adapter.Provider{7: p}, AllowLive: true, CallbackBaseURL: "https://pay.example.invalid"}
			authorization, owner, err := s.BeginPreauthorizationCapture(t.Context(), order.TradeNo)
			if err != nil || !owner {
				t.Fatal("capture claim", owner, err)
			}
			result, err := g.captureClaimedPreauthorization(t.Context(), order, authorization, p)
			if err != nil || result.ExternalError == nil || p.captures != 1 {
				t.Fatal("capture fixture did not persist its error", result, err)
			}
			release, _ := g.ReleaseAuthorization(t.Context(), order.MerchantID, order.TradeNo)
			if result.Authorization.CaptureState != "unknown" || p.unfreezes != 0 || release.Sent {
				t.Fatalf("unverified capture rejection admitted unfreeze: capture=%s sends=%d release=%s sent=%v", result.Authorization.CaptureState, p.unfreezes, release.State, release.Sent)
			}
			if _, owner, err = s.BeginPreauthorizationCapture(t.Context(), order.TradeNo); err != nil || owner {
				t.Fatal("capture obtained a second send", owner, err)
			}
		})
	}
}

func TestPreauthorizationUnverifiedNotFoundCannotAuthorizeCaptureOrQuery(t *testing.T) {
	plain := &adapter.ProviderError{Code: "40004:ACQ.TRADE_NOT_EXIST"}
	for name, failure := range map[string]error{
		"plain":     plain,
		"unknown":   &adapter.UnknownOutcome{Operation: "query", Cause: plain},
		"cancelled": errors.Join(plain, context.Canceled),
	} {
		t.Run(name, func(t *testing.T) {
			s, order := releaseTestOrder(t)
			p := &manualCaptureProvider{queryFailure: failure}
			p.remainingKnown = true
			g := ProviderFlow{Store: s, Providers: map[int64]adapter.Provider{7: p}, AllowLive: true, CallbackBaseURL: "https://pay.example.invalid"}
			result, queryErr := g.Query(t.Context(), order.MerchantID, order.TradeNo)
			_, captureErr := g.CapturePreauthorization(t.Context(), order.MerchantID, order.TradeNo)
			if queryErr == nil || result.State == "AUTHORIZED" || captureErr == nil || p.writes.Load() != 0 {
				t.Fatalf("unverified not-found became trusted evidence: query=%s err=%v captureErr=%v captureSends=%d", result.State, queryErr, captureErr, p.writes.Load())
			}
		})
	}
}

func TestPreauthorizationRejectionRequiresAuthenticatedOriginalOperation(t *testing.T) {
	p := &preauthFlowProvider{}
	query := preauthorizationTestQueryNotFound()
	if !authorizationTradeNotFound(query, p) {
		t.Fatal("authenticated original query not-found must remain usable", query)
	}
	wrongOperation := preauthorizationTestRejection("alipay.trade.close", "40002", "isv.invalid-parameter", false)
	for _, operation := range []string{"alipay.trade.pay", "alipay.fund.auth.order.unfreeze"} {
		refused := preauthorizationTestRejection(operation, "40002", "isv.invalid-parameter", false)
		if !definiteAuthenticatedPreauthorizationRejection(refused, p, operation) {
			t.Fatal("authenticated original rejection was lost", operation, refused)
		}
		for _, failure := range []error{query, wrongOperation, &adapter.ProviderError{Code: "40002:isv.invalid-parameter"}, &adapter.UnknownOutcome{Operation: operation, Cause: refused}, errors.Join(refused, context.Canceled), errors.Join(refused, context.DeadlineExceeded), preauthorizationTestRejection(operation, "40002", "isv.invalid-parameter", true)} {
			if definiteAuthenticatedPreauthorizationRejection(failure, p, operation) {
				t.Fatal("uncertain, wrong-operation or unverified error became a rejection", operation, failure)
			}
		}
		if authorizationTradeNotFound(refused, p) || definiteAuthenticatedPreauthorizationRejection(refused, &flowProvider{}, operation) {
			t.Fatal("provider or operation identity was not bound")
		}
	}
	if authorizationTradeNotFound(preauthorizationTestRejection("alipay.trade.close", "40004", "ACQ.TRADE_NOT_EXIST", false), p) || authorizationTradeNotFound(query, &flowProvider{}) {
		t.Fatal("not-found from another operation or provider authorized payment")
	}
	for _, failure := range []error{&adapter.UnknownOutcome{Operation: "query", Cause: query}, errors.Join(query, context.Canceled), errors.Join(query, context.DeadlineExceeded)} {
		if authorizationTradeNotFound(failure, p) {
			t.Fatal("uncertain query error became authenticated not-found", failure)
		}
	}
}
