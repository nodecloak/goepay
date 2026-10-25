-- Notification destinations are encrypted, explicit configuration. They are
-- never an authenticated login identity or a source of payment authority.
CREATE TABLE merchant_notice_profiles (
 id bigserial PRIMARY KEY,
 merchant_id bigint NOT NULL REFERENCES merchants(id),
 revision bigint NOT NULL CHECK(revision>0),
 previous_id bigint REFERENCES merchant_notice_profiles(id),
 previous_hash bytea CHECK(previous_hash IS NULL OR octet_length(previous_hash)=32),
 payload_cipher bytea NOT NULL CHECK(octet_length(payload_cipher) BETWEEN 28 AND 65536),
 body_hash bytea NOT NULL CHECK(octet_length(body_hash)=32),
 profile_hash bytea NOT NULL CHECK(octet_length(profile_hash)=32),
 actor_role text NOT NULL CHECK(actor_role IN ('merchant','admin')),
 actor_id bigint NOT NULL CHECK(actor_id>0),actor_name text NOT NULL CHECK(length(actor_name)>0),actor_class text NOT NULL DEFAULT '',
 created_at timestamptz NOT NULL,
 UNIQUE(merchant_id,revision),
 CHECK((revision=1)=(previous_id IS NULL)),CHECK((previous_id IS NULL)=(previous_hash IS NULL)),
 CHECK(actor_role='merchant' AND actor_id=merchant_id AND actor_class='' OR actor_role='admin' AND actor_class IN ('super','operator'))
);
CREATE TABLE merchant_notice_current (
 merchant_id bigint PRIMARY KEY REFERENCES merchants(id),
 profile_id bigint NOT NULL UNIQUE REFERENCES merchant_notice_profiles(id),
 revision bigint NOT NULL CHECK(revision>0),
 profile_hash bytea NOT NULL CHECK(octet_length(profile_hash)=32)
);
CREATE FUNCTION merchant_notice_profile_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE prior bigint; rev bigint; proof bytea; active boolean;
BEGIN
 IF TG_OP<>'INSERT' THEN RAISE EXCEPTION 'immutable notice profile'; END IF;
 SELECT enabled INTO active FROM merchants WHERE id=NEW.merchant_id FOR UPDATE;
 IF active IS DISTINCT FROM true THEN RAISE EXCEPTION 'notice owner unavailable'; END IF;
 SELECT profile_id,revision,profile_hash INTO prior,rev,proof FROM merchant_notice_current WHERE merchant_id=NEW.merchant_id;
 IF NEW.revision<>COALESCE(rev,0)+1 OR NEW.previous_id IS DISTINCT FROM prior OR NEW.previous_hash IS DISTINCT FROM proof
 THEN RAISE EXCEPTION 'notice profile must advance original revision'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER merchant_notice_profile_identity BEFORE INSERT OR UPDATE OR DELETE ON merchant_notice_profiles FOR EACH ROW EXECUTE FUNCTION merchant_notice_profile_guard();
CREATE FUNCTION merchant_notice_current_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE p merchant_notice_profiles%ROWTYPE;
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'notice head cannot be deleted'; END IF;
 PERFORM id FROM merchants WHERE id=NEW.merchant_id FOR UPDATE;
 SELECT * INTO p FROM merchant_notice_profiles WHERE id=NEW.profile_id;
 IF p.id IS NULL OR (p.merchant_id,p.revision,p.profile_hash) IS DISTINCT FROM (NEW.merchant_id,NEW.revision,NEW.profile_hash)
 THEN RAISE EXCEPTION 'notice head requires original profile'; END IF;
 IF TG_OP='INSERT' THEN
  IF p.revision<>1 OR p.previous_id IS NOT NULL THEN RAISE EXCEPTION 'notice head cannot be backfilled'; END IF;
 ELSE
  IF NEW.merchant_id<>OLD.merchant_id OR NEW.revision<>OLD.revision+1 OR p.previous_id<>OLD.profile_id OR p.previous_hash<>OLD.profile_hash
  THEN RAISE EXCEPTION 'notice head cannot roll back'; END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER merchant_notice_current_identity BEFORE INSERT OR UPDATE OR DELETE ON merchant_notice_current FOR EACH ROW EXECUTE FUNCTION merchant_notice_current_guard();
CREATE FUNCTION merchant_notice_profile_complete() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM merchant_notice_current c JOIN merchant_notice_profiles p ON p.id=c.profile_id
  WHERE c.merchant_id=NEW.merchant_id AND (p.merchant_id,p.revision,p.profile_hash)=(c.merchant_id,c.revision,c.profile_hash)
  AND c.revision=(SELECT max(q.revision) FROM merchant_notice_profiles q WHERE q.merchant_id=c.merchant_id))
 THEN RAISE EXCEPTION 'incomplete notice configuration'; END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER merchant_notice_profile_complete_row AFTER INSERT ON merchant_notice_profiles DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION merchant_notice_profile_complete();
CREATE CONSTRAINT TRIGGER merchant_notice_profile_complete_head AFTER INSERT OR UPDATE ON merchant_notice_current DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION merchant_notice_profile_complete();

-- Source facts are born in the original verified payment transaction. Saving
-- preferences, reconfirming an old receipt, or changing a transport cannot
-- manufacture another event for the same payment.
ALTER TABLE console_login_audit ADD COLUMN subject_id bigint,
 ADD COLUMN auth_version bigint, ADD COLUMN source_transaction xid8,
 ADD CONSTRAINT console_login_notice_metadata CHECK(
 (subject_id IS NULL AND auth_version IS NULL AND source_transaction IS NULL)
 OR (subject_id IS NOT NULL AND auth_version IS NOT NULL AND subject_id>0 AND auth_version>0 AND source_transaction IS NOT NULL AND role='merchant' AND success));
ALTER TABLE backoffice_audit ADD COLUMN notice_source_transaction xid8,
 ADD CONSTRAINT merchant_notice_domain_audit_metadata CHECK(notice_source_transaction IS NULL OR (resource='domains' AND operation='update' AND actor_role='admin' AND actor_id>0));
CREATE TABLE merchant_notice_timer_sources (
 id bigserial PRIMARY KEY,merchant_id bigint NOT NULL REFERENCES merchants(id),
 profile_id bigint NOT NULL REFERENCES merchant_notice_profiles(id),
 profile_hash bytea NOT NULL CHECK(octet_length(profile_hash)=32),
 scene text NOT NULL CHECK(scene IN('balance','group','daily_report')),
 business_key text NOT NULL CHECK(length(business_key) BETWEEN 1 AND 64),
 body_hash bytea NOT NULL CHECK(octet_length(body_hash)=32),
 source_hash bytea NOT NULL CHECK(octet_length(source_hash)=32),
 source_transaction xid8 NOT NULL DEFAULT pg_current_xact_id(),created_at timestamptz NOT NULL,
 UNIQUE(merchant_id,scene,business_key)
);
CREATE FUNCTION merchant_notice_timer_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP<>'INSERT' THEN RAISE EXCEPTION 'immutable notice timer source'; END IF;
 IF NEW.source_transaction<>pg_current_xact_id() OR NEW.created_at<transaction_timestamp() OR NEW.created_at>clock_timestamp()
 OR NOT EXISTS(SELECT 1 FROM merchant_notice_current c JOIN merchants m ON m.id=c.merchant_id WHERE c.merchant_id=NEW.merchant_id AND m.enabled AND (c.profile_id,c.profile_hash)=(NEW.profile_id,NEW.profile_hash))
 THEN RAISE EXCEPTION 'notice timer requires original profile, transaction and database clock'; END IF;
 IF NEW.scene IN('balance','daily_report') AND NEW.business_key<>to_char(NEW.created_at AT TIME ZONE 'Asia/Shanghai','YYYY-MM-DD')
 THEN RAISE EXCEPTION 'notice timer cannot backfill a business day'; END IF;
 IF NEW.scene='group' THEN
  IF NEW.business_key !~ '^purchase:[1-9][0-9]*$' THEN RAISE EXCEPTION 'notice membership requires original purchase'; END IF;
  IF NOT EXISTS(SELECT 1 FROM membership_purchases p JOIN memberships m ON m.merchant_id=p.merchant_id WHERE p.id=substring(NEW.business_key from 10)::bigint AND p.merchant_id=NEW.merchant_id AND (m.group_id,m.expires_at)=(p.group_id,p.expires_at) AND p.expires_at<=NEW.created_at AND p.expires_at>=(SELECT min(created_at) FROM merchant_notice_profiles WHERE merchant_id=p.merchant_id) AND p.id=(SELECT max(q.id) FROM membership_purchases q WHERE q.merchant_id=p.merchant_id))
  THEN RAISE EXCEPTION 'notice membership requires original latest expired purchase'; END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER merchant_notice_timer_identity BEFORE INSERT OR UPDATE OR DELETE ON merchant_notice_timer_sources FOR EACH ROW EXECUTE FUNCTION merchant_notice_timer_guard();
CREATE TABLE merchant_notice_events (
 id bigserial PRIMARY KEY,
 merchant_id bigint NOT NULL REFERENCES merchants(id),
 order_id bigint UNIQUE REFERENCES orders(id),
 login_audit_id bigint UNIQUE REFERENCES console_login_audit(id),
 settlement_id bigint UNIQUE REFERENCES settlement_payouts(request_id),
 timer_id bigint UNIQUE REFERENCES merchant_notice_timer_sources(id),
 domain_audit_id bigint UNIQUE REFERENCES backoffice_audit(id),
 job_id bigint UNIQUE REFERENCES jobs(id),
 scene text NOT NULL CHECK(scene IN('order','login','settle','balance','group','daily_report','domain_audit')),
 profile_id bigint REFERENCES merchant_notice_profiles(id),
 profile_hash bytea CHECK(profile_hash IS NULL OR octet_length(profile_hash)=32),
 snapshot_state text NOT NULL CHECK(snapshot_state IN('captured','absent','unavailable','excluded')),
 payload_cipher bytea NOT NULL CHECK(octet_length(payload_cipher) BETWEEN 28 AND 65536),
 body_hash bytea NOT NULL CHECK(octet_length(body_hash)=32),
 event_hash bytea NOT NULL CHECK(octet_length(event_hash)=32),
 source_transaction xid8 NOT NULL DEFAULT pg_current_xact_id(),
 created_at timestamptz NOT NULL,
 CHECK((profile_id IS NULL)=(profile_hash IS NULL)),
 CHECK((snapshot_state='captured')=(profile_id IS NOT NULL)),
 CHECK(job_id IS NULL OR snapshot_state='captured'),
 CHECK(scene='order' AND order_id IS NOT NULL AND login_audit_id IS NULL AND settlement_id IS NULL AND timer_id IS NULL AND domain_audit_id IS NULL
 OR scene='login' AND order_id IS NULL AND login_audit_id IS NOT NULL AND settlement_id IS NULL AND timer_id IS NULL AND domain_audit_id IS NULL
 OR scene='settle' AND order_id IS NULL AND login_audit_id IS NULL AND settlement_id IS NOT NULL AND timer_id IS NULL AND domain_audit_id IS NULL
 OR scene IN('balance','group','daily_report') AND order_id IS NULL AND login_audit_id IS NULL AND settlement_id IS NULL AND timer_id IS NOT NULL AND domain_audit_id IS NULL
 OR scene='domain_audit' AND order_id IS NULL AND login_audit_id IS NULL AND settlement_id IS NULL AND timer_id IS NULL AND domain_audit_id IS NOT NULL)
);
CREATE INDEX merchant_notice_events_owner ON merchant_notice_events(merchant_id,id);
CREATE FUNCTION merchant_notice_event_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE o orders%ROWTYPE; l console_login_audit%ROWTYPE;
BEGIN
 IF TG_OP<>'INSERT' THEN RAISE EXCEPTION 'immutable notice source'; END IF;
 IF NEW.source_transaction<>pg_current_xact_id()
 OR (NEW.profile_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM merchant_notice_current WHERE merchant_id=NEW.merchant_id AND profile_id=NEW.profile_id AND profile_hash=NEW.profile_hash))
 OR (NEW.job_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM jobs WHERE id=NEW.job_id AND kind='merchant.notice' AND event_key='merchant-notice:'||NEW.id::text AND payload=jsonb_build_object('event_id',NEW.id::text) AND state='pending' AND attempts=0 AND lease_token IS NULL AND lease_until IS NULL))
 THEN RAISE EXCEPTION 'notice source requires original transaction and profile'; END IF;
 IF NEW.scene='domain_audit' THEN
  IF NOT EXISTS(SELECT 1 FROM backoffice_audit a JOIN backoffice_records d ON d.resource='domains' AND d.id=a.resource_id WHERE a.id=NEW.domain_audit_id AND a.resource='domains' AND a.operation='update' AND a.actor_role='admin' AND a.actor_id>0 AND a.notice_source_transaction=NEW.source_transaction AND a.changes->>'merchant_id'=NEW.merchant_id::text AND d.owner_id=NEW.merchant_id AND (d.body->'domain',d.body->'approved')=(a.changes->'domain',a.changes->'approved'))
  THEN RAISE EXCEPTION 'notice domain review requires original admin audit'; END IF;
  RETURN NEW;
 END IF;
 IF NEW.timer_id IS NOT NULL THEN
  IF NOT EXISTS(SELECT 1 FROM merchant_notice_timer_sources t WHERE t.id=NEW.timer_id AND (t.merchant_id,t.scene,t.profile_id,t.profile_hash,t.source_transaction)=(NEW.merchant_id,NEW.scene,NEW.profile_id,NEW.profile_hash,NEW.source_transaction))
  THEN RAISE EXCEPTION 'notice timer event requires original source'; END IF;
  RETURN NEW;
 END IF;
 IF NEW.scene='settle' THEN
  IF NOT EXISTS(SELECT 1 FROM settlement_payouts p JOIN ledger_requests r ON r.id=p.request_id
   JOIN transfers t ON t.id=p.transfer_id JOIN provider_transfer_attempts a ON a.transfer_id=t.id
   JOIN journals j ON j.event_key='settlement-payout:'||p.request_id::text||':principal'
   WHERE p.request_id=NEW.settlement_id AND r.merchant_id=NEW.merchant_id AND p.send_state='claimed'
   AND t.status='succeeded' AND a.state='succeeded' AND a.provider_transfer_no<>'' AND j.transaction_id=NEW.source_transaction)
  THEN RAISE EXCEPTION 'notice settlement source requires original successful payout'; END IF;
  RETURN NEW;
 END IF;
 IF NEW.scene='login' THEN
  SELECT * INTO l FROM console_login_audit WHERE id=NEW.login_audit_id;
  IF l.id IS NULL OR l.role<>'merchant' OR l.success IS DISTINCT FROM true OR l.subject_id IS DISTINCT FROM NEW.merchant_id OR l.auth_version IS NULL OR l.source_transaction IS DISTINCT FROM NEW.source_transaction
  THEN RAISE EXCEPTION 'notice login source requires original successful audit'; END IF;
  RETURN NEW;
 END IF;
 SELECT * INTO o FROM orders WHERE id=NEW.order_id;
 IF o.id IS NULL OR o.merchant_id<>NEW.merchant_id OR o.status<>'paid' OR o.paid_at IS NULL
 OR NOT EXISTS(SELECT 1 FROM verified_payment_receipts WHERE order_id=o.id)
 OR (o.merchant_amount>0 AND NOT EXISTS(SELECT 1 FROM journals WHERE event_key='payment:'||o.trade_no AND transaction_id=NEW.source_transaction))
 OR (o.platform_fee>0 AND NOT EXISTS(SELECT 1 FROM journals WHERE event_key='payment-fee:'||o.trade_no AND transaction_id=NEW.source_transaction))
 THEN RAISE EXCEPTION 'notice source must be born with original paid journal and profile'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER merchant_notice_event_identity BEFORE INSERT OR UPDATE OR DELETE ON merchant_notice_events FOR EACH ROW EXECUTE FUNCTION merchant_notice_event_guard();
CREATE FUNCTION merchant_notice_timer_complete() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM merchant_notice_events e WHERE e.timer_id=NEW.id AND (e.merchant_id,e.scene,e.profile_id,e.profile_hash,e.source_transaction)=(NEW.merchant_id,NEW.scene,NEW.profile_id,NEW.profile_hash,NEW.source_transaction))
 THEN RAISE EXCEPTION 'incomplete original notice timer event'; END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER merchant_notice_timer_complete_row AFTER INSERT ON merchant_notice_timer_sources DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION merchant_notice_timer_complete();
CREATE INDEX merchant_notice_transfer_report ON transfers(merchant_id,created_at,id) WHERE status='succeeded';

CREATE TABLE merchant_notice_claims (
 id bigserial PRIMARY KEY,event_id bigint NOT NULL REFERENCES merchant_notice_events(id),
 job_id bigint NOT NULL REFERENCES jobs(id),
 transport text NOT NULL CHECK(transport IN('wechat_template','smtp','sms','robot','telegram','qqbot','voice')),
 profile_id bigint NOT NULL REFERENCES merchant_notice_profiles(id),
 event_hash bytea NOT NULL CHECK(octet_length(event_hash)=32),
 profile_hash bytea NOT NULL CHECK(octet_length(profile_hash)=32),
 recipient_hash bytea NOT NULL CHECK(octet_length(recipient_hash)=32),
 scope_hash bytea NOT NULL CHECK(octet_length(scope_hash)=32),
 lease_hash bytea NOT NULL CHECK(octet_length(lease_hash)=32),
 claim_hash bytea NOT NULL CHECK(octet_length(claim_hash)=32),
 transaction_id xid8 NOT NULL DEFAULT pg_current_xact_id(),created_at timestamptz NOT NULL,
 UNIQUE(event_id,transport)
);
CREATE TABLE merchant_notice_results (
 claim_id bigint PRIMARY KEY REFERENCES merchant_notice_claims(id),
 claim_hash bytea NOT NULL CHECK(octet_length(claim_hash)=32),
 state text NOT NULL CHECK(state IN('accepted','unknown','not_sent')),
 result_hash bytea NOT NULL CHECK(octet_length(result_hash)=32),
 transaction_id xid8 NOT NULL DEFAULT pg_current_xact_id(),created_at timestamptz NOT NULL
);
CREATE FUNCTION merchant_notice_claim_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP<>'INSERT' THEN RAISE EXCEPTION 'immutable notice delivery claim'; END IF;
 IF NEW.transaction_id<>pg_current_xact_id() OR NOT EXISTS(
  SELECT 1 FROM merchant_notice_events e JOIN merchant_notice_current p ON p.merchant_id=e.merchant_id
  JOIN merchants m ON m.id=e.merchant_id JOIN jobs j ON j.id=NEW.job_id AND j.id=e.job_id
  WHERE e.id=NEW.event_id AND e.snapshot_state='captured'
  AND (e.event_hash,e.profile_id,e.profile_hash)=(NEW.event_hash,NEW.profile_id,NEW.profile_hash)
  AND (p.profile_id,p.profile_hash)=(e.profile_id,e.profile_hash) AND m.enabled
  AND j.kind='merchant.notice' AND j.event_key='merchant-notice:'||e.id::text AND j.payload=jsonb_build_object('event_id',e.id::text)
  AND j.state='running' AND j.lease_until>clock_timestamp() AND j.lease_token IS NOT NULL)
 THEN RAISE EXCEPTION 'notice claim requires original event, profile and current job lease'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER merchant_notice_claim_identity BEFORE INSERT OR UPDATE OR DELETE ON merchant_notice_claims FOR EACH ROW EXECUTE FUNCTION merchant_notice_claim_guard();
CREATE FUNCTION merchant_notice_result_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP<>'INSERT' THEN RAISE EXCEPTION 'immutable notice result'; END IF;
 IF NEW.transaction_id<>pg_current_xact_id() OR NOT EXISTS(SELECT 1 FROM merchant_notice_claims WHERE id=NEW.claim_id AND claim_hash=NEW.claim_hash)
 THEN RAISE EXCEPTION 'notice result requires original permanent claim'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER merchant_notice_result_identity BEFORE INSERT OR UPDATE OR DELETE ON merchant_notice_results FOR EACH ROW EXECUTE FUNCTION merchant_notice_result_guard();
