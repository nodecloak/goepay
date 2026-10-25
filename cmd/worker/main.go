package main

import (
	"context"
	"crypto/rsa"
	"goepay/internal/bootstrap"
	"goepay/internal/compat"
	"goepay/internal/notification"
	"goepay/internal/platform/httpclient"
	"goepay/internal/platform/jobs"
	"goepay/internal/reconciliation"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	pool, store, e := bootstrap.Connect(ctx)
	if e != nil {
		slog.Error("worker startup failed")
		os.Exit(1)
	}
	defer pool.Close()
	var key *rsa.PrivateKey
	if path := os.Getenv("GOEPAY_PLATFORM_PRIVATE_KEY_FILE"); path != "" {
		raw, e := os.ReadFile(path)
		if e != nil {
			slog.Error("cannot read platform key")
			os.Exit(1)
		}
		key, e = compat.ParsePrivateKey(string(raw))
		if e != nil {
			slog.Error("invalid platform key")
			os.Exit(1)
		}
	}
	q := jobs.Queue{Pool: pool}
	h := notification.Handler{Store: store, Client: httpclient.New(), PlatformKey: key}
	flow, e := configuredReadOnlyFlow(store)
	if e != nil {
		slog.Error("reconciliation configuration rejected")
		os.Exit(1)
	}
	recoveryFlow, e := configuredScanRecoveryFlow(store)
	if e != nil {
		slog.Error("original scan recovery configuration rejected")
		os.Exit(1)
	}
	kinds := []string{"payment.notify"}
	if recoveryFlow != nil {
		kinds = append(kinds, "payment.scan-recover")
	}
	noticeRuntime, e := newChannelNoticeRuntime(ctx, store, os.Getenv)
	if e != nil {
		slog.Error("administrator notice configuration rejected")
		os.Exit(1)
	}
	if noticeRuntime != nil {
		kinds = append(kinds, "admin.channel-stopped")
	}
	merchantNotices, e := newMerchantNoticeRuntime(pool, os.Getenv)
	if e != nil {
		slog.Error("merchant notice configuration rejected")
		os.Exit(1)
	}
	if merchantNotices != nil {
		kinds = append(kinds, "merchant.notice")
	}
	businessNotices, e := newBusinessNoticeRuntime(store.AdminBusinessNotices(), os.Getenv)
	if e != nil {
		slog.Error("administrator business notice configuration rejected")
		os.Exit(1)
	}
	if businessNotices != nil {
		kinds = append(kinds, "admin.business-notice")
	}
	var channels []int64
	if flow != nil {
		kinds = append(kinds, "payment.reconcile", "refund.reconcile", "transfer.reconcile", "transfer.bank_reconcile", "authorization.release.reconcile", "channel-sharing.reconcile", "channel-sharing-return.reconcile", "channel-sharing-finish.reconcile")
		for id := range flow.Providers {
			channels = append(channels, id)
		}
	}
	reconciler := reconciliation.Handler{Pool: pool, Flow: flow}
	slog.Info("payment worker started", "provider_reconciliation", flow != nil, "scan_recovery", recoveryFlow != nil)
	for ctx.Err() == nil {
		var j jobs.Job
		if flow == nil {
			j, e = q.Claim(ctx, kinds, time.Minute)
		} else {
			j, e = q.ClaimProviderChannels(ctx, kinds, time.Minute, channels)
		}
		if e != nil {
			if !jobs.IsEmpty(e) {
				slog.Error("job claim failed")
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second):
			}
			continue
		}
		jobCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		var recoveryDelay time.Duration
		if j.Kind == "payment.scan-recover" {
			recoveryDelay, e = recoveryFlow.ExecuteScanRecovery(jobCtx, j.ID, j.Token)
		} else if j.Kind == "admin.channel-stopped" {
			noticeSender, noticeTo, noticeError := noticeRuntime.Resolve(jobCtx)
			if noticeError != nil || noticeSender == nil {
				e = errChannelNoticeConfiguration
			} else {
				e = store.DeliverChannelStopNotice(jobCtx, j.ID, j.Token, noticeTo, noticeSender)
			}
		} else if j.Kind == "merchant.notice" {
			e = merchantNotices.Handle(jobCtx, j.ID, j.Token)
		} else if j.Kind == "admin.business-notice" {
			e = businessNotices.Handle(jobCtx, j.ID, j.Token)
		} else if j.Kind == "payment.notify" {
			e = h.Handle(jobCtx, j)
		} else {
			e = reconciler.Handle(jobCtx, j)
		}
		cancel()
		finishCtx, finishCancel := context.WithTimeout(context.Background(), 5*time.Second)
		if e == nil && j.Kind == "payment.scan-recover" && recoveryDelay > 0 {
			e = q.Retry(finishCtx, j, "original scan query interval", recoveryDelay)
		} else if e == nil {
			e = q.Complete(finishCtx, j)
		} else {
			shift := j.Attempts
			if shift > 10 {
				shift = 10
			}
			delay := time.Duration(1<<shift) * time.Second
			// Provider errors may contain upstream identifiers. Persist a fixed
			// diagnostic; the business record remains available for review.
			reason := "notification delivery failed"
			if j.Kind == "admin.channel-stopped" {
				reason = "administrator notice requires original claim review"
			} else if j.Kind == "merchant.notice" {
				reason = "merchant notice requires original configuration or claim review"
			} else if j.Kind == "admin.business-notice" {
				reason = "administrator business notice requires original source or claim review"
			} else if j.Kind != "payment.notify" {
				reason = "provider outcome unresolved; reservation preserved"
			}
			e = q.Retry(finishCtx, j, reason, delay)
		}
		finishCancel()
		if e != nil {
			slog.Error("job completion failed", "job_id", j.ID)
		}
	}
}
