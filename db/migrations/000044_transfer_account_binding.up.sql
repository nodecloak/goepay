CREATE TABLE provider_transfer_accounts (
 transfer_id bigint PRIMARY KEY REFERENCES provider_transfer_attempts(transfer_id),
 account_hash bytea NOT NULL CHECK(octet_length(account_hash)=32),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE TRIGGER provider_transfer_account_immutable BEFORE UPDATE OR DELETE ON provider_transfer_accounts FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
