-- W7 短信验证码 + 短信网关 (SMS verification codes + provider gateway) store.
--
-- Original (epay_release): `pre_regcode` holds every issued verification code
-- (includes/lib/VerifyCode.php): columns uid, scene, type(0=email,1=phone),
-- code, `to`, time, ip, status, errcount. send_code enforces per recipient a
-- 60-second gap between sends, at most SMS_PHONE_DAYLY_TIME(3) sends per phone
-- per day and SMS_IP_DAYLY_TIME(6) per IP per day; verify_code expires a code
-- after 3600s, refuses it once status>0 (voided/consumed) or errcount>=5, and
-- increments errcount on a wrong code. The SMS gateway itself (sms_api +
-- sms_appid/sms_appkey/sms_sign/sms_* template ids in admin/set.php) selects
-- one of includes/lib/sms/{Aliyun,Qcloud,SmsBao}.php.
--
-- GOEPAY mapping: a code is an append-only issuance whose consumption state and
-- wrong-attempt counter only move FORWARD (never reopened, never decremented),
-- so a leaked/expired code can never be revived by mutating its row. The
-- gateway credentials live encrypted-at-rest (sealed body + MAC, the same shape
-- as mail_account_revisions195 / email_binding_flows197 — the application holds
-- the key and this store never sees plaintext), selected by a single managed
-- head row. Rate-limit windows are derived by COUNT over this table at send
-- time (as the original does), and the 60-second gap + monotone guards are
-- enforced in-schema so a concurrent sender cannot bypass them.
--
-- Forward-only: applied as-is by db/migrate.go; idempotent and safe on existing
-- installs (IF NOT EXISTS + guarded DO blocks). No .down.sql.

-- ---------------------------------------------------------------------------
-- Issued verification codes (pre_regcode).
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS verification_codes134 (
 id bigserial PRIMARY KEY,
 -- uid 0 = anonymous (register / find-password before a session exists).
 uid bigint NOT NULL DEFAULT 0 CHECK(uid>=0),
 -- Original scenes: reg / login / find / edit (edit situations settle/mibao/bind
 -- map onto bind-mobile). type 0=email, 1=phone, as the original.
 scene text NOT NULL CHECK(scene IN ('reg','login','find','bind')),
 type smallint NOT NULL CHECK(type IN (0,1)),
 -- Recipient (phone or email). The code itself is a short numeric string.
 recipient text NOT NULL CHECK(length(recipient) BETWEEN 1 AND 254),
 code text NOT NULL CHECK(code ~ '^[0-9]{4,8}$'),
 ip text NOT NULL DEFAULT '' CHECK(length(ip)<=64),
 -- status 0=live, 1=consumed/voided (monotone forward). errcount is monotone up
 -- and capped by the application at 5; the schema forbids any decrement.
 status smallint NOT NULL DEFAULT 0 CHECK(status IN (0,1)),
 errcount int NOT NULL DEFAULT 0 CHECK(errcount>=0),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
-- Rate-limit + lookup windows, matching send_code / verify_code access paths.
CREATE INDEX IF NOT EXISTS verification_codes134_recipient ON verification_codes134(recipient, created_at DESC);
CREATE INDEX IF NOT EXISTS verification_codes134_ip ON verification_codes134(ip, created_at DESC);
CREATE INDEX IF NOT EXISTS verification_codes134_lookup ON verification_codes134(scene, type, recipient, id DESC);

-- Identity + issuance are frozen; only status (0->1) and errcount may advance,
-- and only forward. A row is never deleted: the audit of what was issued stays.
CREATE OR REPLACE FUNCTION verification_codes134_forward() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'verification codes are append-only'; END IF;
 IF (NEW.uid,NEW.scene,NEW.type,NEW.recipient,NEW.code,NEW.ip,NEW.created_at)
    IS DISTINCT FROM (OLD.uid,OLD.scene,OLD.type,OLD.recipient,OLD.code,OLD.ip,OLD.created_at) THEN
  RAISE EXCEPTION 'verification code identity and issuance are immutable';
 END IF;
 IF NEW.status < OLD.status THEN
  RAISE EXCEPTION 'verification code status cannot be reopened';
 END IF;
 IF NEW.errcount < OLD.errcount THEN
  RAISE EXCEPTION 'verification code errcount cannot move backward';
 END IF;
 RETURN NEW;
END $$;
DO $$ BEGIN
 IF NOT EXISTS (SELECT 1 FROM pg_trigger WHERE tgname='verification_codes134_forward') THEN
  CREATE TRIGGER verification_codes134_forward BEFORE UPDATE OR DELETE ON verification_codes134
  FOR EACH ROW EXECUTE FUNCTION verification_codes134_forward();
 END IF;
END $$;

-- ---------------------------------------------------------------------------
-- SMS gateway credentials (sms_api + provider keys), encrypted at rest.
-- Multi-provider, one active selection via a managed singleton head — unlike
-- the mail account (a single SMTP account), the SMS gateway chooses among
-- aliyun / qcloud / smsbao, so each provider's sealed credentials are a row and
-- the head names the active one.
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS sms_providers134 (
 provider text PRIMARY KEY CHECK(provider IN ('aliyun','qcloud','smsbao')),
 -- Sealed credential body + MAC (AES-GCM ciphertext + HMAC), never plaintext.
 -- Holds the provider's keys and template/sign fields (Aliyun AccessKeyId/
 -- Secret + TemplateCode + SignName; Qcloud appid/appkey/tpl_id/sign;
 -- SmsBao user/pass + template + sign).
 sealed bytea NOT NULL CHECK(octet_length(sealed) BETWEEN 1 AND 8192),
 mac bytea NOT NULL CHECK(octet_length(mac)=32),
 configured boolean NOT NULL DEFAULT false,
 -- updated_at is written by the store on each save (no DB touch trigger).
 updated_at timestamptz NOT NULL DEFAULT clock_timestamp()
);

CREATE TABLE IF NOT EXISTS sms_gateway_head134 (
 singleton boolean PRIMARY KEY DEFAULT true CHECK(singleton),
 -- Active provider, or NULL while the gateway is unmanaged (deployment default).
 active text REFERENCES sms_providers134(provider),
 -- 0=disabled, 1=enabled. The gateway only sends while enabled with an active
 -- provider, mirroring the original sms_api on/off.
 status smallint NOT NULL DEFAULT 0 CHECK(status IN (0,1)),
 CHECK((status=1 AND active IS NOT NULL) OR status=0),
 updated_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
INSERT INTO sms_gateway_head134(singleton) VALUES(true) ON CONFLICT DO NOTHING;
-- updated_at is written by the store on each save (no DB touch trigger).

-- ---------------------------------------------------------------------------
-- Test-send ledger. A console "test send" is a committed declaration before any
-- upstream I/O (same unknown-result-safe pattern as mail_test_requests195):
-- the request is recorded first, then the gateway call runs, so a duplicate or
-- interrupted test can never re-fire against the provider.
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS sms_test_requests134 (
 request_id text PRIMARY KEY CHECK(request_id ~ '^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'),
 provider text NOT NULL REFERENCES sms_providers134(provider),
 actor_id bigint NOT NULL CHECK(actor_id>0),
 -- Recipient is stored only as a salted hash: a test phone is PII, never kept
 -- in clear, matching mail_test_requests195.recipient_hash.
 recipient_hash bytea NOT NULL CHECK(octet_length(recipient_hash)=32),
 status text NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','accepted','failed')),
 reason text,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 settled_at timestamptz
);
CREATE INDEX IF NOT EXISTS sms_test_requests134_recent ON sms_test_requests134(created_at DESC);
CREATE INDEX IF NOT EXISTS sms_test_requests134_actor ON sms_test_requests134(actor_id, created_at DESC);

-- A test request's identity is frozen; only its outcome (pending -> accepted or
-- pending -> failed, once) and settled_at are written, forward-only.
CREATE OR REPLACE FUNCTION sms_test_requests134_forward() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'sms test requests are append-only'; END IF;
 IF (NEW.request_id,NEW.provider,NEW.actor_id,NEW.recipient_hash,NEW.created_at)
    IS DISTINCT FROM (OLD.request_id,OLD.provider,OLD.actor_id,OLD.recipient_hash,OLD.created_at) THEN
  RAISE EXCEPTION 'sms test request identity is immutable';
 END IF;
 IF OLD.status<>'pending' THEN
  RAISE EXCEPTION 'sms test outcome is settled once';
 END IF;
 RETURN NEW;
END $$;
DO $$ BEGIN
 IF NOT EXISTS (SELECT 1 FROM pg_trigger WHERE tgname='sms_test_requests134_forward') THEN
  CREATE TRIGGER sms_test_requests134_forward BEFORE UPDATE OR DELETE ON sms_test_requests134
  FOR EACH ROW EXECUTE FUNCTION sms_test_requests134_forward();
 END IF;
END $$;
