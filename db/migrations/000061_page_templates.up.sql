-- Built-in presentation only. These settings never select payment accounts.
CREATE TABLE page_template_heads (
 kind text PRIMARY KEY CHECK(kind IN ('home','pay','qrpay')),
 selected_name text NOT NULL CHECK(length(selected_name) BETWEEN 1 AND 64),
 revision bigint NOT NULL DEFAULT 0 CHECK(revision>=0),
 updated_at timestamptz NOT NULL DEFAULT now()
);
INSERT INTO page_template_heads(kind,selected_name) VALUES('home','default'),('pay','default'),('qrpay','default');
CREATE TABLE page_template_configs (
 kind text NOT NULL REFERENCES page_template_heads(kind),
 name text NOT NULL CHECK(length(name) BETWEEN 1 AND 64),
 config jsonb NOT NULL CHECK(jsonb_typeof(config)='object' AND octet_length(config::text)<=8192),
 revision bigint NOT NULL CHECK(revision>0),
 updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(kind,name)
);
CREATE TABLE page_template_assets (
 id text PRIMARY KEY CHECK(id~'^[0-9a-f]{64}$'),
 mime text NOT NULL CHECK(mime IN ('image/png','image/jpeg')),
 width integer NOT NULL CHECK(width BETWEEN 1 AND 2048),
 height integer NOT NULL CHECK(height BETWEEN 1 AND 2048),
 content bytea NOT NULL CHECK(octet_length(content) BETWEEN 1 AND 2097152),
 created_at timestamptz NOT NULL DEFAULT now(),
 CHECK(width::bigint*height::bigint<=1048576)
);
CREATE TRIGGER page_template_assets_immutable BEFORE UPDATE OR DELETE ON page_template_assets FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
