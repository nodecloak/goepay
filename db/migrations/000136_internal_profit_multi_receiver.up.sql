-- 站内分账 multi-receiver allocation (a4 N3). The original pre_psinternal `info`
-- carried a list of recipient+rate legs; GOEPAY authors and validates that list
-- in the profitsharing-rules body (a `receivers` array), and this migration adds
-- the allocation-table support so each leg can be paid as its own ledger request
-- while the existing immutability/evidence invariants are kept.
--
-- order_profit_allocations was UNIQUE(order_id,rule_id): a rule allocates to an
-- order exactly once. To pay multiple receivers of one rule, each leg becomes its
-- own row keyed additionally by recipient_id. This migration is ADDITIVE and does
-- not touch existing rows: it adds a NULLable recipient_id column (existing
-- single-receiver rows keep NULL and remain governed by the original
-- UNIQUE(order_id,rule_id)), and adds a partial unique index covering only the
-- new per-receiver rows (recipient_id IS NOT NULL). No backfill, no down.
--
-- Forward-only, idempotent (IF NOT EXISTS / guarded), safe on existing installs.

ALTER TABLE order_profit_allocations
  ADD COLUMN IF NOT EXISTS recipient_id bigint REFERENCES merchants(id);

-- The original UNIQUE(order_id,rule_id) allowed a rule to allocate to an order
-- exactly once. Multi-receiver needs one row per leg, so replace it with two
-- partial unique indexes that together keep the same guarantee: at most one
-- legacy single-receiver row (recipient_id NULL) per (order,rule), and at most
-- one row per (order,rule,recipient) for multi-receiver legs. Dropping the old
-- constraint changes no existing row; the NULL-partial index re-covers them.
ALTER TABLE order_profit_allocations
  DROP CONSTRAINT IF EXISTS order_profit_allocations_order_id_rule_id_key;
CREATE UNIQUE INDEX IF NOT EXISTS order_profit_allocations_single_uniq
  ON order_profit_allocations(order_id, rule_id)
  WHERE recipient_id IS NULL;

-- Per-receiver rows of a multi-receiver rule are unique on (order,rule,recipient).
-- Partial so it never conflicts with legacy rows whose recipient_id is NULL.
CREATE UNIQUE INDEX IF NOT EXISTS order_profit_allocations_recipient_uniq
  ON order_profit_allocations(order_id, rule_id, recipient_id)
  WHERE recipient_id IS NOT NULL;

-- Help the execution-time idempotency lookup and per-recipient reporting.
CREATE INDEX IF NOT EXISTS order_profit_allocations_recipient_idx
  ON order_profit_allocations(recipient_id)
  WHERE recipient_id IS NOT NULL;

-- Extend the allocation evidence + source guard so a per-leg row (recipient_id
-- NOT NULL) is proved against a request whose external_no carries the recipient
-- suffix 'order:<trade>:rule:<rule>:recipient:<recipient>', while a legacy single
-- row (recipient_id NULL) keeps the original 'order:<trade>:rule:<rule>' form.
-- Each leg's amount is still floor(order.amount * rate_bps / 10000) — a per-leg
-- floor, remainder dropped (never redistributed), matching the original. The
-- per-leg request's recipient must equal the row's recipient_id. Everything else
-- (reserve/review journals, pending event, audit) is unchanged. CREATE OR REPLACE
-- only redefines logic; it rewrites no rows.
CREATE OR REPLACE VIEW order_income_internal_allocation_evidence AS
SELECT a.id,a.order_id,a.amount,r.status,
 r.kind='profitsharing' AND r.merchant_id=o.merchant_id AND r.amount=a.amount AND r.recipient_id>0 AND r.recipient_id<>o.merchant_id
 AND (a.recipient_id IS NULL OR r.recipient_id=a.recipient_id)
 AND r.external_no = CASE WHEN a.recipient_id IS NULL
   THEN 'order:'||o.trade_no||':rule:'||a.rule_id::text
   ELSE 'order:'||o.trade_no||':rule:'||a.rule_id::text||':recipient:'||a.recipient_id::text END
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

CREATE OR REPLACE FUNCTION order_profit_source_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE e order_income_source_evidence%ROWTYPE; o orders%ROWTYPE; request ledger_requests%ROWTYPE;
BEGIN
 PERFORM id FROM orders WHERE id=NEW.order_id FOR UPDATE;
 SELECT * INTO STRICT o FROM orders WHERE id=NEW.order_id;
 SELECT * INTO e FROM order_income_source_evidence WHERE order_id=NEW.order_id;
 SELECT * INTO STRICT request FROM ledger_requests WHERE id=NEW.request_id;
 IF e.valid IS NOT TRUE OR e.pending_refunds<>0 OR NEW.amount>e.remaining OR NEW.amount<>floor(o.amount::numeric*NEW.rate_bps/10000) OR
  request.kind<>'profitsharing' OR request.status<>'pending' OR request.merchant_id<>o.merchant_id OR request.amount<>NEW.amount OR
  (NEW.recipient_id IS NOT NULL AND request.recipient_id<>NEW.recipient_id) OR
  request.external_no<>(CASE WHEN NEW.recipient_id IS NULL
    THEN 'order:'||o.trade_no||':rule:'||NEW.rule_id::text
    ELSE 'order:'||o.trade_no||':rule:'||NEW.rule_id::text||':recipient:'||NEW.recipient_id::text END) OR
  EXISTS(SELECT 1 FROM order_income_holds WHERE order_id=o.id AND state='held') OR
  EXISTS(SELECT 1 FROM channel_sharing_fundings f WHERE f.order_id=o.id AND NOT EXISTS(SELECT 1 FROM channel_sharing_completions c WHERE c.order_id=f.order_id))
 THEN RAISE EXCEPTION 'profit allocation exceeds retained original income'; END IF;
 RETURN NEW;
END $$;
