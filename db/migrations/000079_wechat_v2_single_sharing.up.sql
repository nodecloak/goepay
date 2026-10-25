-- Add the original direct V2 single-allocation product. Existing rows and
-- immutable MAC terms are unchanged; no historical declaration is inferred.
ALTER TABLE channel_sharing_receivers DROP CONSTRAINT channel_sharing_receivers_product_check;
ALTER TABLE channel_sharing_receivers ADD CONSTRAINT channel_sharing_receivers_product_check CHECK(product IN ('wxpay_v3_direct','wxpay_v2_single_direct','alipay_direct'));
ALTER TABLE channel_sharing_receivers DROP CONSTRAINT channel_sharing_receivers_check;
ALTER TABLE channel_sharing_receivers ADD CONSTRAINT channel_sharing_receivers_check CHECK(provider='wxpay' AND product IN ('wxpay_v3_direct','wxpay_v2_single_direct') OR provider='alipay' AND product='alipay_direct');
ALTER TABLE channel_sharing_rules DROP CONSTRAINT channel_sharing_rules_check;
ALTER TABLE channel_sharing_rules ADD CONSTRAINT channel_sharing_rules_check CHECK(provider='wxpay' AND product IN ('wxpay_v3_direct','wxpay_v2_single_direct') OR provider='alipay' AND product='alipay_direct');
ALTER TABLE provider_attempt_sharing DROP CONSTRAINT provider_attempt_sharing_check1;
ALTER TABLE provider_attempt_sharing ADD CONSTRAINT provider_attempt_sharing_check1 CHECK(declared AND rule_id IS NOT NULL AND receiver_count>0 AND refund_mode='merchant_funded' AND auto_finish AND
 (provider='wxpay' AND product IN ('wxpay_v3_direct','wxpay_v2_single_direct') OR provider='alipay' AND product='alipay_direct')
 OR NOT declared AND receiver_count=0 AND refund_mode='' AND NOT auto_finish AND product='');

CREATE OR REPLACE FUNCTION channel_sharing_finish_identity_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE d provider_attempt_sharing%ROWTYPE; p channel_sharing_orders%ROWTYPE;
BEGIN
 PERFORM id FROM orders WHERE id=NEW.order_id FOR UPDATE;
 IF TG_OP='INSERT' THEN
  SELECT * INTO STRICT d FROM provider_attempt_sharing WHERE order_id=NEW.order_id;
  SELECT * INTO p FROM channel_sharing_orders WHERE order_id=NEW.order_id;
  IF EXISTS(SELECT 1 FROM channel_sharing_finishes x WHERE x.order_id=NEW.order_id AND x.state<>'failed') OR NEW.previous_finish_id IS DISTINCT FROM (SELECT max(x.id) FROM channel_sharing_finishes x WHERE x.order_id=NEW.order_id) OR NEW.state<>'unsent' OR NOT d.declared OR d.purpose<>'payment' OR d.product NOT IN ('wxpay_v3_direct','wxpay_v2_single_direct') OR
   (NEW.merchant_id,NEW.gross_amount,NEW.merchant_net) IS DISTINCT FROM (d.merchant_id,d.gross_amount,d.merchant_net) OR
   NOT EXISTS(SELECT 1 FROM verified_payment_receipts r WHERE r.order_id=NEW.order_id AND r.channel_id=d.channel_id AND r.provider_trade_no=NEW.provider_trade_no) OR
   EXISTS(SELECT 1 FROM recharge_intents WHERE order_id=NEW.order_id) OR
   p.id IS NOT NULL AND (p.id IS DISTINCT FROM NEW.original_sharing_id OR p.state<>'failed' OR p.completed OR p.last_observation_id IS NOT NULL OR p.failure_proof IS NULL) OR
   p.id IS NULL AND NEW.original_sharing_id IS NOT NULL OR
   EXISTS(SELECT 1 FROM channel_sharing_completions WHERE order_id=NEW.order_id)
  THEN RAISE EXCEPTION 'finish requires an unspent declared original payment or proved unaccepted PS'; END IF;
 ELSE
  IF (NEW.id,NEW.order_id,NEW.merchant_id,NEW.external_no,NEW.out_no,NEW.provider_trade_no,NEW.gross_amount,NEW.merchant_net,NEW.shared_amount,NEW.expected_unsplit,NEW.release_amount,NEW.original_sharing_id,NEW.previous_finish_id,NEW.request_hash,NEW.actor_role,NEW.actor_id,NEW.actor_name,NEW.actor_class,NEW.created_at)
    IS DISTINCT FROM (OLD.id,OLD.order_id,OLD.merchant_id,OLD.external_no,OLD.out_no,OLD.provider_trade_no,OLD.gross_amount,OLD.merchant_net,OLD.shared_amount,OLD.expected_unsplit,OLD.release_amount,OLD.original_sharing_id,OLD.previous_finish_id,OLD.request_hash,OLD.actor_role,OLD.actor_id,OLD.actor_name,OLD.actor_class,OLD.created_at)
   OR OLD.state IN ('succeeded','failed') AND NEW IS DISTINCT FROM OLD OR OLD.claimed_at IS NOT NULL AND NEW.claimed_at IS DISTINCT FROM OLD.claimed_at OR OLD.provider_order_no<>'' AND NEW.provider_order_no<>OLD.provider_order_no
  THEN RAISE EXCEPTION 'immutable original finish request or terminal result'; END IF;
  IF OLD.state='unsent' AND NEW.state<>'claimed' OR OLD.state<>'unsent' AND NEW.state IN ('unsent','claimed') AND NEW IS DISTINCT FROM OLD THEN RAISE EXCEPTION 'finish cannot be rearmed'; END IF;
 END IF;
 RETURN NEW;
END $$;

CREATE OR REPLACE VIEW channel_sharing_finish_invariants AS
SELECT p.id AS finish_id,p.order_id,
 d.declared AND d.purpose='payment' AND d.product IN ('wxpay_v3_direct','wxpay_v2_single_direct') AND o.status='paid'
 AND (p.merchant_id,p.gross_amount,p.merchant_net,p.release_amount,p.shared_amount,p.expected_unsplit)=(o.merchant_id,o.amount,o.merchant_amount,f.amount,0,o.amount)
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
