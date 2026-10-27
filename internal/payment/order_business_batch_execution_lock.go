package payment

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5"
	"time"
)

// Lock sessions are independently bounded, so a batch cannot retain a pool slot
// while its own domain transactions wait for that same pool. Saturation returns
// a retryable conflict rather than queueing unlimited new PostgreSQL sessions.
var orderBusinessBatch185LockSlots = make(chan struct{}, 4)

func (s *Store) acquireOrderBusinessBatch185Execution(ctx context.Context, id int64) (func(), error) {
	select {
	case orderBusinessBatch185LockSlots <- struct{}{}:
	default:
		return nil, ErrConflict
	}
	releaseSlot := func() { <-orderBusinessBatch185LockSlots }
	cfg := s.pool.Config()
	connCfg := cfg.ConnConfig.Copy()
	if connCfg.ConnectTimeout == 0 || connCfg.ConnectTimeout > 3*time.Second {
		connCfg.ConnectTimeout = 3 * time.Second
	}
	opening, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if cfg.BeforeConnect != nil {
		if e := cfg.BeforeConnect(opening, connCfg); e != nil {
			releaseSlot()
			return nil, e
		}
	}
	conn, e := pgx.ConnectConfig(opening, connCfg)
	if e != nil {
		releaseSlot()
		return nil, e
	}
	closeConn := func() {
		cleanup, c := context.WithTimeout(context.Background(), 2*time.Second)
		defer c()
		_ = conn.Close(cleanup)
		releaseSlot()
	}
	if cfg.AfterConnect != nil {
		if e = cfg.AfterConnect(opening, conn); e != nil {
			closeConn()
			return nil, e
		}
	}
	key := fmt.Sprintf("goepay/order-business-batch185/execute/%d", id)
	var owned bool
	if e = conn.QueryRow(ctx, `SELECT pg_try_advisory_lock(hashtextextended($1,0))`, key).Scan(&owned); e != nil {
		closeConn()
		return nil, e
	}
	if !owned {
		closeConn()
		return nil, ErrConflict
	}
	// Closing the owned physical connection releases this session lock even when
	// request cancellation prevents an ordinary explicit unlock query.
	return closeConn, nil
}
