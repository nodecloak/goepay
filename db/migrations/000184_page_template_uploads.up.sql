-- 上传的页面模板 (uploaded page templates) replace the custom home HTML editor
-- of 000183. Presentation only: these rows never select payment accounts.
-- Every upload or restore appends an immutable version with its files; a
-- removal is an event, so the history stays. The newest version of a template
-- is the one that is shown. The 000183 tables stay as they are, unused, so no
-- deployment loses a row.

-- File contents, addressed by their SHA-256 and shared between versions.
CREATE TABLE page_template_blobs (
 digest text PRIMARY KEY CHECK(digest~'^[0-9a-f]{64}$'),
 content bytea NOT NULL CHECK(octet_length(content)<=3145728),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE TRIGGER page_template_blobs_immutable BEFORE UPDATE OR DELETE ON page_template_blobs FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();

-- One row per uploaded template of a kind. The name is the one selected in
-- page_template_heads and configured in page_template_configs.
CREATE TABLE page_template_uploads (
 id bigserial PRIMARY KEY,
 kind text NOT NULL REFERENCES page_template_heads(kind),
 name text NOT NULL CHECK(name~'^[a-z][a-z0-9-]{0,30}[a-z0-9]$'),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 UNIQUE(kind,name)
);
CREATE TRIGGER page_template_uploads_immutable BEFORE UPDATE OR DELETE ON page_template_uploads FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();

-- Every upload or restore. file_key is the random part of the public file
-- addresses (/template-files/<file_key>/<path>) of this version.
CREATE TABLE page_template_upload_versions (
 id bigserial PRIMARY KEY,
 upload_id bigint NOT NULL REFERENCES page_template_uploads(id),
 version integer NOT NULL CHECK(version>0),
 file_key text NOT NULL UNIQUE CHECK(file_key~'^[0-9a-f]{32}$'),
 title text NOT NULL CHECK(length(title) BETWEEN 1 AND 40),
 author text NOT NULL CHECK(length(author)<=40),
 description text NOT NULL CHECK(length(description)<=200),
 package_version text NOT NULL CHECK(package_version~'^[0-9A-Za-z.+-]{0,32}$'),
 digest text NOT NULL CHECK(digest~'^[0-9a-f]{64}$'),
 files integer NOT NULL CHECK(files BETWEEN 2 AND 300),
 size_bytes bigint NOT NULL CHECK(size_bytes BETWEEN 1 AND 25165824),
 restored_from integer CHECK(restored_from>0 AND restored_from<version),
 actor_id bigint NOT NULL CHECK(actor_id>0),
 actor_name text NOT NULL CHECK(length(actor_name) BETWEEN 1 AND 64),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 UNIQUE(upload_id,version)
);
CREATE TRIGGER page_template_upload_versions_immutable BEFORE UPDATE OR DELETE ON page_template_upload_versions FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();

CREATE TABLE page_template_upload_files (
 version_id bigint NOT NULL REFERENCES page_template_upload_versions(id),
 path text NOT NULL CHECK(octet_length(path) BETWEEN 1 AND 200 AND path~'^[A-Za-z0-9_][A-Za-z0-9._@+-]{0,79}(/[A-Za-z0-9_][A-Za-z0-9._@+-]{0,79}){0,7}$'),
 digest text NOT NULL REFERENCES page_template_blobs(digest),
 PRIMARY KEY(version_id,path)
);
CREATE UNIQUE INDEX page_template_upload_files_folded ON page_template_upload_files(version_id,lower(path));
CREATE TRIGGER page_template_upload_files_immutable BEFORE UPDATE OR DELETE ON page_template_upload_files FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();

-- Upload, restore, remove and select of an uploaded template. A template is
-- removed while its newest event is 'remove'. Each event has one notice job.
CREATE TABLE page_template_upload_events (
 id bigserial PRIMARY KEY,
 upload_id bigint NOT NULL,
 operation text NOT NULL CHECK(operation IN('upload','restore','remove','select')),
 version integer NOT NULL,
 restored_from integer CHECK(restored_from>0 AND restored_from<version),
 actor_id bigint NOT NULL CHECK(actor_id>0),
 actor_name text NOT NULL CHECK(length(actor_name) BETWEEN 1 AND 64),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 CHECK((operation='restore')=(restored_from IS NOT NULL)),
 FOREIGN KEY(upload_id,version) REFERENCES page_template_upload_versions(upload_id,version)
);
CREATE INDEX page_template_upload_events_latest ON page_template_upload_events(upload_id,id DESC);
CREATE TRIGGER page_template_upload_events_immutable BEFORE UPDATE OR DELETE ON page_template_upload_events FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();

-- Administrator notice of each event: one claim, committed before any
-- transport I/O, so a crash or an uncertain answer never sends twice.
CREATE TABLE page_template_notices (
 event_id bigint PRIMARY KEY REFERENCES page_template_upload_events(id),
 job_id bigint NOT NULL UNIQUE REFERENCES jobs(id),
 recipient_hash bytea NOT NULL CHECK(octet_length(recipient_hash)=32),
 lease_hash bytea NOT NULL CHECK(octet_length(lease_hash)=32),
 claimed_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE TRIGGER page_template_notices_immutable BEFORE UPDATE OR DELETE ON page_template_notices FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE TABLE page_template_notice_results (
 event_id bigint PRIMARY KEY REFERENCES page_template_notices(event_id),
 state text NOT NULL CHECK(state IN('accepted','unknown')),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE TRIGGER page_template_notice_results_immutable BEFORE UPDATE OR DELETE ON page_template_notice_results FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
