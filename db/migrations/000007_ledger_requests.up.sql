CREATE TABLE ledger_requests (
 id bigserial PRIMARY KEY, kind text NOT NULL CHECK(kind IN ('settlements','internal-transfers','profitsharing')),
 merchant_id bigint NOT NULL REFERENCES merchants(id), recipient_id bigint REFERENCES merchants(id),
 external_no text NOT NULL CHECK(length(external_no) BETWEEN 1 AND 128), amount bigint NOT NULL CHECK(amount>0),
 status text NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','approved','rejected')),
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(kind,merchant_id,external_no), CHECK((kind='settlements' AND recipient_id IS NULL) OR (kind<>'settlements' AND recipient_id IS NOT NULL AND recipient_id<>merchant_id))
);
CREATE TABLE ledger_request_events (
 id bigserial PRIMARY KEY, request_id bigint NOT NULL REFERENCES ledger_requests(id),
 actor text NOT NULL, status text NOT NULL, created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TRIGGER ledger_request_events_immutable BEFORE UPDATE OR DELETE ON ledger_request_events FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
