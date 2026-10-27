CREATE TABLE mail_test_requests195 (
 request_id uuid PRIMARY KEY,
 actor_id bigint NOT NULL CHECK(actor_id>0),
 created_at timestamptz NOT NULL,
 facts jsonb NOT NULL CHECK(jsonb_typeof(facts)='object'),
 proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32),
 UNIQUE(request_id,proof_hash)
);
CREATE INDEX mail_test_requests195_actor_time ON mail_test_requests195(actor_id,created_at);
CREATE TRIGGER mail_test_request195_immutable BEFORE UPDATE OR DELETE ON mail_test_requests195 FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE TABLE mail_test_outcomes195 (
 request_id uuid PRIMARY KEY,
 request_proof bytea NOT NULL CHECK(octet_length(request_proof)=32),
 facts jsonb NOT NULL CHECK(jsonb_typeof(facts)='object'),
 proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32),
 FOREIGN KEY(request_id,request_proof) REFERENCES mail_test_requests195(request_id,proof_hash)
);
CREATE TRIGGER mail_test_outcome195_immutable BEFORE UPDATE OR DELETE ON mail_test_outcomes195 FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
