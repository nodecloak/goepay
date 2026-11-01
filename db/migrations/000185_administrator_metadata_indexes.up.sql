-- Directory metadata is read from recorded facts, without copying credentials.
CREATE INDEX console_login_admin_success_metadata
 ON console_login_audit(username,created_at DESC)
 WHERE role='admin' AND success;
CREATE INDEX backoffice_admin_creation_metadata
 ON backoffice_audit(resource_id,created_at)
 WHERE resource='admin-users' AND operation='create';
