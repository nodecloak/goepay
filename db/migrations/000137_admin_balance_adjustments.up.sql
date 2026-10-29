-- Admin direct balance credit/debit (epay_pro admin user/recharge + user/withdraw;
-- epay_release admin/ajax_user.php changeUserMoney(...,'后台加款'/'后台扣款')). Deep
-- audit a3 F4. The original let an admin arbitrarily add or subtract a merchant's
-- wallet balance, recorded as a pre_record row.
--
-- GOEPAY keeps its invariants: the money move itself is a NEW immutable journal
-- (event_key 'admin-adjust:<id>', posted clearing<->available), idempotent by the
-- journal event_key and additionally by (merchant_id,external_no) here; a debit is
-- rejected by the accounts CHECK(owner_id=0 OR balance>=0) if it would drive the
-- wallet below zero. This table is the actor + reason + direction audit the ledger
-- journal cannot carry on its own, mirroring deposit_actions. It is append-only;
-- a correction is another adjustment, never an UPDATE/DELETE of a row here.
--
-- Idempotent and safe on existing installs (IF NOT EXISTS; no backfill).
CREATE TABLE IF NOT EXISTS admin_balance_adjustments (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 merchant_id bigint NOT NULL REFERENCES merchants(id),
 direction text NOT NULL CHECK(direction IN ('credit','debit')),
 amount bigint NOT NULL CHECK(amount>0),
 reason text NOT NULL CHECK(length(reason) BETWEEN 1 AND 500),
 external_no text NOT NULL CHECK(length(external_no) BETWEEN 1 AND 128),
 actor text NOT NULL CHECK(length(actor) BETWEEN 1 AND 128),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 UNIQUE(merchant_id,external_no)
);
CREATE INDEX IF NOT EXISTS admin_balance_adjustments_merchant_idx ON admin_balance_adjustments(merchant_id,id DESC);
