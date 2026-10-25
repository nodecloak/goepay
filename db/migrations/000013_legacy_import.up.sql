CREATE TABLE legacy_import_runs (
 export_id text PRIMARY KEY,source text NOT NULL CHECK(source IN ('epay_pro','epay_release')),
 sha256 text NOT NULL CHECK(length(sha256)=64),report jsonb NOT NULL,imported_at timestamptz NOT NULL DEFAULT now()
);
CREATE TRIGGER legacy_import_runs_immutable BEFORE UPDATE OR DELETE ON legacy_import_runs FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
