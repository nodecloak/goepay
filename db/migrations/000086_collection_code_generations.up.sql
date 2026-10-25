-- Permanent, explicitly revocable collection codes. The public digest is not
-- the private authenticator; neither clients nor generic CRUD can rewrite it.
CREATE TABLE collection_code_generations (
 code_id bigint PRIMARY KEY REFERENCES backoffice_records(id),
 merchant_id bigint NOT NULL REFERENCES merchants(id),
 external_no text NOT NULL CHECK(external_no ~ '^[A-Za-z0-9_-]{8,64}$'),
 recipient_name text NOT NULL CHECK(length(recipient_name) BETWEEN 1 AND 100),
 style_id bigint NOT NULL CHECK(style_id>=0),
 origin text NOT NULL,
 fixed_amount_cents bigint NOT NULL CHECK(fixed_amount_cents BETWEEN 0 AND 100000000),
 request_hash bytea NOT NULL CHECK(octet_length(request_hash)=32),
 config_hash bytea NOT NULL CHECK(octet_length(config_hash)=32),
 proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32),
 created_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(merchant_id,external_no)
);
CREATE TRIGGER collection_generation_immutable BEFORE UPDATE OR DELETE ON collection_code_generations FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
