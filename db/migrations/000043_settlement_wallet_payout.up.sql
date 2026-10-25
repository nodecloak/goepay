CREATE TABLE settlement_payouts (
 request_id bigint PRIMARY KEY REFERENCES settlement_recipients(request_id),
 transfer_id bigint UNIQUE NOT NULL REFERENCES provider_transfer_attempts(transfer_id),
 channel_id bigint NOT NULL CHECK(channel_id>0),
 provider_kind text NOT NULL CHECK(provider_kind IN ('wxpay','alipay')),
 app_id text NOT NULL CHECK(length(app_id) BETWEEN 1 AND 128),
 recipient_hash bytea NOT NULL CHECK(octet_length(recipient_hash)=32),
 gross_amount bigint NOT NULL CHECK(gross_amount>0),net_amount bigint NOT NULL CHECK(net_amount>0),fee_amount bigint NOT NULL CHECK(fee_amount>=0),
 send_state text NOT NULL DEFAULT 'unsent' CHECK(send_state IN ('unsent','claimed','cancelled')),
 claimed_at timestamptz,cancelled_at timestamptz,created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 CHECK(gross_amount=net_amount+fee_amount),
 CHECK((send_state='unsent' AND claimed_at IS NULL AND cancelled_at IS NULL) OR (send_state='claimed' AND claimed_at IS NOT NULL AND cancelled_at IS NULL) OR (send_state='cancelled' AND claimed_at IS NULL AND cancelled_at IS NOT NULL)),
 UNIQUE(request_id,transfer_id)
);
CREATE FUNCTION settlement_payout_identity_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'immutable settlement payout'; END IF;
 IF ROW(NEW.request_id,NEW.transfer_id,NEW.channel_id,NEW.provider_kind,NEW.app_id,NEW.recipient_hash,NEW.gross_amount,NEW.net_amount,NEW.fee_amount,NEW.created_at) IS DISTINCT FROM ROW(OLD.request_id,OLD.transfer_id,OLD.channel_id,OLD.provider_kind,OLD.app_id,OLD.recipient_hash,OLD.gross_amount,OLD.net_amount,OLD.fee_amount,OLD.created_at) OR OLD.send_state<>'unsent' THEN RAISE EXCEPTION 'immutable settlement payout intent'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER settlement_payout_identity BEFORE UPDATE OR DELETE ON settlement_payouts FOR EACH ROW EXECUTE FUNCTION settlement_payout_identity_guard();
CREATE TABLE settlement_payout_cancellations (
 request_id bigint PRIMARY KEY,transfer_id bigint UNIQUE NOT NULL,created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 FOREIGN KEY(request_id,transfer_id) REFERENCES settlement_payouts(request_id,transfer_id)
);
CREATE TRIGGER settlement_payout_cancellation_immutable BEFORE UPDATE OR DELETE ON settlement_payout_cancellations FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE FUNCTION settlement_payout_cancel_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM settlement_payouts p JOIN transfers t ON t.id=p.transfer_id JOIN provider_transfer_attempts a ON a.transfer_id=t.id WHERE p.request_id=NEW.request_id AND p.transfer_id=NEW.transfer_id AND p.send_state='cancelled' AND p.claimed_at IS NULL AND t.status='pending' AND a.state='processing' AND a.provider_transfer_no='') THEN RAISE EXCEPTION 'settlement cancellation requires proof unsent'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER settlement_payout_cancel BEFORE INSERT ON settlement_payout_cancellations FOR EACH ROW EXECUTE FUNCTION settlement_payout_cancel_guard();
CREATE TABLE settlement_payout_events (
 id bigserial PRIMARY KEY,request_id bigint NOT NULL REFERENCES settlement_payouts(request_id),
 action text NOT NULL CHECK(action IN ('prepare','claim','result','query','cancel')),
 actor_role text NOT NULL CHECK(actor_role IN ('admin','merchant')),actor_id bigint NOT NULL CHECK(actor_id>0),actor_name text NOT NULL CHECK(length(actor_name) BETWEEN 1 AND 128),
 result_state text NOT NULL CHECK(length(result_state)<=64),created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE TRIGGER settlement_payout_event_immutable BEFORE UPDATE OR DELETE ON settlement_payout_events FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();

CREATE OR REPLACE VIEW settlement_funding_invariants AS
SELECT f.request_id,
 r.kind='settlements' AND r.amount=f.gross_amount AND a.owner_id=r.merchant_id AND a.kind='settlement_reserve' AND a.scope_id=r.id
 AND j.event_key='request:'||r.id::text||':reserve'
 AND (SELECT count(*)=2 FROM ledger_entries e WHERE e.journal_id=j.id)
 AND (SELECT COALESCE(sum(e.amount),0) FROM ledger_entries e WHERE e.journal_id=j.id AND e.account_id=a.id)=f.gross_amount
 AND (SELECT COALESCE(sum(e.amount),0) FROM ledger_entries e JOIN accounts x ON x.id=e.account_id WHERE e.journal_id=j.id AND x.owner_id=r.merchant_id AND x.kind='available' AND x.scope_id=0)=-f.gross_amount
 AND CASE WHEN p.request_id IS NULL THEN
  CASE WHEN r.status IN ('pending','approved') THEN a.balance=f.gross_amount AND (SELECT count(*)=1 FROM ledger_entries e WHERE e.account_id=a.id)
  WHEN r.status='rejected' THEN a.balance=0 AND (SELECT count(*)=2 FROM ledger_entries e WHERE e.account_id=a.id)
   AND EXISTS(SELECT 1 FROM journals q WHERE q.event_key='request:'||r.id::text||':review'
    AND (SELECT count(*)=2 FROM ledger_entries e WHERE e.journal_id=q.id)
    AND (SELECT COALESCE(sum(e.amount),0) FROM ledger_entries e WHERE e.journal_id=q.id AND e.account_id=a.id)=-f.gross_amount
    AND (SELECT COALESCE(sum(e.amount),0) FROM ledger_entries e JOIN accounts x ON x.id=e.account_id WHERE e.journal_id=q.id AND x.owner_id=r.merchant_id AND x.kind='available' AND x.scope_id=0)=f.gross_amount)
  ELSE false END
 ELSE
  r.status='approved' AND p.gross_amount=f.gross_amount AND p.net_amount=st.net_amount AND p.fee_amount=st.fee_amount
  AND t.merchant_id=r.merchant_id AND t.amount=p.net_amount AND t.cost_amount=p.net_amount AND t.fee_amount=0 AND t.fee_rate_bps=0
  AND pa.channel_id=p.channel_id AND sr.identity_hash=p.recipient_hash AND c.recipient_type=p.provider_kind
  AND (c.app_id='' OR c.app_id=p.app_id) AND NOT EXISTS(SELECT 1 FROM transfer_batch_items bi WHERE bi.transfer_id=p.transfer_id)
  AND CASE WHEN t.status IN ('pending','unknown') THEN
   p.send_state IN ('unsent','claimed') AND (p.send_state='claimed' OR (t.status='pending' AND pa.state='processing' AND pa.provider_transfer_no=''))
   AND pa.state IN ('processing','pending','unknown') AND (t.status<>'unknown' OR pa.state='unknown')
   AND a.balance=f.gross_amount AND (SELECT count(*)=1 FROM ledger_entries e WHERE e.account_id=a.id)
  WHEN t.status='succeeded' THEN
   p.send_state='claimed' AND pa.state='succeeded' AND pa.provider_transfer_no<>'' AND a.balance=0
   AND (SELECT count(*)=CASE WHEN p.fee_amount>0 THEN 3 ELSE 2 END FROM ledger_entries e WHERE e.account_id=a.id)
   AND EXISTS(SELECT 1 FROM journals q WHERE q.event_key='settlement-payout:'||r.id::text||':principal'
    AND (SELECT count(*)=2 FROM ledger_entries e WHERE e.journal_id=q.id)
    AND (SELECT COALESCE(sum(e.amount),0) FROM ledger_entries e WHERE e.journal_id=q.id AND e.account_id=a.id)=-p.net_amount
    AND (SELECT COALESCE(sum(e.amount),0) FROM ledger_entries e JOIN accounts x ON x.id=e.account_id WHERE e.journal_id=q.id AND x.owner_id=0 AND x.kind='clearing' AND x.scope_id=0)=p.net_amount)
   AND (p.fee_amount=0 OR EXISTS(SELECT 1 FROM journals q WHERE q.event_key='settlement-payout:'||r.id::text||':fee'
    AND (SELECT count(*)=2 FROM ledger_entries e WHERE e.journal_id=q.id)
    AND (SELECT COALESCE(sum(e.amount),0) FROM ledger_entries e WHERE e.journal_id=q.id AND e.account_id=a.id)=-p.fee_amount
    AND (SELECT COALESCE(sum(e.amount),0) FROM ledger_entries e JOIN accounts x ON x.id=e.account_id WHERE e.journal_id=q.id AND x.owner_id=0 AND x.kind='fees' AND x.scope_id=0)=p.fee_amount))
  WHEN t.status='failed' THEN
   pa.state='failed' AND (p.send_state='claimed' OR (p.send_state='cancelled' AND EXISTS(SELECT 1 FROM settlement_payout_cancellations pc WHERE pc.request_id=p.request_id AND pc.transfer_id=p.transfer_id)))
   AND a.balance=0 AND (SELECT count(*)=2 FROM ledger_entries e WHERE e.account_id=a.id)
   AND EXISTS(SELECT 1 FROM journals q WHERE q.event_key='settlement-payout:'||r.id::text||':release'
    AND (SELECT count(*)=2 FROM ledger_entries e WHERE e.journal_id=q.id)
    AND (SELECT COALESCE(sum(e.amount),0) FROM ledger_entries e WHERE e.journal_id=q.id AND e.account_id=a.id)=-p.gross_amount
    AND (SELECT COALESCE(sum(e.amount),0) FROM ledger_entries e JOIN accounts x ON x.id=e.account_id WHERE e.journal_id=q.id AND x.owner_id=r.merchant_id AND x.kind='available' AND x.scope_id=0)=p.gross_amount)
  ELSE false END
 END AS valid
FROM settlement_fundings f JOIN ledger_requests r ON r.id=f.request_id JOIN accounts a ON a.id=f.account_id JOIN journals j ON j.id=f.reserve_journal_id
LEFT JOIN settlement_payouts p ON p.request_id=r.id LEFT JOIN settlement_request_terms st ON st.request_id=r.id LEFT JOIN transfers t ON t.id=p.transfer_id LEFT JOIN provider_transfer_attempts pa ON pa.transfer_id=t.id
LEFT JOIN settlement_recipients sr ON sr.request_id=r.id LEFT JOIN settlement_recipient_configs c ON c.id=sr.config_id;
CREATE CONSTRAINT TRIGGER settlement_payout_funding AFTER INSERT OR UPDATE ON settlement_payouts DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION settlement_funding_deferred_guard();
CREATE FUNCTION settlement_transfer_funding_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE rid bigint;
BEGIN
 IF TG_TABLE_NAME='transfers' THEN SELECT request_id INTO rid FROM settlement_payouts WHERE transfer_id=NEW.id;
 ELSE SELECT request_id INTO rid FROM settlement_payouts WHERE transfer_id=NEW.transfer_id; END IF;
 IF rid IS NOT NULL AND NOT EXISTS(SELECT 1 FROM settlement_funding_invariants WHERE request_id=rid AND valid) THEN RAISE EXCEPTION 'invalid settlement payout funding'; END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER settlement_transfer_funding AFTER UPDATE ON transfers DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION settlement_transfer_funding_guard();
CREATE CONSTRAINT TRIGGER settlement_attempt_funding AFTER UPDATE ON provider_transfer_attempts DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION settlement_transfer_funding_guard();
