CREATE TABLE captcha_credentials203 (
 id boolean PRIMARY KEY DEFAULT true CHECK(id),
 revision bigint NOT NULL CHECK(revision>0),
 ciphertext bytea NOT NULL,
 proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32),
 updated_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE TABLE public_testpay203_intents (
 id text PRIMARY KEY,
 browser_hash bytea NOT NULL CHECK(octet_length(browser_hash)=32),
 csrf_hash bytea NOT NULL CHECK(octet_length(csrf_hash)=32),
 merchant_id bigint NOT NULL REFERENCES merchants(id),
 out_trade_no text NOT NULL UNIQUE,
 config_hash text NOT NULL CHECK(length(config_hash)=64),
 amount bigint NOT NULL DEFAULT 0 CHECK(amount>=0),
 type_id text NOT NULL DEFAULT '',
 pay_type text NOT NULL DEFAULT '',
 state text NOT NULL CHECK(state IN ('fresh','admitted','submitted','unknown','blocked')),
 order_trade_no text NOT NULL DEFAULT '',
 expires_at timestamptz NOT NULL,
 proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 UNIQUE(browser_hash,csrf_hash)
);
CREATE INDEX public_testpay203_browser_original ON public_testpay203_intents(browser_hash,out_trade_no);
CREATE TABLE captcha_challenges203 (
 token_hash bytea PRIMARY KEY CHECK(octet_length(token_hash)=32),
 intent_id text NOT NULL REFERENCES public_testpay203_intents(id),
 browser_hash bytea NOT NULL CHECK(octet_length(browser_hash)=32),
 purpose text NOT NULL CHECK(purpose IN ('testpay','login','payment-defend')),
 config_hash text NOT NULL,
 protocol jsonb NOT NULL,
 expires_at timestamptz NOT NULL,
 consumed_at timestamptz,
 proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32)
);
CREATE TABLE captcha_used_proofs203 (
 proof_hash bytea PRIMARY KEY CHECK(octet_length(proof_hash)=32),
 challenge_hash bytea NOT NULL REFERENCES captcha_challenges203(token_hash),
 purpose text NOT NULL CHECK(purpose IN ('testpay','login','payment-defend')),
 consumed_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE FUNCTION captcha_used_proofs203_immutable() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'CAPTCHA proof consumption is immutable'; END $$;
CREATE TRIGGER captcha_used_proofs203_immutable BEFORE UPDATE OR DELETE ON captcha_used_proofs203 FOR EACH ROW EXECUTE FUNCTION captcha_used_proofs203_immutable();
