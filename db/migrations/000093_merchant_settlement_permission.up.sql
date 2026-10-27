-- An independent revision for the original merchant settlement permission.
-- Existing original financial facts, scopes and proofs are not rewritten.
CREATE FUNCTION merchant_settlement_permission(profile jsonb) RETURNS text
LANGUAGE sql IMMUTABLE STRICT AS $$
 SELECT CASE WHEN jsonb_typeof(profile)<>'object' THEN 'invalid'
 WHEN jsonb_typeof(v) IN ('number','string') AND v#>>'{}' IN ('0','1') THEN v#>>'{}'
 ELSE 'invalid' END
 FROM (SELECT COALESCE(profile->'settle',profile->'settle_status','1'::jsonb) AS v) s
$$;
ALTER TABLE merchants ADD COLUMN settlement_permission_revision bigint NOT NULL DEFAULT 1 CHECK(settlement_permission_revision>0);
CREATE FUNCTION merchant_settlement_permission_revision_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='INSERT' THEN
  IF NEW.settlement_permission_revision<>1 THEN RAISE EXCEPTION 'initial settlement permission revision must be1'; END IF;
 ELSE
  IF NEW.settlement_permission_revision<>OLD.settlement_permission_revision THEN RAISE EXCEPTION 'settlement permission revision cannot be assigned'; END IF;
  IF merchant_settlement_permission(NEW.profile) IS DISTINCT FROM merchant_settlement_permission(OLD.profile) THEN
   NEW.settlement_permission_revision:=OLD.settlement_permission_revision+1;
  END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER merchant_settlement_permission_revision BEFORE INSERT OR UPDATE ON merchants FOR EACH ROW EXECUTE FUNCTION merchant_settlement_permission_revision_guard();
