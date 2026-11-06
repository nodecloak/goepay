-- Idle expiry for console sessions: the time of the session's last
-- authenticated request. NULL marks a session created before idle tracking;
-- its next request starts the clock. Absolute expiry (expires_at) still applies.
ALTER TABLE console_sessions ADD COLUMN last_seen_at timestamptz;
