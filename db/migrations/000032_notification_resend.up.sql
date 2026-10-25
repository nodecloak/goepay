CREATE INDEX jobs_payment_notify_trade ON jobs((payload->>'trade_no'),created_at DESC,id DESC) WHERE kind='payment.notify';
CREATE TABLE notification_send_controls (
 order_id bigint PRIMARY KEY REFERENCES orders(id),
 last_manual_at timestamptz NOT NULL
);
CREATE TABLE notification_resend_requests (
 order_id bigint NOT NULL REFERENCES orders(id),
 request_hash text NOT NULL CHECK(request_hash ~ '^[a-f0-9]{64}$'),
 job_id bigint NOT NULL REFERENCES jobs(id),
 actor_role text NOT NULL CHECK(actor_role IN ('admin','merchant')),
 actor_id bigint NOT NULL,
 created_at timestamptz NOT NULL DEFAULT statement_timestamp(),
 PRIMARY KEY(order_id,request_hash)
);
