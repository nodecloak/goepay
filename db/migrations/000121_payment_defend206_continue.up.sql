-- Opaque, one-time continue token for the create-order human-verification jump.
-- It replaces the original timeless md5 __defend key: the token is bound to the
-- exact signed request, merchant, order number, browser, purpose, policy
-- fingerprint and a short deadline, and is consumable exactly once. No order,
-- provider attempt or send is created before a valid token is consumed.
CREATE TABLE payment_defend206_continue (
 token_hash bytea PRIMARY KEY CHECK(octet_length(token_hash)=32),
 binding_hash bytea NOT NULL CHECK(octet_length(binding_hash)=32),
 purpose text NOT NULL CHECK(purpose='create-order'),
 mode text NOT NULL CHECK(mode IN ('0','1','2')),
 policy_fingerprint text NOT NULL CHECK(policy_fingerprint~'^[0-9a-f]{64}$'),
 expires_at timestamptz NOT NULL,
 consumed_at timestamptz,
 proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32)
);
-- The minted context is immutable; only the one-shot consumed_at transition
-- (NULL -> a timestamp) is permitted, and never a re-consume or a delete.
CREATE FUNCTION payment_defend206_continue_original() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF TG_OP='DELETE' OR (NEW.token_hash,NEW.binding_hash,NEW.purpose,NEW.mode,NEW.policy_fingerprint,NEW.expires_at,NEW.proof_hash)
 IS DISTINCT FROM (OLD.token_hash,OLD.binding_hash,OLD.purpose,OLD.mode,OLD.policy_fingerprint,OLD.expires_at,OLD.proof_hash)
 OR (OLD.consumed_at IS NOT NULL AND NEW.consumed_at IS DISTINCT FROM OLD.consumed_at) THEN
 RAISE EXCEPTION 'payment defend206 continue token context is immutable'; END IF; RETURN NEW;
END $$;
CREATE TRIGGER payment_defend206_continue_original BEFORE UPDATE OR DELETE ON payment_defend206_continue FOR EACH ROW EXECUTE FUNCTION payment_defend206_continue_original();
