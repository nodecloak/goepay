-- Menu permission changes revoke existing sessions and outstanding funds
-- authorizations just like account/class changes. Existing accounts and
-- sessions remain unchanged until their authority is actually edited.
CREATE OR REPLACE FUNCTION bump_console_auth_version() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.password_hash IS DISTINCT FROM OLD.password_hash OR NEW.subject_id IS DISTINCT FROM OLD.subject_id OR NEW.enabled IS DISTINCT FROM OLD.enabled OR NEW.admin_class IS DISTINCT FROM OLD.admin_class OR NEW.login_name IS DISTINCT FROM OLD.login_name OR NEW.permissions IS DISTINCT FROM OLD.permissions OR NEW.auth_version>OLD.auth_version THEN
  NEW.auth_version:=OLD.auth_version+1;
 ELSE NEW.auth_version:=OLD.auth_version; END IF;
 RETURN NEW;
END $$;
