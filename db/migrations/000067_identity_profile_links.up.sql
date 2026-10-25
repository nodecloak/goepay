-- A standalone identity URLLink is a single irreversible external operation.
-- It does not consume or cancel the original mini SDK identity request.
CREATE TABLE profile_identity_links (
 request_id text PRIMARY KEY REFERENCES profile_identity_lookup_requests(id),
 parent_proof bytea NOT NULL CHECK(octet_length(parent_proof)=32),
 parent_created_at timestamptz NOT NULL,
 actor_class text NOT NULL,
 created_at timestamptz NOT NULL,
 expires_at timestamptz NOT NULL,
 requested_expires_at timestamptz NOT NULL,
 birth_xid xid8 NOT NULL DEFAULT pg_current_xact_id(),
 source_proof bytea NOT NULL CHECK(octet_length(source_proof)=32),
 state_proof bytea NOT NULL CHECK(octet_length(state_proof)=32),
 status text NOT NULL CHECK(status IN ('creating','ready','failed','unknown')),
 link_cipher bytea,
 completed_at timestamptz,
 CHECK(parent_created_at<=created_at AND created_at<expires_at AND expires_at<=parent_created_at+interval '5 minutes'),
 CHECK(requested_expires_at=date_trunc('second',expires_at)),
 CHECK((status='creating' AND completed_at IS NULL AND link_cipher IS NULL) OR
       (status='ready' AND completed_at IS NOT NULL AND link_cipher IS NOT NULL AND octet_length(link_cipher)>28) OR
       (status IN ('failed','unknown') AND completed_at IS NOT NULL AND link_cipher IS NULL))
);
CREATE TABLE profile_identity_link_events (
 request_id text NOT NULL REFERENCES profile_identity_links(request_id),
 operation text NOT NULL CHECK(operation IN ('created','claimed','ready','failed','unknown')),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(request_id,operation)
);
CREATE FUNCTION guard_profile_identity_link() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'identity link immutable'; END IF;
 IF TG_OP='INSERT' THEN
  IF NEW.birth_xid<>pg_current_xact_id() OR NEW.status<>'creating' THEN RAISE EXCEPTION 'identity link birth'; END IF;
  RETURN NEW;
 END IF;
 IF ROW(NEW.request_id,NEW.parent_proof,NEW.parent_created_at,NEW.actor_class,NEW.created_at,NEW.expires_at,NEW.requested_expires_at,NEW.birth_xid,NEW.source_proof)
 IS DISTINCT FROM ROW(OLD.request_id,OLD.parent_proof,OLD.parent_created_at,OLD.actor_class,OLD.created_at,OLD.expires_at,OLD.requested_expires_at,OLD.birth_xid,OLD.source_proof)
 OR OLD.status<>'creating' OR NEW.status NOT IN ('ready','failed','unknown')
 THEN RAISE EXCEPTION 'identity link transition'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER profile_identity_link_guard BEFORE INSERT OR UPDATE OR DELETE ON profile_identity_links FOR EACH ROW EXECUTE FUNCTION guard_profile_identity_link();
CREATE FUNCTION guard_profile_identity_link_event() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE q profile_identity_links;
BEGIN
 IF TG_OP<>'INSERT' THEN RAISE EXCEPTION 'identity link audit immutable'; END IF;
 SELECT * INTO STRICT q FROM profile_identity_links WHERE request_id=NEW.request_id;
 IF (NEW.operation IN ('created','claimed') AND (q.birth_xid<>pg_current_xact_id() OR q.status<>'creating'))
 OR (NEW.operation NOT IN ('created','claimed') AND NEW.operation<>q.status)
 THEN RAISE EXCEPTION 'identity link audit transition'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER profile_identity_link_event_guard BEFORE INSERT OR UPDATE OR DELETE ON profile_identity_link_events FOR EACH ROW EXECUTE FUNCTION guard_profile_identity_link_event();
CREATE FUNCTION check_profile_identity_link_audit() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE q profile_identity_links;
BEGIN
 SELECT * INTO STRICT q FROM profile_identity_links WHERE request_id=NEW.request_id;
 IF (SELECT count(*) FROM profile_identity_link_events WHERE request_id=q.request_id AND operation IN ('created','claimed'))<>2
 OR (q.status<>'creating' AND NOT EXISTS(SELECT 1 FROM profile_identity_link_events WHERE request_id=q.request_id AND operation=q.status))
 THEN RAISE EXCEPTION 'identity link requires audit'; END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER profile_identity_link_audit_complete AFTER INSERT OR UPDATE ON profile_identity_links DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION check_profile_identity_link_audit();
