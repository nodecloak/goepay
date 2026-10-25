CREATE TABLE backoffice_records (
 id bigserial PRIMARY KEY,
 resource text NOT NULL CHECK(resource IN ('groups','domains','invitation-codes','announcements','settings','channels','blacklist','collection-styles','profitsharing-rules','pay-types','pay-rolls','weixin-apps','onboarding-channels','onboarding-applications','subchannels','merchant-risk','complaints','complaint-tasks','risk-records','cron-tasks','settlement-settings','collection-codes','plugin-configs','template-configs','addon-configs')),
 owner_id bigint REFERENCES merchants(id),
 body jsonb NOT NULL CHECK(jsonb_typeof(body)='object'),
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX backoffice_records_list ON backoffice_records(resource,owner_id,id DESC);
CREATE UNIQUE INDEX backoffice_domain_unique ON backoffice_records(owner_id,lower(body->>'domain')) WHERE resource='domains';
CREATE UNIQUE INDEX backoffice_invitation_unique ON backoffice_records((body->>'code')) WHERE resource='invitation-codes';
CREATE UNIQUE INDEX backoffice_setting_unique ON backoffice_records((body->>'key')) WHERE resource='settings';
CREATE UNIQUE INDEX backoffice_group_unique ON backoffice_records(lower(body->>'name')) WHERE resource='groups';
CREATE UNIQUE INDEX backoffice_style_unique ON backoffice_records(lower(body->>'name')) WHERE resource='collection-styles';
ALTER TABLE merchants ADD COLUMN profile jsonb NOT NULL DEFAULT '{}'::jsonb CHECK(jsonb_typeof(profile)='object');
CREATE TABLE IF NOT EXISTS console_users(username text NOT NULL,role text NOT NULL CHECK(role IN ('admin','merchant')),subject_id bigint NOT NULL,display_name text NOT NULL,password_hash text NOT NULL,enabled boolean NOT NULL DEFAULT true,PRIMARY KEY(username,role));
ALTER TABLE console_users ADD COLUMN id bigserial UNIQUE;
CREATE SEQUENCE console_subject_seq START WITH 10000;
CREATE TABLE backoffice_audit (
 id bigserial PRIMARY KEY, actor_role text NOT NULL, actor_id bigint NOT NULL, actor_name text NOT NULL,
 resource text NOT NULL, resource_id bigint NOT NULL, operation text NOT NULL,
 changes jsonb NOT NULL DEFAULT '{}'::jsonb, created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX backoffice_audit_lookup ON backoffice_audit(resource,resource_id,id DESC);
CREATE TRIGGER backoffice_audit_immutable BEFORE UPDATE OR DELETE ON backoffice_audit FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();

