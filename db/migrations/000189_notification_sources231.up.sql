-- No historical source is backfilled. New notification authority is created
-- only alongside the first verified complaint/payment/confirmation package.
ALTER TABLE complaints133 ADD COLUMN notification_transaction231 xid8;
ALTER TABLE wechat_transfer_packages ADD COLUMN notification_transaction231 xid8;
ALTER TABLE runtime_channel_success_events ADD COLUMN notification_transaction231 xid8;
CREATE TABLE notification_sources231 (
 id bigserial PRIMARY KEY, source_key text NOT NULL UNIQUE CHECK(length(source_key) BETWEEN 1 AND 250),
 kind text NOT NULL CHECK(kind IN('complain','wxtransfer','channellimit')),
 merchant_id bigint REFERENCES merchants(id), reference_id bigint NOT NULL CHECK(reference_id>0),
 channel_id bigint NOT NULL CHECK(channel_id>0), facts_hash bytea NOT NULL CHECK(octet_length(facts_hash)=32),
 source_transaction xid8 NOT NULL DEFAULT pg_current_xact_id(),created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE FUNCTION notification_source_guard231() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP<>'INSERT' THEN RAISE EXCEPTION 'immutable notification source';END IF;
 IF NEW.source_transaction<>pg_current_xact_id() THEN RAISE EXCEPTION 'notification source requires original transaction'; END IF;
 IF NEW.kind='complain' AND NOT EXISTS(SELECT 1 FROM complaints133 WHERE id=NEW.reference_id AND channel_id=NEW.channel_id AND notification_transaction231=NEW.source_transaction)
 OR NEW.kind='wxtransfer' AND NOT EXISTS(SELECT 1 FROM wechat_transfer_packages w JOIN transfers t ON t.id=w.transfer_id JOIN provider_transfer_attempts a ON a.transfer_id=t.id WHERE t.id=NEW.reference_id AND t.merchant_id=NEW.merchant_id AND a.channel_id=NEW.channel_id AND t.status IN('pending','unknown') AND w.notification_transaction231=NEW.source_transaction)
 OR NEW.kind='channellimit' AND NOT EXISTS(SELECT 1 FROM runtime_channel_success_events WHERE order_id=NEW.reference_id AND channel_id=NEW.channel_id AND notification_transaction231=NEW.source_transaction)
 THEN RAISE EXCEPTION 'notification requires original verified origin';END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER notification_source_identity231 BEFORE INSERT OR UPDATE OR DELETE ON notification_sources231 FOR EACH ROW EXECUTE FUNCTION notification_source_guard231();
CREATE FUNCTION notification_origin_immutable231() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN IF NEW.notification_transaction231 IS DISTINCT FROM OLD.notification_transaction231 THEN RAISE EXCEPTION 'immutable notification origin';END IF;RETURN NEW;END $$;
CREATE TRIGGER complaint_notification_origin231 BEFORE UPDATE ON complaints133 FOR EACH ROW EXECUTE FUNCTION notification_origin_immutable231();
CREATE TRIGGER transfer_notification_origin231 BEFORE UPDATE ON wechat_transfer_packages FOR EACH ROW EXECUTE FUNCTION notification_origin_immutable231();
ALTER TABLE merchant_notice_events ADD COLUMN supplement_id bigint UNIQUE REFERENCES notification_sources231(id);
DO $$ DECLARE c record;BEGIN
 FOR c IN SELECT conname FROM pg_constraint WHERE conrelid='merchant_notice_events'::regclass AND contype='c' AND pg_get_constraintdef(oid) LIKE '%snapshot_state%'
 LOOP EXECUTE format('ALTER TABLE merchant_notice_events DROP CONSTRAINT %I',c.conname);END LOOP;
END $$;
ALTER TABLE merchant_notice_events ADD CONSTRAINT merchant_notice_snapshot231 CHECK(snapshot_state IN('captured','absent','unavailable','excluded','direct'));
ALTER TABLE merchant_notice_events ADD CONSTRAINT merchant_notice_profile231 CHECK((snapshot_state='captured')=(profile_id IS NOT NULL));
DO $$ DECLARE c record; BEGIN
 FOR c IN SELECT conname FROM pg_constraint WHERE conrelid='merchant_notice_events'::regclass AND contype='c' AND pg_get_constraintdef(oid) LIKE '%scene%'
 LOOP EXECUTE format('ALTER TABLE merchant_notice_events DROP CONSTRAINT %I',c.conname);END LOOP;
END $$;
ALTER TABLE merchant_notice_events ADD CONSTRAINT merchant_notice_direct231 CHECK(snapshot_state<>'direct' OR scene='wxtransfer');
ALTER TABLE merchant_notice_events ADD CONSTRAINT merchant_notice_job231 CHECK(job_id IS NULL OR snapshot_state='captured' OR snapshot_state='direct' AND scene='wxtransfer');
ALTER TABLE merchant_notice_claims ALTER COLUMN profile_id DROP NOT NULL;
ALTER TABLE merchant_notice_claims ALTER COLUMN profile_hash DROP NOT NULL;
ALTER TABLE merchant_notice_claims ADD CONSTRAINT merchant_notice_claim_profile231 CHECK((profile_id IS NULL)=(profile_hash IS NULL));
DO $$ DECLARE original text;BEGIN
 SELECT pg_get_functiondef('merchant_notice_claim_guard()'::regprocedure) INTO original;
 IF position('IF NEW.transaction_id<>pg_current_xact_id() OR NOT EXISTS(' IN original)=0 THEN RAISE EXCEPTION 'unexpected claim guard';END IF;
 EXECUTE replace(original,'IF NEW.transaction_id<>pg_current_xact_id() OR NOT EXISTS(',
 'IF NEW.profile_id IS NULL THEN
 IF NEW.transport<>''wechat_template'' OR NEW.profile_hash IS NOT NULL OR NEW.transaction_id<>pg_current_xact_id() OR NOT EXISTS(
 SELECT 1 FROM merchant_notice_events e JOIN notification_sources231 n ON n.id=e.supplement_id JOIN merchants m ON m.id=e.merchant_id JOIN jobs j ON j.id=e.job_id AND j.id=NEW.job_id
 WHERE e.id=NEW.event_id AND e.scene=''wxtransfer'' AND e.snapshot_state=''direct'' AND e.profile_id IS NULL AND e.profile_hash IS NULL AND e.event_hash=NEW.event_hash
 AND n.kind=''wxtransfer'' AND n.merchant_id=e.merchant_id AND n.source_transaction=e.source_transaction AND m.enabled
 AND j.kind=''merchant.notice'' AND j.event_key=''merchant-notice:''||e.id::text AND j.payload=jsonb_build_object(''event_id'',e.id::text)
 AND j.state=''running'' AND j.lease_until>clock_timestamp() AND j.lease_token IS NOT NULL)
 THEN RAISE EXCEPTION ''direct notice requires original confirmation source and lease'';END IF;RETURN NEW;END IF;
 IF NEW.transaction_id<>pg_current_xact_id() OR NOT EXISTS(');
END $$;
ALTER TABLE merchant_notice_events ADD CONSTRAINT merchant_notice_source_shape231 CHECK(
 scene='order' AND order_id IS NOT NULL AND num_nonnulls(login_audit_id,settlement_id,timer_id,domain_audit_id,supplement_id)=0
 OR scene='login' AND login_audit_id IS NOT NULL AND num_nonnulls(order_id,settlement_id,timer_id,domain_audit_id,supplement_id)=0
 OR scene='settle' AND settlement_id IS NOT NULL AND num_nonnulls(order_id,login_audit_id,timer_id,domain_audit_id,supplement_id)=0
 OR scene IN('balance','group','daily_report') AND timer_id IS NOT NULL AND num_nonnulls(order_id,login_audit_id,settlement_id,domain_audit_id,supplement_id)=0
 OR scene='domain_audit' AND domain_audit_id IS NOT NULL AND num_nonnulls(order_id,login_audit_id,settlement_id,timer_id,supplement_id)=0
 OR scene IN('complain','wxtransfer') AND supplement_id IS NOT NULL AND num_nonnulls(order_id,login_audit_id,settlement_id,timer_id,domain_audit_id)=0
);
-- Retain every pre-existing source check; insert the new source branch into
-- the original guarded function rather than replacing it with permissive SQL.
DO $$ DECLARE original text; BEGIN
 SELECT pg_get_functiondef('merchant_notice_event_guard()'::regprocedure) INTO original;
 IF position('IF NEW.scene=''domain_audit'' THEN' IN original)=0 THEN RAISE EXCEPTION 'unexpected notice guard';END IF;
 EXECUTE replace(original,'IF NEW.scene=''domain_audit'' THEN',
 'IF NEW.supplement_id IS NOT NULL THEN
 IF NOT EXISTS(SELECT 1 FROM notification_sources231 n WHERE n.id=NEW.supplement_id AND n.kind=NEW.scene AND n.merchant_id=NEW.merchant_id AND n.source_transaction=NEW.source_transaction) THEN RAISE EXCEPTION ''notice supplement requires original verified source'';END IF;
 RETURN NEW;END IF;
 IF NEW.scene=''domain_audit'' THEN');
END $$;
