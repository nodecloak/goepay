CREATE TABLE console_mfa (
 username text NOT NULL, role text NOT NULL,
 secret bytea, pending_secret bytea, pending_until timestamptz,
 last_counter bigint NOT NULL DEFAULT -1,
 PRIMARY KEY(username,role), FOREIGN KEY(username,role) REFERENCES console_users(username,role) ON DELETE CASCADE
);
