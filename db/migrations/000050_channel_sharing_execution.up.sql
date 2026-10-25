CREATE TABLE channel_sharing_orders (
 id bigserial PRIMARY KEY,order_id bigint NOT NULL UNIQUE REFERENCES channel_sharing_fundings(order_id),
 merchant_id bigint NOT NULL REFERENCES merchants(id),external_no text NOT NULL CHECK(external_no ~ '^[A-Za-z0-9_-]{1,128}$'),
 out_no text NOT NULL UNIQUE CHECK(out_no ~ '^PS[a-f0-9]{30}$'),provider_trade_no text NOT NULL CHECK(length(provider_trade_no) BETWEEN 1 AND 64),
 request_hash bytea NOT NULL CHECK(octet_length(request_hash)=32),auto_finish boolean NOT NULL CHECK(auto_finish),
 receiver_count integer NOT NULL CHECK(receiver_count BETWEEN 1 AND 50),
 state text NOT NULL DEFAULT 'unsent' CHECK(state IN ('unsent','claimed','pending','unknown','succeeded','failed','partial')),
 provider_order_no text NOT NULL DEFAULT '' CHECK(length(provider_order_no)<=64),completed boolean NOT NULL DEFAULT false,
 last_observation_id bigint,failure_proof bytea CHECK(failure_proof IS NULL OR octet_length(failure_proof)=32),actor_role text NOT NULL CHECK(actor_role IN ('admin','merchant')),actor_id bigint NOT NULL CHECK(actor_id>0),actor_name text NOT NULL,actor_class text NOT NULL DEFAULT '',
 created_at timestamptz NOT NULL DEFAULT statement_timestamp(),claimed_at timestamptz,
 UNIQUE(merchant_id,external_no),CHECK((state='unsent')=(claimed_at IS NULL)),
 CHECK(NOT completed OR state IN ('succeeded','failed','partial')),CHECK(state NOT IN ('succeeded','partial') OR completed),
 CHECK((state='failed' AND last_observation_id IS NULL)=(failure_proof IS NOT NULL))
);
CREATE TABLE channel_sharing_observations (
 id bigserial PRIMARY KEY,sharing_id bigint NOT NULL REFERENCES channel_sharing_orders(id),fingerprint bytea NOT NULL CHECK(octet_length(fingerprint)=32),
 source text NOT NULL CHECK(source IN ('submit','query')),provider_order_no text NOT NULL CHECK(length(provider_order_no)<=64),
 state text NOT NULL CHECK(state IN ('pending','succeeded','failed','partial')),
 items jsonb NOT NULL CHECK(jsonb_typeof(items)='array' AND jsonb_array_length(items) BETWEEN 1 AND 50),
 actor_role text NOT NULL,actor_id bigint NOT NULL,actor_name text NOT NULL,created_at timestamptz NOT NULL DEFAULT statement_timestamp(),
 UNIQUE(sharing_id,fingerprint)
);
ALTER TABLE channel_sharing_orders ADD CONSTRAINT channel_sharing_last_observation FOREIGN KEY(last_observation_id) REFERENCES channel_sharing_observations(id);
CREATE TABLE channel_sharing_order_items (
 sharing_id bigint NOT NULL REFERENCES channel_sharing_orders(id),position integer NOT NULL CHECK(position BETWEEN 1 AND 50),
 receiver_id bigint NOT NULL REFERENCES channel_sharing_receivers(id),amount bigint NOT NULL CHECK(amount>0),
 state text NOT NULL DEFAULT 'pending' CHECK(state IN ('pending','succeeded','failed')),
 provider_detail_no text NOT NULL DEFAULT '' CHECK(length(provider_detail_no)<=64),failure_code text NOT NULL DEFAULT '' CHECK(length(failure_code)<=256),
 last_observation_id bigint REFERENCES channel_sharing_observations(id),paid_journal_id bigint UNIQUE REFERENCES journals(id),
 PRIMARY KEY(sharing_id,receiver_id),UNIQUE(sharing_id,position),CHECK((state='succeeded')=(paid_journal_id IS NOT NULL)),CHECK(state<>'succeeded' OR length(provider_detail_no)>0)
);
CREATE TABLE channel_sharing_completions (
 order_id bigint PRIMARY KEY REFERENCES channel_sharing_fundings(order_id),sharing_id bigint NOT NULL UNIQUE REFERENCES channel_sharing_orders(id),
 proof_kind text NOT NULL CHECK(proof_kind IN ('wechat_remaining_zero','alipay_royalty_finish')),
 expected_unsplit bigint NOT NULL CHECK(expected_unsplit>=0),observed_remaining bigint,
 proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32),
 release_amount bigint NOT NULL CHECK(release_amount>=0),release_journal_id bigint UNIQUE REFERENCES journals(id),
 created_at timestamptz NOT NULL DEFAULT statement_timestamp(),CHECK((release_amount=0)=(release_journal_id IS NULL)),
 CHECK(proof_kind='wechat_remaining_zero' AND observed_remaining=0 AND observed_remaining IS NOT NULL OR proof_kind='alipay_royalty_finish' AND observed_remaining IS NULL)
);
CREATE TRIGGER channel_sharing_observation_immutable BEFORE UPDATE OR DELETE ON channel_sharing_observations FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE TRIGGER channel_sharing_completion_immutable BEFORE UPDATE OR DELETE ON channel_sharing_completions FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE TRIGGER channel_sharing_order_no_delete BEFORE DELETE ON channel_sharing_orders FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE TRIGGER channel_sharing_order_item_no_delete BEFORE DELETE ON channel_sharing_order_items FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();

CREATE FUNCTION channel_sharing_order_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE obs channel_sharing_observations%ROWTYPE;
BEGIN
 IF TG_OP='INSERT' THEN IF NEW.state<>'unsent' OR NEW.completed OR NEW.last_observation_id IS NOT NULL OR NEW.provider_order_no<>'' THEN RAISE EXCEPTION 'new sharing operation must be unsent'; END IF; RETURN NEW; END IF;
 IF (NEW.id,NEW.order_id,NEW.merchant_id,NEW.external_no,NEW.out_no,NEW.provider_trade_no,NEW.request_hash,NEW.auto_finish,NEW.receiver_count,NEW.actor_role,NEW.actor_id,NEW.actor_name,NEW.actor_class,NEW.created_at)
 IS DISTINCT FROM (OLD.id,OLD.order_id,OLD.merchant_id,OLD.external_no,OLD.out_no,OLD.provider_trade_no,OLD.request_hash,OLD.auto_finish,OLD.receiver_count,OLD.actor_role,OLD.actor_id,OLD.actor_name,OLD.actor_class,OLD.created_at)
 OR OLD.claimed_at IS NOT NULL AND NEW.claimed_at IS DISTINCT FROM OLD.claimed_at OR OLD.provider_order_no<>'' AND NEW.provider_order_no<>OLD.provider_order_no OR OLD.completed AND NEW IS DISTINCT FROM OLD
 THEN RAISE EXCEPTION 'immutable original sharing operation'; END IF;
 IF OLD.state='unsent' THEN IF NEW.state<>'claimed' THEN RAISE EXCEPTION 'sharing operation must claim before I/O'; END IF; RETURN NEW; END IF;
 IF NEW.state='unsent' OR NEW.state='claimed' AND OLD.state<>'claimed' THEN RAISE EXCEPTION 'sharing operation cannot resend'; END IF;
 IF NEW.last_observation_id IS NOT NULL THEN
  SELECT * INTO STRICT obs FROM channel_sharing_observations WHERE id=NEW.last_observation_id;
  IF obs.sharing_id<>NEW.id OR obs.provider_order_no<>NEW.provider_order_no OR obs.state<>NEW.state OR NEW.completed<>(obs.state<>'pending') THEN RAISE EXCEPTION 'sharing result must match original observation'; END IF;
 ELSIF NEW.completed OR NEW.provider_order_no<>'' THEN RAISE EXCEPTION 'missing sharing result evidence';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER channel_sharing_order_binding BEFORE INSERT OR UPDATE ON channel_sharing_orders FOR EACH ROW EXECUTE FUNCTION channel_sharing_order_guard();
CREATE FUNCTION channel_sharing_order_item_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE obs channel_sharing_observations%ROWTYPE;
BEGIN
 IF TG_OP='INSERT' THEN IF NEW.state<>'pending' OR NEW.last_observation_id IS NOT NULL OR NEW.paid_journal_id IS NOT NULL THEN RAISE EXCEPTION 'new sharing recipient must be pending'; END IF; RETURN NEW; END IF;
 IF (NEW.sharing_id,NEW.position,NEW.receiver_id,NEW.amount) IS DISTINCT FROM (OLD.sharing_id,OLD.position,OLD.receiver_id,OLD.amount) OR OLD.state IN ('succeeded','failed') AND NEW IS DISTINCT FROM OLD THEN RAISE EXCEPTION 'immutable original sharing recipient result'; END IF;
 SELECT * INTO STRICT obs FROM channel_sharing_observations WHERE id=NEW.last_observation_id;
 IF obs.sharing_id<>NEW.sharing_id OR NOT EXISTS(SELECT 1 FROM jsonb_array_elements(obs.items) x WHERE x->>'receiver_id'=NEW.receiver_id::text AND x->>'amount'=NEW.amount::text AND x->>'state'=NEW.state AND COALESCE(x->>'provider_detail_no','')=NEW.provider_detail_no AND COALESCE(x->>'failure_code','')=NEW.failure_code)
 THEN RAISE EXCEPTION 'sharing recipient result lacks its exact original observation'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER channel_sharing_order_item_binding BEFORE INSERT OR UPDATE ON channel_sharing_order_items FOR EACH ROW EXECUTE FUNCTION channel_sharing_order_item_guard();

CREATE OR REPLACE VIEW channel_sharing_funding_invariants AS
SELECT f.order_id,
 d.declared AND o.status='paid' AND f.amount=d.merchant_net AND d.merchant_net=o.merchant_amount
 AND (a.owner_id,a.kind,a.scope_id)=(o.merchant_id,'channel_sharing_reserve',o.id)
 AND j.event_key='channel-sharing:'||o.id::text||':reserve'
 AND EXISTS(SELECT 1 FROM verified_payment_receipts p WHERE p.order_id=o.id AND p.channel_id=d.channel_id AND length(p.provider_trade_no)>0)
 AND a.balance=f.amount-COALESCE((SELECT sum(i.amount) FROM channel_sharing_order_items i JOIN channel_sharing_orders s ON s.id=i.sharing_id WHERE s.order_id=o.id AND i.state='succeeded'),0)-COALESCE(c.release_amount,0)
 AND (SELECT count(*) FROM ledger_entries e WHERE e.account_id=a.id)=1+(SELECT count(*) FROM channel_sharing_order_items i JOIN channel_sharing_orders s ON s.id=i.sharing_id WHERE s.order_id=o.id AND i.state='succeeded')+CASE WHEN c.release_amount>0 THEN 1 ELSE 0 END
 AND (SELECT count(*)=2 FROM ledger_entries e WHERE e.journal_id=j.id)
 AND (SELECT COALESCE(sum(e.amount),0) FROM ledger_entries e WHERE e.journal_id=j.id AND e.account_id=a.id)=f.amount
 AND (SELECT COALESCE(sum(e.amount),0) FROM ledger_entries e JOIN accounts av ON av.id=e.account_id WHERE e.journal_id=j.id AND (av.owner_id,av.kind,av.scope_id)=(o.merchant_id,'available',0))=-f.amount
 AND EXISTS(SELECT 1 FROM journals p WHERE p.event_key='payment:'||o.trade_no
  AND (SELECT count(*)=2 FROM ledger_entries e WHERE e.journal_id=p.id)
  AND (SELECT COALESCE(sum(e.amount),0) FROM ledger_entries e JOIN accounts av ON av.id=e.account_id WHERE e.journal_id=p.id AND (av.owner_id,av.kind,av.scope_id)=(o.merchant_id,'available',0))=f.amount
  AND (SELECT COALESCE(sum(e.amount),0) FROM ledger_entries e JOIN accounts cl ON cl.id=e.account_id WHERE e.journal_id=p.id AND (cl.owner_id,cl.kind,cl.scope_id)=(0,'clearing',0))=-f.amount)
 AND NOT EXISTS(SELECT 1 FROM channel_sharing_order_items i JOIN channel_sharing_orders s ON s.id=i.sharing_id WHERE s.order_id=o.id AND i.state='succeeded' AND NOT EXISTS(
  SELECT 1 FROM journals q WHERE q.id=i.paid_journal_id AND q.event_key='channel-sharing:'||s.id::text||':receiver:'||i.receiver_id::text||':paid'
  AND (SELECT count(*)=2 FROM ledger_entries e WHERE e.journal_id=q.id)
  AND (SELECT COALESCE(sum(e.amount),0) FROM ledger_entries e WHERE e.journal_id=q.id AND e.account_id=a.id)=-i.amount
  AND (SELECT COALESCE(sum(e.amount),0) FROM ledger_entries e JOIN accounts cl ON cl.id=e.account_id WHERE e.journal_id=q.id AND (cl.owner_id,cl.kind,cl.scope_id)=(0,'clearing',0))=i.amount))
 AND (c.order_id IS NULL OR EXISTS(SELECT 1 FROM channel_sharing_orders s WHERE s.id=c.sharing_id AND s.order_id=o.id AND s.completed AND s.auto_finish
  AND (d.provider='wxpay' AND c.proof_kind='wechat_remaining_zero' OR d.provider='alipay' AND c.proof_kind='alipay_royalty_finish')
  AND c.release_amount=f.amount-COALESCE((SELECT sum(i.amount) FROM channel_sharing_order_items i WHERE i.sharing_id=s.id AND i.state='succeeded'),0)
  AND c.expected_unsplit=d.gross_amount-COALESCE((SELECT sum(i.amount) FROM channel_sharing_order_items i WHERE i.sharing_id=s.id AND i.state='succeeded'),0)
  AND (c.release_amount=0 OR EXISTS(SELECT 1 FROM journals q WHERE q.id=c.release_journal_id AND q.event_key='channel-sharing:'||s.id::text||':release'
   AND (SELECT count(*)=2 FROM ledger_entries e WHERE e.journal_id=q.id)
   AND (SELECT COALESCE(sum(e.amount),0) FROM ledger_entries e WHERE e.journal_id=q.id AND e.account_id=a.id)=-c.release_amount
   AND (SELECT COALESCE(sum(e.amount),0) FROM ledger_entries e JOIN accounts av ON av.id=e.account_id WHERE e.journal_id=q.id AND (av.owner_id,av.kind,av.scope_id)=(o.merchant_id,'available',0))=c.release_amount)))) AS valid
FROM channel_sharing_fundings f JOIN provider_attempt_sharing d ON d.order_id=f.order_id JOIN orders o ON o.id=f.order_id JOIN accounts a ON a.id=f.account_id JOIN journals j ON j.id=f.reserve_journal_id LEFT JOIN channel_sharing_completions c ON c.order_id=f.order_id;

CREATE FUNCTION channel_sharing_execution_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE sid bigint; s channel_sharing_orders%ROWTYPE; d provider_attempt_sharing%ROWTYPE; cnt integer; total bigint; success integer; failed integer;
BEGIN
 IF TG_TABLE_NAME='channel_sharing_orders' THEN sid:=NEW.id; ELSE sid:=NEW.sharing_id; END IF;
 SELECT * INTO STRICT s FROM channel_sharing_orders WHERE id=sid;
 SELECT * INTO STRICT d FROM provider_attempt_sharing WHERE order_id=s.order_id;
 IF s.state='unsent' OR s.claimed_at IS NULL OR NOT EXISTS(SELECT 1 FROM backoffice_audit a WHERE a.resource='channel-sharing-orders' AND a.resource_id=s.id AND a.operation='submit' AND a.changes->>'state'='claimed' AND (a.actor_role,a.actor_id,a.actor_name)=(s.actor_role,s.actor_id,s.actor_name)) THEN RAISE EXCEPTION 'sharing intent lacks its original actor claim audit'; END IF;
 IF s.last_observation_id IS NULL AND s.state IN ('unknown','failed') AND NOT EXISTS(SELECT 1 FROM backoffice_audit a WHERE a.resource='channel-sharing-orders' AND a.resource_id=s.id AND a.operation='submit-result' AND a.changes->>'state'=s.state AND (a.actor_role,a.actor_id,a.actor_name)=(s.actor_role,s.actor_id,s.actor_name)) THEN RAISE EXCEPTION 'sharing outcome lacks its original actor result audit'; END IF;
 IF EXISTS(SELECT 1 FROM channel_sharing_observations v WHERE v.sharing_id=s.id AND NOT EXISTS(SELECT 1 FROM backoffice_audit a WHERE a.resource='channel-sharing-orders' AND a.resource_id=s.id AND a.operation=v.source||'-result' AND a.changes->>'state'=v.state AND (a.actor_role,a.actor_id,a.actor_name)=(v.actor_role,v.actor_id,v.actor_name))) THEN RAISE EXCEPTION 'sharing observation lacks its actual observer audit'; END IF;
 IF EXISTS(SELECT 1 FROM channel_sharing_completions WHERE sharing_id=s.id) AND NOT EXISTS(SELECT 1 FROM backoffice_audit a WHERE a.resource='channel-sharing-orders' AND a.resource_id=s.id AND a.operation='release-remainder' AND a.changes->>'state'='completed') THEN RAISE EXCEPTION 'sharing release lacks its actor audit'; END IF;
 IF d.provider='wxpay' AND s.last_observation_id IS NOT NULL AND length(s.provider_order_no)=0 THEN RAISE EXCEPTION 'wechat sharing observation requires its provider order number'; END IF;
 IF NOT d.declared OR s.merchant_id<>d.merchant_id OR NOT s.auto_finish OR NOT EXISTS(SELECT 1 FROM verified_payment_receipts p WHERE p.order_id=s.order_id AND p.channel_id=d.channel_id AND p.provider_trade_no=s.provider_trade_no) THEN RAISE EXCEPTION 'sharing operation lacks original paid declaration'; END IF;
 SELECT count(*),COALESCE(sum(amount),0),count(*) FILTER(WHERE state='succeeded'),count(*) FILTER(WHERE state='failed') INTO cnt,total,success,failed FROM channel_sharing_order_items WHERE sharing_id=s.id;
 IF cnt<>s.receiver_count OR cnt<>d.receiver_count OR total>d.merchant_net OR EXISTS(SELECT 1 FROM channel_sharing_order_items i LEFT JOIN provider_attempt_sharing_items p ON p.attempt_id=d.attempt_id AND p.receiver_id=i.receiver_id WHERE i.sharing_id=s.id AND (p.attempt_id IS NULL OR p.amount<>i.amount)) THEN RAISE EXCEPTION 'sharing operation changed original recipient terms'; END IF;
 IF s.completed AND (success+failed<>cnt OR s.state<>CASE WHEN failed=0 THEN 'succeeded' WHEN success=0 THEN 'failed' ELSE 'partial' END) THEN RAISE EXCEPTION 'sharing completion requires all recipient outcomes'; END IF;
 IF NOT EXISTS(SELECT 1 FROM channel_sharing_funding_invariants WHERE order_id=s.order_id AND valid IS TRUE) THEN RAISE EXCEPTION 'invalid original sharing funding'; END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER channel_sharing_execution_complete AFTER INSERT OR UPDATE ON channel_sharing_orders DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION channel_sharing_execution_guard();
CREATE CONSTRAINT TRIGGER channel_sharing_execution_item_complete AFTER INSERT OR UPDATE ON channel_sharing_order_items DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION channel_sharing_execution_guard();
CREATE CONSTRAINT TRIGGER channel_sharing_execution_completion_complete AFTER INSERT ON channel_sharing_completions DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION channel_sharing_execution_guard();

CREATE OR REPLACE FUNCTION channel_sharing_refund_hold_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 PERFORM id FROM orders WHERE id=NEW.order_id FOR UPDATE;
 IF EXISTS(SELECT 1 FROM channel_sharing_fundings f WHERE f.order_id=NEW.order_id AND NOT EXISTS(SELECT 1 FROM channel_sharing_completions c WHERE c.order_id=f.order_id)) THEN RAISE EXCEPTION USING ERRCODE='55000',MESSAGE='channel sharing funds are frozen',CONSTRAINT='channel_sharing_income_blocked'; END IF;
 RETURN NEW;
END $$;
CREATE OR REPLACE FUNCTION channel_sharing_open_payment_receiver_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF OLD.state='bound' AND NEW.state='unbinding' AND EXISTS(SELECT 1 FROM provider_attempt_sharing_items i JOIN provider_attempt_sharing d ON d.attempt_id=i.attempt_id WHERE i.receiver_id=OLD.id AND d.declared AND NOT EXISTS(SELECT 1 FROM channel_sharing_completions c WHERE c.order_id=d.order_id)) THEN RAISE EXCEPTION 'receiver is required by an original sharing payment'; END IF;
 RETURN NEW;
END $$;
