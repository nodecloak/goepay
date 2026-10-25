package printing

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"goepay/internal/platform/jobs"
	"io"
	"strconv"
	"time"
)

func (s *Store) RunOne(ctx context.Context, enabled, sandbox bool) (bool, error) {
	return s.runOne(ctx, enabled, sandbox, LocalFake{}, 20*time.Second)
}

var defaultExternalPrinter = NewExcelsecuConnector()

// RunExcelsecuOne is used only by the explicitly enabled external worker.
// It reuses one bounded HTTP client and never consumes a payment/transfer job.
func (s *Store) RunExcelsecuOne(ctx context.Context, enabled bool) (bool, error) {
	if !enabled {
		return false, ErrState
	}
	return s.runTask(ctx, defaultExternalPrinter, 20*time.Second)
}
func parsePrintingTask(raw []byte, kind string) (int64, error) {
	if len(raw) > 128 {
		return 0, ErrInvalid
	}
	key := "order_id"
	if kind == "receipt-print.send" {
		key = "intent_id"
	} else if kind != "receipt-print.prepare" {
		return 0, ErrInvalid
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	t, e := d.Token()
	if e != nil || t != json.Delim('{') {
		return 0, ErrInvalid
	}
	if !d.More() {
		return 0, ErrInvalid
	}
	t, e = d.Token()
	if e != nil || t != key {
		return 0, ErrInvalid
	}
	var v string
	if d.Decode(&v) != nil || d.More() {
		return 0, ErrInvalid
	}
	t, e = d.Token()
	if e != nil || t != json.Delim('}') {
		return 0, ErrInvalid
	}
	if _, e = d.Token(); e != io.EOF {
		return 0, ErrInvalid
	}
	id, e := strconv.ParseInt(v, 10, 64)
	if e != nil || id <= 0 || strconv.FormatInt(id, 10) != v {
		return 0, ErrInvalid
	}
	return id, nil
}
func (s *Store) runOne(ctx context.Context, enabled, sandbox bool, connector Connector, deadline time.Duration) (bool, error) {
	if !enabled || !sandbox {
		return false, ErrState
	}
	return s.runTask(ctx, connector, deadline)
}

func (s *Store) runTask(ctx context.Context, connector Connector, deadline time.Duration) (bool, error) {
	if deadline <= 0 || deadline > 20*time.Second {
		return false, ErrInvalid
	}
	platform, e := s.readPlatform(ctx, s.pool, 0)
	if e != nil && !errors.Is(e, ErrNotFound) {
		return false, e
	}
	if e == nil && !printingConnectorMatches(platform.V.Provider, connector) {
		return false, ErrUnsupported
	}
	q := jobs.Queue{Pool: s.pool}
	job, e := q.Claim(ctx, []string{"receipt-print.prepare", "receipt-print.send"}, time.Minute)
	if jobs.IsEmpty(e) {
		return false, nil
	}
	if e != nil {
		return false, e
	}
	taskCtx, taskCancel := context.WithTimeout(ctx, deadline)
	id, parseErr := parsePrintingTask(job.Payload, job.Kind)
	if parseErr != nil {
		e = parseErr
	} else if job.Kind == "receipt-print.prepare" {
		_, e = s.PrepareAutomatic(taskCtx, id)
	} else {
		_, e = s.Dispatch(taskCtx, id, connector)
	}
	taskCancel()
	finish, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if e == nil {
		return true, q.Complete(finish, job)
	}
	save := q.Retry(finish, job, "print task unavailable; original intent preserved", time.Minute)
	return true, errors.Join(e, save)
}
