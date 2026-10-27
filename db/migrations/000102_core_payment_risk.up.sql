-- The old settings/cron metadata is neither imported nor enabled.
CREATE TABLE core_payment_risk_policy (
 id smallint PRIMARY KEY CHECK(id=1),
 revision bigint NOT NULL CHECK(revision>0),
 settings jsonb NOT NULL CHECK(jsonb_typeof(settings)='object'),
 saved_at timestamptz NOT NULL,
 proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32)
);
CREATE TRIGGER core_payment_risk_policy_no_delete BEFORE DELETE ON core_payment_risk_policy FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();

CREATE TABLE core_risk_origins (
 order_id bigint PRIMARY KEY REFERENCES orders(id),
 attempt_id bigint NOT NULL UNIQUE REFERENCES provider_attempts(id),
 ips text[] NOT NULL,
 admitted_at timestamptz NOT NULL,
 facts jsonb NOT NULL CHECK(jsonb_typeof(facts)='object'),
 proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32)
);
CREATE INDEX core_risk_origin_ips ON core_risk_origins USING gin(ips);
CREATE TRIGGER core_risk_origin_immutable BEFORE UPDATE OR DELETE ON core_risk_origins FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
ALTER TABLE provider_attempts ADD COLUMN core_risk_required boolean NOT NULL DEFAULT false;
CREATE FUNCTION guard_core_risk_marker()RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.core_risk_required IS DISTINCT FROM OLD.core_risk_required THEN
  IF OLD.core_risk_required OR NOT NEW.core_risk_required OR NOT EXISTS(
   SELECT 1 FROM core_risk_origins r WHERE r.attempt_id=NEW.id AND r.facts->>'Transaction'=pg_current_xact_id()::text
  )THEN RAISE EXCEPTION 'invalid core risk origin marker';END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER core_risk_marker_guard BEFORE UPDATE OF core_risk_required ON provider_attempts FOR EACH ROW EXECUTE FUNCTION guard_core_risk_marker();
CREATE TABLE core_risk_successes (
 order_id bigint PRIMARY KEY REFERENCES core_risk_origins(order_id),
 day date NOT NULL,
 facts jsonb NOT NULL CHECK(jsonb_typeof(facts)='object'),
 proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32)
);
CREATE INDEX core_risk_success_day ON core_risk_successes(day,order_id);
CREATE TRIGGER core_risk_success_immutable BEFORE UPDATE OR DELETE ON core_risk_successes FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();

CREATE TABLE core_risk_actions (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 kind text NOT NULL CHECK(kind IN('ip','buyer','merchant','channel','subchannel')),
 subject_key text NOT NULL,
 revision bigint NOT NULL CHECK(revision>0),
 facts jsonb NOT NULL CHECK(jsonb_typeof(facts)='object'),
 proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32),
 UNIQUE(kind,subject_key,revision)
);
CREATE TRIGGER core_risk_action_immutable BEFORE UPDATE OR DELETE ON core_risk_actions FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE TABLE core_risk_check_runs (
 external_no text PRIMARY KEY,
 facts jsonb NOT NULL CHECK(jsonb_typeof(facts)='object'),
 proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32)
);
CREATE TRIGGER core_risk_check_run_immutable BEFORE UPDATE OR DELETE ON core_risk_check_runs FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE TABLE core_risk_notices (
 action_id bigint PRIMARY KEY REFERENCES core_risk_actions(id),
 job_id bigint UNIQUE REFERENCES jobs(id),
 facts jsonb NOT NULL,
 payload_cipher bytea NOT NULL,
 proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32)
);
CREATE TRIGGER core_risk_notice_immutable BEFORE UPDATE OR DELETE ON core_risk_notices FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE TABLE core_risk_notice_claims (
 action_id bigint PRIMARY KEY REFERENCES core_risk_notices(action_id),
 facts jsonb NOT NULL,
 proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32)
);
CREATE TRIGGER core_risk_notice_claim_immutable BEFORE UPDATE OR DELETE ON core_risk_notice_claims FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE TABLE core_risk_notice_results (
 action_id bigint PRIMARY KEY REFERENCES core_risk_notice_claims(action_id),
 facts jsonb NOT NULL,
 proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32)
);
CREATE TRIGGER core_risk_notice_result_immutable BEFORE UPDATE OR DELETE ON core_risk_notice_results FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE TABLE core_payment_risk_groups (
 group_id bigint PRIMARY KEY REFERENCES backoffice_records(id),
 revision bigint NOT NULL CHECK(revision>0),
 config jsonb NOT NULL CHECK(jsonb_typeof(config)='object'),
 saved_at timestamptz NOT NULL,
 proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32)
);
CREATE TRIGGER core_payment_risk_groups_no_delete BEFORE DELETE ON core_payment_risk_groups FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();

-- Windowed admission reads use existing private facts plus independent witnesses.
CREATE INDEX core_risk_origin_admitted ON core_risk_origins(admitted_at,order_id);
CREATE INDEX core_risk_audit_origin_keys ON backoffice_audit USING gin((changes->'subject_keys')) WHERE resource='core-risk-origins' AND operation='admit';
CREATE INDEX core_risk_audit_origin_time ON backoffice_audit((changes->>'admitted_at'),resource_id) WHERE resource='core-risk-origins' AND operation='admit';
CREATE INDEX core_risk_audit_success_day ON backoffice_audit((changes->>'day'),resource_id) WHERE resource='core-risk-success' AND operation='confirm';
