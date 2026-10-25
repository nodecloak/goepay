CREATE TABLE console_login_audit (
 id bigserial PRIMARY KEY, username text NOT NULL, role text NOT NULL CHECK(role IN ('admin','merchant')),
 ip text NOT NULL, success boolean NOT NULL, created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX console_login_audit_created ON console_login_audit(created_at DESC,id DESC);
CREATE TRIGGER console_login_immutable BEFORE UPDATE OR DELETE ON console_login_audit FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
