-- 000132 Per-menu administrator permissions + login-log retention.
--
-- Pro (route/admin.php system/getFullMenuList + addAdminUser) assigns each
-- sub-administrator a per-menu permission array. GOEPAY reproduces that with a
-- nullable permissions column on console_users: NULL = unrestricted operator
-- (every operator that existed before this migration keeps exactly its current
-- access), a JSON array of menu keys = an explicit per-page grant set enforced
-- server-side. Super administrators ignore the column.
--
-- Forward-only and idempotent: safe to re-apply on an existing install.

ALTER TABLE console_users ADD COLUMN IF NOT EXISTS permissions jsonb;

-- A non-NULL value must be a JSON array of text keys. NULL stays unrestricted.
DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint WHERE conname = 'console_users_permissions_array'
  ) THEN
    ALTER TABLE console_users
      ADD CONSTRAINT console_users_permissions_array
      CHECK (permissions IS NULL OR jsonb_typeof(permissions) = 'array');
  END IF;
END$$;

-- Login audit retention: console_login_audit grows unbounded and carries no
-- immutable financial fact, so the data-cleanup page may prune it by age. The
-- shipped trigger (000008) rejects both UPDATE and DELETE; replace it with an
-- append-only guard that still forbids UPDATE but permits DELETE so retention
-- can remove aged rows. An index on created_at keeps the age-bounded delete and
-- its preview count cheap. Financial ledgers keep their UPDATE+DELETE guard.
CREATE OR REPLACE FUNCTION reject_login_audit_update() RETURNS trigger
  LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'login audit is append-only'; END $$;

DROP TRIGGER IF EXISTS console_login_immutable ON console_login_audit;
DROP TRIGGER IF EXISTS console_login_append_only ON console_login_audit;
CREATE TRIGGER console_login_append_only BEFORE UPDATE ON console_login_audit
  FOR EACH ROW EXECUTE FUNCTION reject_login_audit_update();

CREATE INDEX IF NOT EXISTS console_login_audit_created_at_idx
  ON console_login_audit (created_at);
