-- Administrative assignments remain separate from a time limited purchase.
ALTER TABLE merchants ADD COLUMN base_group_id bigint NOT NULL DEFAULT 0 CHECK(base_group_id>=0);
ALTER TABLE merchants ADD COLUMN group_baseline_unresolved boolean NOT NULL DEFAULT false;
UPDATE merchants SET base_group_id=CASE WHEN COALESCE(profile->>'group_id','') ~ '^[0-9]{1,19}$' AND (profile->>'group_id')::numeric<=9223372036854775807 THEN (profile->>'group_id')::bigint ELSE 0 END;
UPDATE merchants SET group_baseline_unresolved=true WHERE COALESCE(profile->>'group_id','')<>'' AND base_group_id=0 AND profile->>'group_id'<>'0';

-- Old application versions replaced profile.group_id during purchase. Recover
-- its administrator assignment from immutable audit, never guess a zero fee.
DO $$
DECLARE rec record; assigned text;
BEGIN
 FOR rec IN SELECT m.id FROM merchants m JOIN memberships p ON p.merchant_id=m.id WHERE m.profile->>'group_id'=p.group_id::text LOOP
  assigned:=NULL;
  SELECT changes->>'group_id' INTO assigned FROM backoffice_audit WHERE resource='merchants' AND resource_id=rec.id AND operation IN ('create','update') AND changes ? 'group_id' ORDER BY id DESC LIMIT 1;
  IF assigned IS NOT NULL AND (assigned='' OR (assigned ~ '^[0-9]{1,19}$' AND assigned::numeric<=9223372036854775807)) THEN
   UPDATE merchants SET base_group_id=CASE WHEN assigned='' THEN 0 ELSE assigned::bigint END,group_baseline_unresolved=false,profile=jsonb_set(profile,'{group_id}',to_jsonb(assigned)) WHERE id=rec.id;
  ELSE
   UPDATE merchants SET base_group_id=0,group_baseline_unresolved=true,profile=jsonb_set(profile,'{group_id}','""') WHERE id=rec.id;
  END IF;
 END LOOP;
END $$;

CREATE OR REPLACE FUNCTION merchant_group_baseline_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE assigned text;
BEGIN
 IF TG_OP='INSERT' OR COALESCE(NEW.profile->>'group_id','') IS DISTINCT FROM COALESCE(OLD.profile->>'group_id','') THEN
  assigned:=COALESCE(NEW.profile->>'group_id','');
  IF assigned='' THEN NEW.base_group_id:=0;
  ELSIF assigned ~ '^[0-9]{1,19}$' AND assigned::numeric<=9223372036854775807 THEN NEW.base_group_id:=assigned::bigint;
  ELSE RAISE EXCEPTION 'invalid administrator baseline group'; END IF;
  NEW.group_baseline_unresolved:=false;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER merchant_group_baseline_sync BEFORE INSERT OR UPDATE OF profile ON merchants FOR EACH ROW EXECUTE FUNCTION merchant_group_baseline_guard();
