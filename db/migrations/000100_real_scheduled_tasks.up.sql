-- No legacy cron-tasks metadata is consumed or enabled by this migration.
CREATE TABLE scheduled_tasks (
 task_key text PRIMARY KEY CHECK(task_key ~ '^[a-z][a-z0-9_]{0,63}$'),
 enabled boolean NOT NULL DEFAULT false,
 schedule jsonb NOT NULL CHECK(jsonb_typeof(schedule)='object'),
 params jsonb NOT NULL CHECK(jsonb_typeof(params)='object'),
 revision bigint NOT NULL CHECK(revision>0),
 generation bigint NOT NULL CHECK(generation>0),
 saved_at timestamptz NOT NULL,
 last_run_id bigint,
 last_manual_run_id bigint,
 auto_slot bigint NOT NULL DEFAULT -1 CHECK(auto_slot>=-1),
 proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32)
);
CREATE TRIGGER scheduled_task_no_delete BEFORE DELETE ON scheduled_tasks FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE TABLE scheduled_task_runs (
 id bigserial PRIMARY KEY,
 task_key text NOT NULL REFERENCES scheduled_tasks(task_key),
 generation bigint NOT NULL CHECK(generation>0),
 trigger_kind text NOT NULL CHECK(trigger_kind IN ('manual','schedule')),
 slot_no bigint NOT NULL CHECK(slot_no>=-1),
 scheduled_for timestamptz NOT NULL,
 facts jsonb NOT NULL CHECK(jsonb_typeof(facts)='object'),
 source_hash bytea NOT NULL CHECK(octet_length(source_hash)=32),
 created_at timestamptz NOT NULL,
 state text NOT NULL DEFAULT 'pending' CHECK(state IN ('pending','running','completed')),
 lease_token bytea,
 lease_epoch bigint NOT NULL DEFAULT 0 CHECK(lease_epoch>=0),
 lease_until timestamptz,
 started_at timestamptz,
 progress_index bigint NOT NULL DEFAULT 0 CHECK(progress_index>=0),
 progress_counts jsonb NOT NULL CHECK(jsonb_typeof(progress_counts)='object'),
 progress_hash bytea NOT NULL CHECK(octet_length(progress_hash)=32),
 UNIQUE(task_key,id),
 CHECK((trigger_kind='manual')=(slot_no=-1)),
 CHECK((state='running')=(lease_token IS NOT NULL AND lease_until IS NOT NULL))
);
CREATE UNIQUE INDEX scheduled_task_auto_slot ON scheduled_task_runs(task_key,generation,slot_no) WHERE trigger_kind='schedule';
ALTER TABLE scheduled_tasks ADD FOREIGN KEY(task_key,last_run_id) REFERENCES scheduled_task_runs(task_key,id);
ALTER TABLE scheduled_tasks ADD FOREIGN KEY(task_key,last_manual_run_id) REFERENCES scheduled_task_runs(task_key,id);
CREATE TABLE scheduled_task_results (
 run_id bigint PRIMARY KEY REFERENCES scheduled_task_runs(id),
 status text NOT NULL CHECK(status IN ('success','failed','cancelled')),
 counts jsonb NOT NULL CHECK(jsonb_typeof(counts)='object'),
 error_code text NOT NULL CHECK(error_code IN ('','business_state_changed','execution_failed','config_changed')),
 completed_at timestamptz NOT NULL,
 proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32)
);
CREATE TRIGGER scheduled_task_result_immutable BEFORE UPDATE OR DELETE ON scheduled_task_results FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE FUNCTION guard_scheduled_task_intent() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' OR (NEW.id,NEW.task_key,NEW.generation,NEW.trigger_kind,NEW.slot_no,NEW.scheduled_for,NEW.facts,NEW.source_hash,NEW.created_at)
  IS DISTINCT FROM (OLD.id,OLD.task_key,OLD.generation,OLD.trigger_kind,OLD.slot_no,OLD.scheduled_for,OLD.facts,OLD.source_hash,OLD.created_at) THEN
  RAISE EXCEPTION 'immutable scheduled task intent';
 END IF;
 IF OLD.state='completed' OR NEW.lease_epoch<OLD.lease_epoch OR NEW.progress_index<OLD.progress_index OR NEW.started_at IS DISTINCT FROM OLD.started_at AND OLD.started_at IS NOT NULL THEN
  RAISE EXCEPTION 'invalid scheduled task lease transition';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER scheduled_task_intent_immutable BEFORE UPDATE OR DELETE ON scheduled_task_runs FOR EACH ROW EXECUTE FUNCTION guard_scheduled_task_intent();

-- These rows admit an existing original job attempt; they never enqueue a new
-- payment/notification or reset a dead job. Consumers retain their original lease.
CREATE TABLE scheduled_job_admissions (
 id bigserial PRIMARY KEY,
 run_id bigint NOT NULL REFERENCES scheduled_task_runs(id),
 job_id bigint NOT NULL REFERENCES jobs(id),
 next_attempt integer NOT NULL CHECK(next_attempt>0),
 facts jsonb NOT NULL CHECK(jsonb_typeof(facts)='object'),
 valid_until timestamptz NOT NULL,
 proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32),
 UNIQUE(run_id,job_id,next_attempt)
);
CREATE TRIGGER scheduled_job_admission_immutable BEFORE UPDATE OR DELETE ON scheduled_job_admissions FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE TABLE scheduled_job_claims (
 admission_id bigint PRIMARY KEY REFERENCES scheduled_job_admissions(id),
 job_id bigint NOT NULL REFERENCES jobs(id),
 attempt integer NOT NULL CHECK(attempt>0),
 token_hash bytea NOT NULL CHECK(octet_length(token_hash)=32),
 claimed_at timestamptz NOT NULL,
 proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32),
 UNIQUE(job_id,token_hash)
);
CREATE TRIGGER scheduled_job_claim_immutable BEFORE UPDATE OR DELETE ON scheduled_job_claims FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();

CREATE TABLE scheduled_notification_failures (
 id bigserial PRIMARY KEY,
 job_id bigint NOT NULL REFERENCES jobs(id),
 order_id bigint NOT NULL REFERENCES orders(id),
 merchant_id bigint NOT NULL REFERENCES merchants(id),
 attempt integer NOT NULL CHECK(attempt>0),
 token_hash bytea NOT NULL CHECK(octet_length(token_hash)=32),
 delivery_kind text NOT NULL CHECK(delivery_kind IN('ack','transport','read')),
 previous_id bigint REFERENCES scheduled_notification_failures(id),
 previous_hash bytea,
 failure_count integer NOT NULL CHECK(failure_count BETWEEN 1 AND 5),
 observed_at timestamptz NOT NULL,
 proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32),
 UNIQUE(job_id,attempt,token_hash),
 CHECK((previous_id IS NULL)=(previous_hash IS NULL)),
 CHECK(previous_hash IS NULL OR octet_length(previous_hash)=32)
);
CREATE TRIGGER scheduled_notification_failure_immutable BEFORE UPDATE OR DELETE ON scheduled_notification_failures FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
ALTER TABLE jobs ADD COLUMN scheduled_notification_failure_head bigint REFERENCES scheduled_notification_failures(id);
CREATE FUNCTION guard_scheduled_notification_failure_head()RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.scheduled_notification_failure_head IS DISTINCT FROM OLD.scheduled_notification_failure_head THEN
  IF NEW.scheduled_notification_failure_head IS NULL OR NOT EXISTS(
   SELECT 1 FROM scheduled_notification_failures f WHERE f.id=NEW.scheduled_notification_failure_head AND f.job_id=NEW.id
   AND f.attempt=NEW.attempts AND f.previous_id IS NOT DISTINCT FROM OLD.scheduled_notification_failure_head
  ) THEN RAISE EXCEPTION 'invalid original notification failure head';END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER scheduled_notification_failure_head_guard BEFORE UPDATE OF scheduled_notification_failure_head ON jobs FOR EACH ROW EXECUTE FUNCTION guard_scheduled_notification_failure_head();
CREATE TABLE scheduled_notification_stops (
 id bigserial PRIMARY KEY,
 merchant_id bigint NOT NULL REFERENCES merchants(id),
 original_revision bigint NOT NULL CHECK(original_revision>0),
 trigger_failure_id bigint NOT NULL REFERENCES scheduled_notification_failures(id),
 risk_record_id bigint NOT NULL REFERENCES backoffice_records(id),
 facts jsonb NOT NULL CHECK(jsonb_typeof(facts)='object'),
 created_at timestamptz NOT NULL,
 proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32),
 UNIQUE(merchant_id,original_revision)
);
CREATE TRIGGER scheduled_notification_stop_immutable BEFORE UPDATE OR DELETE ON scheduled_notification_stops FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE TABLE scheduled_notification_mail_claims (
 stop_id bigint PRIMARY KEY REFERENCES scheduled_notification_stops(id),
 job_id bigint NOT NULL UNIQUE REFERENCES jobs(id),
 stop_hash bytea NOT NULL CHECK(octet_length(stop_hash)=32),
 recipient_hash bytea NOT NULL CHECK(octet_length(recipient_hash)=32),
 account_hash bytea NOT NULL CHECK(octet_length(account_hash)=32),
 lease_hash bytea NOT NULL CHECK(octet_length(lease_hash)=32),
 started_at timestamptz NOT NULL,
 proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32)
);
CREATE TRIGGER scheduled_notification_mail_claim_immutable BEFORE UPDATE OR DELETE ON scheduled_notification_mail_claims FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
ALTER TABLE jobs ADD COLUMN scheduled_notification_mail_head bigint REFERENCES scheduled_notification_mail_claims(stop_id);
CREATE FUNCTION guard_scheduled_notification_mail_head()RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.scheduled_notification_mail_head IS DISTINCT FROM OLD.scheduled_notification_mail_head THEN
  IF OLD.scheduled_notification_mail_head IS NOT NULL OR NEW.scheduled_notification_mail_head IS NULL OR NOT EXISTS(
   SELECT 1 FROM scheduled_notification_mail_claims c WHERE c.stop_id=NEW.scheduled_notification_mail_head AND c.job_id=NEW.id
  ) THEN RAISE EXCEPTION 'immutable original notification mail head';END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER scheduled_notification_mail_head_guard BEFORE UPDATE OF scheduled_notification_mail_head ON jobs FOR EACH ROW EXECUTE FUNCTION guard_scheduled_notification_mail_head();
CREATE TABLE scheduled_notification_mail_results (
 stop_id bigint PRIMARY KEY REFERENCES scheduled_notification_mail_claims(stop_id),
 state text NOT NULL CHECK(state IN('accepted','unknown')),
 completed_at timestamptz NOT NULL,
 proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32)
);
CREATE TRIGGER scheduled_notification_mail_result_immutable BEFORE UPDATE OR DELETE ON scheduled_notification_mail_results FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE INDEX scheduled_task_commit_audit ON backoffice_audit((changes->>'task'),id)
 WHERE resource='scheduled-tasks' AND resource_id=0 AND actor_role='admin' AND actor_id>0 AND operation IN('configure','manual-run');
CREATE INDEX scheduled_task_run_origin_head ON backoffice_audit((changes->>'task'),resource_id DESC)
 WHERE resource='scheduled-task-runs' AND operation='intent' AND actor_role='system' AND actor_id=0;
CREATE INDEX scheduled_task_run_manual_head ON scheduled_task_runs(task_key,id DESC)WHERE trigger_kind='manual';
CREATE INDEX scheduled_task_execution_audit ON backoffice_audit(resource_id,operation,id DESC)
 WHERE resource='scheduled-task-execution' AND actor_role='system' AND actor_id=0;
CREATE INDEX scheduled_notification_failure_head_audit ON backoffice_audit((changes->>'job_id'),resource_id DESC)
 WHERE resource='scheduled-notification-failures' AND operation='observed' AND actor_role='system' AND actor_id=0;
