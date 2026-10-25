-- PF is a separate durable request. Acceptance is never a release proof.
CREATE TABLE channel_sharing_finishes (
 id bigserial PRIMARY KEY,order_id bigint NOT NULL REFERENCES channel_sharing_fundings(order_id),
 merchant_id bigint NOT NULL REFERENCES merchants(id),external_no text NOT NULL CHECK(external_no ~ '^[A-Za-z0-9_-]{1,128}$'),
 out_no text NOT NULL UNIQUE CHECK(out_no ~ '^PF[a-f0-9]{30}$'),provider_trade_no text NOT NULL CHECK(length(provider_trade_no) BETWEEN 1 AND 64),
 gross_amount bigint NOT NULL CHECK(gross_amount>0),merchant_net bigint NOT NULL CHECK(merchant_net>0),shared_amount bigint NOT NULL DEFAULT 0 CHECK(shared_amount=0),
 expected_unsplit bigint NOT NULL CHECK(expected_unsplit=gross_amount-shared_amount AND expected_unsplit>0),
 release_amount bigint NOT NULL CHECK(release_amount=merchant_net-shared_amount),request_hash bytea NOT NULL CHECK(octet_length(request_hash)=32),
 original_sharing_id bigint REFERENCES channel_sharing_orders(id),previous_finish_id bigint REFERENCES channel_sharing_finishes(id),
 state text NOT NULL DEFAULT 'unsent' CHECK(state IN ('unsent','claimed','pending','unknown','succeeded','failed')),
 provider_order_no text NOT NULL DEFAULT '' CHECK(length(provider_order_no)<=64),last_observation_id bigint,
 release_journal_id bigint UNIQUE REFERENCES journals(id),failure_proof bytea CHECK(failure_proof IS NULL OR octet_length(failure_proof)=32),
 actor_role text NOT NULL CHECK(actor_role IN ('merchant','admin')),actor_id bigint NOT NULL CHECK(actor_id>0),actor_name text NOT NULL,actor_class text NOT NULL DEFAULT '',
 created_at timestamptz NOT NULL DEFAULT statement_timestamp(),claimed_at timestamptz,
 UNIQUE(merchant_id,external_no),CHECK((state='unsent')=(claimed_at IS NULL)),CHECK((state='succeeded')=(release_journal_id IS NOT NULL)),
 CHECK((state='failed' AND last_observation_id IS NULL)=(failure_proof IS NOT NULL))
);
CREATE UNIQUE INDEX channel_sharing_finish_one_active ON channel_sharing_finishes(order_id) WHERE state<>'failed';
CREATE INDEX channel_sharing_finish_order_history ON channel_sharing_finishes(order_id,id DESC);
CREATE INDEX channel_sharing_finish_merchant_history ON channel_sharing_finishes(merchant_id,id DESC);
CREATE TABLE channel_sharing_finish_observations (
 id bigserial PRIMARY KEY,finish_id bigint NOT NULL REFERENCES channel_sharing_finishes(id),fingerprint bytea NOT NULL CHECK(octet_length(fingerprint)=32),
 source text NOT NULL CHECK(source IN ('submit','query')),provider_order_no text NOT NULL CHECK(length(provider_order_no) BETWEEN 1 AND 64),
 state text NOT NULL CHECK(state IN ('pending','succeeded','failed')),unsplit_amount bigint NOT NULL CHECK(unsplit_amount>=0),
 actor_role text NOT NULL,actor_id bigint NOT NULL,actor_name text NOT NULL,created_at timestamptz NOT NULL DEFAULT statement_timestamp(),
 UNIQUE(finish_id,fingerprint),CHECK(source<>'submit' OR state='pending')
);
ALTER TABLE channel_sharing_finishes ADD CONSTRAINT channel_sharing_finish_observation FOREIGN KEY(last_observation_id) REFERENCES channel_sharing_finish_observations(id);
CREATE TRIGGER channel_sharing_finish_no_delete BEFORE DELETE ON channel_sharing_finishes FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE TRIGGER channel_sharing_finish_observation_immutable BEFORE UPDATE OR DELETE ON channel_sharing_finish_observations FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE FUNCTION channel_sharing_finish_identity_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE d provider_attempt_sharing%ROWTYPE; p channel_sharing_orders%ROWTYPE;
BEGIN
 PERFORM id FROM orders WHERE id=NEW.order_id FOR UPDATE;
 IF TG_OP='INSERT' THEN
  SELECT * INTO STRICT d FROM provider_attempt_sharing WHERE order_id=NEW.order_id;
  SELECT * INTO p FROM channel_sharing_orders WHERE order_id=NEW.order_id;
  IF EXISTS(SELECT 1 FROM channel_sharing_finishes x WHERE x.order_id=NEW.order_id AND x.state<>'failed') OR NEW.previous_finish_id IS DISTINCT FROM (SELECT max(x.id) FROM channel_sharing_finishes x WHERE x.order_id=NEW.order_id) OR NEW.state<>'unsent' OR NOT d.declared OR d.purpose<>'payment' OR d.product<>'wxpay_v3_direct' OR
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
CREATE TRIGGER channel_sharing_finish_identity BEFORE INSERT OR UPDATE ON channel_sharing_finishes FOR EACH ROW EXECUTE FUNCTION channel_sharing_finish_identity_guard();
CREATE FUNCTION channel_sharing_ps_finish_exclusion_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 PERFORM id FROM orders WHERE id=NEW.order_id FOR UPDATE;
 IF EXISTS(SELECT 1 FROM channel_sharing_finishes WHERE order_id=NEW.order_id) AND (TG_OP='INSERT' OR NEW IS DISTINCT FROM OLD) THEN RAISE EXCEPTION 'original payment already claimed by independent finish'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER channel_sharing_ps_finish_exclusion BEFORE INSERT OR UPDATE ON channel_sharing_orders FOR EACH ROW EXECUTE FUNCTION channel_sharing_ps_finish_exclusion_guard();
CREATE FUNCTION channel_sharing_completion_finish_exclusion_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 PERFORM id FROM orders WHERE id=NEW.order_id FOR UPDATE;
 IF EXISTS(SELECT 1 FROM channel_sharing_finishes WHERE order_id=NEW.order_id) THEN RAISE EXCEPTION 'independent finish excludes auto finish completion'; END IF;RETURN NEW;
END $$;
CREATE TRIGGER channel_sharing_completion_finish_exclusion BEFORE INSERT ON channel_sharing_completions FOR EACH ROW EXECUTE FUNCTION channel_sharing_completion_finish_exclusion_guard();

CREATE VIEW channel_sharing_finish_invariants AS
SELECT p.id AS finish_id,p.order_id,
 d.declared AND d.purpose='payment' AND d.product='wxpay_v3_direct' AND o.status='paid'
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

CREATE VIEW channel_sharing_fund_releases AS
SELECT order_id,release_amount FROM channel_sharing_completions
UNION ALL
SELECT order_id,release_amount FROM channel_sharing_finishes p WHERE p.state='succeeded' AND EXISTS(SELECT 1 FROM channel_sharing_finish_invariants v WHERE v.finish_id=p.id AND v.valid IS TRUE);

CREATE OR REPLACE VIEW channel_sharing_funding_invariants AS
SELECT f.order_id,
 d.declared AND o.status='paid' AND f.amount=d.merchant_net AND d.merchant_net=o.merchant_amount
 AND (a.owner_id,a.kind,a.scope_id)=(o.merchant_id,'channel_sharing_reserve',o.id)
 AND j.event_key='channel-sharing:'||o.id::text||':reserve'
 AND EXISTS(SELECT 1 FROM verified_payment_receipts p WHERE p.order_id=o.id AND p.channel_id=d.channel_id AND length(p.provider_trade_no)>0)
 AND a.balance=f.amount-COALESCE((SELECT sum(i.amount) FROM channel_sharing_order_items i JOIN channel_sharing_orders s ON s.id=i.sharing_id WHERE s.order_id=o.id AND i.state='succeeded'),0)-(COALESCE(c.release_amount,0)+COALESCE(pf.release_amount,0))
 AND (SELECT count(*) FROM ledger_entries e WHERE e.account_id=a.id)=1+(SELECT count(*) FROM channel_sharing_order_items i JOIN channel_sharing_orders s ON s.id=i.sharing_id WHERE s.order_id=o.id AND i.state='succeeded')+CASE WHEN c.release_amount>0 OR pf.release_amount>0 THEN 1 ELSE 0 END
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
 AND NOT (c.order_id IS NOT NULL AND pf.order_id IS NOT NULL)
 AND (pf.order_id IS NULL OR pf.state='succeeded' AND EXISTS(SELECT 1 FROM channel_sharing_finish_invariants z WHERE z.finish_id=pf.id AND z.valid IS TRUE))
 AND (c.order_id IS NULL OR EXISTS(SELECT 1 FROM channel_sharing_orders s WHERE s.id=c.sharing_id AND s.order_id=o.id AND s.completed AND s.auto_finish
  AND (d.provider='wxpay' AND c.proof_kind='wechat_remaining_zero' OR d.provider='alipay' AND c.proof_kind='alipay_royalty_finish')
  AND c.release_amount=f.amount-COALESCE((SELECT sum(i.amount) FROM channel_sharing_order_items i WHERE i.sharing_id=s.id AND i.state='succeeded'),0)
  AND c.expected_unsplit=d.gross_amount-COALESCE((SELECT sum(i.amount) FROM channel_sharing_order_items i WHERE i.sharing_id=s.id AND i.state='succeeded'),0)
  AND (c.release_amount=0 OR EXISTS(SELECT 1 FROM journals q WHERE q.id=c.release_journal_id AND q.event_key='channel-sharing:'||s.id::text||':release'
   AND (SELECT count(*)=2 FROM ledger_entries e WHERE e.journal_id=q.id)
   AND (SELECT COALESCE(sum(e.amount),0) FROM ledger_entries e WHERE e.journal_id=q.id AND e.account_id=a.id)=-c.release_amount
   AND (SELECT COALESCE(sum(e.amount),0) FROM ledger_entries e JOIN accounts av ON av.id=e.account_id WHERE e.journal_id=q.id AND (av.owner_id,av.kind,av.scope_id)=(o.merchant_id,'available',0))=c.release_amount)))) AS valid
FROM channel_sharing_fundings f JOIN provider_attempt_sharing d ON d.order_id=f.order_id JOIN orders o ON o.id=f.order_id JOIN accounts a ON a.id=f.account_id JOIN journals j ON j.id=f.reserve_journal_id LEFT JOIN channel_sharing_completions c ON c.order_id=f.order_id LEFT JOIN channel_sharing_finishes pf ON pf.order_id=f.order_id AND pf.state='succeeded';

CREATE FUNCTION channel_sharing_finish_complete_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE fid bigint; oid bigint;
BEGIN
 IF TG_TABLE_NAME='channel_sharing_finishes' THEN fid:=NEW.id; ELSE fid:=NEW.finish_id; END IF;
 SELECT order_id INTO oid FROM channel_sharing_finishes WHERE id=fid;
 IF NOT EXISTS(SELECT 1 FROM channel_sharing_finish_invariants WHERE finish_id=fid AND valid IS TRUE) OR NOT EXISTS(SELECT 1 FROM channel_sharing_funding_invariants WHERE order_id=oid AND valid IS TRUE) THEN RAISE EXCEPTION 'finish lacks its original payment, result, funding or actor proof'; END IF;RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER channel_sharing_finish_complete AFTER INSERT OR UPDATE ON channel_sharing_finishes DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION channel_sharing_finish_complete_guard();
CREATE CONSTRAINT TRIGGER channel_sharing_finish_observation_complete AFTER INSERT ON channel_sharing_finish_observations DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION channel_sharing_finish_complete_guard();
CREATE OR REPLACE FUNCTION channel_sharing_payment_reference_active(oid bigint) RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT NOT EXISTS(SELECT 1 FROM channel_sharing_fund_releases WHERE order_id=oid)
$$;
CREATE OR REPLACE FUNCTION channel_sharing_refund_hold_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 PERFORM id FROM orders WHERE id=NEW.order_id FOR UPDATE;
 IF EXISTS(SELECT 1 FROM channel_sharing_fundings f WHERE f.order_id=NEW.order_id AND NOT EXISTS(SELECT 1 FROM channel_sharing_fund_releases c WHERE c.order_id=f.order_id)) THEN RAISE EXCEPTION USING ERRCODE='55000',MESSAGE='channel sharing funds are frozen',CONSTRAINT='channel_sharing_income_blocked'; END IF;RETURN NEW;
END $$;

CREATE OR REPLACE FUNCTION order_profit_source_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE e order_income_source_evidence%ROWTYPE; o orders%ROWTYPE; request ledger_requests%ROWTYPE;
BEGIN
 PERFORM id FROM orders WHERE id=NEW.order_id FOR UPDATE;
 SELECT * INTO STRICT o FROM orders WHERE id=NEW.order_id;
 SELECT * INTO e FROM order_income_source_evidence WHERE order_id=NEW.order_id;
 SELECT * INTO STRICT request FROM ledger_requests WHERE id=NEW.request_id;
 IF e.valid IS NOT TRUE OR e.pending_refunds<>0 OR NEW.amount>e.remaining OR NEW.amount<>floor(o.amount::numeric*NEW.rate_bps/10000) OR
  request.kind<>'profitsharing' OR request.status<>'pending' OR request.merchant_id<>o.merchant_id OR request.amount<>NEW.amount OR request.external_no<>'order:'||o.trade_no||':rule:'||NEW.rule_id::text OR
  EXISTS(SELECT 1 FROM order_income_holds WHERE order_id=o.id AND state='held') OR
  EXISTS(SELECT 1 FROM channel_sharing_fundings f WHERE f.order_id=o.id AND NOT EXISTS(SELECT 1 FROM channel_sharing_fund_releases c WHERE c.order_id=f.order_id))
 THEN RAISE EXCEPTION 'profit allocation exceeds retained original income'; END IF;
 RETURN NEW;
END $$;
