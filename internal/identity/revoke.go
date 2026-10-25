package identity

// RevokeUser removes all sessions for an account after a password change.
func (s *Sessions) RevokeUser(username, role string) {
	if s.pool != nil {
		s.revokePersistent(username, role)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for token, session := range s.items {
		if session.User.Username == username && session.User.Role == role {
			delete(s.items, token)
		}
	}
}
