-- Buyer authorization is not money received by the platform merchant.
CREATE TABLE payment_authorizations (
 order_id bigint PRIMARY KEY REFERENCES orders(id),
 channel_id bigint NOT NULL CHECK(channel_id>0),
 auth_no text NOT NULL CHECK(length(auth_no) BETWEEN 1 AND 256),
 buyer_id text NOT NULL CHECK(length(buyer_id) BETWEEN 1 AND 256),
 amount bigint NOT NULL CHECK(amount>0),
 capture_state text NOT NULL DEFAULT 'none' CHECK(capture_state IN ('none','processing','unknown','failed','succeeded')),
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(channel_id,auth_no)
);
CREATE FUNCTION guard_payment_authorization() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF (NEW.order_id,NEW.channel_id,NEW.auth_no,NEW.buyer_id,NEW.amount) IS DISTINCT FROM (OLD.order_id,OLD.channel_id,OLD.auth_no,OLD.buyer_id,OLD.amount) THEN RAISE EXCEPTION 'authorization identity is immutable'; END IF;
 IF OLD.capture_state='succeeded' AND NEW.capture_state<>'succeeded' OR OLD.capture_state<>'none' AND NEW.capture_state='none' OR OLD.capture_state IN ('unknown','failed') AND NEW.capture_state='processing' THEN RAISE EXCEPTION 'authorization capture cannot be resent'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER payment_authorization_guard BEFORE UPDATE ON payment_authorizations FOR EACH ROW EXECUTE FUNCTION guard_payment_authorization();
CREATE TRIGGER payment_authorization_no_delete BEFORE DELETE ON payment_authorizations FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
