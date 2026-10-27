CREATE TABLE site_brand_assets193 (
 id text PRIMARY KEY CHECK(id ~ '^[a-f0-9]{64}$'),
 mime text NOT NULL CHECK(mime IN ('image/png','image/jpeg')),
 width integer NOT NULL CHECK(width BETWEEN 1 AND 2048),
 height integer NOT NULL CHECK(height BETWEEN 1 AND 2048),
 content bytea NOT NULL CHECK(octet_length(content) BETWEEN 1 AND 2097152),
 CHECK(width::bigint*height<=1048576)
);
CREATE TRIGGER site_brand_asset193_immutable BEFORE UPDATE OR DELETE ON site_brand_assets193 FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE TABLE site_brand_revisions193 (
 revision bigint PRIMARY KEY CHECK(revision>0),
 facts jsonb NOT NULL CHECK(jsonb_typeof(facts)='object'),
 proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32)
);
CREATE TRIGGER site_brand_revision193_immutable BEFORE UPDATE OR DELETE ON site_brand_revisions193 FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE TABLE site_brand_head193 (
 singleton boolean PRIMARY KEY DEFAULT true CHECK(singleton),
 revision bigint NOT NULL CHECK(revision>=0)
);
INSERT INTO site_brand_head193(singleton,revision)VALUES(true,0);
CREATE TRIGGER site_brand_head193_delete BEFORE DELETE ON site_brand_head193 FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
