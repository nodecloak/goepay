ALTER TABLE console_users ADD COLUMN admin_class text NOT NULL DEFAULT 'operator' CHECK(admin_class IN ('super','operator'));
UPDATE console_users SET admin_class='super' WHERE role='admin' AND username='admin' AND subject_id=1;
CREATE OR REPLACE FUNCTION bump_console_auth_version() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.password_hash IS DISTINCT FROM OLD.password_hash OR NEW.subject_id IS DISTINCT FROM OLD.subject_id OR NEW.enabled IS DISTINCT FROM OLD.enabled OR NEW.admin_class IS DISTINCT FROM OLD.admin_class OR NEW.auth_version>OLD.auth_version THEN
  NEW.auth_version:=OLD.auth_version+1;
 ELSE NEW.auth_version:=OLD.auth_version; END IF;
 RETURN NEW;
END $$;
CREATE OR REPLACE FUNCTION guard_last_super_admin() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF OLD.role='admin' AND OLD.admin_class='super' AND OLD.enabled AND (TG_OP='DELETE' OR NEW.admin_class<>'super' OR NOT NEW.enabled) THEN
  PERFORM pg_advisory_xact_lock(74973234);
  IF NOT EXISTS(SELECT 1 FROM console_users WHERE role='admin' AND admin_class='super' AND enabled AND id<>OLD.id) THEN
   RAISE EXCEPTION 'last super administrator is required';
  END IF;
 END IF;
 IF TG_OP='DELETE' THEN RETURN OLD; END IF; RETURN NEW;
END $$;
CREATE TRIGGER last_super_admin BEFORE UPDATE OR DELETE ON console_users FOR EACH ROW EXECUTE FUNCTION guard_last_super_admin();
