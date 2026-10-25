ALTER TABLE payment_authorizations ADD COLUMN release_state text NOT NULL DEFAULT 'none' CHECK(release_state IN ('none','checking','processing','reversal_pending','unknown','failed','succeeded'));
CREATE TABLE payment_authorization_releases (
 order_id bigint PRIMARY KEY REFERENCES payment_authorizations(order_id),
 channel_id bigint NOT NULL CHECK(channel_id>0),
 request_no text NOT NULL UNIQUE,
 auth_no text NOT NULL,
 amount bigint NOT NULL CHECK(amount>0),
 state text NOT NULL DEFAULT 'checking' CHECK(state IN ('checking','processing','reversal_pending','unknown','failed','succeeded')),
 sent boolean NOT NULL DEFAULT false,
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE FUNCTION guard_authorization_release() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF (NEW.order_id,NEW.channel_id,NEW.request_no,NEW.auth_no,NEW.amount) IS DISTINCT FROM (OLD.order_id,OLD.channel_id,OLD.request_no,OLD.auth_no,OLD.amount) THEN RAISE EXCEPTION 'authorization release identity is immutable'; END IF;
 IF OLD.sent AND NOT NEW.sent OR OLD.state='succeeded' AND NEW.state<>'succeeded' OR OLD.sent AND NEW.state='checking' THEN RAISE EXCEPTION 'authorization release cannot be resent or reversed'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER authorization_release_guard BEFORE UPDATE ON payment_authorization_releases FOR EACH ROW EXECUTE FUNCTION guard_authorization_release();
CREATE TRIGGER authorization_release_no_delete BEFORE DELETE ON payment_authorization_releases FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE FUNCTION guard_capture_after_release() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.capture_state='processing' AND OLD.capture_state<>'processing' AND NEW.release_state<>'none' THEN RAISE EXCEPTION 'capture blocked by authorization release intent'; END IF;
 IF OLD.release_state<>'none' AND NEW.release_state='none' THEN RAISE EXCEPTION 'authorization release intent cannot be reset'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER capture_after_release_guard BEFORE UPDATE ON payment_authorizations FOR EACH ROW EXECUTE FUNCTION guard_capture_after_release();
