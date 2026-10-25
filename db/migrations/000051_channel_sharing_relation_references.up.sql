-- Drafts are local data. Only a claimed remote operation owns a global
-- relationship; successful local bindings hold independently audited leases.
ALTER TABLE channel_sharing_receivers DROP CONSTRAINT channel_sharing_receivers_relation_hash_key;
ALTER TABLE channel_sharing_receivers DROP CONSTRAINT channel_sharing_receivers_relation_auth_hash_key;
CREATE INDEX channel_sharing_receiver_physical ON channel_sharing_receivers(relation_hash);
CREATE INDEX channel_sharing_receiver_authenticated ON channel_sharing_receivers(relation_auth_hash);

CREATE TABLE channel_sharing_relations (
 id bigserial PRIMARY KEY,relation_hash bytea NOT NULL UNIQUE CHECK(octet_length(relation_hash)=32),
 relation_auth_hash bytea NOT NULL CHECK(octet_length(relation_auth_hash)=32),
 source_receiver_id bigint NOT NULL REFERENCES channel_sharing_receivers(id),
 state text NOT NULL DEFAULT 'unbound' CHECK(state IN ('unbound','binding','bound','unbinding','unknown')),
 last_operation_id bigint REFERENCES channel_sharing_receiver_operations(id),
 bound_operation_id bigint REFERENCES channel_sharing_receiver_operations(id),
 created_at timestamptz NOT NULL DEFAULT statement_timestamp()
);
CREATE UNIQUE INDEX channel_sharing_relation_active_auth ON channel_sharing_relations(relation_auth_hash) WHERE state<>'unbound';
ALTER TABLE channel_sharing_receiver_operations ADD COLUMN relation_id bigint REFERENCES channel_sharing_relations(id);
ALTER TABLE channel_sharing_receiver_operations ADD COLUMN source text NOT NULL DEFAULT 'remote' CHECK(source IN ('remote','reference'));
ALTER TABLE channel_sharing_receiver_operations ADD COLUMN reference_operation_id bigint REFERENCES channel_sharing_receiver_operations(id);
ALTER TABLE channel_sharing_receiver_operations ADD CONSTRAINT channel_sharing_reference_proof_shape CHECK(source='remote' AND reference_operation_id IS NULL OR source='reference' AND reference_operation_id IS NOT NULL);

-- A historical failed bind remains unbound; unknown/claimed never becomes a
-- successful proof during migration. No drafts with no operation are registered.
INSERT INTO channel_sharing_relations(relation_hash,relation_auth_hash,source_receiver_id,state,last_operation_id,bound_operation_id)
SELECT r.relation_hash,r.relation_auth_hash,r.id,CASE WHEN r.state IN ('draft','unbound') THEN 'unbound' ELSE r.state END,r.last_operation_id,
 CASE WHEN r.state='binding' OR r.state='unknown' AND last_op.action='bind' THEN NULL ELSE
 (SELECT id FROM channel_sharing_receiver_operations b WHERE b.receiver_id=r.id AND b.action='bind' AND b.state='succeeded' ORDER BY id DESC LIMIT 1) END
FROM channel_sharing_receivers r LEFT JOIN channel_sharing_receiver_operations last_op ON last_op.id=r.last_operation_id
WHERE EXISTS(SELECT 1 FROM channel_sharing_receiver_operations op WHERE op.receiver_id=r.id);
UPDATE channel_sharing_receiver_operations op SET relation_id=g.id FROM channel_sharing_receivers r JOIN channel_sharing_relations g ON g.relation_hash=r.relation_hash WHERE op.receiver_id=r.id;
ALTER TABLE channel_sharing_receiver_operations ALTER COLUMN relation_id SET NOT NULL;
CREATE INDEX channel_sharing_receiver_operation_relation ON channel_sharing_receiver_operations(relation_id,id);

CREATE TABLE channel_sharing_relation_leases (
 id bigserial PRIMARY KEY,relation_id bigint NOT NULL REFERENCES channel_sharing_relations(id),
 receiver_id bigint NOT NULL REFERENCES channel_sharing_receivers(id),
 bind_operation_id bigint NOT NULL UNIQUE REFERENCES channel_sharing_receiver_operations(id),
 released_by_operation_id bigint UNIQUE REFERENCES channel_sharing_receiver_operations(id),
 created_at timestamptz NOT NULL DEFAULT statement_timestamp(),released_at timestamptz,
 CHECK((released_by_operation_id IS NULL)=(released_at IS NULL))
);
CREATE UNIQUE INDEX channel_sharing_relation_receiver_lease ON channel_sharing_relation_leases(receiver_id) WHERE released_by_operation_id IS NULL;
CREATE INDEX channel_sharing_relation_live_leases ON channel_sharing_relation_leases(relation_id) WHERE released_by_operation_id IS NULL;
INSERT INTO channel_sharing_relation_leases(relation_id,receiver_id,bind_operation_id,created_at)
SELECT g.id,r.id,g.bound_operation_id,b.created_at FROM channel_sharing_relations g JOIN channel_sharing_receivers r ON r.id=g.source_receiver_id JOIN channel_sharing_receiver_operations b ON b.id=g.bound_operation_id
WHERE r.state IN ('bound','unbinding') OR r.state='unknown' AND EXISTS(SELECT 1 FROM channel_sharing_receiver_operations op WHERE op.id=r.last_operation_id AND op.action='unbind');

-- 50 supplies completions. During an installation with no financial extension,
-- every declared original payment remains protected conservatively.
CREATE FUNCTION channel_sharing_payment_reference_active(oid bigint) RETURNS boolean LANGUAGE plpgsql STABLE AS $$
DECLARE active boolean;
BEGIN
 IF to_regclass('channel_sharing_completions') IS NULL THEN RETURN true; END IF;
 EXECUTE 'SELECT NOT EXISTS(SELECT 1 FROM channel_sharing_completions WHERE order_id=$1)' INTO active USING oid;
 RETURN active;
END $$;
CREATE FUNCTION channel_sharing_receiver_has_references(rid bigint) RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT EXISTS(SELECT 1 FROM channel_sharing_rule_items i JOIN channel_sharing_rules r ON r.id=i.rule_id JOIN channel_sharing_rule_current c ON c.rule_id=r.id WHERE i.receiver_id=rid AND r.enabled)
 OR EXISTS(SELECT 1 FROM provider_attempt_sharing_items i JOIN provider_attempt_sharing d ON d.attempt_id=i.attempt_id WHERE i.receiver_id=rid AND d.declared AND channel_sharing_payment_reference_active(d.order_id))
$$;
CREATE OR REPLACE FUNCTION channel_sharing_open_payment_receiver_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF OLD.state='bound' AND NEW.state='unbinding' AND EXISTS(SELECT 1 FROM provider_attempt_sharing_items i JOIN provider_attempt_sharing d ON d.attempt_id=i.attempt_id WHERE i.receiver_id=OLD.id AND d.declared AND channel_sharing_payment_reference_active(d.order_id))
 THEN RAISE EXCEPTION 'receiver is required by an original sharing payment'; END IF;
 RETURN NEW;
END $$;

CREATE FUNCTION channel_sharing_relation_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE op channel_sharing_receiver_operations%ROWTYPE; r channel_sharing_receivers%ROWTYPE;
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'immutable sharing relation history'; END IF;
 SELECT * INTO STRICT r FROM channel_sharing_receivers WHERE id=NEW.source_receiver_id;
 IF (r.relation_hash,r.relation_auth_hash) IS DISTINCT FROM (NEW.relation_hash,NEW.relation_auth_hash) THEN RAISE EXCEPTION 'sharing relation source identity mismatch'; END IF;
 IF TG_OP='INSERT' THEN
  IF NEW.state<>'unbound' OR NEW.last_operation_id IS NOT NULL OR NEW.bound_operation_id IS NOT NULL THEN RAISE EXCEPTION 'new sharing relation must be unbound'; END IF;RETURN NEW;
 END IF;
 IF (NEW.id,NEW.relation_hash,NEW.created_at) IS DISTINCT FROM (OLD.id,OLD.relation_hash,OLD.created_at) THEN RAISE EXCEPTION 'immutable sharing physical identity'; END IF;
 IF NEW IS NOT DISTINCT FROM OLD THEN RETURN NEW; END IF;
 IF OLD.state='unbound' AND NEW.state='unbound' AND NEW.last_operation_id IS NOT DISTINCT FROM OLD.last_operation_id AND NEW.bound_operation_id IS NOT DISTINCT FROM OLD.bound_operation_id
  AND NOT EXISTS(SELECT 1 FROM channel_sharing_relation_leases WHERE relation_id=OLD.id AND released_by_operation_id IS NULL) THEN RETURN NEW; END IF;
 SELECT * INTO op FROM channel_sharing_receiver_operations WHERE id=NEW.last_operation_id;
 IF op.id IS NULL OR op.relation_id<>OLD.id OR op.source<>'remote' THEN RAISE EXCEPTION 'missing original remote relation operation'; END IF;
 IF (NEW.source_receiver_id,NEW.relation_auth_hash) IS DISTINCT FROM (OLD.source_receiver_id,OLD.relation_auth_hash) AND NOT(OLD.state='unbound' AND NEW.state='binding') THEN RAISE EXCEPTION 'live sharing relation cannot change source'; END IF;
 IF OLD.state='unbound' AND NEW.state='binding' AND op.action='bind' AND op.state='claimed' AND op.receiver_id=NEW.source_receiver_id AND NEW.bound_operation_id IS NULL THEN RETURN NEW; END IF;
 IF OLD.state='bound' AND NEW.state='unbinding' AND op.action='unbind' AND op.state='claimed' AND NEW.bound_operation_id=OLD.bound_operation_id THEN RETURN NEW; END IF;
 IF NEW.last_operation_id IS DISTINCT FROM OLD.last_operation_id THEN RAISE EXCEPTION 'sharing result changed original remote operation'; END IF;
 IF OLD.state='binding' AND op.action='bind' AND
 ((NEW.state='bound' AND op.state='succeeded' AND NEW.bound_operation_id=op.id) OR (NEW.state='unbound' AND op.state='failed' AND NEW.bound_operation_id IS NULL) OR (NEW.state='unknown' AND op.state='unknown' AND NEW.bound_operation_id IS NULL)) THEN RETURN NEW; END IF;
 IF OLD.state='unbinding' AND op.action='unbind' AND NEW.bound_operation_id=OLD.bound_operation_id AND
 ((NEW.state='unbound' AND op.state='succeeded') OR (NEW.state='bound' AND op.state='failed') OR (NEW.state='unknown' AND op.state='unknown')) THEN RETURN NEW; END IF;
 RAISE EXCEPTION 'invalid global sharing relation transition';
END $$;
CREATE TRIGGER channel_sharing_relation_binding BEFORE INSERT OR UPDATE OR DELETE ON channel_sharing_relations FOR EACH ROW EXECUTE FUNCTION channel_sharing_relation_guard();

CREATE FUNCTION channel_sharing_reference_operation_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE g channel_sharing_relations%ROWTYPE; r channel_sharing_receivers%ROWTYPE; src channel_sharing_receivers%ROWTYPE; proof channel_sharing_receiver_operations%ROWTYPE;
BEGIN
 IF TG_OP='UPDATE' THEN
  IF (NEW.relation_id,NEW.source,NEW.reference_operation_id) IS DISTINCT FROM (OLD.relation_id,OLD.source,OLD.reference_operation_id) THEN RAISE EXCEPTION 'immutable sharing reference proof'; END IF;RETURN NEW;
 END IF;
 SELECT * INTO STRICT g FROM channel_sharing_relations WHERE id=NEW.relation_id;
 SELECT * INTO STRICT r FROM channel_sharing_receivers WHERE id=NEW.receiver_id;
 SELECT * INTO STRICT src FROM channel_sharing_receivers WHERE id=g.source_receiver_id;
 IF r.relation_hash<>g.relation_hash OR r.relation_auth_hash<>g.relation_auth_hash THEN RAISE EXCEPTION 'receiver changed global relation'; END IF;
 IF NEW.source='remote' THEN
  IF NEW.action='bind' AND g.state<>'unbound' OR NEW.action='unbind' AND g.state<>'bound' THEN RAISE EXCEPTION 'global relation already has a send barrier'; END IF;
  IF NEW.action='unbind' AND ((SELECT count(*) FROM channel_sharing_relation_leases WHERE relation_id=g.id AND released_by_operation_id IS NULL)<>1 OR NOT EXISTS(SELECT 1 FROM channel_sharing_relation_leases WHERE relation_id=g.id AND receiver_id=r.id AND released_by_operation_id IS NULL)) THEN RAISE EXCEPTION 'remote unbind requires the last original lease'; END IF;
 ELSE
  SELECT * INTO proof FROM channel_sharing_receiver_operations WHERE id=NEW.reference_operation_id;
  IF g.state<>'bound' OR proof.id IS NULL OR proof.id IS DISTINCT FROM g.bound_operation_id OR proof.source<>'remote' OR proof.action<>'bind' OR proof.state<>'succeeded' OR proof.relation_id<>g.id
   OR (r.provider,r.app_id,r.product,r.account_hash) IS DISTINCT FROM (src.provider,src.app_id,src.product,src.account_hash)
  THEN RAISE EXCEPTION 'reference has no matching successful remote binding'; END IF;
  IF NEW.action='unbind' AND (SELECT count(*) FROM channel_sharing_relation_leases WHERE relation_id=g.id AND released_by_operation_id IS NULL)<=1 THEN RAISE EXCEPTION 'last sharing lease requires remote unbind'; END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER channel_sharing_reference_operation_binding BEFORE INSERT OR UPDATE ON channel_sharing_receiver_operations FOR EACH ROW EXECUTE FUNCTION channel_sharing_reference_operation_guard();

CREATE FUNCTION channel_sharing_lease_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'immutable sharing lease history'; END IF;
 IF TG_OP='INSERT' THEN IF NEW.released_by_operation_id IS NOT NULL OR NEW.released_at IS NOT NULL THEN RAISE EXCEPTION 'new sharing lease must be active'; END IF;RETURN NEW;END IF;
 IF (NEW.id,NEW.relation_id,NEW.receiver_id,NEW.bind_operation_id,NEW.created_at) IS DISTINCT FROM (OLD.id,OLD.relation_id,OLD.receiver_id,OLD.bind_operation_id,OLD.created_at) OR OLD.released_by_operation_id IS NOT NULL AND NEW IS DISTINCT FROM OLD THEN RAISE EXCEPTION 'immutable sharing lease proof'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER channel_sharing_lease_binding BEFORE INSERT OR UPDATE OR DELETE ON channel_sharing_relation_leases FOR EACH ROW EXECUTE FUNCTION channel_sharing_lease_guard();

CREATE FUNCTION channel_sharing_relation_complete() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE gid bigint; g channel_sharing_relations%ROWTYPE; proof channel_sharing_receiver_operations%ROWTYPE; last_op channel_sharing_receiver_operations%ROWTYPE; last_receiver channel_sharing_receivers%ROWTYPE; cnt bigint; expected_receiver_state text;
BEGIN
 IF TG_TABLE_NAME='channel_sharing_relations' THEN gid:=NEW.id;
 ELSIF TG_TABLE_NAME='channel_sharing_receivers' THEN SELECT id INTO gid FROM channel_sharing_relations WHERE relation_hash=NEW.relation_hash;
 ELSE gid:=NEW.relation_id;END IF;
 IF gid IS NULL THEN RETURN NULL;END IF;
 SELECT * INTO STRICT g FROM channel_sharing_relations WHERE id=gid;
 -- Validate both directions. Checking only claimed operations would miss an
 -- operation changed to failed while its global/receiver barrier stayed live.
 IF g.last_operation_id IS NULL THEN
  IF g.state<>'unbound' OR g.bound_operation_id IS NOT NULL THEN RAISE EXCEPTION 'global sharing state is missing its original operation'; END IF;
 ELSE
  SELECT * INTO STRICT last_op FROM channel_sharing_receiver_operations WHERE id=g.last_operation_id;
  SELECT * INTO STRICT last_receiver FROM channel_sharing_receivers WHERE id=last_op.receiver_id;
  IF last_op.source<>'remote' OR last_op.relation_id<>g.id OR last_receiver.relation_hash<>g.relation_hash THEN RAISE EXCEPTION 'global sharing state changed its original remote operation'; END IF;
  IF g.state='binding' THEN
   IF last_op.action<>'bind' OR last_op.state<>'claimed' OR g.source_receiver_id<>last_receiver.id OR g.bound_operation_id IS NOT NULL THEN RAISE EXCEPTION 'global binding lacks its claimed original bind'; END IF;
   expected_receiver_state:='binding';
  ELSIF g.state='unbinding' THEN
   IF last_op.action<>'unbind' OR last_op.state<>'claimed' THEN RAISE EXCEPTION 'global unbinding lacks its claimed original unbind'; END IF;
   expected_receiver_state:='unbinding';
  ELSIF g.state='unknown' THEN
   IF last_op.state<>'unknown' OR last_op.action='bind' AND (g.source_receiver_id<>last_receiver.id OR g.bound_operation_id IS NOT NULL) OR last_op.action='unbind' AND g.bound_operation_id IS NULL THEN RAISE EXCEPTION 'global uncertainty lacks its original uncertain operation'; END IF;
   expected_receiver_state:='unknown';
  ELSIF g.state='bound' THEN
   IF NOT(last_op.action='bind' AND last_op.state='succeeded' AND g.bound_operation_id=last_op.id OR last_op.action='unbind' AND last_op.state='failed') THEN RAISE EXCEPTION 'global bound state differs from its original result'; END IF;
   expected_receiver_state:='bound';
  ELSE
   IF NOT(last_op.action='bind' AND last_op.state='failed' AND g.bound_operation_id IS NULL OR last_op.action='unbind' AND last_op.state='succeeded') THEN RAISE EXCEPTION 'global unbound state differs from its original result'; END IF;
   expected_receiver_state:=CASE WHEN last_op.action='bind' THEN 'draft' ELSE 'unbound' END;
  END IF;
  IF g.state IN ('binding','unbinding','unknown') AND last_receiver.last_operation_id IS DISTINCT FROM last_op.id OR
   last_receiver.last_operation_id=last_op.id AND last_receiver.state<>expected_receiver_state THEN RAISE EXCEPTION 'original receiver differs from its global operation result'; END IF;
 END IF;
 SELECT count(*) INTO cnt FROM channel_sharing_relation_leases WHERE relation_id=gid AND released_by_operation_id IS NULL;
 IF g.state='bound' AND cnt<1 OR g.state IN ('unbound','binding') AND cnt<>0 OR g.state='unbinding' AND cnt<>1 THEN RAISE EXCEPTION 'global sharing relation has invalid live leases'; END IF;
 IF g.state IN ('bound','unbinding') OR g.state='unknown' AND g.bound_operation_id IS NOT NULL THEN
  SELECT * INTO proof FROM channel_sharing_receiver_operations WHERE id=g.bound_operation_id;
  IF proof.id IS NULL OR proof.relation_id<>gid OR proof.source<>'remote' OR proof.action<>'bind' OR proof.state<>'succeeded' THEN RAISE EXCEPTION 'global sharing relation has no remote binding proof'; END IF;
 END IF;
 IF EXISTS(SELECT 1 FROM channel_sharing_relation_leases l JOIN channel_sharing_receivers r ON r.id=l.receiver_id JOIN channel_sharing_receiver_operations b ON b.id=l.bind_operation_id
  LEFT JOIN channel_sharing_receiver_operations u ON u.id=l.released_by_operation_id
  WHERE l.relation_id=gid AND (b.receiver_id<>l.receiver_id OR b.relation_id<>gid OR b.action<>'bind' OR b.state<>'succeeded'
   OR l.released_by_operation_id IS NOT NULL AND (u.id IS NULL OR u.receiver_id<>l.receiver_id OR u.relation_id<>gid OR u.action<>'unbind' OR u.state<>'succeeded')
   OR l.released_by_operation_id IS NULL AND (r.state NOT IN ('bound','unbinding','unknown') OR g.state NOT IN ('bound','unbinding','unknown') OR b.source='remote' AND b.id IS DISTINCT FROM g.bound_operation_id OR b.source='reference' AND b.reference_operation_id IS DISTINCT FROM g.bound_operation_id)))
 THEN RAISE EXCEPTION 'invalid sharing lease operation proof'; END IF;
 IF EXISTS(SELECT 1 FROM channel_sharing_receivers r WHERE r.relation_hash=g.relation_hash AND
  ((r.state IN ('bound','unbinding') OR r.state='unknown' AND EXISTS(SELECT 1 FROM channel_sharing_receiver_operations op WHERE op.id=r.last_operation_id AND op.action='unbind'))
    IS DISTINCT FROM EXISTS(SELECT 1 FROM channel_sharing_relation_leases l WHERE l.receiver_id=r.id AND l.released_by_operation_id IS NULL)))
 THEN RAISE EXCEPTION 'receiver binding is missing its original sharing lease'; END IF;
 IF EXISTS(SELECT 1 FROM channel_sharing_receiver_operations op WHERE op.relation_id=gid AND op.source='reference' AND
  (op.state<>'succeeded' OR NOT EXISTS(SELECT 1 FROM channel_sharing_receiver_operations p WHERE p.id=op.reference_operation_id AND p.relation_id=gid AND p.source='remote' AND p.action='bind' AND p.state='succeeded') OR
   op.action='bind' AND NOT EXISTS(SELECT 1 FROM channel_sharing_relation_leases l WHERE l.bind_operation_id=op.id AND l.receiver_id=op.receiver_id) OR
   op.action='unbind' AND NOT EXISTS(SELECT 1 FROM channel_sharing_relation_leases l WHERE l.released_by_operation_id=op.id AND l.receiver_id=op.receiver_id)))
 THEN RAISE EXCEPTION 'uncommitted sharing reference result'; END IF;
 IF EXISTS(SELECT 1 FROM channel_sharing_receiver_operations op WHERE op.relation_id=gid AND op.source='remote' AND op.state IN ('claimed','unknown') AND
  (op.id IS DISTINCT FROM g.last_operation_id OR g.state NOT IN ('binding','unbinding','unknown') OR NOT EXISTS(SELECT 1 FROM channel_sharing_receivers r WHERE r.id=op.receiver_id AND r.last_operation_id=op.id AND r.state IN ('binding','unbinding','unknown'))))
 THEN RAISE EXCEPTION 'remote sharing operation lost its global send barrier'; END IF;
 IF EXISTS(SELECT 1 FROM channel_sharing_receiver_operations op WHERE op.relation_id=gid AND op.state<>'unsent' AND
  (NOT EXISTS(SELECT 1 FROM backoffice_audit a WHERE a.resource='channel-sharing-receiver-operations' AND a.resource_id=op.id AND a.operation=op.action AND a.actor_role=op.actor_role AND a.actor_id=op.actor_id AND a.actor_name=op.actor_name AND a.changes->>'state'='claimed')
   OR op.state<>'claimed' AND NOT EXISTS(SELECT 1 FROM backoffice_audit a WHERE a.resource='channel-sharing-receiver-operations' AND a.resource_id=op.id AND a.operation=op.action AND a.actor_role=op.actor_role AND a.actor_id=op.actor_id AND a.actor_name=op.actor_name AND a.changes->>'state'=op.state)))
 THEN RAISE EXCEPTION 'sharing operation is missing its original actor audit'; END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER channel_sharing_relation_complete_registry AFTER INSERT OR UPDATE ON channel_sharing_relations DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION channel_sharing_relation_complete();
CREATE CONSTRAINT TRIGGER channel_sharing_relation_complete_leases AFTER INSERT OR UPDATE ON channel_sharing_relation_leases DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION channel_sharing_relation_complete();
CREATE CONSTRAINT TRIGGER channel_sharing_relation_complete_receivers AFTER INSERT OR UPDATE ON channel_sharing_receivers DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION channel_sharing_relation_complete();
CREATE CONSTRAINT TRIGGER channel_sharing_relation_complete_operations AFTER INSERT OR UPDATE ON channel_sharing_receiver_operations DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION channel_sharing_relation_complete();
