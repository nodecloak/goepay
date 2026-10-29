-- W7 (c) 快捷登录 OAuth state store. The merchant quick-login flow
-- (epay_release user/connect.php, QC.php, Oauth.php) redirects to the provider
-- with an anti-CSRF `state`; on callback the state must match what was issued
-- for THIS browser and THIS purpose. GOEPAY stores only a hash of the state and
-- of the browser nonce (never the raw values), single-use and short-lived.
--
-- purpose:
--   'login' — anonymous start from the login page; on callback a BOUND openid
--             logs that merchant in, an unknown openid is refused (never an
--             auto-registration / auto-login).
--   'bind'  — authenticated start from 账户设置; merchant_id is the signed-in
--             merchant, and the callback links openid -> that merchant through
--             the bindings path.
--
-- Forward-only: applied as-is by db/migrate.go; idempotent and safe on existing
-- installs (IF NOT EXISTS + guarded DO block). No .down.sql.

CREATE TABLE IF NOT EXISTS connect_login_states135 (
 state_hash bytea PRIMARY KEY CHECK(octet_length(state_hash)=32),
 provider text NOT NULL CHECK(provider IN ('qq','wx','alipay','telegram')),
 purpose text NOT NULL CHECK(purpose IN ('login','bind')),
 -- Hash of the browser nonce cookie set when the state was issued; the callback
 -- must present the same nonce, so a state stolen from another browser is useless.
 browser_hash bytea NOT NULL CHECK(octet_length(browser_hash)=32),
 -- Signed-in merchant for a 'bind' start; NULL for a 'login' start.
 merchant_id bigint REFERENCES merchants(id),
 created_at timestamptz NOT NULL DEFAULT now(),
 expires_at timestamptz NOT NULL DEFAULT now()+interval '10 minutes',
 consumed_at timestamptz,
 -- bind states carry a merchant; login states never do.
 CHECK((purpose='bind' AND merchant_id IS NOT NULL) OR (purpose='login' AND merchant_id IS NULL)),
 CHECK(expires_at>created_at AND expires_at<=created_at+interval '10 minutes')
);
CREATE INDEX IF NOT EXISTS connect_login_states135_sweep ON connect_login_states135(expires_at) WHERE consumed_at IS NULL;

-- Identity + issuance frozen; consumed_at is written at most once (single-use);
-- no row is ever updated otherwise or deleted (an expired row is swept by a
-- separate maintenance job, not mutated here).
CREATE OR REPLACE FUNCTION connect_login_states135_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'connect-login state is append-only'; END IF;
 IF (NEW.state_hash,NEW.provider,NEW.purpose,NEW.browser_hash,NEW.merchant_id,NEW.created_at,NEW.expires_at)
    IS DISTINCT FROM (OLD.state_hash,OLD.provider,OLD.purpose,OLD.browser_hash,OLD.merchant_id,OLD.created_at,OLD.expires_at) THEN
  RAISE EXCEPTION 'connect-login state identity is immutable';
 END IF;
 IF OLD.consumed_at IS NOT NULL THEN
  RAISE EXCEPTION 'connect-login state is single-use';
 END IF;
 RETURN NEW;
END $$;
DO $$ BEGIN
 IF NOT EXISTS (SELECT 1 FROM pg_trigger WHERE tgname='connect_login_states135_guard') THEN
  CREATE TRIGGER connect_login_states135_guard BEFORE UPDATE OR DELETE ON connect_login_states135
  FOR EACH ROW EXECUTE FUNCTION connect_login_states135_guard();
 END IF;
END $$;
