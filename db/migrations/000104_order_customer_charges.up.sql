-- All genuinely new104 quotes/orders create a dual-amount birth. Existing order
-- columns, values, financial proofs and migration files are not rewritten.
ALTER TABLE orders ADD COLUMN actual_charge_amount104 bigint CHECK(actual_charge_amount104>0);
ALTER TABLE orders ADD COLUMN actual_charge_required104 boolean NOT NULL DEFAULT false;
ALTER TABLE orders DROP CONSTRAINT order_fee_balanced;
ALTER TABLE orders ADD CONSTRAINT order_fee_balanced CHECK(merchant_amount>=0 AND merchant_amount<=COALESCE(actual_charge_amount104,amount) AND platform_fee=COALESCE(actual_charge_amount104,amount)-merchant_amount);
ALTER TABLE orders DROP CONSTRAINT orders_fee_policy_check;
ALTER TABLE orders ADD CONSTRAINT orders_fee_policy_check CHECK(fee_policy IN ('none','merchant_deduct') OR (fee_policy='buyer_additive' AND actual_charge_required104 AND actual_charge_amount104 IS NOT NULL));
CREATE FUNCTION order_customer_charge104_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.actual_charge_required104<>(NEW.actual_charge_amount104 IS NOT NULL) OR (NEW.actual_charge_amount104 IS NOT NULL AND NEW.actual_charge_amount104<NEW.amount) THEN RAISE EXCEPTION 'invalid authenticated customer charge birth'; END IF;
 IF TG_OP='UPDATE' AND (NEW.actual_charge_amount104,NEW.actual_charge_required104) IS DISTINCT FROM (OLD.actual_charge_amount104,OLD.actual_charge_required104) THEN RAISE EXCEPTION 'immutable customer charge birth'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER order_customer_charge104_immutable BEFORE INSERT OR UPDATE ON orders FOR EACH ROW EXECUTE FUNCTION order_customer_charge104_guard();

CREATE TABLE customer_charge_policies104 (
 kind text NOT NULL CHECK(kind IN ('merchant','global','parent')),
 owner_id bigint NOT NULL,
 merchant_id bigint REFERENCES merchants(id),
 revision bigint NOT NULL CHECK(revision>0),
 facts jsonb NOT NULL CHECK(jsonb_typeof(facts)='object'),
 proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32),
 PRIMARY KEY(kind,owner_id),
 CHECK((kind='global' AND owner_id=0 AND merchant_id IS NULL) OR (kind='parent' AND owner_id>0 AND merchant_id IS NULL) OR (kind='merchant' AND owner_id>0 AND merchant_id=owner_id))
);
CREATE FUNCTION customer_charge_policy104_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'customer charge policy deletion prohibited'; END IF;
 IF (NEW.kind,NEW.owner_id,NEW.merchant_id) IS DISTINCT FROM (OLD.kind,OLD.owner_id,OLD.merchant_id) OR NEW.revision<>OLD.revision+1 THEN RAISE EXCEPTION 'invalid customer charge policy revision'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER customer_charge_policy104_mutation BEFORE UPDATE OR DELETE ON customer_charge_policies104 FOR EACH ROW EXECUTE FUNCTION customer_charge_policy104_guard();
CREATE TABLE customer_charge_policy_witnesses104 (
 kind text NOT NULL CHECK(kind IN('merchant','global','parent')),
 owner_id bigint NOT NULL,
 revision bigint NOT NULL CHECK(revision>0),
 commitment text NOT NULL CHECK(commitment ~ '^[0-9a-f]{64}$'),
 proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32),
 PRIMARY KEY(kind,owner_id,revision),
 CHECK((kind='global' AND owner_id=0) OR (kind<>'global' AND owner_id>0))
);
CREATE TRIGGER customer_charge_policy104_witness_immutable BEFORE UPDATE OR DELETE ON customer_charge_policy_witnesses104 FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE TABLE order_customer_charges104 (
 order_id bigint PRIMARY KEY REFERENCES orders(id),
 merchant_id bigint NOT NULL REFERENCES merchants(id),
 facts jsonb NOT NULL CHECK(jsonb_typeof(facts)='object'),
 proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32)
);
CREATE TRIGGER order_customer_charge104_source_immutable BEFORE UPDATE OR DELETE ON order_customer_charges104 FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE INDEX customer_charge_policy104_audit ON backoffice_audit(resource_id,(changes->>'kind'),id DESC) WHERE resource='customer-charge-policies104';
CREATE INDEX order_customer_charge104_audit ON backoffice_audit(resource_id,id DESC) WHERE resource='order-customer-charges104';

-- Actual customer gross for new104 births; NULL preserves every old row.
-- 000054_channel_sharing_payment_purpose.up.sql provider_attempt_sharing_guard
CREATE OR REPLACE FUNCTION provider_attempt_sharing_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE aid bigint; d provider_attempt_sharing%ROWTYPE; r channel_sharing_rules%ROWTYPE; expected_count integer; actual_count integer; expected_sum numeric; actual_sum numeric;
BEGIN
 IF TG_TABLE_NAME='provider_attempt_sharing' THEN aid:=NEW.attempt_id;
 ELSIF TG_TABLE_NAME='provider_attempt_sharing_items' THEN aid:=NEW.attempt_id;
 ELSE aid:=NEW.attempt_id; END IF;
 SELECT * INTO d FROM provider_attempt_sharing WHERE attempt_id=aid;
 IF d.attempt_id IS NULL THEN RAISE EXCEPTION 'missing original sharing declaration'; END IF;
 IF NOT EXISTS(SELECT 1 FROM provider_attempt_accounts WHERE attempt_id=aid) THEN RAISE EXCEPTION 'missing original payment account proof'; END IF;
 IF NOT EXISTS(SELECT 1 FROM provider_attempts a JOIN orders o ON o.id=a.order_id WHERE a.id=aid AND
  (a.order_id,a.channel_id,a.method,o.merchant_id,o.type,COALESCE(o.actual_charge_amount104,o.amount),o.merchant_amount)=(d.order_id,d.channel_id,d.method,d.merchant_id,d.provider,d.gross_amount,d.merchant_net))
 THEN RAISE EXCEPTION 'invalid original sharing payment binding'; END IF;
 IF d.rule_id IS NOT NULL THEN
  SELECT * INTO STRICT r FROM channel_sharing_rules WHERE id=d.rule_id;
  IF (r.merchant_id,r.channel_id) IS DISTINCT FROM (d.merchant_id,d.channel_id) THEN RAISE EXCEPTION 'sharing policy owner mismatch'; END IF;
 END IF;
 SELECT count(*),COALESCE(sum(amount),0) INTO actual_count,actual_sum FROM provider_attempt_sharing_items WHERE attempt_id=aid;
 IF actual_count<>d.receiver_count OR actual_sum>d.merchant_net THEN RAISE EXCEPTION 'invalid sharing payment allocations'; END IF;
 IF d.purpose='recharge' AND d.declared THEN RAISE EXCEPTION 'balance recharge cannot declare profit sharing'; END IF;
 IF NOT d.declared THEN
  IF d.purpose='payment' AND d.rule_id IS NOT NULL AND r.enabled AND r.methods ? d.method AND d.gross_amount>=r.minimum_amount AND EXISTS(SELECT 1 FROM channel_sharing_rule_items i WHERE i.rule_id=r.id AND CASE WHEN i.mode='fixed' THEN i.fixed_amount ELSE floor(d.gross_amount::numeric*i.rate_bps/10000) END>0)
  THEN RAISE EXCEPTION 'effective sharing policy requires an explicit declaration'; END IF;
  RETURN NULL;
 END IF;
 IF NOT r.enabled OR NOT(r.methods ? d.method) OR d.gross_amount<r.minimum_amount OR
  (r.provider,r.app_id,r.product,r.account_hash,r.refund_mode,r.auto_finish) IS DISTINCT FROM (d.provider,d.app_id,d.product,d.account_hash,d.refund_mode,d.auto_finish)
 THEN RAISE EXCEPTION 'original sharing declaration policy mismatch'; END IF;
 SELECT count(*),COALESCE(sum(amount),0) INTO expected_count,expected_sum FROM
  (SELECT CASE WHEN mode='fixed' THEN fixed_amount ELSE floor(d.gross_amount::numeric*rate_bps/10000) END AS amount FROM channel_sharing_rule_items WHERE rule_id=r.id) x WHERE amount>0;
 IF actual_count<>expected_count OR actual_sum<>expected_sum OR EXISTS(
  SELECT 1 FROM provider_attempt_sharing_items x LEFT JOIN channel_sharing_rule_items i ON i.rule_id=r.id AND i.receiver_id=x.receiver_id
  WHERE x.attempt_id=aid AND (i.rule_id IS NULL OR x.amount<>CASE WHEN i.mode='fixed' THEN i.fixed_amount ELSE floor(d.gross_amount::numeric*i.rate_bps/10000) END))
 THEN RAISE EXCEPTION 'sharing allocations differ from original policy'; END IF;
 RETURN NULL;
END $$;
-- 000079_wechat_v2_single_sharing.up.sql channel_sharing_finish_invariants
CREATE OR REPLACE VIEW channel_sharing_finish_invariants AS
SELECT p.id AS finish_id,p.order_id,
 d.declared AND d.purpose='payment' AND d.product IN ('wxpay_v3_direct','wxpay_v2_single_direct') AND o.status='paid'
 AND (p.merchant_id,p.gross_amount,p.merchant_net,p.release_amount,p.shared_amount,p.expected_unsplit)=(o.merchant_id,COALESCE(o.actual_charge_amount104,o.amount),o.merchant_amount,f.amount,0,COALESCE(o.actual_charge_amount104,o.amount))
 AND (a.owner_id,a.kind,a.scope_id)=(o.merchant_id,'channel_sharing_reserve',o.id)
 AND EXISTS(SELECT 1 FROM verified_payment_receipts v WHERE v.order_id=o.id AND v.channel_id=d.channel_id AND v.provider_trade_no=p.provider_trade_no)
 AND NOT EXISTS(SELECT 1 FROM channel_sharing_completions WHERE order_id=o.id)
 AND (p.original_sharing_id IS NULL AND NOT EXISTS(SELECT 1 FROM channel_sharing_orders WHERE order_id=o.id) OR EXISTS(SELECT 1 FROM channel_sharing_orders s WHERE s.id=p.original_sharing_id AND s.order_id=o.id AND s.state='failed' AND NOT s.completed AND s.last_observation_id IS NULL AND s.failure_proof IS NOT NULL))
 AND (p.previous_finish_id IS NULL OR EXISTS(SELECT 1 FROM channel_sharing_finishes old WHERE old.id=p.previous_finish_id AND old.order_id=p.order_id AND old.id<p.id AND old.state='failed'))
 AND p.state<>'unsent' AND p.claimed_at IS NOT NULL
 AND EXISTS(SELECT 1 FROM backoffice_audit b WHERE b.resource='channel-sharing-finishes' AND b.resource_id=p.id AND b.operation='submit' AND b.changes->>'state'='claimed' AND (b.actor_role,b.actor_id,b.actor_name)=(p.actor_role,p.actor_id,p.actor_name))
 AND (p.last_observation_id IS NULL AND p.state IN ('claimed','unknown','failed') AND p.provider_order_no='' OR EXISTS(SELECT 1 FROM channel_sharing_finish_observations v WHERE v.id=p.last_observation_id AND (v.finish_id,v.provider_order_no,v.state)=(p.id,p.provider_order_no,p.state)))
 AND (p.last_observation_id IS NOT NULL OR p.state='claimed' OR EXISTS(SELECT 1 FROM backoffice_audit b WHERE b.resource='channel-sharing-finishes' AND b.resource_id=p.id AND b.operation='submit-result' AND b.changes->>'state'=p.state AND (b.actor_role,b.actor_id,b.actor_name)=(p.actor_role,p.actor_id,p.actor_name)))
 AND (p.last_observation_id IS NULL OR p.last_observation_id=(SELECT max(v.id) FROM channel_sharing_finish_observations v WHERE v.finish_id=p.id))
 AND NOT EXISTS(SELECT 1 FROM channel_sharing_finish_observations v WHERE v.finish_id=p.id AND
  (v.provider_order_no<>p.provider_order_no OR v.state<>'pending' AND (v.id IS DISTINCT FROM p.last_observation_id OR v.state<>p.state) OR v.state='succeeded' AND (v.source<>'query' OR v.unsplit_amount<>p.expected_unsplit) OR v.state<>'succeeded' AND v.unsplit_amount<>0 OR
   NOT EXISTS(SELECT 1 FROM backoffice_audit b WHERE b.resource='channel-sharing-finishes' AND b.resource_id=p.id AND b.operation=v.source||'-result' AND b.changes->>'state'=v.state AND (b.actor_role,b.actor_id,b.actor_name)=(v.actor_role,v.actor_id,v.actor_name))))
 AND (p.state<>'succeeded' AND p.release_journal_id IS NULL OR EXISTS(
  SELECT 1 FROM channel_sharing_finish_observations v JOIN journals j ON j.id=p.release_journal_id
  WHERE v.id=p.last_observation_id AND v.source='query' AND v.state='succeeded' AND v.unsplit_amount=p.expected_unsplit
   AND j.event_key='channel-sharing-finish:'||p.id::text||':release'
   AND (SELECT count(*)=2 FROM ledger_entries e WHERE e.journal_id=j.id)
   AND (SELECT COALESCE(sum(e.amount),0) FROM ledger_entries e WHERE e.journal_id=j.id AND e.account_id=f.account_id)=-p.release_amount
   AND (SELECT COALESCE(sum(e.amount),0) FROM ledger_entries e JOIN accounts av ON av.id=e.account_id WHERE e.journal_id=j.id AND (av.owner_id,av.kind,av.scope_id)=(p.merchant_id,'available',0))=p.release_amount)) AS valid
FROM channel_sharing_finishes p JOIN channel_sharing_fundings f ON f.order_id=p.order_id JOIN orders o ON o.id=p.order_id
JOIN provider_attempt_sharing d ON d.order_id=p.order_id JOIN accounts a ON a.id=f.account_id;
-- 000063_frozen_income_refunds.up.sql refund_funding_invariants
CREATE OR REPLACE VIEW refund_funding_invariants AS SELECT f.refund_id,
 (f.order_id,f.merchant_id,f.merchant_amount,f.fee_amount)=(r.order_id,r.merchant_id,r.merchant_amount,r.fee_amount)
 AND (a.owner_id,a.kind,a.scope_id)=(r.merchant_id,CASE WHEN f.hold_id IS NULL THEN 'reserved' ELSE 'order_income_refund_reserve' END,CASE WHEN f.hold_id IS NULL THEN 0 ELSE r.id END)
 AND (f.hold_id IS NULL OR (h.order_id,h.merchant_id,h.request_hash)=(r.order_id,r.merchant_id,f.hold_funding_hash))
 AND (f.hold_id IS NULL OR a.balance=CASE WHEN r.status IN('pending','unknown') THEN r.merchant_amount ELSE 0 END)
 AND (f.hold_id IS NULL OR (SELECT count(*) FROM ledger_entries x WHERE x.account_id=a.id)=
 (CASE WHEN f.hold_amount>0 THEN 1 ELSE 0 END)+(CASE WHEN f.available_amount>0 THEN 1 ELSE 0 END)+
 CASE WHEN r.status IN('pending','unknown') THEN 0 WHEN r.status='succeeded' THEN CASE WHEN r.merchant_amount>0 THEN 1 ELSE 0 END ELSE (CASE WHEN f.hold_amount>0 THEN 1 ELSE 0 END)+(CASE WHEN f.available_amount>0 THEN 1 ELSE 0 END) END)
 AND (SELECT COALESCE(sum(q.amount),0) FROM refunds q WHERE q.order_id=r.order_id AND q.status<>'failed')<=COALESCE(o.actual_charge_amount104,o.amount)
 AND (o.refund_fee_payer='merchant' AND r.fee_amount=0 OR o.refund_fee_payer='platform' AND (SELECT COALESCE(sum(q.merchant_amount),0) FROM refunds q WHERE q.order_id=r.order_id AND q.status<>'failed')<=o.merchant_amount AND (SELECT COALESCE(sum(q.fee_amount),0) FROM refunds q WHERE q.order_id=r.order_id AND q.status<>'failed')<=o.platform_fee)
 AND refund_merchant_reserve_valid(r.id) IS TRUE AND refund_merchant_resolution_valid(r.id) IS TRUE
 AND EXISTS(SELECT 1 FROM backoffice_audit b WHERE b.resource='refund-fundings' AND b.resource_id=r.id AND b.operation='reserve' AND (b.actor_role,b.actor_id,b.actor_name)=('system',0,'refund-engine'))
 AND (r.status IN('pending','unknown') OR EXISTS(SELECT 1 FROM backoffice_audit b WHERE b.resource='refund-fundings' AND b.resource_id=r.id AND b.operation=r.status AND (b.actor_role,b.actor_id,b.actor_name)=('system',0,'refund-engine')))
 AS valid FROM refund_fundings f JOIN refunds r ON r.id=f.refund_id JOIN orders o ON o.id=r.order_id JOIN accounts a ON a.id=f.reserve_account_id LEFT JOIN order_income_hold_fundings h ON h.hold_id=f.hold_id;
-- 000063_frozen_income_refunds.up.sql order_income_source_evidence
CREATE OR REPLACE VIEW order_income_source_evidence AS
SELECT o.id AS order_id,o.merchant_id,o.merchant_amount AS merchant_net,
 r.gross AS refund_gross,r.amount AS refund_merchant_amount,r.pending AS pending_refunds,
 p.amount AS channel_sharing_amount,a.amount AS internal_allocated_amount,
 GREATEST(0::numeric,o.merchant_amount::numeric-r.amount-p.amount-a.amount)::bigint AS remaining,
 o.status='paid' AND o.merchant_amount>0 AND NOT EXISTS(SELECT 1 FROM recharge_intents z WHERE z.order_id=o.id)
 AND r.gross<=COALESCE(o.actual_charge_amount104,o.amount) AND r.valid AND a.valid
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
 COALESCE(bool_and(f.status<>'succeeded' OR refund_merchant_reserve_valid(f.id) IS TRUE AND refund_merchant_resolution_valid(f.id) IS TRUE),true) AS valid
 FROM refunds f WHERE f.order_id=o.id
) r
CROSS JOIN LATERAL (
 SELECT order_income_channel_sharing_outflow(o.id) AS amount
) p
CROSS JOIN LATERAL (
 SELECT COALESCE(sum(i.amount) FILTER(WHERE i.status<>'rejected'),0)::bigint AS amount,COALESCE(bool_and(i.valid IS TRUE),true) AS valid
 FROM order_income_internal_allocation_evidence i WHERE i.order_id=o.id
) a;

-- New104 authorization and barcode sources use authenticated payable C.
-- 000068_preauth_observations.up.sql
CREATE OR REPLACE FUNCTION guard_preauth_observation() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF TG_OP='INSERT' THEN
  IF NEW.status<>'processing' OR NEW.rest_amount IS NOT NULL OR NEW.observed_at IS NOT NULL OR NEW.result_proof IS NOT NULL THEN RAISE EXCEPTION 'observation must begin as a query claim'; END IF;
  IF NOT EXISTS(SELECT 1 FROM orders o JOIN provider_attempts p ON p.order_id=o.id JOIN payment_authorizations a ON a.order_id=o.id JOIN provider_attempt_accounts ac ON ac.attempt_id=p.id WHERE o.id=NEW.order_id AND o.merchant_id=NEW.merchant_id AND o.trade_no=NEW.trade_no AND COALESCE(o.actual_charge_amount104,o.amount)=NEW.amount AND o.type='alipay' AND p.id=NEW.attempt_id AND p.channel_id=NEW.channel_id AND p.method='preauth' AND a.channel_id=NEW.channel_id AND a.amount=NEW.amount AND a.auth_no=NEW.auth_no AND a.buyer_id=NEW.buyer_id AND ac.account_hash=NEW.account_hash) THEN RAISE EXCEPTION 'original authorization source mismatch';END IF;
 ELSE
  IF (NEW.id,NEW.client_request_no,NEW.merchant_id,NEW.order_id,NEW.attempt_id,NEW.channel_id,NEW.trade_no,NEW.app_id,NEW.auth_no,NEW.buyer_id,NEW.amount,NEW.account_hash,NEW.merchant_revision,NEW.session_hash,NEW.actor_username,NEW.actor_role,NEW.actor_id,NEW.actor_auth_version,NEW.source_proof,NEW.created_at,NEW.expires_at)
  IS DISTINCT FROM(OLD.id,OLD.client_request_no,OLD.merchant_id,OLD.order_id,OLD.attempt_id,OLD.channel_id,OLD.trade_no,OLD.app_id,OLD.auth_no,OLD.buyer_id,OLD.amount,OLD.account_hash,OLD.merchant_revision,OLD.session_hash,OLD.actor_username,OLD.actor_role,OLD.actor_id,OLD.actor_auth_version,OLD.source_proof,OLD.created_at,OLD.expires_at)
  OR OLD.status<>'processing' OR NEW.status NOT IN('verified','unknown') THEN RAISE EXCEPTION 'immutable observation source or result';END IF;
 END IF;RETURN NEW;END $$;
-- 000078_scan_recovery_origins.up.sql
CREATE OR REPLACE FUNCTION scan_recovery_origin_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS (
  SELECT 1 FROM provider_attempts p JOIN orders o ON o.id=p.order_id
  JOIN provider_attempt_identities i ON i.attempt_id=p.id
  JOIN provider_attempt_accounts a ON a.attempt_id=p.id
  WHERE p.id=NEW.attempt_id AND p.method='scan' AND p.channel_id=NEW.channel_id
   AND (o.id,o.merchant_id,o.trade_no,o.type,COALESCE(o.actual_charge_amount104,o.amount))=(NEW.order_id,NEW.merchant_id,NEW.trade_no,NEW.provider,NEW.amount)
   AND i.request_hash=NEW.request_identity_hash AND a.recovery_version=1 AND a.recovery_source_hash=NEW.proof_hash
 ) THEN RAISE EXCEPTION 'original barcode recovery source mismatch';END IF;
 IF (SELECT count(*) FROM backoffice_audit WHERE resource='scan-recovery-origins' AND resource_id=NEW.attempt_id)<>1
 OR (SELECT count(*) FROM backoffice_audit WHERE resource='scan-recovery-origins' AND resource_id=NEW.attempt_id
      AND actor_role='system' AND actor_id=0 AND actor_name='scan-recovery' AND operation='create'
      AND changes=jsonb_build_object('proof',encode(NEW.proof_hash,'hex'),'order_id',NEW.order_id::text))<>1
 THEN RAISE EXCEPTION 'missing original barcode recovery audit';END IF;
 RETURN NULL;
END $$;

-- New104 reward and cost source gross is authenticated actual C; old NULL is M.
-- 000070_referral_daily_liabilities.up.sql
CREATE OR REPLACE FUNCTION guard_referral_daily_terms_birth() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF NOT EXISTS(SELECT 1 FROM orders o JOIN referral_order_terms t ON t.order_id=o.id JOIN accounts a ON a.id=NEW.pending_account_id WHERE o.id=NEW.order_id AND o.status='created' AND o.created_at=transaction_timestamp()
 AND (o.merchant_id,COALESCE(o.actual_charge_amount104,o.amount),o.platform_fee)=(NEW.merchant_id,NEW.amount,NEW.platform_fee)
 AND t.enabled AND (t.promoter_id,t.reward_amount,t.policy_versions,t.proof_hash)=(NEW.promoter_id,NEW.reward_amount,NEW.policy_versions,NEW.terms_hash)
 AND (a.owner_id,a.kind,a.scope_id,a.balance)=(0::bigint,'referral_daily_pending',o.id,0::bigint)) THEN RAISE EXCEPTION 'daily terms require original created-order source';END IF;RETURN NEW;END $$;
-- 000070_referral_daily_liabilities.up.sql
CREATE OR REPLACE VIEW referral_daily_liability_sources AS
SELECT l.order_id,
 o.status='paid' AND b.enabled AND b.purpose='payment'
 AND (d.merchant_id,d.promoter_id,d.amount,d.platform_fee,d.reward_amount,d.terms_hash,d.policy_versions)=(o.merchant_id,b.promoter_id,COALESCE(o.actual_charge_amount104,o.amount),o.platform_fee,b.reward_amount,b.proof_hash,b.policy_versions)
 AND d.source_version=2 AND d.mode='daily' AND d.timezone='Asia/Shanghai-UTC+08'
 AND (p.id,p.owner_id,p.kind,p.scope_id)=(d.pending_account_id,0::bigint,'referral_daily_pending',o.id)
 AND l.pending_account_id=p.id AND f.owner_id=0 AND f.kind='fees' AND f.scope_id=0
 AND l.activated_at>=o.created_at AND l.business_day=((l.activated_at AT TIME ZONE 'UTC')+interval '8 hours')::date AND l.eligible_day=l.business_day+1
 AND (v.channel_id,v.provider_trade_no)=(l.channel_id,l.provider_trade_no)
 AND j.event_key='referral-daily-funding:'||o.id::text
 AND (SELECT count(*)=2 FROM ledger_entries e WHERE e.journal_id=j.id)
 AND (SELECT COALESCE(sum(e.amount),0)=-d.reward_amount FROM ledger_entries e WHERE e.journal_id=j.id AND e.account_id=f.id)
 AND (SELECT COALESCE(sum(e.amount),0)=d.reward_amount FROM ledger_entries e WHERE e.journal_id=j.id AND e.account_id=p.id)
 AND EXISTS(SELECT 1 FROM journals fj WHERE fj.event_key='payment-fee:'||o.trade_no AND fj.transaction_id=j.transaction_id
 AND (SELECT count(*)=2 FROM ledger_entries e WHERE e.journal_id=fj.id)
 AND (SELECT COALESCE(sum(e.amount),0)=o.platform_fee FROM ledger_entries e WHERE e.journal_id=fj.id AND e.account_id=f.id)
 AND (SELECT COALESCE(sum(e.amount),0)=-o.platform_fee FROM ledger_entries e JOIN accounts a ON a.id=e.account_id WHERE e.journal_id=fj.id AND a.owner_id=0 AND a.kind='clearing' AND a.scope_id=0))
 AND EXISTS(SELECT 1 FROM referral_daily_events e WHERE e.order_id=o.id AND e.operation='funded' AND e.reference_id=0 AND e.actor_name='payment-engine') AS valid
FROM referral_daily_liabilities l JOIN referral_daily_terms d ON d.order_id=l.order_id JOIN referral_order_terms b ON b.order_id=l.order_id JOIN orders o ON o.id=l.order_id
JOIN accounts p ON p.id=l.pending_account_id JOIN accounts f ON f.id=l.fees_account_id JOIN journals j ON j.id=l.funding_journal_id JOIN verified_payment_receipts v ON v.order_id=l.order_id;
-- 000071_referral_profit_costs.up.sql
CREATE OR REPLACE FUNCTION referral_profit_birth_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE o orders; t referral_order_terms;
BEGIN
 SELECT * INTO STRICT o FROM orders WHERE id=NEW.order_id FOR UPDATE;
 SELECT * INTO STRICT t FROM referral_order_terms WHERE order_id=o.id;
 IF o.status<>'created' OR o.created_at<>transaction_timestamp() OR t.purpose<>'payment'
 OR (o.merchant_id,o.type,COALESCE(o.actual_charge_amount104,o.amount),o.platform_fee,t.reward_amount,t.proof_hash) IS DISTINCT FROM
 (NEW.merchant_id,NEW.provider,NEW.amount,NEW.platform_fee,NEW.reward_amount,NEW.terms_hash)
 THEN RAISE EXCEPTION 'profit cost must be born with exact original terms'; END IF;
 RETURN NEW;
END $$;
