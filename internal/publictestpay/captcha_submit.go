package publictestpay

import (
	"context"
	"crypto/hmac"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"goepay/internal/captcha"
	"goepay/internal/ledger"
)

type challengeRecord struct {
	Token, Browser            []byte
	IntentID, Purpose, Config string
	Protocol                  captcha.Challenge
	Expires                   time.Time
}

func (s *Service) challengeProof(c challengeRecord) []byte {
	c.Expires = c.Expires.UTC()
	raw, _ := json.Marshal(c)
	return s.sum("captcha-challenge", raw)
}
func (s *Service) challenge(w http.ResponseWriter, r *http.Request, source string) {
	tx, e := s.pool.Begin(r.Context())
	if e != nil {
		fail(w, e)
		return
	}
	defer tx.Rollback(r.Context())
	c, e := s.config(r.Context(), tx)
	if e != nil {
		fail(w, e)
		return
	}
	if !c.Open {
		fail(w, ErrClosed)
		return
	}
	if !c.Captcha || s.verifier == nil || c.CaptchaConfig.Key == "" {
		fail(w, captcha.ErrUnavailable)
		return
	}
	browser, e := s.browser(r)
	if e != nil {
		fail(w, e)
		return
	}
	var in Intent
	var proof []byte
	e = tx.QueryRow(r.Context(), `SELECT id,browser_hash,csrf_hash,merchant_id,out_trade_no,config_hash,amount,type_id,pay_type,state,order_trade_no,expires_at,proof_hash FROM public_testpay203_intents WHERE browser_hash=$1 AND state='fresh' ORDER BY created_at DESC,id DESC LIMIT 1 FOR UPDATE`, s.sum("browser", []byte(browser))).Scan(&in.ID, &in.Browser, &in.CSRF, &in.MerchantID, &in.OutTradeNo, &in.ConfigHash, &in.Amount, &in.TypeID, &in.PayType, &in.State, &in.OrderTradeNo, &in.ExpiresAt, &proof)
	if e != nil || !hmac.Equal(proof, s.intentProof(in)) {
		fail(w, ErrForbidden)
		return
	}
	if in.ConfigHash != c.Fingerprint {
		fail(w, ErrConflict)
		return
	}
	if e = alive(r.Context(), tx, in.ExpiresAt); e != nil {
		fail(w, e)
		return
	}
	protocol, e := s.verifier.Init(r.Context(), c.CaptchaConfig, "testpay", source)
	if e != nil {
		fail(w, captcha.ErrUnavailable)
		return
	}
	if protocol.Version != c.CaptchaConfig.Version || protocol.GT != c.CaptchaConfig.ID || protocol.Success != 1 || protocol.CaptchaToken != "" {
		fail(w, captcha.ErrUnavailable)
		return
	}
	token, e := randomToken()
	if e != nil {
		fail(w, e)
		return
	}
	record := challengeRecord{Token: s.sum("captcha-token", []byte(token)), Browser: in.Browser, IntentID: in.ID, Purpose: "testpay", Config: c.Fingerprint, Protocol: protocol, Expires: time.Now().UTC().Truncate(time.Microsecond).Add(2 * time.Minute)}
	if record.Expires.After(in.ExpiresAt) {
		record.Expires = in.ExpiresAt
	}
	raw, _ := json.Marshal(protocol)
	if _, e = tx.Exec(r.Context(), `INSERT INTO captcha_challenges203(token_hash,intent_id,browser_hash,purpose,config_hash,protocol,expires_at,proof_hash)VALUES($1,$2,$3,$4,$5,$6::jsonb,$7,$8)`, record.Token, record.IntentID, record.Browser, record.Purpose, record.Config, string(raw), record.Expires, s.challengeProof(record)); e != nil {
		fail(w, e)
		return
	}
	if e = alive(r.Context(), tx, in.ExpiresAt); e != nil {
		fail(w, e)
		return
	}
	if e = tx.Commit(r.Context()); e != nil {
		fail(w, e)
		return
	}
	protocol.CaptchaToken = token
	reply(w, 200, "", protocol)
}
func (s *Service) consume(ctx context.Context, tx pgx.Tx, in Intent, c configuration, proof captcha.Proof, source string) error {
	if s.verifier == nil || c.CaptchaConfig.Key == "" {
		return captcha.ErrUnavailable
	}
	if !tokenPattern.MatchString(proof.CaptchaToken) {
		return captcha.ErrInvalid
	}
	var record challengeRecord
	var raw, seal []byte
	var consumed *time.Time
	e := tx.QueryRow(ctx, `SELECT token_hash,browser_hash,intent_id,purpose,config_hash,protocol,expires_at,consumed_at,proof_hash FROM captcha_challenges203 WHERE token_hash=$1 FOR UPDATE`, s.sum("captcha-token", []byte(proof.CaptchaToken))).Scan(&record.Token, &record.Browser, &record.IntentID, &record.Purpose, &record.Config, &raw, &record.Expires, &consumed, &seal)
	if e != nil || json.Unmarshal(raw, &record.Protocol) != nil || !hmac.Equal(seal, s.challengeProof(record)) || consumed != nil || record.IntentID != in.ID || !hmac.Equal(record.Browser, in.Browser) || record.Purpose != "testpay" || record.Config != c.Fingerprint {
		return captcha.ErrInvalid
	}
	if e = alive(ctx, tx, record.Expires); e != nil {
		return captcha.ErrInvalid
	}
	if e = s.verifier.Verify(ctx, c.CaptchaConfig, record.Protocol, proof, source); e != nil {
		return e
	}
	if e = alive(ctx, tx, record.Expires); e != nil {
		return captcha.ErrInvalid
	}
	if e = alive(ctx, tx, in.ExpiresAt); e != nil {
		return e
	}
	if e = s.credentials.Claim(ctx, tx, c.CaptchaConfig, proof, "testpay", record.Token); e != nil {
		return e
	}
	native := proof.CaptchaID + "\n" + proof.LotNumber
	if c.CaptchaConfig.Version == 0 {
		native = proof.GeetestChallenge + "\n" + proof.GeetestValidate + "\n" + proof.GeetestSeccode
	}
	hash := s.sum("native-proof", []byte(native))
	tag, e := tx.Exec(ctx, `INSERT INTO captcha_used_proofs203(proof_hash,challenge_hash,purpose)VALUES($1,$2,'testpay') ON CONFLICT DO NOTHING`, hash, record.Token)
	if e != nil {
		return e
	}
	if tag.RowsAffected() != 1 {
		return captcha.ErrInvalid
	}
	_, e = tx.Exec(ctx, "UPDATE captcha_challenges203 SET consumed_at=clock_timestamp() WHERE token_hash=$1", record.Token)
	return e
}

type submitInput struct {
	Money  string `json:"money"`
	TypeID string `json:"typeid"`
	CSRF   string `json:"csrf_token"`
	captcha.Proof
	proofFields map[string]bool
}

func (s *Service) submit(w http.ResponseWriter, r *http.Request, input submitInput, source string) {
	browser, e := s.browser(r)
	if e != nil || !tokenPattern.MatchString(input.CSRF) {
		fail(w, ErrForbidden)
		return
	}
	amount, e := ledger.Parse(input.Money)
	id, idErr := strconv.ParseInt(input.TypeID, 10, 64)
	if !moneyPattern.MatchString(input.Money) || e != nil || amount <= 0 || amount > 99999999 || idErr != nil || id <= 0 || strconv.FormatInt(id, 10) != input.TypeID {
		fail(w, ErrInvalid)
		return
	}
	tx, e := s.pool.Begin(r.Context())
	if e != nil {
		fail(w, e)
		return
	}
	defer tx.Rollback(r.Context())
	if e = s.lockBrowserAdmission203(r.Context(), tx, browser); e != nil {
		fail(w, e)
		return
	}
	c, e := s.config(r.Context(), tx)
	if e != nil {
		fail(w, e)
		return
	}
	if !c.Open {
		fail(w, ErrClosed)
		return
	}
	in, e := s.readIntent(r.Context(), tx, browser, input.CSRF)
	if e != nil {
		fail(w, e)
		return
	}
	if in.ConfigHash != c.Fingerprint || in.MerchantID != c.MerchantID {
		fail(w, ErrConflict)
		return
	}
	if in.State != "fresh" {
		if in.Amount != amount || in.TypeID != input.TypeID {
			fail(w, ErrConflict)
			return
		}
		order, e := s.store.Order(r.Context(), in.MerchantID, in.OutTradeNo)
		if e != nil || order.Amount != in.Amount || order.Name != "支付测试" || order.Type != in.PayType || order.Param != "public-testpay203:"+in.ID {
			fail(w, ErrConflict)
			return
		}
		target, e := s.engine.Checkout(r.Context(), order)
		if e != nil {
			fail(w, ErrConflict)
			return
		}
		reply(w, 200, "", map[string]string{"url": target})
		return
	}
	if original, originalError := s.outstandingIntent203(r.Context(), tx, browser); originalError == nil {
		reply(w, 409, "原测试订单结果尚未核实，请只读核对", map[string]string{"trade_no": original.OutTradeNo})
		return
	} else if !errors.Is(originalError, pgx.ErrNoRows) {
		fail(w, originalError)
		return
	}
	if e = alive(r.Context(), tx, in.ExpiresAt); e != nil {
		fail(w, e)
		return
	}
	types, e := s.engine.Types(r.Context(), in.MerchantID)
	if e != nil {
		fail(w, e)
		return
	}
	kind := ""
	for _, typ := range types {
		if typ.ID == input.TypeID {
			kind = typ.Name
		}
	}
	if kind == "" {
		fail(w, ErrInvalid)
		return
	}
	if c.Captcha {
		keys := []string{"captcha_token", "captcha_id", "lot_number", "pass_token", "gen_time", "captcha_output"}
		if c.CaptchaConfig.Version == 0 {
			keys = []string{"captcha_token", "geetest_challenge", "geetest_validate", "geetest_seccode"}
		}
		if len(input.proofFields) != len(keys) {
			fail(w, captcha.ErrInvalid)
			return
		}
		for _, key := range keys {
			if !input.proofFields[key] {
				fail(w, captcha.ErrInvalid)
				return
			}
		}
		if e = s.consume(r.Context(), tx, in, c, input.Proof, source); e != nil {
			fail(w, e)
			return
		}
	} else if len(input.proofFields) > 0 {
		fail(w, ErrInvalid)
		return
	}
	in.Amount, in.TypeID, in.PayType, in.State = amount, input.TypeID, kind, "admitted"
	if e = s.saveIntent(r.Context(), tx, in); e != nil {
		fail(w, e)
		return
	}
	if e = alive(r.Context(), tx, in.ExpiresAt); e != nil {
		fail(w, e)
		return
	}
	if e = tx.Commit(r.Context()); e != nil {
		fail(w, e)
		return
	}
	// Admission and one-time proof consumption commit before any provider work.
	// A crash or uncertain result cannot turn a repeated POST into another send.
	execution, e := s.pool.Begin(r.Context())
	if e != nil {
		fail(w, ErrConflict)
		return
	}
	defer execution.Rollback(r.Context())
	current, e := s.config(r.Context(), execution)
	if e != nil || !current.Open || current.Fingerprint != in.ConfigHash {
		fail(w, ErrConflict)
		return
	}
	in, e = s.readIntent(r.Context(), execution, browser, input.CSRF)
	if e != nil || in.State != "admitted" {
		fail(w, ErrConflict)
		return
	}
	if e = alive(r.Context(), execution, in.ExpiresAt); e != nil {
		fail(w, e)
		return
	}
	trade, target, sendError := s.engine.Create(r.Context(), in, source)
	if sendError != nil {
		in.State = "unknown"
	} else {
		in.State = "submitted"
		in.OrderTradeNo = trade
	}
	if e = s.saveIntent(r.Context(), execution, in); e != nil {
		fail(w, ErrConflict)
		return
	}
	if e = execution.Commit(r.Context()); e != nil {
		fail(w, ErrConflict)
		return
	}
	if sendError != nil {
		fail(w, ErrConflict)
		return
	}
	reply(w, 200, "", map[string]string{"url": target})
}
