ALTER TABLE backoffice_records DROP CONSTRAINT backoffice_records_resource_check;
ALTER TABLE backoffice_records ADD CONSTRAINT backoffice_records_resource_check CHECK(resource IN ('groups','domains','invitation-codes','announcements','settings','channels','blacklist','collection-styles','profitsharing-rules','pay-types','pay-rolls','weixin-apps','onboarding-channels','onboarding-applications','subchannels','merchant-risk','complaints','complaint-tasks','risk-records','cron-tasks','settlement-settings','collection-codes','plugin-configs','template-configs','addon-configs','order-subject-policies'));
CREATE UNIQUE INDEX subject_policy_active_scope ON backoffice_records((body->>'scope'),(body->>'scope_id'))
 WHERE resource='order-subject-policies' AND body->'enabled'='true'::jsonb;
CREATE TABLE provider_subject_snapshots (
 order_id bigint PRIMARY KEY REFERENCES orders(id),
 channel_id bigint NOT NULL CHECK(channel_id>0),
 subject text NOT NULL,
 configured boolean NOT NULL,
 policy_snapshot jsonb NOT NULL CHECK(jsonb_typeof(policy_snapshot)='object'),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 CHECK((configured AND octet_length(subject) BETWEEN 1 AND 128) OR (NOT configured AND octet_length(subject)<=256))
);
CREATE TRIGGER provider_subject_immutable BEFORE UPDATE OR DELETE ON provider_subject_snapshots FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
-- Independent of application configuration, a title selected for one upstream
-- channel cannot be consumed by a different channel's external intent.
CREATE FUNCTION guard_provider_subject_channel() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF EXISTS(SELECT 1 FROM provider_subject_snapshots WHERE order_id=NEW.order_id AND channel_id<>NEW.channel_id) THEN
  RAISE EXCEPTION 'provider subject conflicts with immutable channel';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER provider_attempt_subject BEFORE INSERT ON provider_attempts FOR EACH ROW EXECUTE FUNCTION guard_provider_subject_channel();
