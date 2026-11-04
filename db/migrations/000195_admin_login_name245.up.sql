-- Login names are mutable; canonical usernames remain the stable key for
-- sessions, encrypted MFA associated data, funds credentials and historical facts.
ALTER TABLE console_users ADD COLUMN login_name text;
ALTER TABLE console_users ADD CONSTRAINT console_users_admin_login_name245
 CHECK (login_name IS NULL OR (role='admin' AND login_name ~ '^[a-zA-Z0-9_.@-]{3,100}$'));
CREATE UNIQUE INDEX console_users_effective_login245
 ON console_users(role,COALESCE(login_name,username));

CREATE OR REPLACE FUNCTION bump_console_auth_version() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.password_hash IS DISTINCT FROM OLD.password_hash OR NEW.subject_id IS DISTINCT FROM OLD.subject_id OR NEW.enabled IS DISTINCT FROM OLD.enabled OR NEW.admin_class IS DISTINCT FROM OLD.admin_class OR NEW.login_name IS DISTINCT FROM OLD.login_name OR NEW.auth_version>OLD.auth_version THEN
  NEW.auth_version:=OLD.auth_version+1;
 ELSE NEW.auth_version:=OLD.auth_version; END IF;
 RETURN NEW;
END $$;
