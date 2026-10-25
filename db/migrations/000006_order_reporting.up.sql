CREATE TABLE order_reporting (
 order_id bigint PRIMARY KEY REFERENCES orders(id),
 buyer text NOT NULL DEFAULT '', province text NOT NULL DEFAULT '',
 created_at timestamptz NOT NULL DEFAULT now()
);
