-- New sources only. Historical audits/requests retain NULL metadata and all
-- original values. Business UID0 switches are independent from risk notices.
ALTER TABLE backoffice_audit ADD COLUMN business_notice_scene text,
 ADD COLUMN business_notice_transaction xid8,
 ADD CONSTRAINT admin_business_audit_source CHECK(
  business_notice_scene IS NULL AND business_notice_transaction IS NULL
  OR business_notice_scene IS NOT NULL AND business_notice_transaction IS NOT NULL AND actor_id>0 AND (
   business_notice_scene='regaudit' AND resource='enrollment' AND operation='register' AND actor_role='public' AND COALESCE(changes->>'pay'='2',false)
   OR business_notice_scene='domain' AND resource='domains' AND operation IN('create','update') AND actor_role='merchant'));
ALTER TABLE ledger_request_events ADD COLUMN business_notice_transaction xid8,
 ADD CONSTRAINT admin_business_request_source CHECK(business_notice_transaction IS NULL OR status='pending' AND actor~'^merchant:[1-9][0-9]*$');

CREATE TABLE admin_business_notice_events (
 id bigserial PRIMARY KEY,
 merchant_id bigint NOT NULL REFERENCES merchants(id),
 scene text NOT NULL CHECK(scene IN('regaudit','apply','domain','internal_transfer')),
 mode text NOT NULL CHECK(mode IN('0','1','2','4','5')),
 audit_id bigint UNIQUE REFERENCES backoffice_audit(id),
 request_event_id bigint UNIQUE REFERENCES ledger_request_events(id),
 job_id bigint UNIQUE REFERENCES jobs(id),
 source_transaction xid8 NOT NULL,
 created_at timestamptz NOT NULL,
 payload_cipher bytea NOT NULL CHECK(octet_length(payload_cipher) BETWEEN 28 AND 65536),
 body_hash bytea NOT NULL CHECK(octet_length(body_hash)=32),
 proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32),
 CHECK(num_nonnulls(audit_id,request_event_id)=1),
 CHECK((scene IN('regaudit','domain'))=(audit_id IS NOT NULL)),
 CHECK((mode='0')=(job_id IS NULL)),
 UNIQUE(id,job_id,merchant_id)
);
CREATE TABLE admin_business_notice_claims (
 event_id bigint PRIMARY KEY REFERENCES admin_business_notice_events(id),
 job_id bigint NOT NULL REFERENCES jobs(id),merchant_id bigint NOT NULL,
 transport text NOT NULL CHECK(transport IN('smtp','robot','telegram','qqbot')),
 blocked boolean NOT NULL,
 recipient_hash bytea NOT NULL CHECK(octet_length(recipient_hash)=32),
 scope_hash bytea NOT NULL CHECK(octet_length(scope_hash)=32),
 lease_hash bytea NOT NULL CHECK(octet_length(lease_hash)=32),
 created_at timestamptz NOT NULL,transaction_id xid8 NOT NULL,
 proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32),
 FOREIGN KEY(event_id,job_id,merchant_id) REFERENCES admin_business_notice_events(id,job_id,merchant_id),
 UNIQUE(event_id,proof_hash)
);
CREATE TABLE admin_business_notice_results (
 event_id bigint PRIMARY KEY REFERENCES admin_business_notice_claims(event_id),
 claim_hash bytea NOT NULL CHECK(octet_length(claim_hash)=32),
 state text NOT NULL CHECK(state IN('accepted','unknown','not_sent')),
 created_at timestamptz NOT NULL,transaction_id xid8 NOT NULL,
 proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32),
 FOREIGN KEY(event_id,claim_hash) REFERENCES admin_business_notice_claims(event_id,proof_hash)
);
CREATE FUNCTION admin_business_notice_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'immutable administrator business notice'; END $$;
CREATE TRIGGER admin_business_event_immutable BEFORE UPDATE OR DELETE ON admin_business_notice_events FOR EACH ROW EXECUTE FUNCTION admin_business_notice_immutable();
CREATE TRIGGER admin_business_claim_immutable BEFORE UPDATE OR DELETE ON admin_business_notice_claims FOR EACH ROW EXECUTE FUNCTION admin_business_notice_immutable();
CREATE TRIGGER admin_business_result_immutable BEFORE UPDATE OR DELETE ON admin_business_notice_results FOR EACH ROW EXECUTE FUNCTION admin_business_notice_immutable();
CREATE INDEX admin_business_event_history ON admin_business_notice_events(id DESC);
