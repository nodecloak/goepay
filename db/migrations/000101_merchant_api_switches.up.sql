-- Personal API preferences are independent of existing administrator grants.
-- No historical merchant, profile, intent, journal or proof is rewritten.
CREATE TABLE merchant_api_switches (
 merchant_id bigint PRIMARY KEY REFERENCES merchants(id),
 keylogin integer NOT NULL CHECK(keylogin IN(0,1)),
 refund integer NOT NULL CHECK(refund IN(0,1)),
 transfer integer NOT NULL CHECK(transfer IN(0,1)),
 revision bigint NOT NULL CHECK(revision>0),
 proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32)
);
CREATE TRIGGER merchant_api_switches_no_delete BEFORE DELETE ON merchant_api_switches
 FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
