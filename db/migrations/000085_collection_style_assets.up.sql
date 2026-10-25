-- Receipt-card backgrounds have their own publication lifecycle and a larger
-- bounded canvas than page-template logos. Never publish an uploaded draft by
-- borrowing the page-template asset endpoint.
CREATE TABLE collection_style_assets (
 id text PRIMARY KEY CHECK(id ~ '^[0-9a-f]{64}$'),
 mime text NOT NULL CHECK(mime IN ('image/png','image/jpeg')),
 width integer NOT NULL CHECK(width BETWEEN 1 AND 4096),
 height integer NOT NULL CHECK(height BETWEEN 1 AND 4096),
 content bytea NOT NULL CHECK(octet_length(content) BETWEEN 1 AND 4194304),
 created_at timestamptz NOT NULL DEFAULT now(),
 CHECK(width::bigint*height::bigint<=4194304)
);
CREATE TRIGGER collection_style_asset_immutable BEFORE UPDATE OR DELETE ON collection_style_assets FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
