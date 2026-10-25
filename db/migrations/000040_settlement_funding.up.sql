ALTER TABLE accounts ADD COLUMN scope_id bigint NOT NULL DEFAULT 0;
ALTER TABLE accounts DROP CONSTRAINT accounts_owner_id_kind_key;
ALTER TABLE accounts ADD CONSTRAINT accounts_owner_kind_scope_key UNIQUE(owner_id,kind,scope_id);
ALTER TABLE accounts DROP CONSTRAINT accounts_kind_check;
ALTER TABLE accounts ADD CONSTRAINT accounts_kind_check CHECK(kind IN ('available','reserved','clearing','deposit','deposit_reserved','fees','fee_reserved','bank_service_fee_hold','bank_fee_expense','order_income_hold','settlement_reserve'));
ALTER TABLE accounts ADD CONSTRAINT accounts_scope_domain CHECK((kind='settlement_reserve' AND owner_id>0 AND scope_id>0) OR (kind<>'settlement_reserve' AND scope_id=0));

CREATE TABLE settlement_fundings (
 request_id bigint PRIMARY KEY REFERENCES ledger_requests(id),
 account_id bigint UNIQUE NOT NULL REFERENCES accounts(id),
 gross_amount bigint NOT NULL CHECK(gross_amount>0),
 reserve_journal_id bigint UNIQUE NOT NULL REFERENCES journals(id),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE TRIGGER settlement_funding_immutable BEFORE UPDATE OR DELETE ON settlement_fundings FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE FUNCTION settlement_account_identity_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF OLD.kind='settlement_reserve' AND (TG_OP='DELETE' OR NEW.owner_id<>OLD.owner_id OR NEW.kind<>OLD.kind OR NEW.scope_id<>OLD.scope_id) THEN RAISE EXCEPTION 'immutable settlement account identity'; END IF;
 RETURN CASE WHEN TG_OP='DELETE' THEN OLD ELSE NEW END;
END $$;
CREATE TRIGGER settlement_account_identity BEFORE UPDATE OR DELETE ON accounts FOR EACH ROW EXECUTE FUNCTION settlement_account_identity_guard();

CREATE VIEW settlement_funding_invariants AS
SELECT f.request_id,
 r.kind='settlements' AND r.amount=f.gross_amount AND a.owner_id=r.merchant_id AND a.kind='settlement_reserve' AND a.scope_id=r.id
 AND j.event_key='request:'||r.id::text||':reserve'
 AND (SELECT count(*)=2 FROM ledger_entries e WHERE e.journal_id=j.id)
 AND (SELECT COALESCE(sum(e.amount),0) FROM ledger_entries e WHERE e.journal_id=j.id AND e.account_id=a.id)=f.gross_amount
 AND (SELECT COALESCE(sum(e.amount),0) FROM ledger_entries e JOIN accounts x ON x.id=e.account_id WHERE e.journal_id=j.id AND x.owner_id=r.merchant_id AND x.kind='available' AND x.scope_id=0)=-f.gross_amount
 AND CASE WHEN r.status IN ('pending','approved') THEN
   a.balance=f.gross_amount AND (SELECT count(*)=1 FROM ledger_entries e WHERE e.account_id=a.id)
 WHEN r.status='rejected' THEN
   a.balance=0 AND (SELECT count(*)=2 FROM ledger_entries e WHERE e.account_id=a.id)
   AND EXISTS(SELECT 1 FROM journals q WHERE q.event_key='request:'||r.id::text||':review'
    AND (SELECT count(*)=2 FROM ledger_entries e WHERE e.journal_id=q.id)
    AND (SELECT COALESCE(sum(e.amount),0) FROM ledger_entries e WHERE e.journal_id=q.id AND e.account_id=a.id)=-f.gross_amount
    AND (SELECT COALESCE(sum(e.amount),0) FROM ledger_entries e JOIN accounts x ON x.id=e.account_id WHERE e.journal_id=q.id AND x.owner_id=r.merchant_id AND x.kind='available' AND x.scope_id=0)=f.gross_amount)
 ELSE false END AS valid
FROM settlement_fundings f JOIN ledger_requests r ON r.id=f.request_id JOIN accounts a ON a.id=f.account_id JOIN journals j ON j.id=f.reserve_journal_id;

CREATE FUNCTION settlement_funding_deferred_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE rid bigint;
BEGIN
 IF TG_TABLE_NAME='accounts' THEN IF NEW.kind<>'settlement_reserve' THEN RETURN NULL; END IF; rid:=NEW.scope_id;
 ELSIF TG_TABLE_NAME='ledger_entries' THEN SELECT scope_id INTO rid FROM accounts WHERE id=NEW.account_id AND kind='settlement_reserve'; IF rid IS NULL THEN RETURN NULL; END IF;
 ELSIF TG_TABLE_NAME='ledger_requests' THEN rid:=NEW.id; IF NOT EXISTS(SELECT 1 FROM settlement_fundings WHERE request_id=rid) THEN RETURN NULL; END IF;
 ELSE rid:=NEW.request_id; END IF;
 IF NOT EXISTS(SELECT 1 FROM settlement_funding_invariants WHERE request_id=rid AND valid) THEN RAISE EXCEPTION 'invalid settlement funding evidence'; END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER settlement_funding_valid AFTER INSERT ON settlement_fundings DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION settlement_funding_deferred_guard();
CREATE CONSTRAINT TRIGGER settlement_account_funding AFTER INSERT OR UPDATE ON accounts DEFERRABLE INITIALLY DEFERRED FOR EACH ROW WHEN(NEW.kind='settlement_reserve') EXECUTE FUNCTION settlement_funding_deferred_guard();
CREATE CONSTRAINT TRIGGER settlement_entry_funding AFTER INSERT ON ledger_entries DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION settlement_funding_deferred_guard();
CREATE CONSTRAINT TRIGGER settlement_request_funding AFTER UPDATE ON ledger_requests DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION settlement_funding_deferred_guard();

CREATE TABLE settlement_recipient_configs (
 id bigserial PRIMARY KEY, merchant_id bigint NOT NULL REFERENCES merchants(id),
 recipient_type text NOT NULL CHECK(recipient_type IN ('alipay','wxpay','bank')),
 app_id text NOT NULL CHECK(length(app_id)<=128),
 identity_hash bytea NOT NULL CHECK(octet_length(identity_hash)=32),
 recipient_cipher bytea NOT NULL CHECK(octet_length(recipient_cipher)>28),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 UNIQUE(merchant_id,id), CHECK(recipient_type<>'wxpay' OR length(app_id)>0)
);
CREATE TRIGGER settlement_config_immutable BEFORE UPDATE OR DELETE ON settlement_recipient_configs FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE TABLE settlement_recipient_current (
 merchant_id bigint PRIMARY KEY REFERENCES merchants(id),config_id bigint NOT NULL,
 FOREIGN KEY(merchant_id,config_id) REFERENCES settlement_recipient_configs(merchant_id,id)
);
CREATE TABLE settlement_recipients (
 request_id bigint PRIMARY KEY REFERENCES settlement_fundings(request_id),
 config_id bigint NOT NULL REFERENCES settlement_recipient_configs(id),
 identity_hash bytea NOT NULL CHECK(octet_length(identity_hash)=32),
 recipient_cipher bytea NOT NULL CHECK(octet_length(recipient_cipher)>28),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE FUNCTION settlement_recipient_binding_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM ledger_requests r JOIN settlement_recipient_configs c ON c.id=NEW.config_id AND c.merchant_id=r.merchant_id WHERE r.id=NEW.request_id AND r.kind='settlements' AND c.identity_hash=NEW.identity_hash) THEN RAISE EXCEPTION 'invalid settlement recipient binding'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER settlement_recipient_binding BEFORE INSERT ON settlement_recipients FOR EACH ROW EXECUTE FUNCTION settlement_recipient_binding_guard();
CREATE TRIGGER settlement_recipient_immutable BEFORE UPDATE OR DELETE ON settlement_recipients FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
