-- Phone-verified enrollment identity (register / find-password by phone), the
-- SMS counterpart of enrollment_emails. The original epay_release user/reg.php
-- and findpwd.php register / reset an account after a VerifyCode type 1 (phone)
-- code; the code lifecycle itself lives in verification_codes134 (migration
-- 000134). This table records the COMPLETED phone claim so that:
--   * a phone already registered is refused (UNIQUE phone_hash), as reg.php does
--     ("该手机号码已被注册"), and
--   * find-password can resolve the account for a verified phone.
--
-- Mirrors enrollment_emails exactly: the claim is established only by a
-- completed verification, is one-to-one with a merchant, and is immutable
-- (a future change-phone flow needs an explicit evidence lifecycle, just as the
-- email binding does). Only a phone_hash (salted HMAC) is stored for lookup; the
-- clear phone is kept in the same row for operator reference, never used as
-- authorization. Nothing here is a credential.
--
-- Forward-only: applied as-is by db/migrate.go; idempotent and safe on existing
-- installs (IF NOT EXISTS + guarded trigger). No .down.sql. It adds a new table
-- only and changes no existing row.

CREATE TABLE IF NOT EXISTS enrollment_phones134 (
 phone text PRIMARY KEY CHECK(length(phone) BETWEEN 5 AND 20),
 phone_hash bytea UNIQUE NOT NULL CHECK(octet_length(phone_hash)=32),
 merchant_id bigint UNIQUE NOT NULL REFERENCES merchants(id),
 username text NOT NULL,
 role text NOT NULL DEFAULT 'merchant' CHECK(role='merchant'),
 verified_at timestamptz NOT NULL DEFAULT statement_timestamp(),
 FOREIGN KEY(username,role) REFERENCES console_users(username,role)
);

-- Phone claims are established by a completed verification, never editable
-- profile data; the row is append-only like enrollment_emails.
DO $$ BEGIN
 IF NOT EXISTS (SELECT 1 FROM pg_trigger WHERE tgname='enrollment_phone134_immutable') THEN
  CREATE TRIGGER enrollment_phone134_immutable BEFORE UPDATE OR DELETE ON enrollment_phones134
  FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
 END IF;
END $$;
