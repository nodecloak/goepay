CREATE TABLE crypto_addresses (
 network text NOT NULL CHECK(network IN ('tron','ethereum')), address text NOT NULL,
 role text NOT NULL CHECK(role IN ('deposit','treasury','hot')), used boolean NOT NULL DEFAULT false,
 PRIMARY KEY(network,address)
);
CREATE TABLE crypto_invoices (
 id text PRIMARY KEY, merchant_id bigint NOT NULL REFERENCES merchants(id), out_trade_no text NOT NULL,
 network text NOT NULL, address text NOT NULL, amount bigint NOT NULL CHECK(amount>0 AND amount<=9000000000000000),
 received bigint NOT NULL DEFAULT 0 CHECK(received>=0), fee bigint NOT NULL CHECK(fee>=0 AND fee<=amount),
 state text NOT NULL DEFAULT 'pending' CHECK(state IN ('pending','underpaid','paid','overpaid','expired','late')),
 credited boolean NOT NULL DEFAULT false, token_hash bytea NOT NULL, token_cipher bytea NOT NULL,
 notify_url text NOT NULL DEFAULT '',return_url text NOT NULL DEFAULT '',cny_amount bigint NOT NULL DEFAULT 0 CHECK(cny_amount>=0),name text NOT NULL DEFAULT 'product',param text NOT NULL DEFAULT '',version text NOT NULL DEFAULT 'native' CHECK(version IN ('native','v1','v2')),
 expires_at timestamptz NOT NULL,created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 UNIQUE(merchant_id,out_trade_no),UNIQUE(network,address),FOREIGN KEY(network,address) REFERENCES crypto_addresses(network,address)
);
CREATE TABLE crypto_deposits (
 network text NOT NULL,tx_id text NOT NULL,event_index integer NOT NULL CHECK(event_index>=0),
 block_height bigint NOT NULL CHECK(block_height>=0),block_hash text NOT NULL,address text NOT NULL,
 invoice_id text REFERENCES crypto_invoices(id),amount bigint NOT NULL CHECK(amount>0 AND amount<=9000000000000000),
 occurred_at timestamptz NOT NULL,created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(network,tx_id,event_index)
);
CREATE TABLE crypto_accounts (
 id bigserial PRIMARY KEY,network text NOT NULL CHECK(network IN ('tron','ethereum')),owner_id bigint NOT NULL,
 kind text NOT NULL CHECK(kind IN ('available','reserved','clearing','fees','suspense')),
 balance bigint NOT NULL DEFAULT 0 CHECK(balance>=0 OR (owner_id=0 AND kind='clearing')),
 UNIQUE(network,owner_id,kind)
);
CREATE TABLE crypto_journals(id bigserial PRIMARY KEY,network text NOT NULL,reference text NOT NULL,created_at timestamptz NOT NULL DEFAULT clock_timestamp(),UNIQUE(network,reference));
CREATE TABLE crypto_entries(id bigserial PRIMARY KEY,journal_id bigint NOT NULL REFERENCES crypto_journals(id),account_id bigint NOT NULL REFERENCES crypto_accounts(id),amount bigint NOT NULL CHECK(amount<>0));
CREATE FUNCTION crypto_apply_entry() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM crypto_accounts a JOIN crypto_journals j ON j.id=NEW.journal_id WHERE a.id=NEW.account_id AND a.network=j.network) THEN RAISE EXCEPTION 'asset mismatch'; END IF;
 UPDATE crypto_accounts SET balance=balance+NEW.amount WHERE id=NEW.account_id;
 RETURN NEW;
END $$;
CREATE TRIGGER crypto_apply_entry AFTER INSERT ON crypto_entries FOR EACH ROW EXECUTE FUNCTION crypto_apply_entry();
CREATE FUNCTION crypto_balance_journal() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF (SELECT COALESCE(sum(amount),0) FROM crypto_entries WHERE journal_id=NEW.id)<>0 OR (SELECT count(*) FROM crypto_entries WHERE journal_id=NEW.id)<2 THEN RAISE EXCEPTION 'unbalanced asset journal'; END IF;
 RETURN NEW;
END $$;
CREATE CONSTRAINT TRIGGER crypto_balance_journal AFTER INSERT ON crypto_journals DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION crypto_balance_journal();
CREATE FUNCTION crypto_immutable() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'immutable crypto evidence'; END $$;
CREATE TRIGGER crypto_entries_immutable BEFORE UPDATE OR DELETE ON crypto_entries FOR EACH ROW EXECUTE FUNCTION crypto_immutable();
CREATE TRIGGER crypto_journals_immutable BEFORE UPDATE OR DELETE ON crypto_journals FOR EACH ROW EXECUTE FUNCTION crypto_immutable();
CREATE TRIGGER crypto_deposits_immutable BEFORE UPDATE OR DELETE ON crypto_deposits FOR EACH ROW EXECUTE FUNCTION crypto_immutable();
CREATE TABLE crypto_withdrawals (
 id text PRIMARY KEY,merchant_id bigint NOT NULL REFERENCES merchants(id),request_id text NOT NULL,
 kind text NOT NULL CHECK(kind IN ('settlement','refund','exception_refund','sweep')),invoice_id text REFERENCES crypto_invoices(id),
 network text NOT NULL CHECK(network IN ('tron','ethereum')),address text NOT NULL,source text NOT NULL DEFAULT '',
 amount bigint NOT NULL CHECK(amount>0 AND amount<=9000000000000000),
 state text NOT NULL DEFAULT 'requested' CHECK(state IN ('requested','approved','preparing','prepared','signing','signed','broadcasting','unknown','confirmed','failed','cancelled')),
 approved_by bigint NOT NULL DEFAULT 0,cancel_requested boolean NOT NULL DEFAULT false,tx_id text NOT NULL DEFAULT '',prepared jsonb,signed jsonb,nonce bigint,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),last_dispatch_at timestamptz NOT NULL DEFAULT '-infinity',UNIQUE(merchant_id,request_id),UNIQUE(network,tx_id)
);
-- Empty transaction ids are not transaction identities.
ALTER TABLE crypto_withdrawals DROP CONSTRAINT crypto_withdrawals_network_tx_id_key;
CREATE UNIQUE INDEX crypto_withdrawals_tx ON crypto_withdrawals(network,tx_id) WHERE tx_id<>'';
CREATE INDEX crypto_withdrawals_dispatch ON crypto_withdrawals(network,last_dispatch_at,created_at,id) WHERE state NOT IN ('requested','confirmed','failed','cancelled');
CREATE TABLE crypto_scan_cursors(network text PRIMARY KEY,height bigint NOT NULL,block_hash text NOT NULL DEFAULT '');
CREATE TABLE crypto_sweep_queue(network text NOT NULL,address text NOT NULL,revision bigint NOT NULL DEFAULT 1,failures integer NOT NULL DEFAULT 0 CHECK(failures>=0),next_attempt timestamptz NOT NULL DEFAULT clock_timestamp(),PRIMARY KEY(network,address));
CREATE INDEX crypto_sweep_due ON crypto_sweep_queue(network,next_attempt);
CREATE TABLE crypto_nonces(network text NOT NULL,address text NOT NULL,next_nonce bigint NOT NULL,PRIMARY KEY(network,address));
CREATE TABLE crypto_released_nonces(network text NOT NULL,address text NOT NULL,nonce bigint NOT NULL CHECK(nonce>=0),cancellation_id text NOT NULL REFERENCES crypto_withdrawals(id),PRIMARY KEY(network,address,nonce));
CREATE TABLE crypto_signer_cancellations(withdrawal_id text PRIMARY KEY REFERENCES crypto_withdrawals(id),binding_sha256 text NOT NULL,created_at timestamptz NOT NULL DEFAULT clock_timestamp());
CREATE TABLE crypto_notifications(invoice_id text PRIMARY KEY REFERENCES crypto_invoices(id),attempts integer NOT NULL DEFAULT 0,next_attempt timestamptz NOT NULL DEFAULT clock_timestamp(),delivered boolean NOT NULL DEFAULT false);
CREATE TABLE crypto_audit(id bigserial PRIMARY KEY,actor_id bigint NOT NULL,operation text NOT NULL,reference text NOT NULL,created_at timestamptz NOT NULL DEFAULT clock_timestamp());
CREATE TABLE crypto_recipients(id text PRIMARY KEY,merchant_id bigint NOT NULL REFERENCES merchants(id),network text NOT NULL CHECK(network IN ('tron','ethereum')),address text NOT NULL,state text NOT NULL DEFAULT 'requested' CHECK(state IN ('requested','approved','revoked')),usable_at timestamptz NOT NULL DEFAULT clock_timestamp()+interval '24 hours',approved_by bigint NOT NULL DEFAULT 0,UNIQUE(merchant_id,network,address));
CREATE TABLE crypto_network_fees(network text NOT NULL,tx_id text NOT NULL,asset text NOT NULL CHECK(asset IN ('TRX','ETH')),atomic_amount numeric(78,0) NOT NULL CHECK(atomic_amount>=0),PRIMARY KEY(network,tx_id));
CREATE TRIGGER crypto_network_fees_immutable BEFORE UPDATE OR DELETE ON crypto_network_fees FOR EACH ROW EXECUTE FUNCTION crypto_immutable();
-- Both assets share the merchant's external order number namespace. The same
-- lock is taken by either table before checking the other table, including
-- direct INSERT paths outside the HTTP handlers.
CREATE FUNCTION crypto_order_namespace() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 PERFORM pg_advisory_xact_lock(hashtextextended(NEW.merchant_id::text||':'||NEW.out_trade_no,201));
 IF TG_TABLE_NAME='orders' THEN
  IF EXISTS(SELECT 1 FROM crypto_invoices WHERE merchant_id=NEW.merchant_id AND out_trade_no=NEW.out_trade_no) THEN RAISE EXCEPTION 'external order number belongs to USDT'; END IF;
 ELSE
  IF to_regclass('orders') IS NOT NULL THEN
   IF EXISTS(SELECT 1 FROM orders WHERE merchant_id=NEW.merchant_id AND out_trade_no=NEW.out_trade_no) THEN RAISE EXCEPTION 'external order number belongs to CNY'; END IF;
  END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER crypto_order_namespace BEFORE INSERT ON crypto_invoices FOR EACH ROW EXECUTE FUNCTION crypto_order_namespace();
DO $$ BEGIN IF to_regclass('orders') IS NOT NULL THEN CREATE TRIGGER crypto_order_namespace BEFORE INSERT ON orders FOR EACH ROW EXECUTE FUNCTION crypto_order_namespace(); END IF; END $$;

-- A journal is complete at the end of its creation transaction. Later inserts
-- cannot reuse its already-validated identity to create unbalanced postings.
ALTER TABLE crypto_journals ADD COLUMN creation_xid xid8 NOT NULL DEFAULT pg_current_xact_id();
CREATE FUNCTION crypto_entry_creation_transaction() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM crypto_journals WHERE id=NEW.journal_id AND creation_xid=pg_current_xact_id()) THEN
  RAISE EXCEPTION 'cannot append to a committed crypto journal';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER crypto_entry_creation_transaction BEFORE INSERT ON crypto_entries FOR EACH ROW EXECUTE FUNCTION crypto_entry_creation_transaction();

-- Check the committed projection rather than NEW.balance: one journal can
-- create/update an account several times before all entries have been added.
CREATE FUNCTION crypto_account_entry_balance() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF EXISTS(SELECT 1 FROM crypto_accounts a WHERE a.id=NEW.id AND a.balance<>(SELECT COALESCE(sum(e.amount),0) FROM crypto_entries e WHERE e.account_id=a.id)) THEN
  RAISE EXCEPTION 'crypto account balance differs from ledger entries';
 END IF;
 RETURN NEW;
END $$;
CREATE CONSTRAINT TRIGGER crypto_account_entry_balance AFTER INSERT OR UPDATE ON crypto_accounts DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION crypto_account_entry_balance();
