package payment

import "goepay/internal/noticeconfig231"

// Internal-only constructor; no credential or pool is exposed to HTTP callers.
func (s *Store) NoticeAccounts231() (*noticeconfig231.Store, error) {
	return noticeconfig231.New(s.pool, s.confirmationKey231)
}
