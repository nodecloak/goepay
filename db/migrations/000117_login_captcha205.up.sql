CREATE TABLE login_captcha205_challenges (
 token_hash bytea PRIMARY KEY CHECK(octet_length(token_hash)=32),
 browser_hash bytea NOT NULL CHECK(octet_length(browser_hash)=32),
 account_hash bytea NOT NULL CHECK(octet_length(account_hash)=32),
 role text NOT NULL CHECK(role='merchant'),
 mode text NOT NULL CHECK(mode='password'),
 purpose text NOT NULL CHECK(purpose='login'),
 config_hash text NOT NULL CHECK(config_hash~'^[0-9a-f]{64}$'),
 protocol jsonb NOT NULL,
 expires_at timestamptz NOT NULL,
 consumed_at timestamptz,
 session_at timestamptz,
 proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32),
 CHECK(session_at IS NULL OR consumed_at IS NOT NULL)
);
CREATE FUNCTION login_captcha205_original() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF TG_OP='DELETE' OR (NEW.token_hash,NEW.browser_hash,NEW.account_hash,NEW.role,NEW.mode,NEW.purpose,NEW.config_hash,NEW.protocol,NEW.expires_at,NEW.proof_hash)
 IS DISTINCT FROM (OLD.token_hash,OLD.browser_hash,OLD.account_hash,OLD.role,OLD.mode,OLD.purpose,OLD.config_hash,OLD.protocol,OLD.expires_at,OLD.proof_hash)
 OR (OLD.consumed_at IS NOT NULL AND NEW.consumed_at IS DISTINCT FROM OLD.consumed_at)
 OR (OLD.session_at IS NOT NULL AND NEW.session_at IS DISTINCT FROM OLD.session_at) THEN
 RAISE EXCEPTION 'Login CAPTCHA original context is immutable'; END IF; RETURN NEW;
END $$;
CREATE TRIGGER login_captcha205_original BEFORE UPDATE OR DELETE ON login_captcha205_challenges FOR EACH ROW EXECUTE FUNCTION login_captcha205_original();

-- One native proof identity across every consumer, separate from wrapper tokens.
-- The earlier 203 claim ledger remains unchanged and is checked by Claim too.
CREATE TABLE captcha_native_claims205 (
 native_hash bytea PRIMARY KEY CHECK(octet_length(native_hash)=32),
 purpose text NOT NULL CHECK(purpose IN ('testpay','login','payment-defend')),
 challenge_hash bytea NOT NULL CHECK(octet_length(challenge_hash)=32),
 consumed_at timestamptz NOT NULL,
 proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32)
);
CREATE TRIGGER captcha_native_claims205_immutable BEFORE UPDATE OR DELETE ON captcha_native_claims205 FOR EACH ROW EXECUTE FUNCTION captcha_used_proofs203_immutable();
