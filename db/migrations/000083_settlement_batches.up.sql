-- A settlement batch classifies original requests. It creates no reservation,
-- approval, transfer or external sending authority.
CREATE TABLE settlement_batches (
 id bigserial PRIMARY KEY,
 batch text UNIQUE NOT NULL CHECK(batch~'^[0-9]{20,27}$'),
 external_no text UNIQUE NOT NULL CHECK(length(external_no) BETWEEN 1 AND 128),
 item_count integer NOT NULL CHECK(item_count BETWEEN 1 AND 100),
 gross_amount bigint NOT NULL CHECK(gross_amount>0),
 fee_amount bigint NOT NULL CHECK(fee_amount>=0),
 net_amount bigint NOT NULL CHECK(net_amount>0 AND net_amount=gross_amount-fee_amount),
 actor_id bigint NOT NULL CHECK(actor_id>0),actor_name text NOT NULL,
 actor_class text NOT NULL CHECK(actor_class IN('super','operator')),
 created_at timestamptz NOT NULL,transaction_id xid8 NOT NULL,
 members_hash bytea NOT NULL CHECK(octet_length(members_hash)=32),
 proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32)
);
CREATE TABLE settlement_batch_items (
 batch_id bigint NOT NULL REFERENCES settlement_batches(id),
 request_id bigint UNIQUE NOT NULL REFERENCES ledger_requests(id),
 position integer NOT NULL CHECK(position BETWEEN 1 AND 100),
 merchant_id bigint NOT NULL REFERENCES merchants(id),
 gross_amount bigint NOT NULL CHECK(gross_amount>0),
 fee_amount bigint NOT NULL CHECK(fee_amount>=0),
 net_amount bigint NOT NULL CHECK(net_amount>0 AND net_amount=gross_amount-fee_amount),
 source_hash bytea NOT NULL CHECK(octet_length(source_hash)=32),
 proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32),
 PRIMARY KEY(batch_id,request_id),UNIQUE(batch_id,position)
);
CREATE TABLE settlement_batch_events (
 id bigserial PRIMARY KEY,batch_id bigint NOT NULL REFERENCES settlement_batches(id),
 operation text NOT NULL CHECK(operation IN('create','archive','complete')),
 actor_id bigint NOT NULL CHECK(actor_id>0),actor_name text NOT NULL,
 actor_class text NOT NULL CHECK(actor_class IN('super','operator')),
 created_at timestamptz NOT NULL,transaction_id xid8 NOT NULL,
 proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32),
 UNIQUE(batch_id,operation)
);
CREATE TRIGGER settlement_batch_immutable BEFORE UPDATE OR DELETE ON settlement_batches FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE TRIGGER settlement_batch_item_immutable BEFORE UPDATE OR DELETE ON settlement_batch_items FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE TRIGGER settlement_batch_event_immutable BEFORE UPDATE OR DELETE ON settlement_batch_events FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
