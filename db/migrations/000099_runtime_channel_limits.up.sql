CREATE TABLE runtime_channel_limits (
 channel_id bigint PRIMARY KEY CHECK(channel_id>0),
 pay_type text NOT NULL CHECK(pay_type IN ('alipay','wxpay')),
 identity_hash bytea NOT NULL CHECK(octet_length(identity_hash)=32),
 daytop bigint NOT NULL CHECK(daytop>=0),
 daymaxorder bigint NOT NULL CHECK(daymaxorder>=0),
 paymin bigint NOT NULL CHECK(paymin>=0),
 paymax bigint NOT NULL CHECK(paymax>=0),
 timestart integer CHECK(timestart BETWEEN 0 AND 23),
 timestop integer CHECK(timestop BETWEEN 0 AND 23),
 rate_limit_seconds bigint NOT NULL CHECK(rate_limit_seconds BETWEEN 0 AND 2147483647),
 rate_limit_count bigint NOT NULL CHECK(rate_limit_count>=0),
 revision bigint NOT NULL CHECK(revision>0),
 proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32),
 CHECK(paymax=0 OR paymin<=paymax),
 CHECK((timestart IS NULL)=(timestop IS NULL))
);
CREATE TRIGGER runtime_channel_limits_no_delete BEFORE DELETE ON runtime_channel_limits FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE TABLE runtime_channel_success_events (
 order_id bigint PRIMARY KEY REFERENCES orders(id),
 channel_id bigint NOT NULL CHECK(channel_id>0),
 merchant_id bigint NOT NULL REFERENCES merchants(id),
 trade_no text NOT NULL,
 day date NOT NULL,
 realmoney bigint NOT NULL CHECK(realmoney>0),
 observed_at timestamptz NOT NULL,
 provider_trade_no text NOT NULL,
 revision bigint NOT NULL CHECK(revision>0),
 previous_hash bytea,
 proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32),
 UNIQUE(channel_id,day,revision)
);
CREATE TRIGGER runtime_channel_success_events_immutable BEFORE UPDATE OR DELETE ON runtime_channel_success_events FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE TABLE runtime_channel_success_days (
 channel_id bigint NOT NULL CHECK(channel_id>0),day date NOT NULL,
 revision bigint NOT NULL CHECK(revision>0),success_count bigint NOT NULL CHECK(success_count>0),
 realmoney numeric(40,0) NOT NULL CHECK(realmoney>0),
 last_order_id bigint NOT NULL REFERENCES runtime_channel_success_events(order_id),
 last_hash bytea NOT NULL CHECK(octet_length(last_hash)=32),
 proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32),
 PRIMARY KEY(channel_id,day)
);
CREATE TRIGGER runtime_channel_success_days_no_delete BEFORE DELETE ON runtime_channel_success_days FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
-- The admission/success audit anchor is scoped to one parent and one UTC+8
-- day; do not rescan every historical success audit for each new request.
CREATE INDEX runtime_channel_success_audit_day ON backoffice_audit
 ((changes->>'channel_id'),(changes->>'day'))
 WHERE resource='runtime-channel-success' AND operation='runtime-channel-success';
