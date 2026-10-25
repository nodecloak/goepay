-- Existing schema49 rows retain their original payment proof unchanged.
ALTER TABLE provider_attempt_sharing ADD COLUMN purpose text NOT NULL DEFAULT 'payment' CHECK(purpose IN ('payment','recharge'));
CREATE FUNCTION provider_attempt_sharing_purpose_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF (NEW.purpose='recharge') IS DISTINCT FROM EXISTS(SELECT 1 FROM recharge_intents WHERE order_id=NEW.order_id) OR NEW.purpose='recharge' AND NEW.declared THEN RAISE EXCEPTION 'sharing declaration changed original business purpose'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER provider_attempt_sharing_purpose BEFORE INSERT ON provider_attempt_sharing FOR EACH ROW EXECUTE FUNCTION provider_attempt_sharing_purpose_guard();
CREATE FUNCTION recharge_intent_birth_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 PERFORM id FROM orders WHERE id=NEW.order_id FOR UPDATE;
 IF NOT EXISTS(SELECT 1 FROM recharge_intents WHERE order_id=NEW.order_id) AND (EXISTS(SELECT 1 FROM provider_attempts WHERE order_id=NEW.order_id) OR EXISTS(SELECT 1 FROM verified_payment_receipts WHERE order_id=NEW.order_id) OR EXISTS(SELECT 1 FROM orders WHERE id=NEW.order_id AND status<>'created')) THEN RAISE EXCEPTION 'recharge intent must precede original payment'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER recharge_intent_birth BEFORE INSERT ON recharge_intents FOR EACH ROW EXECUTE FUNCTION recharge_intent_birth_guard();
CREATE TRIGGER recharge_intent_immutable BEFORE UPDATE OR DELETE ON recharge_intents FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();

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
  (a.order_id,a.channel_id,a.method,o.merchant_id,o.type,o.amount,o.merchant_amount)=(d.order_id,d.channel_id,d.method,d.merchant_id,d.provider,d.gross_amount,d.merchant_net))
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
