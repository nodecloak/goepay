ALTER TABLE enrollment_challenges DROP CONSTRAINT enrollment_challenges_purpose_check;
ALTER TABLE enrollment_challenges ADD CONSTRAINT enrollment_challenges_purpose_check
 CHECK(purpose IN ('register','recover','contact-old','contact-new'));

-- Births and transitions are immutable. The application authenticates the
-- complete transition chain with its enrollment domain key before using it.
CREATE TABLE email_binding_flows197 (
 id text PRIMARY KEY CHECK(length(id)=43),
 merchant_id bigint NOT NULL REFERENCES merchants(id),
 body bytea NOT NULL CHECK(octet_length(body) BETWEEN 1 AND 4096),
 mac bytea NOT NULL CHECK(octet_length(mac)=32),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE INDEX email_binding_flows197_merchant ON email_binding_flows197(merchant_id);
CREATE TRIGGER email_binding_flows197_immutable BEFORE UPDATE OR DELETE ON email_binding_flows197
 FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE TABLE email_binding_events197 (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 flow_id text NOT NULL REFERENCES email_binding_flows197(id),
 revision integer NOT NULL CHECK(revision>0),
 kind text NOT NULL CHECK(kind IN ('send-old','sent-old','failed-old','verify-old','send-new','sent-new','failed-new','complete','close')),
 body bytea NOT NULL CHECK(octet_length(body) BETWEEN 1 AND 4096),
 previous_mac bytea NOT NULL CHECK(octet_length(previous_mac)=32),
 mac bytea NOT NULL CHECK(octet_length(mac)=32),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 UNIQUE(flow_id,revision)
);
CREATE UNIQUE INDEX email_binding_events197_complete ON email_binding_events197(flow_id) WHERE kind='complete';
CREATE TRIGGER email_binding_events197_immutable BEFORE UPDATE OR DELETE ON email_binding_events197
 FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();

-- A changed claim carries its original completion identity. Losing the journal
-- cannot silently downgrade that claim to a pre-migration registration claim.
ALTER TABLE enrollment_emails ADD COLUMN binding_flow197 text REFERENCES email_binding_flows197(id);

-- Registration remains the only unguarded initial INSERT. Existing recovery
-- identities cannot be updated through profile JSON or a general account edit.
CREATE FUNCTION guard_verified_email_change197() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE flow text; facts jsonb; birth jsonb;
BEGIN
 IF TG_OP<>'UPDATE' THEN RAISE EXCEPTION 'verified email claim is immutable'; END IF;
 flow:=current_setting('goepay.email_binding197',true);
 SELECT convert_from(e.body,'UTF8')::jsonb,convert_from(f.body,'UTF8')::jsonb INTO facts,birth
 FROM email_binding_events197 e JOIN email_binding_flows197 f ON f.id=e.flow_id
 WHERE e.flow_id=flow AND e.kind='complete' AND f.merchant_id=OLD.merchant_id;
 IF facts IS NULL OR birth IS NULL OR OLD.merchant_id<>NEW.merchant_id OR OLD.username<>NEW.username OR OLD.role<>NEW.role
  OR birth->>'original_email'<>OLD.email OR facts->>'email'<>NEW.email OR NEW.binding_flow197 IS DISTINCT FROM flow
  OR encode(NEW.email_hash,'hex')<>facts->>'email_hash'
 THEN RAISE EXCEPTION 'verified email transition required'; END IF;
 RETURN NEW;
END $$;
DROP TRIGGER enrollment_email_immutable ON enrollment_emails;
CREATE TRIGGER enrollment_email_immutable BEFORE UPDATE OR DELETE ON enrollment_emails
 FOR EACH ROW EXECUTE FUNCTION guard_verified_email_change197();
