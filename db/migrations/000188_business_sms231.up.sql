-- Business SMS scene templates are independent from verification-code templates.
-- No phone, gateway secret or token is stored in this public template record.
CREATE TABLE IF NOT EXISTS business_sms_settings231 (
 id boolean PRIMARY KEY DEFAULT true CHECK(id),
 revision bigint NOT NULL CHECK(revision>0),
 templates jsonb NOT NULL CHECK(jsonb_typeof(templates)='object'),
 proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32)
);
