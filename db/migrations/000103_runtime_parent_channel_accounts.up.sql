-- Parent account configuration is separate from fee/limit policy facts. Old
-- runtime accounts, quotes, attempts and receipts are never backfilled.
CREATE SEQUENCE runtime_parent_channel_ids AS bigint;
CREATE TABLE runtime_parent_channels (
 id bigint PRIMARY KEY CHECK(id>0),
 source_channel_id bigint NOT NULL CHECK(source_channel_id>0),
 pay_type text NOT NULL CHECK(pay_type IN ('wxpay','alipay')),
 name text NOT NULL CHECK(length(name) BETWEEN 1 AND 120),
 revision bigint NOT NULL CHECK(revision>0),
 account_version bigint NOT NULL CHECK(account_version>0),
 archived boolean NOT NULL DEFAULT false,
 legacy_origin boolean NOT NULL DEFAULT false,
 fee_identity bytea NOT NULL CHECK(octet_length(fee_identity)=32),
 fee_account_hash bytea NOT NULL CHECK(octet_length(fee_account_hash)=32),
 fee_product text NOT NULL CHECK(length(fee_product)>0),
 fee_methods text[] NOT NULL CHECK(cardinality(fee_methods)>0),
 proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32),
 created_at timestamptz NOT NULL,
 updated_at timestamptz NOT NULL
);
CREATE UNIQUE INDEX runtime_parent_channel_names ON runtime_parent_channels(lower(name)) WHERE NOT archived;
CREATE TABLE runtime_parent_channel_accounts (
 channel_id bigint NOT NULL REFERENCES runtime_parent_channels(id),
 version bigint NOT NULL CHECK(version>0),
 source_channel_id bigint NOT NULL CHECK(source_channel_id>0),
 pay_type text NOT NULL CHECK(pay_type IN ('wxpay','alipay')),
 nonce bytea NOT NULL CHECK(octet_length(nonce)=12),
 ciphertext bytea NOT NULL CHECK(octet_length(ciphertext) BETWEEN 16 AND 1048592),
 execution_identity bytea NOT NULL CHECK(octet_length(execution_identity)=32),
 account_hash bytea NOT NULL CHECK(octet_length(account_hash)=32),
 product text NOT NULL CHECK(length(product)>0),
 proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(channel_id,version),
 UNIQUE(channel_id,version,pay_type)
);
ALTER TABLE runtime_parent_channels ADD CONSTRAINT runtime_parent_current_account_fk FOREIGN KEY(id,account_version,pay_type) REFERENCES runtime_parent_channel_accounts(channel_id,version,pay_type) DEFERRABLE INITIALLY DEFERRED;
CREATE TABLE runtime_parent_channel_grants (
 channel_id bigint NOT NULL REFERENCES runtime_parent_channels(id),
 merchant_id bigint NOT NULL REFERENCES merchants(id),
 source_channel_id bigint NOT NULL CHECK(source_channel_id>0),
 proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32),
 PRIMARY KEY(channel_id,merchant_id)
);
CREATE INDEX runtime_parent_grants_owner ON runtime_parent_channel_grants(merchant_id,channel_id);
CREATE TABLE runtime_parent_channel_requests (
 actor_name text NOT NULL,
 request_key text NOT NULL CHECK(request_key ~ '^[a-f0-9]{32}$'),
 channel_id bigint NOT NULL REFERENCES runtime_parent_channels(id),
 request_hash bytea NOT NULL CHECK(octet_length(request_hash)=32),
 PRIMARY KEY(actor_name,request_key)
);
CREATE FUNCTION runtime_parent_source_immutable() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN RAISE EXCEPTION 'immutable runtime parent account source';END$$;
CREATE TRIGGER runtime_parent_account_immutable BEFORE UPDATE OR DELETE ON runtime_parent_channel_accounts FOR EACH ROW EXECUTE FUNCTION runtime_parent_source_immutable();
CREATE TRIGGER runtime_parent_grant_immutable BEFORE UPDATE OR DELETE ON runtime_parent_channel_grants FOR EACH ROW EXECUTE FUNCTION runtime_parent_source_immutable();
CREATE TRIGGER runtime_parent_request_immutable BEFORE UPDATE OR DELETE ON runtime_parent_channel_requests FOR EACH ROW EXECUTE FUNCTION runtime_parent_source_immutable();
CREATE FUNCTION runtime_parent_identity_immutable() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN IF NEW.id<>OLD.id OR NEW.source_channel_id<>OLD.source_channel_id OR NEW.pay_type<>OLD.pay_type OR NEW.legacy_origin<>OLD.legacy_origin OR NEW.fee_identity<>OLD.fee_identity OR NEW.fee_account_hash<>OLD.fee_account_hash OR NEW.fee_product<>OLD.fee_product OR NEW.fee_methods<>OLD.fee_methods THEN RAISE EXCEPTION 'immutable runtime parent identity';END IF;RETURN NEW;END$$;
CREATE TRIGGER runtime_parent_identity_immutable BEFORE UPDATE ON runtime_parent_channels FOR EACH ROW EXECUTE FUNCTION runtime_parent_identity_immutable();
CREATE TRIGGER runtime_parent_head_no_delete BEFORE DELETE ON runtime_parent_channels FOR EACH ROW EXECUTE FUNCTION runtime_parent_source_immutable();
CREATE INDEX runtime_parent_audit_heads ON backoffice_audit(resource_id) WHERE resource='runtime-parent-channels' AND operation='runtime-parent-write';

-- Separate execution source: the 96/99 quote/MAC representation is unchanged.
-- Every new managed route commits this source alongside its original quote.
CREATE TABLE runtime_parent_payment_sources (
 merchant_id bigint NOT NULL,
 out_trade_no text NOT NULL,
 channel_id bigint NOT NULL,
 account_version bigint NOT NULL,
 facts jsonb NOT NULL CHECK(jsonb_typeof(facts)='object'),
 proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32),
 PRIMARY KEY(merchant_id,out_trade_no),
 FOREIGN KEY(merchant_id,out_trade_no) REFERENCES provider_route_assignments(merchant_id,out_trade_no),
 FOREIGN KEY(channel_id,account_version) REFERENCES runtime_parent_channel_accounts(channel_id,version)
);
CREATE TRIGGER runtime_parent_payment_source_immutable BEFORE UPDATE OR DELETE ON runtime_parent_payment_sources FOR EACH ROW EXECUTE FUNCTION runtime_parent_source_immutable();
CREATE INDEX runtime_parent_payment_origin ON backoffice_audit(resource_id,(changes->>'out_trade_no')) WHERE resource='runtime-parent-payment-intents' AND operation='runtime-parent-intent';

-- Transfer products have an independent identity and never reuse a payment
-- product or fee quote as evidence of the original payout account.
CREATE TABLE runtime_parent_transfer_sources (
 transfer_id bigint PRIMARY KEY REFERENCES transfers(id),
 channel_id bigint NOT NULL,
 account_version bigint NOT NULL,
 facts jsonb NOT NULL CHECK(jsonb_typeof(facts)='object'),
 proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32),
 FOREIGN KEY(channel_id,account_version) REFERENCES runtime_parent_channel_accounts(channel_id,version)
);
CREATE TRIGGER runtime_parent_transfer_source_immutable BEFORE UPDATE OR DELETE ON runtime_parent_transfer_sources FOR EACH ROW EXECUTE FUNCTION runtime_parent_source_immutable();
CREATE INDEX runtime_parent_transfer_origin ON backoffice_audit(resource_id) WHERE resource='runtime-parent-transfer-intents' AND operation='runtime-parent-transfer-intent';

CREATE TABLE runtime_parent_receiver_sources (
 receiver_id bigint PRIMARY KEY REFERENCES channel_sharing_receivers(id),
 channel_id bigint NOT NULL,
 account_version bigint NOT NULL,
 facts jsonb NOT NULL CHECK(jsonb_typeof(facts)='object'),
 proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32),
 FOREIGN KEY(channel_id,account_version) REFERENCES runtime_parent_channel_accounts(channel_id,version)
);
CREATE TRIGGER runtime_parent_receiver_source_immutable BEFORE UPDATE OR DELETE ON runtime_parent_receiver_sources FOR EACH ROW EXECUTE FUNCTION runtime_parent_source_immutable();
CREATE INDEX runtime_parent_receiver_origin ON backoffice_audit(resource_id) WHERE resource='runtime-parent-receiver-intents' AND operation='runtime-parent-receiver-intent';

-- The original administrator test action uses ordinary payment orchestration.
-- This immutable registry reserves one request/number before provider I/O; it
-- is not a payment receipt and cannot credit funds or rearm an existing send.
CREATE TABLE runtime_parent_payment_tests (
 actor_id bigint NOT NULL,
 request_key text NOT NULL CHECK(request_key ~ '^[a-f0-9]{32}$'),
 channel_id bigint NOT NULL REFERENCES runtime_parent_channels(id),
 merchant_id bigint NOT NULL REFERENCES merchants(id),
 out_trade_no text NOT NULL,
 facts jsonb NOT NULL CHECK(jsonb_typeof(facts)='object'),
 proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32),
 PRIMARY KEY(actor_id,request_key),
 UNIQUE(merchant_id,out_trade_no)
);
CREATE TRIGGER runtime_parent_payment_test_immutable BEFORE UPDATE OR DELETE ON runtime_parent_payment_tests FOR EACH ROW EXECUTE FUNCTION runtime_parent_source_immutable();
CREATE INDEX runtime_parent_payment_test_origin ON backoffice_audit((changes->>'request_id'),(changes->>'actor_id')) WHERE resource='runtime-parent-payment-tests' AND operation='runtime-parent-payment-test';
