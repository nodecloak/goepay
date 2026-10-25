-- Migration 18 accepted merged post-purchase merchant snapshots as explicit
-- group assignments. An unrelated name/email edit can contain the purchased
-- group. Reassess only histories predating migration 18, once per merchant.
CREATE TABLE IF NOT EXISTS membership_baseline_repairs (
 merchant_id bigint PRIMARY KEY REFERENCES merchants(id),previous_group_id bigint NOT NULL,
 group_id bigint NOT NULL CHECK(group_id>=0),unresolved boolean NOT NULL,
 audit_id bigint REFERENCES backoffice_audit(id),
 reason text NOT NULL CHECK(reason IN ('pre_purchase_audit','distinct_assignment','post_migration_assignment','explicit_assignment','ambiguous_snapshot','missing_audit','invalid_audit')),
 repaired_at timestamptz NOT NULL DEFAULT now()
);
CREATE OR REPLACE FUNCTION reject_baseline_repair_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'immutable membership baseline repair evidence'; END $$;
DROP TRIGGER IF EXISTS membership_baseline_repairs_immutable ON membership_baseline_repairs;
CREATE TRIGGER membership_baseline_repairs_immutable BEFORE UPDATE OR DELETE ON membership_baseline_repairs FOR EACH ROW EXECUTE FUNCTION reject_baseline_repair_mutation();

DO $$
DECLARE rec record; subsequent record; cutoff timestamptz:=clock_timestamp(); migrated timestamptz;
 evidence_id bigint; evidence_at timestamptz; assigned text; purchased bigint;
 target bigint; ambiguous boolean; why text; valid boolean; trace_group bigint;
BEGIN
 IF to_regclass('schema_migrations') IS NOT NULL THEN
  EXECUTE 'SELECT applied_at FROM schema_migrations WHERE name=$1' INTO migrated USING '000018_membership_baseline.up.sql';
  cutoff:=COALESCE(migrated,cutoff);
 END IF;
 FOR rec IN
  SELECT m.id,m.base_group_id,min(p.created_at) AS first_purchase
  FROM merchants m JOIN membership_purchases p ON p.merchant_id=m.id AND p.created_at<cutoff
  WHERE NOT EXISTS(SELECT 1 FROM membership_baseline_repairs r WHERE r.merchant_id=m.id)
  GROUP BY m.id HAVING min(p.created_at)<cutoff
 LOOP
  evidence_id:=NULL; evidence_at:=NULL; assigned:=NULL; purchased:=NULL;
  target:=0; trace_group:=0; ambiguous:=true; why:='missing_audit'; valid:=false;
  SELECT id,created_at,changes->>'group_id' INTO evidence_id,evidence_at,assigned
   FROM backoffice_audit WHERE resource='merchants' AND resource_id=rec.id
    AND operation IN ('create','update') AND changes ? 'group_id' AND created_at<cutoff
   ORDER BY created_at DESC,id DESC LIMIT 1;
  IF evidence_id IS NOT NULL THEN
   IF assigned='' THEN valid:=true;
   ELSIF assigned ~ '^[0-9]{1,19}$' THEN valid:=assigned::numeric<=9223372036854775807;
   END IF;
   IF NOT valid THEN why:='invalid_audit';
   ELSIF evidence_at<rec.first_purchase THEN
    target:=CASE WHEN assigned='' THEN 0 ELSE assigned::bigint END;
    ambiguous:=false; why:='pre_purchase_audit';
   ELSE
    SELECT group_id INTO purchased FROM membership_purchases
     WHERE merchant_id=rec.id AND created_at<=evidence_at AND created_at<cutoff ORDER BY created_at DESC,id DESC LIMIT 1;
    IF purchased IS NOT NULL AND (CASE WHEN assigned='' THEN 0 ELSE assigned::bigint END)<>purchased THEN
     -- A purchase can only write its own group. A different snapshot group
     -- therefore demonstrates an administrator assignment or explicit clear.
     target:=CASE WHEN assigned='' THEN 0 ELSE assigned::bigint END;
     ambiguous:=false; why:='distinct_assignment';
    ELSE why:='ambiguous_snapshot';
    END IF;
   END IF;
  END IF;
  -- Reconstruct the profile produced by frozen migration 18. It used the last
  -- pre-migration merchant audit even when that snapshot was ambiguous.
  IF valid THEN trace_group:=CASE WHEN assigned='' THEN 0 ELSE assigned::bigint END; END IF;
  FOR subsequent IN
   SELECT id,changes FROM backoffice_audit WHERE resource='merchants' AND resource_id=rec.id
    AND operation IN ('create','update') AND changes ? 'group_id' AND created_at>=cutoff
   ORDER BY created_at,id
  LOOP
   assigned:=subsequent.changes->>'group_id';valid:=false;
   IF assigned='' THEN valid:=true;
   ELSIF assigned ~ '^[0-9]{1,19}$' THEN valid:=assigned::numeric<=9223372036854775807;
   END IF;
   IF NOT valid THEN ambiguous:=true;why:='invalid_audit';evidence_id:=subsequent.id;
   ELSE
    purchased:=CASE WHEN assigned='' THEN 0 ELSE assigned::bigint END;
    IF subsequent.changes->'group_assignment'->>'version'='1'
      AND subsequent.changes->'group_assignment'->>'source' IN ('changed','confirmed')
      AND COALESCE(subsequent.changes->'group_assignment'->>'assigned','')=assigned THEN
     target:=purchased;ambiguous:=false;why:='explicit_assignment';evidence_id:=subsequent.id;
    ELSIF purchased IS DISTINCT FROM trace_group THEN
     -- Since migration 18, a purchase never modifies profile.group_id. An
     -- observed profile change therefore proves administrator assignment;
     -- post-18 purchases must not be compared as legacy pollution evidence.
     target:=purchased;ambiguous:=false;why:='post_migration_assignment';evidence_id:=subsequent.id;
    END IF;
    trace_group:=purchased;
   END IF;
  END LOOP;
  IF ambiguous THEN target:=0; END IF;
  -- Frozen migration 18's BEFORE trigger synchronizes the profile change.
  -- Set the final ambiguity flag separately so that trigger cannot clear it.
  UPDATE merchants SET profile=jsonb_set(profile,'{group_id}',to_jsonb(CASE WHEN target=0 THEN '' ELSE target::text END)),config_revision=config_revision+1 WHERE id=rec.id;
  UPDATE merchants SET base_group_id=target,group_baseline_unresolved=ambiguous WHERE id=rec.id;
  INSERT INTO membership_baseline_repairs(merchant_id,previous_group_id,group_id,unresolved,audit_id,reason)
   VALUES(rec.id,rec.base_group_id,target,ambiguous,evidence_id,why) ON CONFLICT DO NOTHING;
 END LOOP;
END $$;
