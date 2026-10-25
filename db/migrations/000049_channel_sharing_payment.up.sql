CREATE TABLE provider_attempt_sharing (
 attempt_id bigint PRIMARY KEY REFERENCES provider_attempts(id),order_id bigint NOT NULL UNIQUE REFERENCES orders(id),
 merchant_id bigint NOT NULL REFERENCES merchants(id),channel_id bigint NOT NULL CHECK(channel_id>0),method text NOT NULL,
 provider text NOT NULL CHECK(length(provider) BETWEEN 1 AND 32),app_id text NOT NULL CHECK(length(app_id)<=256),product text NOT NULL,
 account_hash bytea NOT NULL CHECK(octet_length(account_hash)=32),request_hash bytea NOT NULL CHECK(octet_length(request_hash)=32),
 rule_id bigint REFERENCES channel_sharing_rules(id),declared boolean NOT NULL,
 gross_amount bigint NOT NULL CHECK(gross_amount>0),merchant_net bigint NOT NULL CHECK(merchant_net BETWEEN 0 AND gross_amount),
 refund_mode text NOT NULL,auto_finish boolean NOT NULL,receiver_count integer NOT NULL CHECK(receiver_count BETWEEN 0 AND 50),
 created_at timestamptz NOT NULL DEFAULT statement_timestamp(),
 CHECK(declared AND rule_id IS NOT NULL AND receiver_count>0 AND refund_mode='merchant_funded' AND auto_finish AND
  (provider='wxpay' AND product='wxpay_v3_direct' OR provider='alipay' AND product='alipay_direct')
  OR NOT declared AND receiver_count=0 AND refund_mode='' AND NOT auto_finish AND product='')
);
CREATE TABLE provider_attempt_sharing_items (
 attempt_id bigint NOT NULL REFERENCES provider_attempt_sharing(attempt_id),position integer NOT NULL CHECK(position BETWEEN 1 AND 50),
 receiver_id bigint NOT NULL REFERENCES channel_sharing_receivers(id),amount bigint NOT NULL CHECK(amount>0),
 PRIMARY KEY(attempt_id,position),UNIQUE(attempt_id,receiver_id)
);
CREATE TRIGGER provider_attempt_sharing_immutable BEFORE UPDATE OR DELETE ON provider_attempt_sharing FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE TRIGGER provider_attempt_sharing_item_immutable BEFORE UPDATE OR DELETE ON provider_attempt_sharing_items FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE FUNCTION provider_attempt_sharing_birth_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 PERFORM id FROM orders WHERE id=NEW.order_id FOR UPDATE;
 IF NOT EXISTS(SELECT 1 FROM provider_attempts a JOIN orders o ON o.id=a.order_id WHERE a.id=NEW.attempt_id AND a.order_id=NEW.order_id AND a.state='processing' AND o.status='created' AND a.created_at=transaction_timestamp()
  AND NOT EXISTS(SELECT 1 FROM verified_payment_receipts p WHERE p.order_id=o.id)
  AND NOT EXISTS(SELECT 1 FROM journals j WHERE j.event_key='payment:'||o.trade_no))
 THEN RAISE EXCEPTION 'sharing declaration must precede the original payment attempt'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER provider_attempt_sharing_birth BEFORE INSERT ON provider_attempt_sharing FOR EACH ROW EXECUTE FUNCTION provider_attempt_sharing_birth_guard();
CREATE FUNCTION provider_attempt_original_identity_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF (NEW.id,NEW.order_id,NEW.channel_id,NEW.method,NEW.created_at) IS DISTINCT FROM (OLD.id,OLD.order_id,OLD.channel_id,OLD.method,OLD.created_at) THEN RAISE EXCEPTION 'immutable original provider attempt identity'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER provider_attempt_original_identity BEFORE UPDATE ON provider_attempts FOR EACH ROW EXECUTE FUNCTION provider_attempt_original_identity_guard();
CREATE FUNCTION provider_attempt_sharing_guard() RETURNS trigger LANGUAGE plpgsql AS $$
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
 IF NOT d.declared THEN
  IF d.rule_id IS NOT NULL AND r.enabled AND r.methods ? d.method AND d.gross_amount>=r.minimum_amount AND EXISTS(SELECT 1 FROM channel_sharing_rule_items i WHERE i.rule_id=r.id AND CASE WHEN i.mode='fixed' THEN i.fixed_amount ELSE floor(d.gross_amount::numeric*i.rate_bps/10000) END>0)
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
CREATE CONSTRAINT TRIGGER provider_attempt_sharing_complete AFTER INSERT ON provider_attempt_sharing DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION provider_attempt_sharing_guard();
CREATE CONSTRAINT TRIGGER provider_attempt_sharing_items_complete AFTER INSERT ON provider_attempt_sharing_items DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION provider_attempt_sharing_guard();
CREATE CONSTRAINT TRIGGER provider_attempt_account_sharing AFTER INSERT ON provider_attempt_accounts DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION provider_attempt_sharing_guard();

ALTER TABLE accounts DROP CONSTRAINT accounts_kind_check;
ALTER TABLE accounts ADD CONSTRAINT accounts_kind_check CHECK(kind IN ('available','reserved','clearing','deposit','deposit_reserved','fees','fee_reserved','bank_service_fee_hold','bank_fee_expense','order_income_hold','settlement_reserve','channel_sharing_reserve'));
ALTER TABLE accounts DROP CONSTRAINT accounts_scope_domain;
ALTER TABLE accounts ADD CONSTRAINT accounts_scope_domain CHECK(kind IN ('settlement_reserve','channel_sharing_reserve') AND owner_id>0 AND scope_id>0 OR kind NOT IN ('settlement_reserve','channel_sharing_reserve') AND scope_id=0);
CREATE TABLE channel_sharing_fundings (
 order_id bigint PRIMARY KEY REFERENCES provider_attempt_sharing(order_id),account_id bigint UNIQUE NOT NULL REFERENCES accounts(id),
 amount bigint NOT NULL CHECK(amount>0),reserve_journal_id bigint UNIQUE NOT NULL REFERENCES journals(id),created_at timestamptz NOT NULL DEFAULT statement_timestamp()
);
CREATE TRIGGER channel_sharing_funding_immutable BEFORE UPDATE OR DELETE ON channel_sharing_fundings FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE FUNCTION channel_sharing_account_identity_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF OLD.kind='channel_sharing_reserve' AND (TG_OP='DELETE' OR (NEW.owner_id,NEW.kind,NEW.scope_id) IS DISTINCT FROM (OLD.owner_id,OLD.kind,OLD.scope_id)) THEN RAISE EXCEPTION 'immutable sharing account identity'; END IF;
 RETURN CASE WHEN TG_OP='DELETE' THEN OLD ELSE NEW END;
END $$;
CREATE TRIGGER channel_sharing_account_identity BEFORE UPDATE OR DELETE ON accounts FOR EACH ROW EXECUTE FUNCTION channel_sharing_account_identity_guard();

CREATE VIEW channel_sharing_funding_invariants AS
SELECT f.order_id,
 d.declared AND o.status='paid' AND f.amount=d.merchant_net AND d.merchant_net=o.merchant_amount
 AND (a.owner_id,a.kind,a.scope_id)=(o.merchant_id,'channel_sharing_reserve',o.id)
 AND j.event_key='channel-sharing:'||o.id::text||':reserve'
 AND EXISTS(SELECT 1 FROM verified_payment_receipts p WHERE p.order_id=o.id AND p.channel_id=d.channel_id AND length(p.provider_trade_no)>0)
 AND a.balance=f.amount
 AND (SELECT count(*)=1 FROM ledger_entries e WHERE e.account_id=a.id)
 AND (SELECT count(*)=2 FROM ledger_entries e WHERE e.journal_id=j.id)
 AND (SELECT COALESCE(sum(e.amount),0) FROM ledger_entries e WHERE e.journal_id=j.id AND e.account_id=a.id)=f.amount
 AND (SELECT COALESCE(sum(e.amount),0) FROM ledger_entries e JOIN accounts av ON av.id=e.account_id WHERE e.journal_id=j.id AND (av.owner_id,av.kind,av.scope_id)=(o.merchant_id,'available',0))=-f.amount
 AND EXISTS(SELECT 1 FROM journals p WHERE p.event_key='payment:'||o.trade_no
  AND (SELECT count(*)=2 FROM ledger_entries e WHERE e.journal_id=p.id)
  AND (SELECT COALESCE(sum(e.amount),0) FROM ledger_entries e JOIN accounts av ON av.id=e.account_id WHERE e.journal_id=p.id AND (av.owner_id,av.kind,av.scope_id)=(o.merchant_id,'available',0))=f.amount
  AND (SELECT COALESCE(sum(e.amount),0) FROM ledger_entries e JOIN accounts cl ON cl.id=e.account_id WHERE e.journal_id=p.id AND (cl.owner_id,cl.kind,cl.scope_id)=(0,'clearing',0))=-f.amount)
 AS valid
FROM channel_sharing_fundings f JOIN provider_attempt_sharing d ON d.order_id=f.order_id JOIN orders o ON o.id=f.order_id JOIN accounts a ON a.id=f.account_id JOIN journals j ON j.id=f.reserve_journal_id;
CREATE FUNCTION channel_sharing_funding_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE oid bigint;
BEGIN
 IF TG_TABLE_NAME='accounts' THEN IF NEW.kind<>'channel_sharing_reserve' THEN RETURN NULL; END IF;oid:=NEW.scope_id;
 ELSIF TG_TABLE_NAME='ledger_entries' THEN SELECT scope_id INTO oid FROM accounts WHERE id=NEW.account_id AND kind='channel_sharing_reserve';IF oid IS NULL THEN RETURN NULL;END IF;
 ELSIF TG_TABLE_NAME='orders' THEN
  oid:=NEW.id;
  IF NEW.status<>'paid' OR NOT EXISTS(SELECT 1 FROM provider_attempt_sharing WHERE order_id=oid AND declared) THEN RETURN NULL; END IF;
 ELSE oid:=NEW.order_id;END IF;
 IF NOT EXISTS(SELECT 1 FROM channel_sharing_funding_invariants WHERE order_id=oid AND valid IS TRUE) THEN RAISE EXCEPTION 'invalid channel sharing funding evidence'; END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER channel_sharing_funding_valid AFTER INSERT ON channel_sharing_fundings DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION channel_sharing_funding_guard();
CREATE CONSTRAINT TRIGGER channel_sharing_account_funding AFTER INSERT OR UPDATE ON accounts DEFERRABLE INITIALLY DEFERRED FOR EACH ROW WHEN(NEW.kind='channel_sharing_reserve') EXECUTE FUNCTION channel_sharing_funding_guard();
CREATE CONSTRAINT TRIGGER channel_sharing_entry_funding AFTER INSERT ON ledger_entries DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION channel_sharing_funding_guard();
CREATE CONSTRAINT TRIGGER channel_sharing_paid_funding AFTER UPDATE ON orders DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION channel_sharing_funding_guard();

CREATE FUNCTION channel_sharing_open_payment_receiver_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF OLD.state='bound' AND NEW.state='unbinding' AND EXISTS(SELECT 1 FROM provider_attempt_sharing_items i JOIN provider_attempt_sharing d ON d.attempt_id=i.attempt_id WHERE i.receiver_id=OLD.id AND d.declared)
 THEN RAISE EXCEPTION 'receiver is required by an original sharing payment'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER channel_sharing_open_payment_receiver BEFORE UPDATE ON channel_sharing_receivers FOR EACH ROW EXECUTE FUNCTION channel_sharing_open_payment_receiver_guard();
CREATE FUNCTION channel_sharing_refund_hold_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 PERFORM id FROM orders WHERE id=NEW.order_id FOR UPDATE;
 IF EXISTS(SELECT 1 FROM channel_sharing_fundings WHERE order_id=NEW.order_id) THEN RAISE EXCEPTION USING ERRCODE='55000',MESSAGE='channel sharing funds are frozen',CONSTRAINT='channel_sharing_income_blocked'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER channel_sharing_refund_hold BEFORE INSERT OR UPDATE OF status ON refunds FOR EACH ROW EXECUTE FUNCTION channel_sharing_refund_hold_guard();
CREATE TRIGGER channel_sharing_income_hold BEFORE INSERT ON order_income_holds FOR EACH ROW EXECUTE FUNCTION channel_sharing_refund_hold_guard();
CREATE TRIGGER channel_sharing_internal_allocation_hold BEFORE INSERT ON order_profit_allocations FOR EACH ROW EXECUTE FUNCTION channel_sharing_refund_hold_guard();
