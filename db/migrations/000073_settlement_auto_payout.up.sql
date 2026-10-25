-- Independent bounded wallet payout authority. Generation authority never sends.
CREATE TABLE settlement_auto_authorizations (
 id bigserial PRIMARY KEY,plan_id bigint NOT NULL REFERENCES settlement_plans(id),plan_revision bigint NOT NULL,
 generation_authorization_id bigint NOT NULL REFERENCES settlement_plan_authorizations(id),
 expected_authorization_id bigint REFERENCES settlement_auto_authorizations(id),
 input jsonb NOT NULL CHECK(jsonb_typeof(input)='object'),scope_snapshot jsonb NOT NULL CHECK(jsonb_typeof(scope_snapshot)='object'),
 actor_id bigint NOT NULL CHECK(actor_id>0),actor_name text NOT NULL CHECK(length(actor_name)>0),
 proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32),created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
 FOREIGN KEY(plan_id,plan_revision) REFERENCES settlement_plan_versions(plan_id,revision),
 CHECK(expected_authorization_id IS NULL OR expected_authorization_id<id),UNIQUE(plan_id,expected_authorization_id)
);
CREATE TRIGGER settlement_auto_authority_immutable BEFORE UPDATE OR DELETE ON settlement_auto_authorizations FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE TABLE settlement_auto_heads (
 plan_id bigint PRIMARY KEY REFERENCES settlement_plans(id),authorization_id bigint NOT NULL UNIQUE REFERENCES settlement_auto_authorizations(id)
);
CREATE FUNCTION settlement_auto_head_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE a settlement_auto_authorizations;
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'auto authority head cannot be erased';END IF;
 SELECT * INTO STRICT a FROM settlement_auto_authorizations WHERE id=NEW.authorization_id;
 IF a.plan_id<>NEW.plan_id THEN RAISE EXCEPTION 'auto authority plan mismatch';END IF;
 IF TG_OP='INSERT' THEN
  IF a.expected_authorization_id IS NOT NULL THEN RAISE EXCEPTION 'auto authority must begin with original version';END IF;
 ELSIF (NEW.plan_id<>OLD.plan_id OR a.expected_authorization_id IS DISTINCT FROM OLD.authorization_id) THEN
  RAISE EXCEPTION 'auto authority requires a fresh chained version';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER settlement_auto_head BEFORE INSERT OR UPDATE OR DELETE ON settlement_auto_heads FOR EACH ROW EXECUTE FUNCTION settlement_auto_head_guard();
CREATE TABLE settlement_auto_revocations (
 authorization_id bigint PRIMARY KEY REFERENCES settlement_auto_authorizations(id),
 actor_id bigint NOT NULL CHECK(actor_id>0),actor_name text NOT NULL CHECK(length(actor_name)>0),
 proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32),created_at timestamptz NOT NULL DEFAULT transaction_timestamp()
);
CREATE TRIGGER settlement_auto_revoke_immutable BEFORE UPDATE OR DELETE ON settlement_auto_revocations FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
ALTER TABLE settlement_payout_events DROP CONSTRAINT settlement_payout_events_actor_role_check;
ALTER TABLE settlement_payout_events DROP CONSTRAINT settlement_payout_events_actor_id_check;
ALTER TABLE settlement_payout_events ADD CONSTRAINT settlement_payout_event_actor CHECK
 ((actor_role IN('admin','merchant') AND actor_id>0) OR (actor_role='system' AND actor_id=0 AND actor_name='settlement-auto'));
ALTER TABLE settlement_payouts ADD COLUMN auto_authorization_id bigint REFERENCES settlement_auto_authorizations(id);
CREATE TABLE settlement_auto_claims (
 claim_seq bigserial NOT NULL UNIQUE,
 request_id bigint PRIMARY KEY REFERENCES settlement_payouts(request_id),
 plan_id bigint NOT NULL REFERENCES settlement_plans(id),
 authorization_id bigint NOT NULL REFERENCES settlement_auto_authorizations(id),
 run_id bigint NOT NULL REFERENCES settlement_plan_runs(id),item_id bigint NOT NULL UNIQUE REFERENCES settlement_plan_items(id),
 transfer_id bigint NOT NULL UNIQUE REFERENCES transfers(id),merchant_id bigint NOT NULL REFERENCES merchants(id),
 gross bigint NOT NULL CHECK(gross>0),net bigint NOT NULL CHECK(net>0),fee bigint NOT NULL CHECK(fee>=0),CHECK(gross=net+fee),
 business_date date NOT NULL,claimed_at timestamptz NOT NULL,transaction_id xid8 NOT NULL DEFAULT pg_current_xact_id(),
 proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32)
);
CREATE INDEX settlement_auto_claim_budget ON settlement_auto_claims(plan_id,business_date,run_id,claim_seq);
CREATE TRIGGER settlement_auto_claim_immutable BEFORE UPDATE OR DELETE ON settlement_auto_claims FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE FUNCTION settlement_auto_payout_source_guard() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF NEW.auto_authorization_id IS NOT DISTINCT FROM OLD.auto_authorization_id THEN RETURN NEW;END IF;
 IF OLD.auto_authorization_id IS NOT NULL OR OLD.send_state<>'unsent' OR NEW.send_state<>'claimed'
 OR NOT EXISTS(SELECT 1 FROM settlement_auto_claims c WHERE c.request_id=OLD.request_id AND c.transfer_id=OLD.transfer_id AND c.authorization_id=NEW.auto_authorization_id AND c.transaction_id=pg_current_xact_id())
 THEN RAISE EXCEPTION 'auto payout source requires exact original claim transaction';END IF;RETURN NEW;END $$;
CREATE TRIGGER settlement_auto_payout_source BEFORE UPDATE ON settlement_payouts FOR EACH ROW EXECUTE FUNCTION settlement_auto_payout_source_guard();
CREATE FUNCTION settlement_auto_claim_complete() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF NOT EXISTS(SELECT 1 FROM settlement_auto_claims c JOIN settlement_payouts p ON p.request_id=c.request_id JOIN settlement_plan_items i ON i.id=c.item_id JOIN settlement_plan_runs r ON r.id=i.run_id JOIN settlement_auto_authorizations a ON a.id=c.authorization_id
 WHERE c.request_id=NEW.request_id AND c.transaction_id=pg_current_xact_id() AND p.auto_authorization_id=a.id AND p.send_state='claimed' AND p.transfer_id=c.transfer_id
 AND (i.request_id,i.merchant_id,i.run_id,i.gross,i.fee,i.net)=(c.request_id,c.merchant_id,c.run_id,c.gross,c.fee,c.net) AND i.state='created'
 AND c.plan_id=r.plan_id AND c.business_date=r.business_date AND r.authorization_id=a.generation_authorization_id AND (r.plan_id,r.plan_revision)=(a.plan_id,a.plan_revision)
 AND EXISTS(SELECT 1 FROM settlement_payout_events e WHERE e.request_id=p.request_id AND e.action='claim' AND (e.actor_role,e.actor_id,e.actor_name)=('system',0::bigint,'settlement-auto')))
 THEN RAISE EXCEPTION 'automatic claim requires original run, payout and system audit';END IF;RETURN NULL;END $$;
CREATE CONSTRAINT TRIGGER settlement_auto_claim_requires_source AFTER INSERT ON settlement_auto_claims DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION settlement_auto_claim_complete();
