CREATE TABLE referral_tokens (
 id bigserial PRIMARY KEY,merchant_id bigint NOT NULL REFERENCES merchants(id),version bigint NOT NULL CHECK(version>0),
 enabled boolean NOT NULL,token_hash bytea UNIQUE,token_cipher bytea,token_masked text NOT NULL DEFAULT '' CHECK(length(token_masked)<=32),
 actor_name text NOT NULL CHECK(length(actor_name) BETWEEN 1 AND 128),proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32),
 created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),expires_at timestamptz,UNIQUE(merchant_id,version),
 CHECK(enabled AND token_hash IS NOT NULL AND octet_length(token_hash)=32 AND token_cipher IS NOT NULL AND octet_length(token_cipher)>=60 AND length(token_masked)>0 AND expires_at IS NOT NULL AND expires_at>created_at OR
       NOT enabled AND token_hash IS NULL AND token_cipher IS NULL AND token_masked='' AND expires_at IS NULL)
);
CREATE TABLE referral_token_current (
 merchant_id bigint PRIMARY KEY REFERENCES merchants(id),token_id bigint NOT NULL UNIQUE REFERENCES referral_tokens(id),version bigint NOT NULL CHECK(version>0)
);
CREATE TRIGGER referral_token_immutable BEFORE UPDATE OR DELETE ON referral_tokens FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE FUNCTION referral_token_current_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE r referral_tokens%ROWTYPE;
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'referral token current pointer cannot be removed'; END IF;
 SELECT * INTO STRICT r FROM referral_tokens WHERE id=NEW.token_id;
 IF (r.merchant_id,r.version) IS DISTINCT FROM (NEW.merchant_id,NEW.version) OR
  TG_OP='INSERT' AND NEW.version<>1 OR TG_OP='UPDATE' AND (NEW.merchant_id<>OLD.merchant_id OR NEW.version<>OLD.version+1)
 THEN RAISE EXCEPTION 'referral token version must advance exactly once'; END IF;
 IF NOT EXISTS(SELECT 1 FROM merchants WHERE id=NEW.merchant_id AND enabled) THEN RAISE EXCEPTION 'referral owner is unavailable'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER referral_token_current_identity BEFORE INSERT OR UPDATE OR DELETE ON referral_token_current FOR EACH ROW EXECUTE FUNCTION referral_token_current_guard();
CREATE FUNCTION referral_token_complete_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE mid bigint; current_version bigint;
BEGIN
 mid:=NEW.merchant_id;
 SELECT version INTO current_version FROM referral_token_current WHERE merchant_id=mid;
 IF current_version IS NULL OR (SELECT count(*) FROM referral_tokens WHERE merchant_id=mid)<>current_version OR (SELECT max(version) FROM referral_tokens WHERE merchant_id=mid)<>current_version OR
  EXISTS(SELECT 1 FROM referral_tokens t WHERE t.merchant_id=mid AND NOT EXISTS(
   SELECT 1 FROM backoffice_audit b WHERE b.resource='referral-tokens' AND b.resource_id=t.id AND b.actor_role='merchant' AND b.actor_id=t.merchant_id AND b.actor_name=t.actor_name
    AND b.operation=CASE WHEN NOT t.enabled THEN 'revoke' WHEN t.version=1 THEN 'issue' ELSE 'rotate' END
    AND b.changes->>'version'=t.version::text AND b.changes->>'merchant_id'=t.merchant_id::text AND b.changes->>'enabled'=t.enabled::text))
 THEN RAISE EXCEPTION 'referral token lacks its owner audit and complete version history'; END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER referral_token_complete AFTER INSERT ON referral_tokens DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION referral_token_complete_guard();
CREATE CONSTRAINT TRIGGER referral_token_current_complete AFTER INSERT OR UPDATE ON referral_token_current DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION referral_token_complete_guard();

CREATE TABLE referral_relations (
 id bigserial PRIMARY KEY,invited_id bigint NOT NULL UNIQUE REFERENCES merchants(id),promoter_id bigint NOT NULL REFERENCES merchants(id),
 token_id bigint NOT NULL REFERENCES referral_tokens(id),token_version bigint NOT NULL CHECK(token_version>0),
 challenge_id text NOT NULL UNIQUE REFERENCES enrollment_challenges(id),proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32),
 created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),CHECK(invited_id<>promoter_id)
);
CREATE INDEX referral_relation_promoter ON referral_relations(promoter_id,invited_id);
CREATE TRIGGER referral_relation_immutable BEFORE UPDATE OR DELETE ON referral_relations FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE FUNCTION referral_merchant_birth_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF (NEW.id,NEW.created_at) IS DISTINCT FROM (OLD.id,OLD.created_at) THEN RAISE EXCEPTION 'immutable merchant birth identity'; END IF;RETURN NEW;
END $$;
CREATE TRIGGER referral_merchant_birth BEFORE UPDATE OF id,created_at ON merchants FOR EACH ROW EXECUTE FUNCTION referral_merchant_birth_guard();
CREATE FUNCTION referral_relation_birth_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 PERFORM pg_advisory_xact_lock(74973256);
 IF NEW.created_at<>transaction_timestamp() OR NOT EXISTS(SELECT 1 FROM merchants WHERE id=NEW.invited_id AND created_at=transaction_timestamp() AND enabled) OR
  NOT EXISTS(SELECT 1 FROM referral_tokens t JOIN referral_token_current c ON c.token_id=t.id AND c.merchant_id=t.merchant_id JOIN merchants p ON p.id=t.merchant_id
   WHERE t.id=NEW.token_id AND (t.merchant_id,t.version)=(NEW.promoter_id,NEW.token_version) AND t.enabled AND p.enabled AND t.expires_at>clock_timestamp()) OR
  EXISTS(WITH RECURSIVE ancestors(id,path) AS (
   SELECT NEW.promoter_id,ARRAY[NEW.promoter_id] UNION ALL
   SELECT r.promoter_id,a.path||r.promoter_id FROM referral_relations r JOIN ancestors a ON r.invited_id=a.id WHERE NOT r.promoter_id=ANY(a.path)
  ) SELECT 1 FROM ancestors WHERE id=NEW.invited_id)
 THEN RAISE EXCEPTION 'referral relation must be born with a new verified registration and a current promoter'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER referral_relation_birth BEFORE INSERT ON referral_relations FOR EACH ROW EXECUTE FUNCTION referral_relation_birth_guard();
CREATE VIEW referral_relation_evidence AS
SELECT r.id,
 i.created_at=r.created_at AND r.invited_id<>r.promoter_id AND t.enabled AND (t.merchant_id,t.version)=(r.promoter_id,r.token_version)
 AND EXISTS(SELECT 1 FROM enrollment_emails e JOIN console_users u ON u.username=e.username AND u.role='merchant' AND u.subject_id=e.merchant_id
  JOIN enrollment_challenges c ON c.id=r.challenge_id AND c.email_hash=e.email_hash
  WHERE e.merchant_id=r.invited_id AND e.verified_at>=i.created_at AND c.purpose='register' AND c.state='sent' AND c.consumed_at IS NOT NULL AND c.consumed_at>=i.created_at AND c.consumed_at<c.expires_at AND c.target_merchant_id IS NULL)
 AND EXISTS(SELECT 1 FROM backoffice_audit b WHERE b.resource='enrollment' AND b.resource_id=r.invited_id AND b.operation='register' AND b.actor_role='public' AND b.actor_id=r.invited_id)
 AND EXISTS(SELECT 1 FROM backoffice_audit b WHERE b.resource='referral-relations' AND b.resource_id=r.id AND b.operation='bind' AND b.actor_role='public' AND b.actor_id=r.invited_id
  AND b.changes->>'invited_id'=r.invited_id::text AND b.changes->>'promoter_id'=r.promoter_id::text AND b.changes->>'token_version'=r.token_version::text) AS valid
FROM referral_relations r JOIN merchants i ON i.id=r.invited_id JOIN referral_tokens t ON t.id=r.token_id;
CREATE FUNCTION referral_relation_complete_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM referral_relation_evidence WHERE id=NEW.id AND valid IS TRUE) OR NOT EXISTS(SELECT 1 FROM referral_tokens t JOIN referral_token_current c ON c.token_id=t.id WHERE t.id=NEW.token_id AND t.enabled AND t.expires_at>clock_timestamp()) THEN RAISE EXCEPTION 'referral relation lacks its verified registration and original audit'; END IF;RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER referral_relation_complete AFTER INSERT ON referral_relations DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION referral_relation_complete_guard();
