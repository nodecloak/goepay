CREATE TABLE mail_rate_policies194 (
 revision bigint PRIMARY KEY CHECK(revision>0),
 facts jsonb NOT NULL CHECK(jsonb_typeof(facts)='object'),
 proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32),
 UNIQUE(revision,proof_hash)
);
CREATE TRIGGER mail_rate_policy194_immutable BEFORE UPDATE OR DELETE ON mail_rate_policies194 FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE TABLE mail_rate_witnesses194 (
 revision bigint PRIMARY KEY,
 policy_proof bytea NOT NULL CHECK(octet_length(policy_proof)=32),
 witness_proof bytea NOT NULL CHECK(octet_length(witness_proof)=32),
 FOREIGN KEY(revision,policy_proof) REFERENCES mail_rate_policies194(revision,proof_hash)
);
CREATE TRIGGER mail_rate_witness194_immutable BEFORE UPDATE OR DELETE ON mail_rate_witnesses194 FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE TABLE mail_rate_head194 (
 singleton boolean PRIMARY KEY DEFAULT true CHECK(singleton),
 revision bigint NOT NULL DEFAULT 0 CHECK(revision>=0),
 managed boolean NOT NULL DEFAULT false,
 CHECK((managed AND revision>0)OR(NOT managed AND revision=0))
);
INSERT INTO mail_rate_head194(singleton)VALUES(true);
CREATE FUNCTION guard_mail_rate_head194()RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF OLD.managed AND NOT NEW.managed THEN RAISE EXCEPTION 'mail frequency management cannot be forgotten';END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER mail_rate_head194_update BEFORE UPDATE ON mail_rate_head194 FOR EACH ROW EXECUTE FUNCTION guard_mail_rate_head194();
CREATE TRIGGER mail_rate_head194_delete BEFORE DELETE ON mail_rate_head194 FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
