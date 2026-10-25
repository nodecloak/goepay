-- Retained order income is an original-source budget, independent from the
-- merchant's fungible available wallet. Completed merchant-funded refunds may
-- consume other wallet income without creating new income on this order.
CREATE VIEW order_income_internal_allocation_evidence AS
SELECT a.id,a.order_id,a.amount,r.status,
 r.kind='profitsharing' AND r.merchant_id=o.merchant_id AND r.amount=a.amount AND r.recipient_id>0 AND r.recipient_id<>o.merchant_id
 AND r.external_no='order:'||o.trade_no||':rule:'||a.rule_id::text
 AND EXISTS(SELECT 1 FROM journals j WHERE j.event_key='request:'||r.id::text||':reserve'
  AND (SELECT count(*)=2 FROM ledger_entries e WHERE e.journal_id=j.id)
  AND (SELECT COALESCE(sum(e.amount),0) FROM ledger_entries e JOIN accounts av ON av.id=e.account_id WHERE e.journal_id=j.id AND (av.owner_id,av.kind,av.scope_id)=(o.merchant_id,'available',0))=-a.amount
  AND (SELECT COALESCE(sum(e.amount),0) FROM ledger_entries e JOIN accounts av ON av.id=e.account_id WHERE e.journal_id=j.id AND (av.owner_id,av.kind,av.scope_id)=(o.merchant_id,'reserved',0))=a.amount)
 AND EXISTS(SELECT 1 FROM ledger_request_events ev WHERE ev.request_id=r.id AND ev.status='pending')
 AND CASE WHEN r.status='pending' THEN NOT EXISTS(SELECT 1 FROM journals j WHERE j.event_key='request:'||r.id::text||':review') ELSE
  EXISTS(SELECT 1 FROM ledger_request_events ev WHERE ev.request_id=r.id AND ev.status=r.status)
  AND EXISTS(SELECT 1 FROM journals j WHERE j.event_key='request:'||r.id::text||':review'
   AND (SELECT count(*)=2 FROM ledger_entries e WHERE e.journal_id=j.id)
   AND (SELECT COALESCE(sum(e.amount),0) FROM ledger_entries e JOIN accounts av ON av.id=e.account_id WHERE e.journal_id=j.id AND (av.owner_id,av.kind,av.scope_id)=(o.merchant_id,'reserved',0))=-a.amount
   AND (SELECT COALESCE(sum(e.amount),0) FROM ledger_entries e JOIN accounts av ON av.id=e.account_id WHERE e.journal_id=j.id AND (av.owner_id,av.kind,av.scope_id)=(CASE WHEN r.status='approved' THEN r.recipient_id ELSE o.merchant_id END,'available',0))=a.amount)
 END AS valid
FROM order_profit_allocations a JOIN ledger_requests r ON r.id=a.request_id JOIN orders o ON o.id=a.order_id;

-- Migration 53 may reduce this only by independently proved successful PR.
-- Pending and unknown returns never restore this original source budget.
CREATE FUNCTION order_income_channel_sharing_outflow(oid bigint) RETURNS bigint LANGUAGE sql STABLE AS $$
 SELECT COALESCE(sum(i.amount) FILTER(WHERE i.state='succeeded'),0)::bigint
 FROM channel_sharing_orders s JOIN channel_sharing_order_items i ON i.sharing_id=s.id WHERE s.order_id=oid AND EXISTS(SELECT 1 FROM channel_sharing_funding_invariants v WHERE v.order_id=oid AND v.valid IS TRUE)
$$;

CREATE VIEW order_income_source_evidence AS
SELECT o.id AS order_id,o.merchant_id,o.merchant_amount AS merchant_net,
 r.gross AS refund_gross,r.amount AS refund_merchant_amount,r.pending AS pending_refunds,
 p.amount AS channel_sharing_amount,a.amount AS internal_allocated_amount,
 GREATEST(0::numeric,o.merchant_amount::numeric-r.amount-p.amount-a.amount)::bigint AS remaining,
 o.status='paid' AND o.merchant_amount>0 AND NOT EXISTS(SELECT 1 FROM recharge_intents z WHERE z.order_id=o.id)
 AND r.gross<=o.amount AND r.valid AND a.valid
 AND EXISTS(SELECT 1 FROM journals j WHERE j.event_key='payment:'||o.trade_no
  AND (SELECT count(*)=2 FROM ledger_entries e WHERE e.journal_id=j.id)
  AND (SELECT COALESCE(sum(e.amount),0) FROM ledger_entries e JOIN accounts av ON av.id=e.account_id WHERE e.journal_id=j.id AND (av.owner_id,av.kind,av.scope_id)=(o.merchant_id,'available',0))=o.merchant_amount
  AND (SELECT COALESCE(sum(e.amount),0) FROM ledger_entries e JOIN accounts cl ON cl.id=e.account_id WHERE e.journal_id=j.id AND (cl.owner_id,cl.kind,cl.scope_id)=(0,'clearing',0))=-o.merchant_amount)
 AND NOT EXISTS(SELECT 1 FROM channel_sharing_fundings f WHERE f.order_id=o.id AND NOT EXISTS(SELECT 1 FROM channel_sharing_funding_invariants v WHERE v.order_id=f.order_id AND v.valid IS TRUE))
 AND NOT EXISTS(SELECT 1 FROM provider_attempt_sharing d WHERE d.order_id=o.id AND d.declared AND NOT EXISTS(SELECT 1 FROM channel_sharing_fundings f WHERE f.order_id=d.order_id)) AS valid
FROM orders o
CROSS JOIN LATERAL (
 SELECT COALESCE(sum(f.amount) FILTER(WHERE f.status='succeeded'),0)::bigint AS gross,
 COALESCE(sum(f.merchant_amount) FILTER(WHERE f.status='succeeded'),0)::bigint AS amount,
 count(*) FILTER(WHERE f.status IN ('pending','unknown')) AS pending,
 COALESCE(bool_and(f.status<>'succeeded' OR f.merchant_amount=0 OR
  EXISTS(SELECT 1 FROM journals j WHERE j.event_key='refund:'||f.id::text||':reserve'
   AND (SELECT count(*)=2 FROM ledger_entries e WHERE e.journal_id=j.id)
   AND (SELECT COALESCE(sum(e.amount),0) FROM ledger_entries e JOIN accounts av ON av.id=e.account_id WHERE e.journal_id=j.id AND (av.owner_id,av.kind,av.scope_id)=(o.merchant_id,'available',0))=-f.merchant_amount
   AND (SELECT COALESCE(sum(e.amount),0) FROM ledger_entries e JOIN accounts av ON av.id=e.account_id WHERE e.journal_id=j.id AND (av.owner_id,av.kind,av.scope_id)=(o.merchant_id,'reserved',0))=f.merchant_amount)
  AND EXISTS(SELECT 1 FROM journals j WHERE j.event_key='refund:'||f.id::text||':resolve'
   AND (SELECT count(*)=2 FROM ledger_entries e WHERE e.journal_id=j.id)
   AND (SELECT COALESCE(sum(e.amount),0) FROM ledger_entries e JOIN accounts av ON av.id=e.account_id WHERE e.journal_id=j.id AND (av.owner_id,av.kind,av.scope_id)=(o.merchant_id,'reserved',0))=-f.merchant_amount
   AND (SELECT COALESCE(sum(e.amount),0) FROM ledger_entries e JOIN accounts cl ON cl.id=e.account_id WHERE e.journal_id=j.id AND (cl.owner_id,cl.kind,cl.scope_id)=(0,'clearing',0))=f.merchant_amount)),true) AS valid
 FROM refunds f WHERE f.order_id=o.id
) r
CROSS JOIN LATERAL (
 SELECT order_income_channel_sharing_outflow(o.id) AS amount
) p
CROSS JOIN LATERAL (
 SELECT COALESCE(sum(i.amount) FILTER(WHERE i.status<>'rejected'),0)::bigint AS amount,COALESCE(bool_and(i.valid IS TRUE),true) AS valid
 FROM order_income_internal_allocation_evidence i WHERE i.order_id=o.id
) a;

ALTER TABLE order_income_holds ADD COLUMN channel_sharing_amount bigint NOT NULL DEFAULT 0 CHECK(channel_sharing_amount>=0);
ALTER TABLE order_income_holds ADD COLUMN internal_allocated_amount bigint NOT NULL DEFAULT 0 CHECK(internal_allocated_amount>=0);
ALTER TABLE order_income_holds ADD COLUMN evidence_version integer NOT NULL DEFAULT 1 CHECK(evidence_version IN (1,2));
ALTER TABLE order_income_holds ALTER COLUMN evidence_version SET DEFAULT 2;
ALTER TABLE order_income_holds DROP CONSTRAINT order_income_holds_check;
ALTER TABLE order_income_holds ADD CONSTRAINT order_income_retained_balanced CHECK(amount::numeric+refund_merchant_amount+channel_sharing_amount+internal_allocated_amount=merchant_net);
CREATE FUNCTION order_income_source_snapshot_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE e order_income_source_evidence%ROWTYPE;
BEGIN
 IF TG_OP='UPDATE' THEN
  IF (NEW.channel_sharing_amount,NEW.internal_allocated_amount,NEW.evidence_version) IS DISTINCT FROM (OLD.channel_sharing_amount,OLD.internal_allocated_amount,OLD.evidence_version) THEN RAISE EXCEPTION 'immutable income source snapshot'; END IF;RETURN NEW;
 END IF;
 PERFORM id FROM orders WHERE id=NEW.order_id FOR UPDATE;
 SELECT * INTO e FROM order_income_source_evidence WHERE order_id=NEW.order_id;
 IF NEW.evidence_version<>2 OR e.order_id IS NULL OR e.valid IS NOT TRUE OR e.pending_refunds<>0 OR e.remaining<=0 OR
  (NEW.merchant_id,NEW.merchant_net,NEW.refund_merchant_amount,NEW.channel_sharing_amount,NEW.internal_allocated_amount,NEW.amount) IS DISTINCT FROM (e.merchant_id,e.merchant_net,e.refund_merchant_amount,e.channel_sharing_amount,e.internal_allocated_amount,e.remaining)
 THEN RAISE EXCEPTION 'income hold differs from retained original source evidence'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER order_income_source_snapshot BEFORE INSERT OR UPDATE ON order_income_holds FOR EACH ROW EXECUTE FUNCTION order_income_source_snapshot_guard();

CREATE FUNCTION order_profit_source_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE e order_income_source_evidence%ROWTYPE; o orders%ROWTYPE; request ledger_requests%ROWTYPE;
BEGIN
 PERFORM id FROM orders WHERE id=NEW.order_id FOR UPDATE;
 SELECT * INTO STRICT o FROM orders WHERE id=NEW.order_id;
 SELECT * INTO e FROM order_income_source_evidence WHERE order_id=NEW.order_id;
 SELECT * INTO STRICT request FROM ledger_requests WHERE id=NEW.request_id;
 IF e.valid IS NOT TRUE OR e.pending_refunds<>0 OR NEW.amount>e.remaining OR NEW.amount<>floor(o.amount::numeric*NEW.rate_bps/10000) OR
  request.kind<>'profitsharing' OR request.status<>'pending' OR request.merchant_id<>o.merchant_id OR request.amount<>NEW.amount OR request.external_no<>'order:'||o.trade_no||':rule:'||NEW.rule_id::text OR
  EXISTS(SELECT 1 FROM order_income_holds WHERE order_id=o.id AND state='held') OR
  EXISTS(SELECT 1 FROM channel_sharing_fundings f WHERE f.order_id=o.id AND NOT EXISTS(SELECT 1 FROM channel_sharing_completions c WHERE c.order_id=f.order_id))
 THEN RAISE EXCEPTION 'profit allocation exceeds retained original income'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER order_profit_source BEFORE INSERT ON order_profit_allocations FOR EACH ROW EXECUTE FUNCTION order_profit_source_guard();
CREATE FUNCTION order_profit_request_evidence_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE rid bigint;
BEGIN
 IF TG_TABLE_NAME='ledger_requests' THEN rid:=NEW.id; ELSE rid:=NEW.request_id; END IF;
 IF EXISTS(SELECT 1 FROM order_profit_allocations a WHERE a.request_id=rid AND NOT EXISTS(SELECT 1 FROM order_income_internal_allocation_evidence v WHERE v.id=a.id AND v.valid IS TRUE)) THEN RAISE EXCEPTION 'internal order allocation lacks original request journals and audit'; END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER order_profit_request_evidence AFTER INSERT ON order_profit_allocations DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION order_profit_request_evidence_guard();
CREATE CONSTRAINT TRIGGER order_profit_review_evidence AFTER UPDATE ON ledger_requests DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION order_profit_request_evidence_guard();

CREATE FUNCTION order_income_holding_account_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF OLD.kind='order_income_hold' AND (TG_OP='DELETE' OR (NEW.owner_id,NEW.kind,NEW.scope_id) IS DISTINCT FROM (OLD.owner_id,OLD.kind,OLD.scope_id)) THEN RAISE EXCEPTION 'immutable order income holding account'; END IF;
 RETURN CASE WHEN TG_OP='DELETE' THEN OLD ELSE NEW END;
END $$;
CREATE TRIGGER order_income_holding_account_identity BEFORE UPDATE OR DELETE ON accounts FOR EACH ROW EXECUTE FUNCTION order_income_holding_account_guard();
CREATE VIEW order_income_hold_funding_evidence AS
SELECT h.id,h.merchant_id,
 EXISTS(SELECT 1 FROM journals j WHERE j.event_key='order-income:'||h.id::text||':freeze'
  AND (SELECT count(*)=2 FROM ledger_entries e WHERE e.journal_id=j.id)
  AND (SELECT COALESCE(sum(e.amount),0) FROM ledger_entries e JOIN accounts av ON av.id=e.account_id WHERE e.journal_id=j.id AND (av.owner_id,av.kind,av.scope_id)=(h.merchant_id,'available',0))=-h.amount
  AND (SELECT COALESCE(sum(e.amount),0) FROM ledger_entries e JOIN accounts av ON av.id=e.account_id WHERE e.journal_id=j.id AND (av.owner_id,av.kind,av.scope_id)=(h.merchant_id,'order_income_hold',0))=h.amount)
 AND EXISTS(SELECT 1 FROM order_income_hold_events ev JOIN backoffice_audit a ON a.actor_id=ev.actor_id AND a.actor_name=ev.actor_name AND a.actor_role='admin' AND a.resource='orders' AND a.resource_id=h.order_id AND a.operation='freeze-income' AND a.changes->>'hold_id'=h.id::text AND a.changes->>'amount'=h.amount::text WHERE ev.hold_id=h.id AND ev.action='freeze')
 AND CASE WHEN h.state='held' THEN NOT EXISTS(SELECT 1 FROM journals j WHERE j.event_key='order-income:'||h.id::text||':unfreeze') ELSE
  EXISTS(SELECT 1 FROM journals j WHERE j.event_key='order-income:'||h.id::text||':unfreeze'
   AND (SELECT count(*)=2 FROM ledger_entries e WHERE e.journal_id=j.id)
   AND (SELECT COALESCE(sum(e.amount),0) FROM ledger_entries e JOIN accounts av ON av.id=e.account_id WHERE e.journal_id=j.id AND (av.owner_id,av.kind,av.scope_id)=(h.merchant_id,'order_income_hold',0))=-h.amount
   AND (SELECT COALESCE(sum(e.amount),0) FROM ledger_entries e JOIN accounts av ON av.id=e.account_id WHERE e.journal_id=j.id AND (av.owner_id,av.kind,av.scope_id)=(h.merchant_id,'available',0))=h.amount)
  AND EXISTS(SELECT 1 FROM order_income_hold_events ev JOIN backoffice_audit a ON a.actor_id=ev.actor_id AND a.actor_name=ev.actor_name AND a.actor_role='admin' AND a.resource='orders' AND a.resource_id=h.order_id AND a.operation='unfreeze-income' AND a.changes->>'hold_id'=h.id::text AND a.changes->>'amount'=h.amount::text WHERE ev.hold_id=h.id AND ev.action='unfreeze')
 END AS valid
FROM order_income_holds h;
CREATE FUNCTION order_income_holding_funding_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE owner bigint;
BEGIN
 IF TG_TABLE_NAME='accounts' THEN owner:=NEW.owner_id; ELSE owner:=NEW.merchant_id; END IF;
 IF NOT EXISTS(SELECT 1 FROM accounts a WHERE (a.owner_id,a.kind,a.scope_id)=(owner,'order_income_hold',0) AND a.balance=(SELECT COALESCE(sum(h.amount),0) FROM order_income_holds h WHERE h.merchant_id=owner AND h.state='held'))
 OR EXISTS(SELECT 1 FROM order_income_holds h WHERE h.merchant_id=owner AND NOT EXISTS(SELECT 1 FROM order_income_hold_funding_evidence v WHERE v.id=h.id AND v.valid IS TRUE))
 THEN RAISE EXCEPTION 'income hold lacks its original funding and actor proof'; END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER order_income_holding_funding AFTER INSERT OR UPDATE ON order_income_holds DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION order_income_holding_funding_guard();
CREATE CONSTRAINT TRIGGER order_income_holding_account_funding AFTER INSERT OR UPDATE ON accounts DEFERRABLE INITIALLY DEFERRED FOR EACH ROW WHEN(NEW.kind='order_income_hold') EXECUTE FUNCTION order_income_holding_funding_guard();
