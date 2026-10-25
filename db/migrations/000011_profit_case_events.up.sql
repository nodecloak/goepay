CREATE TABLE order_profit_allocations (
 id bigserial PRIMARY KEY, order_id bigint NOT NULL REFERENCES orders(id), rule_id bigint NOT NULL REFERENCES backoffice_records(id),
 request_id bigint NOT NULL UNIQUE REFERENCES ledger_requests(id), amount bigint NOT NULL CHECK(amount>0),
 rate_bps integer NOT NULL CHECK(rate_bps BETWEEN 1 AND 10000), created_at timestamptz NOT NULL DEFAULT now(), UNIQUE(order_id,rule_id)
);
CREATE TRIGGER order_profit_allocations_immutable BEFORE UPDATE OR DELETE ON order_profit_allocations FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
CREATE TABLE case_comments (
 id bigserial PRIMARY KEY, record_id bigint NOT NULL REFERENCES backoffice_records(id), actor_role text NOT NULL, actor_id bigint NOT NULL,
 actor_name text NOT NULL, content text NOT NULL CHECK(length(content) BETWEEN 1 AND 10000), created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TRIGGER case_comments_immutable BEFORE UPDATE OR DELETE ON case_comments FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
