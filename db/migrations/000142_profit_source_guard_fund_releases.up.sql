-- 000136 redefined order_profit_source_guard() so that each receiver leg of a
-- multi-receiver 站内分账 rule is checked against its own external_no
-- ('order:<trade>:rule:<rule>:recipient:<recipient>'). It started from the
-- 000052 text of the guard, whose last condition checks channel_sharing_completions.
-- 000055 had already moved that condition to channel_sharing_fund_releases: a
-- channel-sharing hold is released by a completion OR a finish, so an order whose
-- hold was released through a finish must not stay blocked. This migration
-- restores the 000055 release check and keeps 000136's per-recipient rules.
--
-- Forward-only; CREATE OR REPLACE only redefines the function and rewrites no
-- rows. 000136 is already applied on installs, so it is fixed here, not edited.
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
  EXISTS(SELECT 1 FROM channel_sharing_fundings f WHERE f.order_id=o.id AND NOT EXISTS(SELECT 1 FROM channel_sharing_fund_releases c WHERE c.order_id=f.order_id))
 THEN RAISE EXCEPTION 'profit allocation exceeds retained original income'; END IF;
 RETURN NEW;
END $$;
