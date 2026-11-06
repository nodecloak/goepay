package server

import (
	"context"
	"encoding/json"
	"html/template"
	"net/http"

	"github.com/jackc/pgx/v5"
	"goepay/internal/compat"
	"goepay/internal/payment"
	"goepay/internal/paymentdefend"
)

// defend206ContinueField carries the one-time JS-jump continue token. It is
// added by the platform jump page AFTER the merchant signed, so it is removed
// before signature verification and is never part of the merchant canonical.
// __defend itself stays an ordinary signature-covered merchant field.
const defend206ContinueField = "__defend_continue"
const defend206ContinueTTLSeconds = 300

// defend206Admission records, for one request, that a continue token was
// consumed in its own committed transaction under a given policy revision.
type defend206Admission struct {
	merchantID   int64
	outTradeNo   string
	continuation string // platform trade_no of an OAuth/mini continuation
	clientIP     string // the original $clientip: direct peer, or signed clientip
	consumed     bool
	fingerprint  string
	kind         string // the pay_verify_type this request verified under
	// The original evaluates checkPayVerifyOpen once per request. Later
	// business writes of the same request (route, order, attempt) reuse that
	// decision so this request's own new order cannot flip it midway.
	decided   bool
	decidedFP string
	triggered bool
}

// defend206Triggered evaluates the original checkPayVerifyOpen for one
// merchant inside the caller's transaction, once per request and policy
// revision. Any evaluation error fails closed.
func (s *Server) defend206Triggered(ctx context.Context, tx pgx.Tx, policy paymentdefend.Policy, adm *defend206Admission) (triggered, known bool, err error) {
	if adm.decided && adm.decidedFP == policy.Fingerprint {
		return adm.triggered, true, nil
	}
	mode, err := s.defend206EffectiveMode(ctx, tx, policy, adm.merchantID)
	if err != nil {
		return true, false, err
	}
	t := paymentdefend.EvaluateStatic(mode, policy.Settings, adm.merchantID)
	if !t.Known {
		if mode != "1" {
			return true, false, nil
		}
		fired, err := paymentdefend.EvaluateRulesTx(ctx, tx, policy.Settings, adm.merchantID, adm.clientIP)
		if err != nil {
			return true, false, err
		}
		t = paymentdefend.Trigger{Triggered: fired, Known: true}
	}
	adm.decided, adm.decidedFP, adm.triggered = true, policy.Fingerprint, t.Triggered
	return t.Triggered, true, nil
}

// defend206EffectiveMode is the effective pay_verify for the merchant.
func (s *Server) defend206EffectiveMode(ctx context.Context, tx pgx.Tx, policy paymentdefend.Policy, merchantID int64) (string, error) {
	return s.PaymentDefend206.EffectiveModeTx(ctx, tx, policy, merchantID)
}

// The guard runs in the same transaction as each durable business write.
func (s *Server) paymentDefendContext206(ctx context.Context, adm *defend206Admission) context.Context {
	guard := func(ctx context.Context, tx pgx.Tx) error {
		if s.PaymentDefend206 == nil {
			if adm != nil && adm.consumed {
				return payment.ErrState
			}
			return paymentdefend.ConfirmUnconfiguredTx(ctx, tx)
		}
		policy, err := s.PaymentDefend206.CurrentTx(ctx, tx)
		if err != nil {
			return err
		}
		if adm == nil || adm.merchantID <= 0 {
			return payment.ErrState
		}
		if adm.continuation != "" {
			// OAuth / mini-program continuation of an order created earlier.
			admitted, err := s.PaymentDefend206.AdmittedTx(ctx, tx, adm.continuation, policy.Fingerprint)
			if err != nil {
				return payment.ErrState
			}
			if admitted {
				return nil
			}
			// Orders from before evidence existed: only while nothing could
			// have required verification.
			groups, err := s.PaymentDefend206.AnyGroupOverrideTx(ctx, tx)
			if err == nil && !groups && policy.Settings["pay_verify"] == "0" {
				return nil
			}
			return payment.ErrState
		}
		admit := func(kind string) error {
			if adm.outTradeNo == "" {
				return nil
			}
			return s.PaymentDefend206.RecordAdmissionTx(ctx, tx, adm.merchantID, adm.outTradeNo, kind, policy.Fingerprint)
		}
		if adm.consumed {
			// Verified this request under this exact policy revision.
			if adm.fingerprint == policy.Fingerprint && policy.Settings["pay_verify_type"] == adm.kind && adm.kind != "3" {
				return admit(adm.kind)
			}
			return payment.ErrState
		}
		triggered, known, err := s.defend206Triggered(ctx, tx, policy, adm)
		if err != nil || !known {
			return payment.ErrState
		}
		if !triggered {
			return admit("none")
		}
		return payment.ErrState
	}
	return payment.WithPaymentDefend206Guard(ctx, guard)
}

func defend206RequestIdentity(path string, p compat.Params) string {
	raw, _ := json.Marshal(p) // map keys are marshalled in sorted order
	return path + "\n" + string(raw)
}

func defend206Browser(r *http.Request, peerIP string) string {
	return r.UserAgent() + "\n" + peerIP
}

var defend206JumpPage = template.Must(template.New("jump206").Parse(`<!DOCTYPE html>
<html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<meta name="robots" content="noindex,nofollow"><title>安全验证</title>
<style>body{font-family:-apple-system,"Microsoft YaHei",sans-serif;background:#f5f5f5;color:#333;margin:0}
.box{max-width:420px;margin:15vh auto;padding:32px 24px;background:#fff;border-radius:6px;text-align:center;box-shadow:0 1px 4px rgba(0,0,0,.08)}
.box button{margin-top:16px;padding:8px 24px;border:0;border-radius:4px;background:#1677ff;color:#fff;font-size:15px;cursor:pointer}</style>
</head><body><div class="box"><p>正在进行安全验证，请稍候…</p>
<form id="defend206" method="post" action="{{.Action}}">{{range .Fields}}<input type="hidden" name="{{.K}}" value="{{.V}}">{{end}}<input type="hidden" name="__defend_continue" value="{{.Token}}">
<noscript><button type="submit">继续支付</button></noscript></form></div>
<script nonce="{{.Nonce}}">document.getElementById("defend206").submit();</script>
</body></html>`))

type defend206Field struct{ K, V string }

// paymentDefendContinuation206 guards the final submit of an OAuth or
// mini-program order: it resumes only an order with admission evidence.
func (s *Server) paymentDefendContinuation206(ctx context.Context, merchantID int64, tradeNo string) context.Context {
	return s.paymentDefendContext206(ctx, &defend206Admission{merchantID: merchantID, continuation: tradeNo})
}
