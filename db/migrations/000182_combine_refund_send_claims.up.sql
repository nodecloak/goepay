-- Every previously existing share is conservatively treated as sent: older
-- versions did not persist a send claim, so pending cannot prove otherwise.
CREATE TABLE combine_refund_send_claims207 (
 refund_id bigint NOT NULL,
 position smallint NOT NULL,
 claimed_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(refund_id, position),
 FOREIGN KEY(refund_id, position) REFERENCES combine_subrefunds207(refund_id, position)
);
INSERT INTO combine_refund_send_claims207(refund_id,position)
 SELECT refund_id,position FROM combine_subrefunds207;

CREATE FUNCTION combine_refund_send_claims207_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'combined refund send claims are immutable'; END $$;
CREATE TRIGGER combine_refund_send_claims207_immutable BEFORE UPDATE OR DELETE
 ON combine_refund_send_claims207 FOR EACH ROW EXECUTE FUNCTION combine_refund_send_claims207_immutable();
