-- 首页自定义 HTML (the home template "custom"). Presentation only: these rows
-- never select payment accounts. Every save or restore appends an immutable
-- version; the newest version is the one the custom template shows.
CREATE TABLE page_template_custom_versions (
 id bigserial PRIMARY KEY,
 kind text NOT NULL DEFAULT 'home' CHECK(kind='home') REFERENCES page_template_heads(kind),
 version integer NOT NULL CHECK(version>0),
 html text NOT NULL CHECK(octet_length(html)<=65536),
 css text NOT NULL CHECK(octet_length(css)<=32768),
 frame boolean NOT NULL,
 restored_from integer CHECK(restored_from>0 AND restored_from<version),
 digest text NOT NULL CHECK(digest~'^[0-9a-f]{64}$'),
 actor_id bigint NOT NULL CHECK(actor_id>0),
 actor_name text NOT NULL CHECK(length(actor_name) BETWEEN 1 AND 64),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 UNIQUE(kind,version)
);
CREATE TRIGGER page_template_custom_versions_immutable BEFORE UPDATE OR DELETE ON page_template_custom_versions FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();

-- Administrator notice of each version: one claim, committed before any
-- transport I/O, so a crash or an uncertain answer never sends twice.
CREATE TABLE page_template_custom_notices (
 version_id bigint PRIMARY KEY REFERENCES page_template_custom_versions(id),
 job_id bigint NOT NULL UNIQUE REFERENCES jobs(id),
 recipient_hash bytea NOT NULL CHECK(octet_length(recipient_hash)=32),
 lease_hash bytea NOT NULL CHECK(octet_length(lease_hash)=32),
 claimed_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE TRIGGER page_template_custom_notices_immutable BEFORE UPDATE OR DELETE ON page_template_custom_notices FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE TABLE page_template_custom_notice_results (
 version_id bigint PRIMARY KEY REFERENCES page_template_custom_notices(version_id),
 state text NOT NULL CHECK(state IN('accepted','unknown')),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE TRIGGER page_template_custom_notice_results_immutable BEFORE UPDATE OR DELETE ON page_template_custom_notice_results FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
