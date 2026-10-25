-- Pro Transfer threshold is a distinct business mode. Existing definitions
-- and all original financial source/checksum rows remain unchanged.
ALTER TABLE settlement_plan_versions DROP CONSTRAINT settlement_plan_versions_mode_check;
ALTER TABLE settlement_plan_versions ADD CONSTRAINT settlement_plan_versions_mode_check CHECK(mode IN('generate_only','balance_threshold'));
CREATE INDEX settlement_plan_original_daily_items ON settlement_plan_items(merchant_id,run_id,id) WHERE state='created';

ALTER TABLE settlement_plan_runs ADD COLUMN threshold_source_version smallint NOT NULL DEFAULT 0 CHECK(threshold_source_version IN(0,1));
ALTER TABLE settlement_plan_runs ADD COLUMN threshold_source_hash bytea;
ALTER TABLE settlement_plan_runs ADD COLUMN threshold_source_sequence bigint NOT NULL DEFAULT 0;
ALTER TABLE settlement_plan_runs ADD CONSTRAINT threshold_run_source_shape CHECK
 ((threshold_source_version=0 AND threshold_source_hash IS NULL AND threshold_source_sequence=0) OR (threshold_source_version=1 AND threshold_source_hash IS NOT NULL AND octet_length(threshold_source_hash)=32 AND threshold_source_sequence>0));
CREATE INDEX settlement_threshold_day_sequence ON settlement_plan_runs(plan_id,business_date,threshold_source_sequence DESC) WHERE threshold_source_version=1;
CREATE TABLE settlement_threshold_selections (
 run_id bigint PRIMARY KEY REFERENCES settlement_plan_runs(id),
 source_sequence bigserial NOT NULL UNIQUE,
 snapshot jsonb NOT NULL CHECK((jsonb_typeof(snapshot)='object' AND jsonb_typeof(snapshot->'candidates')='array' AND jsonb_typeof(snapshot->'selected')='array') IS TRUE),
 created_at timestamptz NOT NULL,transaction_id xid8 NOT NULL DEFAULT pg_current_xact_id(),
 proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32),
 CHECK(jsonb_array_length(snapshot->'candidates') BETWEEN 1 AND 100),CHECK(jsonb_array_length(snapshot->'selected')<=5)
);
CREATE TRIGGER settlement_threshold_selection_immutable BEFORE UPDATE OR DELETE ON settlement_threshold_selections FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();

CREATE TABLE settlement_threshold_intents (
 run_id bigint NOT NULL REFERENCES settlement_plan_runs(id),merchant_id bigint NOT NULL REFERENCES merchants(id),
 item_id bigint NOT NULL UNIQUE REFERENCES settlement_plan_items(id),request_id bigint NOT NULL UNIQUE REFERENCES ledger_requests(id),
 PRIMARY KEY(run_id,merchant_id)
);
CREATE TRIGGER settlement_threshold_intent_immutable BEFORE UPDATE OR DELETE ON settlement_threshold_intents FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();

CREATE OR REPLACE VIEW settlement_plan_item_invariants AS
SELECT i.id,
 i.merchant_id=ANY(v.merchant_ids) AND (a.plan_id,a.plan_revision)=(r.plan_id,r.plan_revision)
 AND ((v.mode='generate_only' AND r.threshold_source_version=0 AND NOT EXISTS(SELECT 1 FROM settlement_threshold_intents z WHERE z.item_id=i.id))
  OR (v.mode='balance_threshold' AND r.threshold_source_version=1 AND EXISTS(SELECT 1 FROM settlement_threshold_selections z WHERE z.run_id=r.id AND z.source_sequence=r.threshold_source_sequence AND z.proof_hash=r.threshold_source_hash)
   AND NOT EXISTS(SELECT 1 FROM settlement_scheduled_intents z WHERE z.item_id=i.id)))
 AND (i.state<>'created' OR (
  q.kind='settlements' AND q.merchant_id=i.merchant_id AND q.status IN ('pending','approved','rejected') AND q.amount=i.gross AND q.settlement_plan_item_id=i.id
  AND (t.gross_amount,t.fee_amount,t.net_amount)=(i.gross,i.fee,i.net)
  AND sr.config_id=i.recipient_config_id AND EXISTS(SELECT 1 FROM settlement_funding_invariants f WHERE f.request_id=q.id AND f.valid IS TRUE)
  AND ((v.mode='generate_only' AND EXISTS(SELECT 1 FROM settlement_scheduled_intents x WHERE (x.merchant_id,x.business_date,x.item_id,x.request_id)=(i.merchant_id,r.business_date,i.id,q.id)))
   OR (v.mode='balance_threshold' AND EXISTS(SELECT 1 FROM settlement_threshold_intents x WHERE (x.run_id,x.merchant_id,x.item_id,x.request_id)=(r.id,i.merchant_id,i.id,q.id))))
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
CREATE CONSTRAINT TRIGGER settlement_threshold_intent_complete AFTER INSERT ON settlement_threshold_intents DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION settlement_plan_item_complete();

DROP TRIGGER settlement_plan_run_immutable ON settlement_plan_runs;
CREATE FUNCTION settlement_threshold_run_source_guard() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'immutable settlement run';END IF;
 IF (to_jsonb(NEW)-ARRAY['threshold_source_version','threshold_source_hash','threshold_source_sequence']) IS DISTINCT FROM (to_jsonb(OLD)-ARRAY['threshold_source_version','threshold_source_hash','threshold_source_sequence'])
 OR OLD.threshold_source_version<>0 OR NEW.threshold_source_version<>1 OR OLD.threshold_source_hash IS NOT NULL
 OR EXISTS(SELECT 1 FROM settlement_plan_items WHERE run_id=OLD.id)
 OR NOT EXISTS(SELECT 1 FROM settlement_threshold_selections s JOIN settlement_plan_versions v ON (v.plan_id,v.revision)=(OLD.plan_id,OLD.plan_revision)
  WHERE s.run_id=OLD.id AND v.mode='balance_threshold' AND s.transaction_id=pg_current_xact_id() AND s.proof_hash=NEW.threshold_source_hash AND s.source_sequence=NEW.threshold_source_sequence)
 THEN RAISE EXCEPTION 'threshold source must originate once in its exact selection transaction';END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER settlement_plan_run_immutable BEFORE UPDATE OR DELETE ON settlement_plan_runs FOR EACH ROW EXECUTE FUNCTION settlement_threshold_run_source_guard();

CREATE FUNCTION settlement_threshold_selection_complete() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE rid bigint;
BEGIN
 IF TG_TABLE_NAME='settlement_plan_runs' THEN rid:=NEW.id;ELSE rid:=NEW.run_id;END IF;
 IF NOT EXISTS(SELECT 1 FROM settlement_plan_runs r JOIN settlement_plan_versions v ON (v.plan_id,v.revision)=(r.plan_id,r.plan_revision)
  WHERE r.id=rid AND (
   (r.threshold_source_version=0 AND NOT EXISTS(SELECT 1 FROM settlement_threshold_selections WHERE run_id=r.id)
    AND (v.mode<>'balance_threshold' OR NOT EXISTS(SELECT 1 FROM settlement_plan_items WHERE run_id=r.id)))
   OR (r.threshold_source_version=1 AND v.mode='balance_threshold' AND EXISTS(
    SELECT 1 FROM settlement_threshold_selections s JOIN settlement_plan_authorizations a ON a.id=r.authorization_id
    WHERE s.run_id=r.id AND s.proof_hash=r.threshold_source_hash AND s.source_sequence=r.threshold_source_sequence
    AND jsonb_array_length(s.snapshot->'candidates')=jsonb_array_length(a.scope_snapshot->'merchants')
    AND (SELECT count(*) FROM settlement_plan_items WHERE run_id=r.id)=jsonb_array_length(s.snapshot->'selected')
    AND NOT EXISTS(SELECT 1 FROM jsonb_array_elements(s.snapshot->'candidates') c WHERE NOT EXISTS(SELECT 1 FROM jsonb_array_elements(a.scope_snapshot->'merchants') m WHERE m->>'merchant_id'=c->>'merchant_id'))
    AND NOT EXISTS(SELECT 1 FROM jsonb_array_elements(a.scope_snapshot->'merchants') m WHERE NOT EXISTS(SELECT 1 FROM jsonb_array_elements(s.snapshot->'candidates') c WHERE c->>'merchant_id'=m->>'merchant_id'))
    AND (SELECT count(DISTINCT value) FROM jsonb_array_elements_text(s.snapshot->'selected'))=jsonb_array_length(s.snapshot->'selected')
    AND NOT EXISTS(SELECT 1 FROM jsonb_array_elements_text(s.snapshot->'selected') x WHERE NOT EXISTS(SELECT 1 FROM jsonb_array_elements(s.snapshot->'candidates') c WHERE c->>'merchant_id'=x.value))
    AND NOT EXISTS(SELECT 1 FROM jsonb_array_elements_text(s.snapshot->'selected') x WHERE NOT EXISTS(SELECT 1 FROM settlement_plan_items i WHERE i.run_id=r.id AND i.merchant_id::text=x.value))
    AND EXISTS(SELECT 1 FROM backoffice_audit b WHERE b.resource='settlement-threshold-selections' AND b.resource_id=r.id AND b.operation='select' AND (b.actor_role,b.actor_id,b.actor_name)=('system',0::bigint,'settlement-scheduler'))
   ))
  )) THEN RAISE EXCEPTION 'incomplete original threshold selection and run items';END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER settlement_threshold_source_complete AFTER INSERT OR UPDATE ON settlement_plan_runs DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION settlement_threshold_selection_complete();
CREATE CONSTRAINT TRIGGER settlement_threshold_selection_complete AFTER INSERT ON settlement_threshold_selections DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION settlement_threshold_selection_complete();
CREATE CONSTRAINT TRIGGER settlement_threshold_selected_items_complete AFTER INSERT ON settlement_plan_items DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION settlement_threshold_selection_complete();

-- The finite day budget cannot switch business families after first authority.
CREATE FUNCTION settlement_threshold_mode_family_guard() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF EXISTS(SELECT 1 FROM settlement_plan_authorizations WHERE plan_id=NEW.plan_id)
 AND EXISTS(SELECT 1 FROM settlement_plan_authorizations a JOIN settlement_plan_versions v ON (v.plan_id,v.revision)=(a.plan_id,a.plan_revision) WHERE a.plan_id=NEW.plan_id AND v.mode<>NEW.mode)
 THEN RAISE EXCEPTION 'authorized settlement plan business mode is immutable';END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER settlement_threshold_mode_family BEFORE INSERT ON settlement_plan_versions FOR EACH ROW EXECUTE FUNCTION settlement_threshold_mode_family_guard();
