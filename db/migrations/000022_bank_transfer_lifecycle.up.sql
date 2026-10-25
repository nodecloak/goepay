-- A fresh install applies all migrations in one transaction; migration 15's
-- zero-balance accounts may still have deferred projection checks pending.
SET CONSTRAINTS account_balance_guard IMMEDIATE;
ALTER TABLE accounts DROP CONSTRAINT accounts_kind_check;
ALTER TABLE accounts ADD CONSTRAINT accounts_kind_check CHECK(kind IN ('available','reserved','clearing','deposit','deposit_reserved','fees','fee_reserved','bank_service_fee_hold','bank_fee_expense'));
ALTER TABLE accounts ADD CONSTRAINT bank_fee_accounts_domain CHECK((kind<>'bank_service_fee_hold' OR (owner_id=0 AND balance>=0)) AND (kind<>'bank_fee_expense' OR (owner_id=0 AND balance<=0)));
INSERT INTO accounts(owner_id,kind) VALUES(0,'bank_service_fee_hold'),(0,'bank_fee_expense') ON CONFLICT DO NOTHING;
SET CONSTRAINTS account_balance_guard DEFERRED;

CREATE TABLE bank_transfer_lifecycle (
 transfer_id bigint PRIMARY KEY REFERENCES provider_transfer_attempts(transfer_id),
 state text NOT NULL DEFAULT 'processing' CHECK(state IN ('processing','succeeded','reversal_pending','reversed','failed')),
 bank_fee_amount bigint CHECK(bank_fee_amount>=0),settled boolean NOT NULL DEFAULT false,return_observed boolean NOT NULL DEFAULT false,
 service_fee_held bigint NOT NULL DEFAULT 0 CHECK(service_fee_held>=0),
 returned_principal bigint NOT NULL DEFAULT 0 CHECK(returned_principal>=0),
 returned_service_fee bigint NOT NULL DEFAULT 0 CHECK(returned_service_fee>=0),
 returned_bank_fee bigint NOT NULL DEFAULT 0 CHECK(returned_bank_fee>=0),
 last_provider_state text NOT NULL DEFAULT '',review_reason text NOT NULL DEFAULT '',
 next_check_at timestamptz,generation bigint NOT NULL DEFAULT 0 CHECK(generation>=0),
 created_at timestamptz NOT NULL DEFAULT now(),updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE OR REPLACE FUNCTION bank_transfer_lifecycle_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.transfer_id<>OLD.transfer_id OR (OLD.bank_fee_amount IS NOT NULL AND NEW.bank_fee_amount IS DISTINCT FROM OLD.bank_fee_amount) OR (OLD.settled AND NOT NEW.settled) OR (OLD.return_observed AND NOT NEW.return_observed) THEN RAISE EXCEPTION 'immutable bank transfer fee and identity'; END IF;
 IF OLD.state IN ('reversed','failed') AND (NEW.state,NEW.settled,NEW.service_fee_held,NEW.returned_principal,NEW.returned_service_fee,NEW.returned_bank_fee) IS DISTINCT FROM (OLD.state,OLD.settled,OLD.service_fee_held,OLD.returned_principal,OLD.returned_service_fee,OLD.returned_bank_fee) THEN RAISE EXCEPTION 'immutable final bank return'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER bank_transfer_lifecycle_immutable BEFORE UPDATE ON bank_transfer_lifecycle FOR EACH ROW EXECUTE FUNCTION bank_transfer_lifecycle_guard();
CREATE TRIGGER bank_transfer_lifecycle_no_delete BEFORE DELETE ON bank_transfer_lifecycle FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE TABLE bank_transfer_observations (
 id bigserial PRIMARY KEY,transfer_id bigint NOT NULL REFERENCES bank_transfer_lifecycle(transfer_id),
 provider_state text NOT NULL,provider_transfer_no text NOT NULL,amount bigint NOT NULL CHECK(amount>0),
 bank_fee_amount bigint CHECK(bank_fee_amount>=0),result_state text NOT NULL,reason text NOT NULL DEFAULT '',
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX bank_transfer_observations_lookup ON bank_transfer_observations(transfer_id,id);
CREATE TRIGGER bank_transfer_observations_immutable BEFORE UPDATE OR DELETE ON bank_transfer_observations FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
