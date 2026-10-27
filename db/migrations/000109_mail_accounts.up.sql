CREATE TABLE mail_account_revisions195 (
 revision bigint PRIMARY KEY CHECK(revision>0),
 facts jsonb NOT NULL CHECK(jsonb_typeof(facts)='object'),
 proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32),
 UNIQUE(revision,proof_hash)
);
CREATE TRIGGER mail_account_revision195_immutable BEFORE UPDATE OR DELETE ON mail_account_revisions195 FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE TABLE mail_account_witnesses195 (
 revision bigint PRIMARY KEY,
 account_proof bytea NOT NULL CHECK(octet_length(account_proof)=32),
 witness_proof bytea NOT NULL CHECK(octet_length(witness_proof)=32),
 FOREIGN KEY(revision,account_proof) REFERENCES mail_account_revisions195(revision,proof_hash)
);
CREATE TRIGGER mail_account_witness195_immutable BEFORE UPDATE OR DELETE ON mail_account_witnesses195 FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE TABLE mail_account_head195 (
 singleton boolean PRIMARY KEY DEFAULT true CHECK(singleton),
 revision bigint NOT NULL DEFAULT 0 CHECK(revision>=0),
 managed boolean NOT NULL DEFAULT false,
 CHECK((managed AND revision>0)OR(NOT managed AND revision=0))
);
INSERT INTO mail_account_head195(singleton)VALUES(true);
CREATE FUNCTION guard_mail_account_head195()RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF OLD.managed AND NOT NEW.managed THEN RAISE EXCEPTION 'mail account management cannot be forgotten';END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER mail_account_head195_update BEFORE UPDATE ON mail_account_head195 FOR EACH ROW EXECUTE FUNCTION guard_mail_account_head195();
CREATE TRIGGER mail_account_head195_delete BEFORE DELETE ON mail_account_head195 FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
