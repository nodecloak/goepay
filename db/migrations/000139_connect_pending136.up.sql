-- W7 (c5) 快捷登录 pending-connect store. When a quick-login callback resolves a
-- provider identity (openid/social_uid) that is NOT yet bound to any merchant,
-- the original connect.php lands the browser on the login page with
-- "请输入商户ID和密钥完成绑定和登录" and remembers the identity in the session.
-- GOEPAY records that resolved identity here as a single-use, short-lived,
-- browser-bound pending-connect; the merchant then confirms with 商户ID + 密钥 on
-- the login page, which binds the identity and logs them in. Only a hash of the
-- state and browser nonce is stored; the social_uid is the verified provider id.
--
-- Forward-only: applied as-is by db/migrate.go; idempotent and safe on existing
-- installs (IF NOT EXISTS + guarded DO block). No .down.sql.

CREATE TABLE IF NOT EXISTS connect_pending136 (
 state_hash bytea PRIMARY KEY CHECK(octet_length(state_hash)=32),
 provider text NOT NULL CHECK(provider IN ('qq','wx','alipay','telegram')),
 social_uid text NOT NULL CHECK(length(social_uid) BETWEEN 1 AND 256),
 browser_hash bytea NOT NULL CHECK(octet_length(browser_hash)=32),
 created_at timestamptz NOT NULL DEFAULT now(),
 expires_at timestamptz NOT NULL DEFAULT now()+interval '10 minutes',
 consumed_at timestamptz,
 CHECK(expires_at>created_at AND expires_at<=created_at+interval '10 minutes')
);
CREATE INDEX IF NOT EXISTS connect_pending136_sweep ON connect_pending136(expires_at) WHERE consumed_at IS NULL;

-- Identity + issuance frozen; consumed_at is written at most once (single-use);
-- no row is ever otherwise updated or deleted.
CREATE OR REPLACE FUNCTION connect_pending136_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'pending-connect is append-only'; END IF;
 IF (NEW.state_hash,NEW.provider,NEW.social_uid,NEW.browser_hash,NEW.created_at,NEW.expires_at)
    IS DISTINCT FROM (OLD.state_hash,OLD.provider,OLD.social_uid,OLD.browser_hash,OLD.created_at,OLD.expires_at) THEN
  RAISE EXCEPTION 'pending-connect identity is immutable';
 END IF;
 IF OLD.consumed_at IS NOT NULL THEN
  RAISE EXCEPTION 'pending-connect is single-use';
 END IF;
 RETURN NEW;
END $$;
DO $$ BEGIN
 IF NOT EXISTS (SELECT 1 FROM pg_trigger WHERE tgname='connect_pending136_guard') THEN
  CREATE TRIGGER connect_pending136_guard BEFORE UPDATE OR DELETE ON connect_pending136
  FOR EACH ROW EXECUTE FUNCTION connect_pending136_guard();
 END IF;
END $$;
