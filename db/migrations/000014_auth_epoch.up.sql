CREATE OR REPLACE FUNCTION bump_console_auth_version() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.password_hash IS DISTINCT FROM OLD.password_hash OR NEW.subject_id IS DISTINCT FROM OLD.subject_id OR NEW.enabled IS DISTINCT FROM OLD.enabled OR NEW.auth_version>OLD.auth_version THEN
  NEW.auth_version:=OLD.auth_version+1;
 ELSE NEW.auth_version:=OLD.auth_version; END IF;
 RETURN NEW;
END $$;
