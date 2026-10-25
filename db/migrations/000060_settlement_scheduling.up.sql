CREATE TABLE settlement_schedule_preferences (
 id bigserial PRIMARY KEY,merchant_id bigint NOT NULL REFERENCES merchants(id),revision bigint NOT NULL CHECK(revision>0),
 enabled boolean NOT NULL,recipient_config_id bigint REFERENCES settlement_recipient_configs(id),actor_name text NOT NULL,
 proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32),created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
 UNIQUE(merchant_id,revision),CHECK(NOT enabled OR recipient_config_id IS NOT NULL)
);
CREATE TABLE settlement_schedule_preference_heads (
 merchant_id bigint PRIMARY KEY REFERENCES merchants(id),preference_id bigint NOT NULL UNIQUE REFERENCES settlement_schedule_preferences(id),revision bigint NOT NULL
);
CREATE TABLE settlement_plans (
 id bigserial PRIMARY KEY,revision bigint NOT NULL DEFAULT 1 CHECK(revision>0),enabled boolean NOT NULL DEFAULT false,
 last_authorization_id bigint,created_at timestamptz NOT NULL DEFAULT transaction_timestamp()
);
CREATE TABLE settlement_plan_versions (
 id bigserial PRIMARY KEY,plan_id bigint NOT NULL REFERENCES settlement_plans(id),revision bigint NOT NULL CHECK(revision>0),
 name text NOT NULL CHECK(length(name) BETWEEN 1 AND 100),mode text NOT NULL CHECK(mode='generate_only'),schedule jsonb NOT NULL,
 merchant_ids bigint[] NOT NULL CHECK(cardinality(merchant_ids) BETWEEN 1 AND 100),actor_id bigint NOT NULL CHECK(actor_id>0),actor_name text NOT NULL,
 proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32),created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),UNIQUE(plan_id,revision)
);
CREATE TABLE settlement_plan_authorizations (
 id bigserial PRIMARY KEY,plan_id bigint NOT NULL REFERENCES settlement_plans(id),plan_revision bigint NOT NULL,
 expected_authorization_id bigint REFERENCES settlement_plan_authorizations(id),valid_from timestamptz NOT NULL,expires_at timestamptz NOT NULL,
 max_item_gross bigint NOT NULL CHECK(max_item_gross BETWEEN 1 AND 100000000000),max_run_gross bigint NOT NULL CHECK(max_run_gross BETWEEN 1 AND 100000000000),
 max_day_gross bigint NOT NULL CHECK(max_day_gross BETWEEN 1 AND 100000000000),max_day_items bigint NOT NULL CHECK(max_day_items BETWEEN 1 AND 100),
 scope_snapshot jsonb NOT NULL,actor_id bigint NOT NULL CHECK(actor_id>0),actor_name text NOT NULL,
 proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32),created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
 FOREIGN KEY(plan_id,plan_revision) REFERENCES settlement_plan_versions(plan_id,revision),CHECK(expires_at>valid_from AND expires_at<=valid_from+interval '31 days'),
 CHECK(max_item_gross<=max_run_gross AND max_run_gross<=max_day_gross)
);
ALTER TABLE settlement_plans ADD FOREIGN KEY(last_authorization_id) REFERENCES settlement_plan_authorizations(id);
CREATE TABLE settlement_plan_revocations (
 authorization_id bigint PRIMARY KEY REFERENCES settlement_plan_authorizations(id),plan_id bigint NOT NULL REFERENCES settlement_plans(id),
 actor_id bigint NOT NULL CHECK(actor_id>0),actor_name text NOT NULL,proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32),created_at timestamptz NOT NULL DEFAULT transaction_timestamp()
);
CREATE TABLE settlement_plan_runs (
 id bigserial PRIMARY KEY,plan_id bigint NOT NULL REFERENCES settlement_plans(id),plan_revision bigint NOT NULL,authorization_id bigint NOT NULL REFERENCES settlement_plan_authorizations(id),
 slot_no bigint NOT NULL CHECK(slot_no>=0),business_date date NOT NULL,scheduled_at timestamptz NOT NULL,
 proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32),created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
 FOREIGN KEY(plan_id,plan_revision) REFERENCES settlement_plan_versions(plan_id,revision),UNIQUE(plan_id,slot_no)
);
CREATE TABLE settlement_plan_items (
 id bigserial PRIMARY KEY,run_id bigint NOT NULL REFERENCES settlement_plan_runs(id),merchant_id bigint NOT NULL REFERENCES merchants(id),
 state text NOT NULL CHECK(state IN ('created','skipped','blocked')),reason text NOT NULL,
 request_id bigint UNIQUE REFERENCES ledger_requests(id),recipient_config_id bigint REFERENCES settlement_recipient_configs(id),
 ledger_available bigint NOT NULL CHECK(ledger_available>=0),eligible bigint NOT NULL CHECK(eligible>=0 AND eligible<=ledger_available),retained bigint NOT NULL CHECK(retained>=0),
 gross bigint NOT NULL CHECK(gross>=0),fee bigint NOT NULL CHECK(fee>=0),net bigint NOT NULL CHECK(net>=0),
 proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32),created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),UNIQUE(run_id,merchant_id),
 CHECK(gross=fee+net),CHECK((state='created')=(request_id IS NOT NULL)),CHECK(state='created' OR gross=0),CHECK(state<>'created' OR net>0)
);
CREATE TABLE settlement_scheduled_intents (
 merchant_id bigint NOT NULL REFERENCES merchants(id),business_date date NOT NULL,item_id bigint NOT NULL UNIQUE REFERENCES settlement_plan_items(id),
 request_id bigint NOT NULL UNIQUE REFERENCES ledger_requests(id),PRIMARY KEY(merchant_id,business_date)
);
CREATE TABLE settlement_plan_budget_uses (
 item_id bigint PRIMARY KEY REFERENCES settlement_plan_items(id),authorization_id bigint NOT NULL REFERENCES settlement_plan_authorizations(id),plan_id bigint NOT NULL REFERENCES settlement_plans(id),
 business_date date NOT NULL,gross bigint NOT NULL CHECK(gross>0),fee bigint NOT NULL CHECK(fee>=0),net bigint NOT NULL CHECK(net>0),CHECK(gross=fee+net)
);
ALTER TABLE ledger_requests ADD COLUMN settlement_plan_item_id bigint UNIQUE REFERENCES settlement_plan_items(id) DEFERRABLE INITIALLY DEFERRED;

CREATE TRIGGER settlement_preference_immutable BEFORE UPDATE OR DELETE ON settlement_schedule_preferences FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE TRIGGER settlement_plan_version_immutable BEFORE UPDATE OR DELETE ON settlement_plan_versions FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE TRIGGER settlement_plan_authorization_immutable BEFORE UPDATE OR DELETE ON settlement_plan_authorizations FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE TRIGGER settlement_plan_revocation_immutable BEFORE UPDATE OR DELETE ON settlement_plan_revocations FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE TRIGGER settlement_plan_run_immutable BEFORE UPDATE OR DELETE ON settlement_plan_runs FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE TRIGGER settlement_plan_item_immutable BEFORE UPDATE OR DELETE ON settlement_plan_items FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE TRIGGER settlement_plan_intent_immutable BEFORE UPDATE OR DELETE ON settlement_scheduled_intents FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE TRIGGER settlement_plan_budget_immutable BEFORE UPDATE OR DELETE ON settlement_plan_budget_uses FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();

CREATE FUNCTION settlement_preference_head_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' OR NOT EXISTS(SELECT 1 FROM settlement_schedule_preferences p WHERE p.id=NEW.preference_id AND (p.merchant_id,p.revision)=(NEW.merchant_id,NEW.revision))
 OR TG_OP='INSERT' AND NEW.revision<>1 OR TG_OP='UPDATE' AND (NEW.merchant_id<>OLD.merchant_id OR NEW.revision<>OLD.revision+1) THEN RAISE EXCEPTION 'invalid settlement preference revision'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER settlement_preference_head BEFORE INSERT OR UPDATE OR DELETE ON settlement_schedule_preference_heads FOR EACH ROW EXECUTE FUNCTION settlement_preference_head_guard();
CREATE FUNCTION settlement_plan_head_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'immutable settlement plan identity'; END IF;
 IF TG_OP='INSERT' AND (NEW.revision<>1 OR NEW.enabled OR NEW.last_authorization_id IS NOT NULL) OR TG_OP='UPDATE' AND (NEW.id<>OLD.id OR NEW.created_at<>OLD.created_at OR NEW.revision NOT IN (OLD.revision,OLD.revision+1) OR NEW.revision<>OLD.revision AND NEW.enabled) THEN RAISE EXCEPTION 'invalid settlement plan head transition'; END IF;
 IF TG_OP='UPDATE' THEN
  IF NEW.last_authorization_id IS DISTINCT FROM OLD.last_authorization_id AND NOT EXISTS(
   SELECT 1 FROM settlement_plan_authorizations a WHERE a.id=NEW.last_authorization_id AND a.plan_id=NEW.id AND a.plan_revision=NEW.revision
   AND a.expected_authorization_id IS NOT DISTINCT FROM OLD.last_authorization_id AND a.created_at=transaction_timestamp()
   AND a.id=(SELECT max(x.id) FROM settlement_plan_authorizations x WHERE x.plan_id=NEW.id)
   AND EXISTS(SELECT 1 FROM backoffice_audit b WHERE b.resource='settlement-plan-authorizations' AND b.resource_id=a.id AND b.operation='authorize'
    AND (b.actor_role,b.actor_id,b.actor_name)=('admin',a.actor_id,a.actor_name) AND b.created_at=transaction_timestamp())
  ) THEN RAISE EXCEPTION 'settlement plan requires a fresh chained authority'; END IF;
  IF NEW.enabled AND NOT OLD.enabled AND NEW.last_authorization_id IS NOT DISTINCT FROM OLD.last_authorization_id THEN RAISE EXCEPTION 'old settlement authority cannot be rearmed'; END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER settlement_plan_head BEFORE INSERT OR UPDATE OR DELETE ON settlement_plans FOR EACH ROW EXECUTE FUNCTION settlement_plan_head_guard();
CREATE FUNCTION settlement_plan_config_complete() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE pid bigint; eid bigint; role text; aid bigint; aname text; v_resource text; v_op text;
BEGIN
 IF TG_TABLE_NAME='settlement_plans' THEN pid:=NEW.id;
 ELSIF TG_TABLE_NAME='settlement_plan_versions' THEN
  pid:=NEW.plan_id;eid:=NEW.id;role:='admin';aid:=NEW.actor_id;aname:=NEW.actor_name;v_resource:='settlement-plan-versions';v_op:='define';
 ELSIF TG_TABLE_NAME='settlement_plan_authorizations' THEN
  pid:=NEW.plan_id;eid:=NEW.id;role:='admin';aid:=NEW.actor_id;aname:=NEW.actor_name;v_resource:='settlement-plan-authorizations';v_op:='authorize';
 ELSIF TG_TABLE_NAME='settlement_plan_revocations' THEN
  pid:=NEW.plan_id;eid:=NEW.authorization_id;role:='admin';aid:=NEW.actor_id;aname:=NEW.actor_name;v_resource:='settlement-plan-authorizations';v_op:='revoke';
 ELSE
  eid:=NEW.id;role:='merchant';aid:=NEW.merchant_id;aname:=NEW.actor_name;v_resource:='settlement-schedule-preferences';v_op:='configure';
  IF NEW.recipient_config_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM settlement_recipient_configs c WHERE c.id=NEW.recipient_config_id AND c.merchant_id=NEW.merchant_id) THEN RAISE EXCEPTION 'foreign scheduled recipient'; END IF;
  IF NOT EXISTS(SELECT 1 FROM settlement_schedule_preference_heads h WHERE h.merchant_id=NEW.merchant_id AND h.revision>0) THEN RAISE EXCEPTION 'orphan settlement preference'; END IF;
 END IF;
 IF pid IS NOT NULL AND NOT EXISTS(SELECT 1 FROM settlement_plans p JOIN settlement_plan_versions v ON (v.plan_id,v.revision)=(p.id,p.revision) WHERE p.id=pid
  AND p.revision=(SELECT count(*) FROM settlement_plan_versions x WHERE x.plan_id=p.id)
  AND p.last_authorization_id IS NOT DISTINCT FROM (SELECT max(x.id) FROM settlement_plan_authorizations x WHERE x.plan_id=p.id)
  AND (NOT p.enabled OR EXISTS(SELECT 1 FROM settlement_plan_authorizations a WHERE a.id=p.last_authorization_id AND (a.plan_id,a.plan_revision)=(p.id,p.revision) AND NOT EXISTS(SELECT 1 FROM settlement_plan_revocations r WHERE r.authorization_id=a.id)))) THEN RAISE EXCEPTION 'incomplete settlement plan authorization'; END IF;
 IF eid IS NOT NULL AND NOT EXISTS(SELECT 1 FROM backoffice_audit a WHERE a.resource=v_resource AND a.resource_id=eid AND a.operation=v_op AND (a.actor_role,a.actor_id,a.actor_name)=(role,aid,aname)) THEN RAISE EXCEPTION 'missing settlement scheduling actor audit'; END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER settlement_plan_complete AFTER INSERT OR UPDATE ON settlement_plans DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION settlement_plan_config_complete();
CREATE CONSTRAINT TRIGGER settlement_plan_version_complete AFTER INSERT ON settlement_plan_versions DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION settlement_plan_config_complete();
CREATE CONSTRAINT TRIGGER settlement_plan_authorization_complete AFTER INSERT ON settlement_plan_authorizations DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION settlement_plan_config_complete();
CREATE CONSTRAINT TRIGGER settlement_plan_revocation_complete AFTER INSERT ON settlement_plan_revocations DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION settlement_plan_config_complete();
CREATE CONSTRAINT TRIGGER settlement_preference_complete AFTER INSERT ON settlement_schedule_preferences DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION settlement_plan_config_complete();

CREATE VIEW settlement_plan_item_invariants AS
SELECT i.id,
 i.merchant_id=ANY(v.merchant_ids) AND (a.plan_id,a.plan_revision)=(r.plan_id,r.plan_revision)
 AND (i.state<>'created' OR (
  q.kind='settlements' AND q.merchant_id=i.merchant_id AND q.status IN ('pending','approved','rejected') AND q.amount=i.gross AND q.settlement_plan_item_id=i.id
  AND (t.gross_amount,t.fee_amount,t.net_amount)=(i.gross,i.fee,i.net)
  AND sr.config_id=i.recipient_config_id AND EXISTS(SELECT 1 FROM settlement_funding_invariants f WHERE f.request_id=q.id AND f.valid IS TRUE)
  AND EXISTS(SELECT 1 FROM settlement_scheduled_intents x WHERE (x.merchant_id,x.business_date,x.item_id,x.request_id)=(i.merchant_id,r.business_date,i.id,q.id))
  AND EXISTS(SELECT 1 FROM settlement_plan_budget_uses b WHERE (b.item_id,b.authorization_id,b.plan_id,b.business_date,b.gross,b.fee,b.net)=(i.id,r.authorization_id,r.plan_id,r.business_date,i.gross,i.fee,i.net))
  AND i.gross<=a.max_item_gross
  AND (SELECT COALESCE(sum(x.gross),0) FROM settlement_plan_items x WHERE x.run_id=r.id AND x.id<=i.id)<=a.max_run_gross
  AND (SELECT COALESCE(sum(b.gross),0) FROM settlement_plan_budget_uses b WHERE b.plan_id=r.plan_id AND b.business_date=r.business_date AND b.item_id<=i.id)<=a.max_day_gross
  AND (SELECT count(*) FROM settlement_plan_budget_uses b WHERE b.plan_id=r.plan_id AND b.business_date=r.business_date AND b.item_id<=i.id)<=a.max_day_items
 ))
 AND EXISTS(SELECT 1 FROM backoffice_audit b WHERE b.resource='settlement-plan-items' AND b.resource_id=i.id AND b.operation='generate' AND b.actor_role='system' AND b.actor_id=0 AND b.actor_name='settlement-scheduler') AS valid
FROM settlement_plan_items i JOIN settlement_plan_runs r ON r.id=i.run_id JOIN settlement_plan_versions v ON (v.plan_id,v.revision)=(r.plan_id,r.plan_revision)
JOIN settlement_plan_authorizations a ON a.id=r.authorization_id LEFT JOIN ledger_requests q ON q.id=i.request_id
LEFT JOIN settlement_request_terms t ON t.request_id=q.id LEFT JOIN settlement_recipients sr ON sr.request_id=q.id;
CREATE FUNCTION settlement_plan_item_complete() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE iid bigint;
BEGIN
 IF TG_TABLE_NAME='ledger_requests' THEN
  SELECT settlement_plan_item_id INTO iid FROM ledger_requests WHERE id=NEW.id;
  IF iid IS NULL THEN
   IF EXISTS(SELECT 1 FROM ledger_request_events e WHERE e.request_id=NEW.id AND e.actor='system:settlement-scheduler') THEN RAISE EXCEPTION 'missing scheduled request source'; END IF;
   RETURN NULL;
  END IF;
  IF NOT EXISTS(SELECT 1 FROM settlement_plan_items i WHERE i.id=iid AND i.state='created' AND i.request_id=NEW.id) THEN RAISE EXCEPTION 'foreign scheduled request source'; END IF;
 ELSIF TG_TABLE_NAME='ledger_request_events' THEN
  IF NEW.actor<>'system:settlement-scheduler' THEN RETURN NULL; END IF;
  SELECT settlement_plan_item_id INTO iid FROM ledger_requests WHERE id=NEW.request_id;
  IF iid IS NULL THEN RAISE EXCEPTION 'missing scheduled request source'; END IF;
  IF NOT EXISTS(SELECT 1 FROM settlement_plan_items i WHERE i.id=iid AND i.state='created' AND i.request_id=NEW.request_id) THEN RAISE EXCEPTION 'foreign scheduled request source'; END IF;
 ELSIF TG_TABLE_NAME='settlement_plan_items' THEN iid:=NEW.id; ELSE iid:=NEW.item_id; END IF;
 IF NOT EXISTS(SELECT 1 FROM settlement_plan_item_invariants WHERE id=iid AND valid IS TRUE) THEN RAISE EXCEPTION 'incomplete scheduled settlement original funding'; END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER settlement_plan_item_complete AFTER INSERT ON settlement_plan_items DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION settlement_plan_item_complete();
CREATE CONSTRAINT TRIGGER settlement_plan_intent_complete AFTER INSERT ON settlement_scheduled_intents DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION settlement_plan_item_complete();
CREATE CONSTRAINT TRIGGER settlement_plan_budget_complete AFTER INSERT ON settlement_plan_budget_uses DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION settlement_plan_item_complete();
CREATE CONSTRAINT TRIGGER settlement_plan_request_complete AFTER INSERT OR UPDATE ON ledger_requests DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION settlement_plan_item_complete();
CREATE CONSTRAINT TRIGGER settlement_plan_request_event_complete AFTER INSERT ON ledger_request_events DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION settlement_plan_item_complete();
CREATE FUNCTION settlement_plan_request_birth() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.settlement_plan_item_id IS NOT DISTINCT FROM OLD.settlement_plan_item_id THEN RETURN NEW; END IF;
 IF OLD.settlement_plan_item_id IS NOT NULL OR NEW.settlement_plan_item_id IS NULL OR NEW.kind<>'settlements' OR NEW.status<>'pending' OR NEW.created_at<transaction_timestamp()
 OR NOT EXISTS(SELECT 1 FROM journals j WHERE j.event_key='request:'||NEW.id::text||':reserve' AND j.created_at=transaction_timestamp())
 OR NOT EXISTS(SELECT 1 FROM ledger_request_events e WHERE e.request_id=NEW.id AND e.actor='system:settlement-scheduler' AND e.status='pending')
 THEN RAISE EXCEPTION 'scheduled request must originate in its reserve transaction'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER settlement_plan_request_birth BEFORE UPDATE OF settlement_plan_item_id ON ledger_requests FOR EACH ROW EXECUTE FUNCTION settlement_plan_request_birth();

CREATE VIEW settlement_plan_head_invariants AS
SELECT p.id,
 EXISTS(SELECT 1 FROM settlement_plan_versions v WHERE v.plan_id=p.id AND v.revision=p.revision)
 AND p.revision=(SELECT count(*) FROM settlement_plan_versions v WHERE v.plan_id=p.id)
 AND p.revision=(SELECT max(v.revision) FROM settlement_plan_versions v WHERE v.plan_id=p.id)
 AND p.last_authorization_id IS NOT DISTINCT FROM (SELECT max(a.id) FROM settlement_plan_authorizations a WHERE a.plan_id=p.id)
 AND (NOT p.enabled OR EXISTS(SELECT 1 FROM settlement_plan_authorizations a WHERE a.id=p.last_authorization_id
  AND (a.plan_id,a.plan_revision)=(p.id,p.revision) AND NOT EXISTS(SELECT 1 FROM settlement_plan_revocations r WHERE r.authorization_id=a.id))) AS valid
FROM settlement_plans p;

CREATE VIEW settlement_plan_definition_invariants AS
SELECT v.id,
 EXISTS(SELECT 1 FROM settlement_plans p WHERE p.id=v.plan_id AND p.revision>=v.revision)
 AND v.revision=(SELECT count(*) FROM settlement_plan_versions x WHERE x.plan_id=v.plan_id AND x.revision<=v.revision)
 AND NOT EXISTS(SELECT 1 FROM unnest(v.merchant_ids) mid WHERE NOT EXISTS(SELECT 1 FROM merchants m WHERE m.id=mid))
 AND EXISTS(SELECT 1 FROM backoffice_audit a WHERE a.resource='settlement-plan-versions' AND a.resource_id=v.id AND a.operation='define'
  AND (a.actor_role,a.actor_id,a.actor_name)=('admin',v.actor_id,v.actor_name)) AS valid
FROM settlement_plan_versions v;

CREATE VIEW settlement_plan_preference_invariants AS
SELECT p.id,
 EXISTS(SELECT 1 FROM settlement_schedule_preference_heads h JOIN settlement_schedule_preferences latest ON latest.id=h.preference_id
  WHERE h.merchant_id=p.merchant_id AND (latest.merchant_id,latest.revision)=(h.merchant_id,h.revision)
  AND h.revision=(SELECT max(x.revision) FROM settlement_schedule_preferences x WHERE x.merchant_id=p.merchant_id))
 AND p.revision=(SELECT count(*) FROM settlement_schedule_preferences x WHERE x.merchant_id=p.merchant_id AND x.revision<=p.revision)
 AND (p.recipient_config_id IS NULL OR EXISTS(SELECT 1 FROM settlement_recipient_configs c WHERE c.id=p.recipient_config_id AND c.merchant_id=p.merchant_id))
 AND EXISTS(SELECT 1 FROM backoffice_audit a WHERE a.resource='settlement-schedule-preferences' AND a.resource_id=p.id AND a.operation='configure'
  AND (a.actor_role,a.actor_id,a.actor_name)=('merchant',p.merchant_id,p.actor_name)) AS valid
FROM settlement_schedule_preferences p;

CREATE VIEW settlement_plan_authorization_invariants AS
SELECT a.id,
 EXISTS(SELECT 1 FROM settlement_plan_versions v WHERE (v.plan_id,v.revision)=(a.plan_id,a.plan_revision)
  AND v.id::text=a.scope_snapshot->>'definition_id' AND encode(v.proof_hash,'base64')=a.scope_snapshot->>'definition_hash')
 AND a.expected_authorization_id IS NOT DISTINCT FROM (SELECT max(x.id) FROM settlement_plan_authorizations x WHERE x.plan_id=a.plan_id AND x.id<a.id)
 AND EXISTS(SELECT 1 FROM backoffice_audit b WHERE b.resource='settlement-plan-authorizations' AND b.resource_id=a.id AND b.operation='authorize'
  AND (b.actor_role,b.actor_id,b.actor_name)=('admin',a.actor_id,a.actor_name))
 AND NOT EXISTS(SELECT 1 FROM settlement_plan_revocations r WHERE r.authorization_id=a.id AND (r.plan_id<>a.plan_id OR NOT EXISTS(
  SELECT 1 FROM backoffice_audit b WHERE b.resource='settlement-plan-authorizations' AND b.resource_id=a.id AND b.operation='revoke'
  AND (b.actor_role,b.actor_id,b.actor_name)=('admin',r.actor_id,r.actor_name)))) AS valid
FROM settlement_plan_authorizations a;

CREATE VIEW settlement_plan_run_invariants AS
SELECT r.id,
 EXISTS(SELECT 1 FROM settlement_plan_authorizations a WHERE a.id=r.authorization_id AND (a.plan_id,a.plan_revision)=(r.plan_id,r.plan_revision))
 AND (r.scheduled_at AT TIME ZONE 'Asia/Shanghai')::date=r.business_date
 AND EXISTS(SELECT 1 FROM backoffice_audit b WHERE b.resource='settlement-plan-runs' AND b.resource_id=r.id AND b.operation='schedule'
  AND b.actor_role='system' AND b.actor_id=0 AND b.actor_name='settlement-scheduler') AS valid
FROM settlement_plan_runs r;

CREATE FUNCTION settlement_plan_run_complete() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM settlement_plan_run_invariants v WHERE v.id=NEW.id AND v.valid IS TRUE)
 OR NOT EXISTS(SELECT 1 FROM jobs j WHERE j.event_key='settlement-plan-run:'||NEW.id::text AND j.kind='settlement-plan.generate' AND j.payload=jsonb_build_object('run_id',NEW.id::text))
 THEN RAISE EXCEPTION 'incomplete original settlement generation run'; END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER settlement_plan_run_complete AFTER INSERT ON settlement_plan_runs DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION settlement_plan_run_complete();
