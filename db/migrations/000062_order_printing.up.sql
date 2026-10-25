CREATE TABLE order_print_origins (
 order_id bigint PRIMARY KEY REFERENCES orders(id),kind text NOT NULL CHECK(kind IN ('api','recharge','collection')),
 collection_code_id bigint REFERENCES backoffice_records(id),proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32),
 CHECK((kind='collection')=(collection_code_id IS NOT NULL))
);
CREATE TRIGGER order_print_origin_immutable BEFORE UPDATE OR DELETE ON order_print_origins FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE TABLE printing_platform_configs (
 id bigserial PRIMARY KEY,revision bigint UNIQUE NOT NULL CHECK(revision>0),enabled boolean NOT NULL,provider text NOT NULL CHECK(provider IN ('local_fake','excelsecu')),
 credential_cipher bytea,actor_id bigint NOT NULL CHECK(actor_id>0),actor_name text NOT NULL,proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32),created_at timestamptz NOT NULL DEFAULT transaction_timestamp()
);
CREATE TABLE printing_platform_head(singleton boolean PRIMARY KEY DEFAULT true CHECK(singleton),config_id bigint NOT NULL UNIQUE REFERENCES printing_platform_configs(id),revision bigint NOT NULL);
CREATE TABLE printing_merchant_configs (
 id bigserial PRIMARY KEY,merchant_id bigint NOT NULL REFERENCES merchants(id),revision bigint NOT NULL CHECK(revision>0),mode smallint NOT NULL CHECK(mode BETWEEN 0 AND 2),
 device_cipher bytea,copies smallint NOT NULL CHECK(copies BETWEEN 1 AND 5),voice boolean NOT NULL,actor_name text NOT NULL,
 proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32),created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),UNIQUE(merchant_id,revision),CHECK(mode=0 OR device_cipher IS NOT NULL)
);
CREATE TABLE printing_merchant_heads(merchant_id bigint PRIMARY KEY REFERENCES merchants(id),config_id bigint UNIQUE NOT NULL REFERENCES printing_merchant_configs(id),revision bigint NOT NULL);
CREATE TRIGGER printing_platform_immutable BEFORE UPDATE OR DELETE ON printing_platform_configs FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE TRIGGER printing_merchant_immutable BEFORE UPDATE OR DELETE ON printing_merchant_configs FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE TABLE payment_print_sources (
 order_id bigint PRIMARY KEY REFERENCES orders(id),merchant_id bigint NOT NULL REFERENCES merchants(id),body jsonb NOT NULL,proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32),created_at timestamptz NOT NULL DEFAULT transaction_timestamp()
);
CREATE TRIGGER payment_print_source_immutable BEFORE UPDATE OR DELETE ON payment_print_sources FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE TABLE printing_intents (
 id bigserial PRIMARY KEY,merchant_id bigint NOT NULL REFERENCES merchants(id),order_id bigint NOT NULL REFERENCES payment_print_sources(order_id),
 source text NOT NULL CHECK(source IN ('auto','manual')),external_no text NOT NULL CHECK(length(external_no) BETWEEN 1 AND 64),
 request_no text NOT NULL UNIQUE,platform_config_id bigint NOT NULL REFERENCES printing_platform_configs(id),merchant_config_id bigint NOT NULL REFERENCES printing_merchant_configs(id),
 body jsonb NOT NULL,proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32),created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
 UNIQUE(merchant_id,external_no)
);
CREATE UNIQUE INDEX printing_one_auto ON printing_intents(order_id) WHERE source='auto';
CREATE TRIGGER printing_intent_immutable BEFORE UPDATE OR DELETE ON printing_intents FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE TABLE printing_claims (
 intent_id bigint PRIMARY KEY REFERENCES printing_intents(id),token bytea NOT NULL CHECK(octet_length(token)=32),proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32),created_at timestamptz NOT NULL DEFAULT transaction_timestamp()
);
CREATE TRIGGER printing_claim_immutable BEFORE UPDATE OR DELETE ON printing_claims FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE TABLE printing_results (
 intent_id bigint PRIMARY KEY REFERENCES printing_claims(intent_id),state text NOT NULL CHECK(state IN ('accepted','rejected','unknown')),
 reason text NOT NULL CHECK(reason IN ('gateway_accepted','gateway_rejected','transport_unknown','connector_unavailable','scope_changed','claim_recovered')),
 proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32),created_at timestamptz NOT NULL DEFAULT transaction_timestamp()
);
CREATE TRIGGER printing_result_immutable BEFORE UPDATE OR DELETE ON printing_results FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE TABLE printing_recoveries (
 id bigserial PRIMARY KEY,intent_id bigint NOT NULL REFERENCES printing_intents(id),merchant_id bigint NOT NULL REFERENCES merchants(id),
 expected_task_id bigint NOT NULL UNIQUE REFERENCES jobs(id),new_task_id bigint NOT NULL UNIQUE REFERENCES jobs(id),sequence_no smallint NOT NULL CHECK(sequence_no BETWEEN 1 AND 5),
 actor_name text NOT NULL,proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32),created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),UNIQUE(intent_id,sequence_no)
);
CREATE TRIGGER printing_recovery_immutable BEFORE UPDATE OR DELETE ON printing_recoveries FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE FUNCTION printing_head_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'immutable printing head'; END IF;
 IF TG_OP='INSERT' AND NEW.revision<>1 OR TG_OP='UPDATE' AND NEW.revision<>OLD.revision+1 THEN RAISE EXCEPTION 'invalid printing config revision'; END IF;
 IF TG_TABLE_NAME='printing_platform_head' THEN
  IF NOT EXISTS(SELECT 1 FROM printing_platform_configs c WHERE (c.id,c.revision)=(NEW.config_id,NEW.revision)) THEN RAISE EXCEPTION 'foreign printing head'; END IF;
 ELSE
  IF NOT EXISTS(SELECT 1 FROM printing_merchant_configs c WHERE (c.id,c.merchant_id,c.revision)=(NEW.config_id,NEW.merchant_id,NEW.revision)) THEN RAISE EXCEPTION 'foreign printing merchant head'; END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER printing_platform_head_guard BEFORE INSERT OR UPDATE OR DELETE ON printing_platform_head FOR EACH ROW EXECUTE FUNCTION printing_head_guard();
CREATE TRIGGER printing_merchant_head_guard BEFORE INSERT OR UPDATE OR DELETE ON printing_merchant_heads FOR EACH ROW EXECUTE FUNCTION printing_head_guard();
CREATE VIEW printing_intent_invariants AS
SELECT i.id, s.merchant_id=i.merchant_id AND m.merchant_id=i.merchant_id AND m.mode>0 AND p.enabled
 AND EXISTS(SELECT 1 FROM backoffice_audit a WHERE a.resource='printing-intents' AND a.resource_id=i.id AND a.operation='create' AND a.actor_name=i.body->>'actor_name'
 AND ((i.source='manual' AND a.actor_role='merchant' AND a.actor_id=i.merchant_id) OR (i.source='auto' AND a.actor_role='system' AND a.actor_id=0 AND a.actor_name='receipt-printer')))
 AS valid FROM printing_intents i JOIN payment_print_sources s ON s.order_id=i.order_id JOIN printing_platform_configs p ON p.id=i.platform_config_id JOIN printing_merchant_configs m ON m.id=i.merchant_config_id;
CREATE FUNCTION printing_intent_complete() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN IF NOT EXISTS(SELECT 1 FROM printing_intent_invariants WHERE id=NEW.id AND valid IS TRUE) THEN RAISE EXCEPTION 'incomplete printing intent'; END IF; RETURN NULL; END $$;
CREATE CONSTRAINT TRIGGER printing_intent_complete AFTER INSERT ON printing_intents DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION printing_intent_complete();
