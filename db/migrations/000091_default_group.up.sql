-- A real, reserved default group. Preserve existing Go routing/fee inheritance
-- until an administrator explicitly configures the default; never auto-open a
-- provider or replace an existing merchant/global roll during an upgrade.
DO $$
DECLARE group_name text := '默认用户组'; suffix integer := 0;
BEGIN
 IF EXISTS(SELECT 1 FROM backoffice_records WHERE id=0 AND resource<>'groups') THEN
  RAISE EXCEPTION 'reserved default group ID 0 belongs to a different resource';
 END IF;
 IF NOT EXISTS(SELECT 1 FROM backoffice_records WHERE id=0) THEN
  WHILE EXISTS(SELECT 1 FROM backoffice_records WHERE resource='groups' AND lower(body->>'name')=lower(group_name)) LOOP
   suffix := suffix+1;
   group_name := '默认用户组（GID 0，迁移' || suffix::text || '）';
  END LOOP;
  INSERT INTO backoffice_records(id,resource,body) VALUES(0,'groups',jsonb_build_object('name',group_name,'enabled',true,'rate_bps',0,'info','{}'::jsonb));
 END IF;
END $$;

CREATE FUNCTION protect_default_group_identity() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF OLD.id=0 AND OLD.resource='groups' THEN
  IF TG_OP='DELETE' THEN RAISE EXCEPTION 'the built-in default group cannot be deleted'; END IF;
  IF NEW.id<>0 OR NEW.resource<>'groups' THEN RAISE EXCEPTION 'the built-in default group identity cannot change'; END IF;
 END IF;
 IF TG_OP='DELETE' THEN RETURN OLD; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER default_group_identity BEFORE DELETE OR UPDATE OF id,resource ON backoffice_records FOR EACH ROW EXECUTE FUNCTION protect_default_group_identity();
