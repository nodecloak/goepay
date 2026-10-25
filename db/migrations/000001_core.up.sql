CREATE SEQUENCE IF NOT EXISTS trade_no_seq AS bigint START WITH 1000000000000000000 MAXVALUE 9223372036854775807 NO CYCLE;
CREATE TABLE IF NOT EXISTS merchants (
 id bigint PRIMARY KEY CHECK(id>0), name text NOT NULL, api_key bytea NOT NULL,
 rsa_public_key text NOT NULL DEFAULT '', signature_algorithm text NOT NULL DEFAULT 'MD5' CHECK(signature_algorithm IN ('MD5','RSA')), enabled boolean NOT NULL DEFAULT true,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS orders (
 id bigserial PRIMARY KEY, trade_no text UNIQUE NOT NULL, merchant_id bigint NOT NULL REFERENCES merchants(id),
 out_trade_no text NOT NULL, name text NOT NULL, type text NOT NULL, amount bigint NOT NULL CHECK(amount>0),
 version text NOT NULL DEFAULT 'v1' CHECK(version IN ('v1','v2')), notify_url text NOT NULL DEFAULT '', return_url text NOT NULL DEFAULT '', param text NOT NULL DEFAULT '',
 status text NOT NULL DEFAULT 'created' CHECK(status IN ('created','submitting','pending','paid','closed','failed','unknown')),
 provider_event_id text, paid_at timestamptz, created_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(merchant_id,out_trade_no)
);
CREATE TABLE IF NOT EXISTS accounts (
 id bigserial PRIMARY KEY, owner_id bigint NOT NULL, kind text NOT NULL CHECK(kind IN ('available','reserved','clearing')),
 balance bigint NOT NULL DEFAULT 0, UNIQUE(owner_id,kind), CHECK(owner_id=0 OR balance>=0)
);
INSERT INTO accounts(owner_id,kind) VALUES(0,'clearing') ON CONFLICT DO NOTHING;
CREATE TABLE IF NOT EXISTS journals (
 id bigserial PRIMARY KEY, event_key text UNIQUE NOT NULL, description text NOT NULL, transaction_id xid8 NOT NULL DEFAULT pg_current_xact_id(), created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS ledger_entries (
 id bigserial PRIMARY KEY, journal_id bigint NOT NULL REFERENCES journals(id), account_id bigint NOT NULL REFERENCES accounts(id),
 amount bigint NOT NULL CHECK(amount<>0), created_at timestamptz NOT NULL DEFAULT now()
);
CREATE OR REPLACE FUNCTION reject_ledger_mutation() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'immutable financial record'; END $$;
CREATE TRIGGER ledger_immutable BEFORE UPDATE OR DELETE ON ledger_entries FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE TRIGGER journal_immutable BEFORE UPDATE OR DELETE ON journals FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE OR REPLACE FUNCTION check_journal_balance() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF (SELECT count(*)<2 OR COALESCE(sum(amount),0)<>0 FROM ledger_entries WHERE journal_id=NEW.id) THEN
 RAISE EXCEPTION 'unbalanced journal %',NEW.id; END IF; RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER journal_balanced AFTER INSERT ON journals DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION check_journal_balance();
CREATE TABLE IF NOT EXISTS refunds (
 id bigserial PRIMARY KEY, merchant_id bigint NOT NULL REFERENCES merchants(id), order_id bigint NOT NULL REFERENCES orders(id),
 refund_no text NOT NULL, amount bigint NOT NULL CHECK(amount>0), status text NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','unknown','succeeded','failed')),
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(), UNIQUE(merchant_id,refund_no)
);
CREATE TABLE IF NOT EXISTS transfers (
 id bigserial PRIMARY KEY, merchant_id bigint NOT NULL REFERENCES merchants(id), transfer_no text NOT NULL,
 amount bigint NOT NULL CHECK(amount>0), status text NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','unknown','succeeded','failed')),
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(), UNIQUE(merchant_id,transfer_no)
);
CREATE TABLE IF NOT EXISTS inbox (
 provider text NOT NULL, event_id text NOT NULL, order_id bigint REFERENCES orders(id), created_at timestamptz NOT NULL DEFAULT now(), PRIMARY KEY(provider,event_id)
);
CREATE TABLE IF NOT EXISTS outbox (
 id bigserial PRIMARY KEY, event_key text UNIQUE NOT NULL, kind text NOT NULL, payload jsonb NOT NULL, created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS jobs (
 id bigserial PRIMARY KEY, event_key text UNIQUE NOT NULL, kind text NOT NULL, payload jsonb NOT NULL,
 state text NOT NULL DEFAULT 'pending' CHECK(state IN ('pending','running','succeeded','dead')),
 attempts integer NOT NULL DEFAULT 0, max_attempts integer NOT NULL DEFAULT 12,
 run_at timestamptz NOT NULL DEFAULT now(), lease_until timestamptz, lease_token text, last_error text NOT NULL DEFAULT '', created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX jobs_claim ON jobs(run_at,id) WHERE state IN ('pending','running');
CREATE INDEX orders_merchant_created ON orders(merchant_id,created_at DESC,id DESC);
CREATE INDEX refunds_order ON refunds(order_id);
CREATE INDEX ledger_account ON ledger_entries(account_id,id);

CREATE OR REPLACE FUNCTION check_ledger_append() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN IF NOT EXISTS(SELECT 1 FROM journals WHERE id=NEW.journal_id AND transaction_id=pg_current_xact_id()) THEN
 RAISE EXCEPTION 'cannot append to historical journal'; END IF; RETURN NEW; END $$;
CREATE TRIGGER ledger_append_guard BEFORE INSERT ON ledger_entries FOR EACH ROW EXECUTE FUNCTION check_ledger_append();
CREATE OR REPLACE FUNCTION check_account_balance() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN IF (SELECT balance FROM accounts WHERE id=NEW.id)<>(SELECT COALESCE(sum(amount),0) FROM ledger_entries WHERE account_id=NEW.id) THEN
 RAISE EXCEPTION 'account balance does not match journal'; END IF; RETURN NULL; END $$;
CREATE CONSTRAINT TRIGGER account_balance_guard AFTER INSERT OR UPDATE ON accounts DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION check_account_balance();

