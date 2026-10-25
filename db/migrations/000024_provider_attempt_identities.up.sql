CREATE TABLE provider_attempt_identities (
 attempt_id bigint PRIMARY KEY REFERENCES provider_attempts(id),
 request_hash bytea NOT NULL CHECK(octet_length(request_hash)=32),
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TRIGGER provider_attempt_identities_immutable BEFORE UPDATE OR DELETE ON provider_attempt_identities FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
