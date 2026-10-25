CREATE TABLE settlement_bank_payouts (
 request_id bigint PRIMARY KEY,
 transfer_id bigint UNIQUE NOT NULL REFERENCES bank_transfer_lifecycle(transfer_id),
 bank_product text NOT NULL CHECK(bank_product IN ('wxpay_v2','alipay_bankcard')),
 fee_policy text NOT NULL DEFAULT 'hold-and-return-v1' CHECK(fee_policy='hold-and-return-v1'),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 FOREIGN KEY(request_id,transfer_id) REFERENCES settlement_payouts(request_id,transfer_id)
);
CREATE TRIGGER settlement_bank_payout_immutable BEFORE UPDATE OR DELETE ON settlement_bank_payouts FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();

CREATE OR REPLACE FUNCTION bank_unsent_cancel_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF OLD.state='cancelled' AND NEW IS DISTINCT FROM OLD THEN RAISE EXCEPTION 'immutable platform unsent cancellation'; END IF;
 IF NEW.state='cancelled'
 AND NOT EXISTS(SELECT 1 FROM transfer_unsent_cancellations c JOIN transfer_batch_items i ON i.transfer_id=c.transfer_id WHERE c.transfer_id=NEW.transfer_id AND i.state='cancelled')
 AND NOT EXISTS(SELECT 1 FROM settlement_payout_cancellations c JOIN settlement_payouts p ON p.request_id=c.request_id AND p.transfer_id=c.transfer_id JOIN settlement_bank_payouts bp ON bp.request_id=p.request_id AND bp.transfer_id=p.transfer_id WHERE c.transfer_id=NEW.transfer_id AND p.send_state='cancelled' AND p.claimed_at IS NULL)
 THEN RAISE EXCEPTION 'bank cancellation requires durable unsent proof'; END IF;
 RETURN NEW;
END $$;

CREATE FUNCTION settlement_journal_matches(event text, debit_account bigint, credit_account bigint, value bigint) RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT value>0 AND EXISTS(SELECT 1 FROM journals j WHERE j.event_key=event
  AND (SELECT count(*)=2 FROM ledger_entries e WHERE e.journal_id=j.id)
  AND (SELECT COALESCE(sum(e.amount),0) FROM ledger_entries e WHERE e.journal_id=j.id AND e.account_id=debit_account)=-value
  AND (SELECT COALESCE(sum(e.amount),0) FROM ledger_entries e WHERE e.journal_id=j.id AND e.account_id=credit_account)=value);
$$;
-- Capture the frozen 43 definition before replacing the public projection.
-- The copy contains its underlying relations, never a reference to itself.
DO $$ BEGIN EXECUTE 'CREATE VIEW settlement_wallet_funding_invariants AS ' || pg_get_viewdef('settlement_funding_invariants'::regclass,true); END $$;
CREATE VIEW settlement_bank_funding_invariants AS
SELECT f.request_id, COALESCE(
 r.kind='settlements' AND r.status='approved' AND r.amount=f.gross_amount
 AND a.owner_id=r.merchant_id AND a.kind='settlement_reserve' AND a.scope_id=r.id
 AND reserve.event_key='request:'||r.id::text||':reserve'
 AND settlement_journal_matches(reserve.event_key,available.id,a.id,f.gross_amount)
 AND p.gross_amount=f.gross_amount AND p.net_amount=st.net_amount AND p.fee_amount=st.fee_amount
 AND t.merchant_id=r.merchant_id AND t.amount=p.net_amount AND t.cost_amount=p.net_amount AND t.fee_amount=0 AND t.fee_rate_bps=0
 AND pa.channel_id=p.channel_id AND sr.identity_hash=p.recipient_hash AND c.recipient_type='bank' AND c.app_id=''
 AND bp.transfer_id=t.id AND bp.bank_product=b.provider_kind
 AND ((bp.bank_product='wxpay_v2' AND p.provider_kind='wxpay') OR (bp.bank_product='alipay_bankcard' AND p.provider_kind='alipay'))
 AND EXISTS(SELECT 1 FROM provider_transfer_accounts acc WHERE acc.transfer_id=t.id)
 AND EXISTS(SELECT 1 FROM provider_transfer_policies pol WHERE pol.transfer_id=t.id AND pol.merchant_id=r.merchant_id)
 AND (bp.bank_product<>'alipay_bankcard' OR EXISTS(SELECT 1 FROM alipay_bank_bindings ab JOIN provider_transfer_policies pol ON pol.transfer_id=ab.transfer_id WHERE ab.transfer_id=t.id AND ab.request_digest=pol.request_hash))
 AND NOT EXISTS(SELECT 1 FROM transfer_batch_items bi WHERE bi.transfer_id=t.id)
 AND b.service_fee_held=0 AND b.returned_service_fee=0
 AND CASE
 WHEN t.status IN ('pending','unknown') THEN
  p.send_state IN ('unsent','claimed') AND (p.send_state='claimed' OR (t.status='pending' AND pa.state='processing' AND pa.provider_transfer_no=''))
  AND pa.state IN ('processing','pending','unknown') AND (t.status<>'unknown' OR pa.state='unknown')
  AND NOT b.settled AND b.state IN ('processing','reversal_pending') AND b.returned_principal=0
  AND a.balance=f.gross_amount AND (SELECT count(*)=1 FROM ledger_entries e WHERE e.account_id=a.id)
 WHEN t.status='failed' THEN
  pa.state='failed' AND NOT b.settled AND a.balance=0 AND (SELECT count(*)=2 FROM ledger_entries e WHERE e.account_id=a.id)
  AND settlement_journal_matches('settlement-payout:'||r.id::text||':release',a.id,available.id,p.gross_amount)
  AND ((p.send_state='cancelled' AND b.state='cancelled' AND NOT b.return_observed AND EXISTS(SELECT 1 FROM settlement_payout_cancellations pc WHERE pc.request_id=p.request_id AND pc.transfer_id=t.id))
   OR (p.send_state='claimed' AND ((b.state='failed' AND b.returned_principal=0) OR (b.state='reversed' AND b.return_observed AND b.returned_principal=p.net_amount))))
 WHEN t.status='succeeded' THEN
  p.send_state='claimed' AND pa.state='succeeded' AND pa.provider_transfer_no<>'' AND b.settled
  AND settlement_journal_matches('settlement-payout:'||r.id::text||':principal',a.id,clearing.id,p.net_amount)
  AND NOT EXISTS(SELECT 1 FROM journals j WHERE j.event_key='settlement-payout:'||r.id::text||':fee')
  AND CASE WHEN b.state IN ('succeeded','reversal_pending') THEN
   b.returned_principal=0 AND a.balance=p.fee_amount AND (SELECT count(*)=2 FROM ledger_entries e WHERE e.account_id=a.id)
  WHEN b.state='reversed' THEN
   b.return_observed AND b.returned_principal=p.net_amount AND a.balance=0
   AND (SELECT count(*)=CASE WHEN p.fee_amount>0 THEN 3 ELSE 2 END FROM ledger_entries e WHERE e.account_id=a.id)
   AND settlement_journal_matches('transfer:'||t.id::text||':bank-return:settlement-principal',clearing.id,available.id,p.net_amount)
   AND (p.fee_amount=0 OR settlement_journal_matches('transfer:'||t.id::text||':bank-return:settlement-fee',a.id,available.id,p.fee_amount))
  ELSE false END
 ELSE false END,false) AS valid
FROM settlement_fundings f JOIN ledger_requests r ON r.id=f.request_id JOIN accounts a ON a.id=f.account_id JOIN journals reserve ON reserve.id=f.reserve_journal_id
JOIN settlement_payouts p ON p.request_id=r.id JOIN settlement_bank_payouts bp ON bp.request_id=r.id
JOIN settlement_request_terms st ON st.request_id=r.id JOIN transfers t ON t.id=p.transfer_id JOIN provider_transfer_attempts pa ON pa.transfer_id=t.id
JOIN bank_transfer_lifecycle b ON b.transfer_id=t.id JOIN settlement_recipients sr ON sr.request_id=r.id JOIN settlement_recipient_configs c ON c.id=sr.config_id
JOIN accounts available ON available.owner_id=r.merchant_id AND available.kind='available' AND available.scope_id=0
JOIN accounts clearing ON clearing.owner_id=0 AND clearing.kind='clearing' AND clearing.scope_id=0;

CREATE OR REPLACE VIEW settlement_funding_invariants AS
 SELECT w.request_id,w.valid FROM settlement_wallet_funding_invariants w WHERE NOT EXISTS(SELECT 1 FROM settlement_bank_payouts bp WHERE bp.request_id=w.request_id)
 UNION ALL SELECT b.request_id,b.valid FROM settlement_bank_funding_invariants b;
CREATE CONSTRAINT TRIGGER settlement_bank_binding_funding AFTER INSERT ON settlement_bank_payouts DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION settlement_funding_deferred_guard();
CREATE CONSTRAINT TRIGGER settlement_bank_lifecycle_funding AFTER INSERT OR UPDATE ON bank_transfer_lifecycle DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION settlement_transfer_funding_guard();
