-- A sharing return recovers an authenticated recipient outflow. It does not
-- reopen the original unsplit reserve or pretend that a buyer was refunded.
CREATE TABLE channel_sharing_returns (
 id bigserial PRIMARY KEY, sharing_id bigint NOT NULL REFERENCES channel_sharing_orders(id),
 order_id bigint NOT NULL REFERENCES orders(id), merchant_id bigint NOT NULL REFERENCES merchants(id),
 receiver_id bigint NOT NULL, external_no text NOT NULL CHECK(external_no ~ '^[A-Za-z0-9_-]{1,128}$'),
 out_no text NOT NULL UNIQUE CHECK(out_no ~ '^PR[a-f0-9]{30}$'), amount bigint NOT NULL CHECK(amount>0),
 request_hash bytea NOT NULL CHECK(octet_length(request_hash)=32),
 failure_proof bytea CHECK(failure_proof IS NULL OR octet_length(failure_proof)=32),
 state text NOT NULL DEFAULT 'unsent' CHECK(state IN ('unsent','claimed','unknown','pending','succeeded','failed')),
 provider_return_no text NOT NULL DEFAULT '' CHECK(length(provider_return_no)<=64),
 last_observation_id bigint, paid_journal_id bigint UNIQUE REFERENCES journals(id),
 actor_role text NOT NULL CHECK(actor_role IN ('admin','merchant')), actor_id bigint NOT NULL CHECK(actor_id>0), actor_name text NOT NULL, actor_class text NOT NULL DEFAULT '',
 created_at timestamptz NOT NULL DEFAULT statement_timestamp(), claimed_at timestamptz,
 UNIQUE(merchant_id,external_no),FOREIGN KEY(sharing_id,receiver_id) REFERENCES channel_sharing_order_items(sharing_id,receiver_id),
 CHECK((state='unsent')=(claimed_at IS NULL)),CHECK((state='succeeded')=(paid_journal_id IS NOT NULL)),
 CHECK((state='failed' AND last_observation_id IS NULL)=(failure_proof IS NOT NULL))
);
CREATE INDEX channel_sharing_returns_original ON channel_sharing_returns(sharing_id,receiver_id,id);
CREATE TABLE channel_sharing_return_observations (
 id bigserial PRIMARY KEY, return_id bigint NOT NULL REFERENCES channel_sharing_returns(id),
 fingerprint bytea NOT NULL CHECK(octet_length(fingerprint)=32), source text NOT NULL CHECK(source IN ('submit','query')),
 provider_return_no text NOT NULL CHECK(length(provider_return_no)<=64), state text NOT NULL CHECK(state IN ('pending','succeeded','failed')),
 amount bigint NOT NULL CHECK(amount>0),actor_role text NOT NULL,actor_id bigint NOT NULL,actor_name text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT statement_timestamp(),UNIQUE(return_id,fingerprint)
);
ALTER TABLE channel_sharing_returns ADD CONSTRAINT channel_sharing_return_last_observation FOREIGN KEY(last_observation_id) REFERENCES channel_sharing_return_observations(id);
CREATE TRIGGER channel_sharing_returns_no_delete BEFORE DELETE ON channel_sharing_returns FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE TRIGGER channel_sharing_return_observations_immutable BEFORE UPDATE OR DELETE ON channel_sharing_return_observations FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE FUNCTION channel_sharing_return_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE obs channel_sharing_return_observations%ROWTYPE;
BEGIN
 PERFORM id FROM orders WHERE id=NEW.order_id FOR UPDATE;
 IF TG_OP='INSERT' THEN
  IF NEW.state<>'unsent' OR NEW.last_observation_id IS NOT NULL OR NEW.provider_return_no<>'' OR NEW.paid_journal_id IS NOT NULL THEN RAISE EXCEPTION 'new sharing return must be unsent'; END IF;
  RETURN NEW;
 END IF;
 IF (NEW.id,NEW.sharing_id,NEW.order_id,NEW.merchant_id,NEW.receiver_id,NEW.external_no,NEW.out_no,NEW.amount,NEW.request_hash,NEW.actor_role,NEW.actor_id,NEW.actor_name,NEW.actor_class,NEW.created_at)
 IS DISTINCT FROM (OLD.id,OLD.sharing_id,OLD.order_id,OLD.merchant_id,OLD.receiver_id,OLD.external_no,OLD.out_no,OLD.amount,OLD.request_hash,OLD.actor_role,OLD.actor_id,OLD.actor_name,OLD.actor_class,OLD.created_at)
 OR OLD.claimed_at IS NOT NULL AND NEW.claimed_at IS DISTINCT FROM OLD.claimed_at
 OR OLD.provider_return_no<>'' AND NEW.provider_return_no<>OLD.provider_return_no
 OR OLD.state IN ('succeeded','failed') AND NEW IS DISTINCT FROM OLD THEN RAISE EXCEPTION 'immutable original sharing return'; END IF;
 IF OLD.state='unsent' THEN
  IF NEW.state<>'claimed' OR NEW.last_observation_id IS NOT NULL OR NEW.provider_return_no<>'' OR NEW.paid_journal_id IS NOT NULL THEN RAISE EXCEPTION 'sharing return must claim before I/O'; END IF;
  RETURN NEW;
 END IF;
 IF NEW.state IN ('unsent','claimed') AND NEW.state<>OLD.state THEN RAISE EXCEPTION 'sharing return cannot resend'; END IF;
 IF NEW.last_observation_id IS NULL THEN
  IF OLD.last_observation_id IS NOT NULL OR NEW.state NOT IN ('claimed','unknown','failed') OR NEW.provider_return_no<>'' OR NEW.paid_journal_id IS NOT NULL THEN RAISE EXCEPTION 'sharing return lacks outcome evidence'; END IF;
 ELSE
  SELECT * INTO STRICT obs FROM channel_sharing_return_observations WHERE id=NEW.last_observation_id;
  IF (obs.return_id,obs.state,obs.provider_return_no,obs.amount) IS DISTINCT FROM (NEW.id,NEW.state,NEW.provider_return_no,NEW.amount) THEN RAISE EXCEPTION 'sharing return must match its exact observation'; END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER channel_sharing_return_binding BEFORE INSERT OR UPDATE ON channel_sharing_returns FOR EACH ROW EXECUTE FUNCTION channel_sharing_return_guard();

CREATE VIEW channel_sharing_return_invariants AS
SELECT r.id AS return_id,
 s.order_id=r.order_id AND s.merchant_id=r.merchant_id AND s.completed AND c.order_id=r.order_id
 AND i.state='succeeded' AND i.paid_journal_id IS NOT NULL AND r.amount<=i.amount
 AND EXISTS(SELECT 1 FROM channel_sharing_funding_invariants f WHERE f.order_id=r.order_id AND f.valid IS TRUE)
 AND (SELECT COALESCE(sum(x.amount),0) FROM channel_sharing_returns x WHERE x.sharing_id=r.sharing_id AND x.receiver_id=r.receiver_id AND x.state<>'failed')<=i.amount
 AND (SELECT count(*) FROM channel_sharing_returns x WHERE x.sharing_id=r.sharing_id AND x.receiver_id=r.receiver_id AND x.state IN ('unsent','claimed','unknown','pending'))<=1
 AND r.state<>'unsent' AND r.claimed_at IS NOT NULL
 AND EXISTS(SELECT 1 FROM backoffice_audit a WHERE a.resource='channel-sharing-returns' AND a.resource_id=r.id AND a.operation='submit' AND a.changes->>'state'='claimed' AND (a.actor_role,a.actor_id,a.actor_name)=(r.actor_role,r.actor_id,r.actor_name))
 AND (r.last_observation_id IS NULL AND r.state IN ('claimed','unknown','failed') AND r.provider_return_no=''
       AND (r.state<>'failed' OR r.failure_proof IS NOT NULL)
       AND (r.state NOT IN ('failed','unknown') OR EXISTS(SELECT 1 FROM backoffice_audit a WHERE a.resource='channel-sharing-returns' AND a.resource_id=r.id AND a.operation='submit-result' AND a.changes->>'state'=r.state AND (a.actor_role,a.actor_id,a.actor_name)=(r.actor_role,r.actor_id,r.actor_name))) OR
      EXISTS(SELECT 1 FROM channel_sharing_return_observations v WHERE v.id=r.last_observation_id AND (v.return_id,v.state,v.amount,v.provider_return_no)=(r.id,r.state,r.amount,r.provider_return_no)
       AND EXISTS(SELECT 1 FROM backoffice_audit a WHERE a.resource='channel-sharing-returns' AND a.resource_id=r.id AND a.operation=v.source||'-result' AND a.changes->>'state'=v.state AND (a.actor_role,a.actor_id,a.actor_name)=(v.actor_role,v.actor_id,v.actor_name))))
 AND (r.state<>'succeeded' AND r.paid_journal_id IS NULL OR EXISTS(
  SELECT 1 FROM journals j WHERE j.id=r.paid_journal_id AND j.event_key='channel-sharing-return:'||r.id::text||':paid'
  AND (SELECT count(*)=2 FROM ledger_entries e WHERE e.journal_id=j.id)
  AND (SELECT COALESCE(sum(e.amount),0) FROM ledger_entries e JOIN accounts a ON a.id=e.account_id WHERE e.journal_id=j.id AND (a.owner_id,a.kind,a.scope_id)=(0,'clearing',0))=-r.amount
  AND (SELECT COALESCE(sum(e.amount),0) FROM ledger_entries e JOIN accounts a ON a.id=e.account_id WHERE e.journal_id=j.id AND (a.owner_id,a.kind,a.scope_id)=(r.merchant_id,'available',0))=r.amount
 )) AS valid
FROM channel_sharing_returns r JOIN channel_sharing_orders s ON s.id=r.sharing_id
JOIN channel_sharing_order_items i ON (i.sharing_id,i.receiver_id)=(r.sharing_id,r.receiver_id)
JOIN channel_sharing_completions c ON c.sharing_id=s.id;
CREATE FUNCTION channel_sharing_return_complete_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE rid bigint;
BEGIN
 IF TG_TABLE_NAME='channel_sharing_returns' THEN rid:=NEW.id; ELSE rid:=NEW.return_id; END IF;
 IF NOT EXISTS(SELECT 1 FROM channel_sharing_return_invariants WHERE return_id=rid AND valid IS TRUE) THEN RAISE EXCEPTION 'invalid original sharing return funding'; END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER channel_sharing_return_complete AFTER INSERT OR UPDATE ON channel_sharing_returns DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION channel_sharing_return_complete_guard();
CREATE CONSTRAINT TRIGGER channel_sharing_return_observation_complete AFTER INSERT ON channel_sharing_return_observations DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION channel_sharing_return_complete_guard();

-- A return restores only this original order's successful recipient outflow.
-- Neither an acceptance nor a missing invariant projection restores income.
CREATE OR REPLACE FUNCTION order_income_channel_sharing_outflow(oid bigint) RETURNS bigint LANGUAGE sql STABLE AS $$
 SELECT GREATEST(0::numeric,
  COALESCE((SELECT sum(i.amount) FROM channel_sharing_orders s JOIN channel_sharing_order_items i ON i.sharing_id=s.id WHERE s.order_id=oid AND i.state='succeeded' AND EXISTS(SELECT 1 FROM channel_sharing_funding_invariants v WHERE v.order_id=s.order_id AND v.valid IS TRUE)),0)
  -COALESCE((SELECT sum(r.amount) FROM channel_sharing_returns r WHERE r.order_id=oid AND r.state='succeeded' AND EXISTS(SELECT 1 FROM channel_sharing_return_invariants v WHERE v.return_id=r.id AND v.valid IS TRUE)),0)
 )::bigint
$$;
