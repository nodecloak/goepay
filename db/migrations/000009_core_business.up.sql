ALTER TABLE merchants ADD COLUMN config_revision bigint NOT NULL DEFAULT 1 CHECK(config_revision>0);
ALTER TABLE accounts DROP CONSTRAINT accounts_kind_check;
ALTER TABLE accounts ADD CONSTRAINT accounts_kind_check CHECK(kind IN ('available','reserved','clearing','deposit','deposit_reserved'));
CREATE TABLE membership_purchases (
 id bigserial PRIMARY KEY, merchant_id bigint NOT NULL REFERENCES merchants(id), group_id bigint NOT NULL REFERENCES backoffice_records(id),
 external_no text NOT NULL, price bigint NOT NULL CHECK(price>=0), group_name text NOT NULL, duration_days integer NOT NULL CHECK(duration_days BETWEEN 1 AND 3660),
 expires_at timestamptz NOT NULL, actor text NOT NULL, created_at timestamptz NOT NULL DEFAULT now(), UNIQUE(merchant_id,external_no)
);
CREATE TABLE memberships (merchant_id bigint PRIMARY KEY REFERENCES merchants(id),group_id bigint NOT NULL REFERENCES backoffice_records(id),expires_at timestamptz NOT NULL);
CREATE TRIGGER membership_purchases_immutable BEFORE UPDATE OR DELETE ON membership_purchases FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE TABLE deposit_actions (
 id bigserial PRIMARY KEY, merchant_id bigint NOT NULL REFERENCES merchants(id), action text NOT NULL CHECK(action IN ('hold','release')),
 external_no text NOT NULL, amount bigint NOT NULL CHECK(amount>0), status text NOT NULL CHECK(status IN ('held','pending','approved','rejected')),
 created_by text NOT NULL, reviewed_by text, created_at timestamptz NOT NULL DEFAULT now(), reviewed_at timestamptz, UNIQUE(merchant_id,external_no)
);
CREATE TABLE deposit_action_events (id bigserial PRIMARY KEY,action_id bigint NOT NULL REFERENCES deposit_actions(id),status text NOT NULL,actor text NOT NULL,created_at timestamptz NOT NULL DEFAULT now());
CREATE TRIGGER deposit_events_immutable BEFORE UPDATE OR DELETE ON deposit_action_events FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE TABLE recharge_intents (id bigserial PRIMARY KEY,order_id bigint NOT NULL UNIQUE REFERENCES orders(id),created_by text NOT NULL,created_at timestamptz NOT NULL DEFAULT now());
