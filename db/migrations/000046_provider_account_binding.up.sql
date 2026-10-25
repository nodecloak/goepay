CREATE TABLE provider_attempt_accounts (
 attempt_id bigint PRIMARY KEY REFERENCES provider_attempts(id),
 account_hash bytea NOT NULL CHECK(octet_length(account_hash)=32),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE TRIGGER provider_attempt_account_immutable BEFORE UPDATE OR DELETE ON provider_attempt_accounts FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
