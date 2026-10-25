-- Operational channel stops never change transfer finality or release money.
CREATE TABLE channel_stop_events (
 id bigserial PRIMARY KEY,channel_id bigint NOT NULL CHECK(channel_id>0),revision bigint NOT NULL CHECK(revision BETWEEN 1 AND 4096),
 previous_id bigint REFERENCES channel_stop_events(id),previous_hash bytea,
 action text NOT NULL CHECK(action IN('pause','resume')),
 request_id bigint NOT NULL REFERENCES settlement_payouts(request_id),transfer_id bigint NOT NULL REFERENCES transfers(id),claim_sequence bigint NOT NULL REFERENCES settlement_auto_claims(claim_seq),
 receipt jsonb NOT NULL CHECK(jsonb_typeof(receipt)='object'),account_hash bytea NOT NULL CHECK(octet_length(account_hash)=32),
 actor_role text NOT NULL,actor_id bigint NOT NULL,actor_name text NOT NULL,actor_class text NOT NULL,
 created_at timestamptz NOT NULL,transaction_id xid8 NOT NULL DEFAULT pg_current_xact_id(),proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32),
 UNIQUE(channel_id,revision),UNIQUE(id,channel_id,revision),
 CHECK((revision=1 AND previous_id IS NULL AND previous_hash IS NULL AND action='pause') OR (revision>1 AND previous_id IS NOT NULL AND previous_hash IS NOT NULL AND octet_length(previous_hash)=32)),
 CHECK((action='pause' AND actor_role='system' AND actor_id=0 AND actor_name='settlement-auto' AND actor_class='') OR (action='resume' AND actor_role='admin' AND actor_id>0 AND actor_name<>'' AND actor_class='super'))
);
CREATE UNIQUE INDEX channel_stop_original_transfer ON channel_stop_events(transfer_id) WHERE action='pause';
CREATE TRIGGER channel_stop_event_immutable BEFORE UPDATE OR DELETE ON channel_stop_events FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE TABLE channel_stop_heads (
 channel_id bigint PRIMARY KEY CHECK(channel_id>0),event_id bigint NOT NULL,revision bigint NOT NULL CHECK(revision BETWEEN 1 AND 4096),proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32),
 FOREIGN KEY(event_id,channel_id,revision) REFERENCES channel_stop_events(id,channel_id,revision) DEFERRABLE INITIALLY DEFERRED
);
CREATE TRIGGER channel_stop_head_no_delete BEFORE DELETE ON channel_stop_heads FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE FUNCTION channel_stop_source_guard() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF NOT EXISTS(SELECT 1 FROM settlement_payouts p JOIN settlement_auto_claims c ON c.request_id=p.request_id WHERE (p.request_id,p.transfer_id,p.channel_id,p.send_state)=(NEW.request_id,NEW.transfer_id,NEW.channel_id,'claimed') AND c.claim_seq=NEW.claim_sequence AND c.transfer_id=NEW.transfer_id AND c.authorization_id=p.auto_authorization_id) THEN RAISE EXCEPTION 'channel stop requires original automatic send claim';END IF;
 IF NEW.revision>1 AND NOT EXISTS(SELECT 1 FROM channel_stop_heads h JOIN channel_stop_events e ON e.id=h.event_id WHERE h.channel_id=NEW.channel_id AND h.revision=NEW.revision-1 AND (h.event_id,h.proof_hash)=(NEW.previous_id,NEW.previous_hash) AND e.action<>NEW.action) THEN RAISE EXCEPTION 'channel stop requires original alternating current source';END IF;
 IF NEW.revision>1 AND NEW.action='pause' AND NOT EXISTS(SELECT 1 FROM channel_stop_events e JOIN settlement_auto_claims c ON c.claim_seq=NEW.claim_sequence WHERE e.id=NEW.previous_id AND e.action='resume' AND c.claimed_at>e.created_at) THEN RAISE EXCEPTION 'channel stop requires send claim after latest explicit restoration';END IF;
 IF NEW.revision=1 AND EXISTS(SELECT 1 FROM channel_stop_heads WHERE channel_id=NEW.channel_id) THEN RAISE EXCEPTION 'channel stop source already exists';END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER channel_stop_source_guard BEFORE INSERT ON channel_stop_events FOR EACH ROW EXECUTE FUNCTION channel_stop_source_guard();
CREATE FUNCTION channel_stop_head_guard() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF TG_OP='UPDATE' AND (NEW.channel_id<>OLD.channel_id OR NEW.revision<>OLD.revision+1 OR NOT EXISTS(SELECT 1 FROM channel_stop_events e WHERE e.id=NEW.event_id AND e.previous_id=OLD.event_id AND e.previous_hash=OLD.proof_hash AND e.transaction_id=pg_current_xact_id())) THEN RAISE EXCEPTION 'channel stop head requires next original source transaction';END IF;
 IF TG_OP='INSERT' AND (NEW.revision<>1 OR NOT EXISTS(SELECT 1 FROM channel_stop_events e WHERE e.id=NEW.event_id AND e.revision=1 AND e.transaction_id=pg_current_xact_id())) THEN RAISE EXCEPTION 'channel stop head requires first original source transaction';END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER channel_stop_head_guard BEFORE INSERT OR UPDATE ON channel_stop_heads FOR EACH ROW EXECUTE FUNCTION channel_stop_head_guard();
CREATE FUNCTION channel_stop_complete() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF NOT EXISTS(SELECT 1 FROM channel_stop_heads h JOIN channel_stop_events e ON e.id=h.event_id WHERE h.channel_id=NEW.channel_id AND (h.channel_id,h.revision,h.proof_hash)=(e.channel_id,e.revision,e.proof_hash) AND h.revision=(SELECT count(*) FROM channel_stop_events s WHERE s.channel_id=h.channel_id)) THEN RAISE EXCEPTION 'incomplete channel stop source history';END IF;RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER channel_stop_head_complete AFTER INSERT OR UPDATE ON channel_stop_heads DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION channel_stop_complete();
CREATE CONSTRAINT TRIGGER channel_stop_event_complete AFTER INSERT ON channel_stop_events DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION channel_stop_complete();
CREATE INDEX channel_stop_audit_channel ON backoffice_audit((changes->>'channel_id'),resource_id,operation) WHERE resource='channel-stops';

CREATE TABLE channel_stop_notice_claims (
 event_id bigint PRIMARY KEY REFERENCES channel_stop_events(id),job_id bigint NOT NULL UNIQUE REFERENCES jobs(id),
 recipient_hash bytea NOT NULL CHECK(octet_length(recipient_hash)=32),lease_hash bytea NOT NULL CHECK(octet_length(lease_hash)=32),
 created_at timestamptz NOT NULL,transaction_id xid8 NOT NULL DEFAULT pg_current_xact_id(),proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32)
);
CREATE TRIGGER channel_stop_notice_claim_immutable BEFORE UPDATE OR DELETE ON channel_stop_notice_claims FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE FUNCTION channel_stop_notice_claim_guard() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF NEW.transaction_id<>pg_current_xact_id() OR NOT EXISTS(SELECT 1 FROM channel_stop_events e JOIN jobs j ON j.id=NEW.job_id WHERE e.id=NEW.event_id AND e.action='pause' AND j.kind='admin.channel-stopped' AND j.event_key='channel-stop:'||e.id::text AND j.payload=jsonb_build_object('event_id',e.id::text) AND j.state='running' AND j.lease_until>clock_timestamp() AND sha256(convert_to(j.lease_token,'UTF8'))=NEW.lease_hash) THEN RAISE EXCEPTION 'channel notice requires original current job claim';END IF;RETURN NEW;
END $$;
CREATE TRIGGER channel_stop_notice_claim_guard BEFORE INSERT ON channel_stop_notice_claims FOR EACH ROW EXECUTE FUNCTION channel_stop_notice_claim_guard();
