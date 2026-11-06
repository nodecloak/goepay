package payment

import "goepay/internal/noticeconfig"

// Internal-only constructor; no credential or pool is exposed to HTTP callers.
func (s *Store) NoticeAccounts231() (*noticeconfig.Store, error) {
	return noticeconfig.New(s.pool, s.confirmationKey231)
}
