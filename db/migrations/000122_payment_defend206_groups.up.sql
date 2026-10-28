-- Per-group override of the create-order verification trigger (original
-- gedit.php config[pay_verify]: '' inherit, '0' off, '1' by rules, '3' forced).
-- Independent of the core risk group MAC; each row is HMAC-proofed and every
-- revision has exactly one ordered administrator audit row.
CREATE TABLE payment_defend206_groups (
 group_id bigint PRIMARY KEY CHECK(group_id>=0),
 revision bigint NOT NULL CHECK(revision>=1),
 pay_verify text NOT NULL CHECK(pay_verify IN ('','0','1','3')),
 saved_at timestamptz NOT NULL,
 proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32)
);
CREATE FUNCTION payment_defend206_groups_original() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF TG_OP='DELETE' OR NEW.group_id<>OLD.group_id OR NEW.revision<>OLD.revision+1 THEN
 RAISE EXCEPTION 'payment defend206 group override history is append-only'; END IF; RETURN NEW;
END $$;
CREATE TRIGGER payment_defend206_groups_original BEFORE UPDATE OR DELETE ON payment_defend206_groups FOR EACH ROW EXECUTE FUNCTION payment_defend206_groups_original();
