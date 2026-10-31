package main

import (
	"context"
	"crypto/rsa"
	"encoding/hex"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"goepay/internal/bootstrap"
	"goepay/internal/compat"
	"goepay/internal/mailaccount195"
	notice "goepay/internal/merchantnotice"
	"goepay/internal/notification"
	"goepay/internal/platform/httpclient"
	"goepay/internal/platform/jobs"
	"goepay/internal/proxyconfig"
	"goepay/internal/reconciliation"
	"goepay/internal/templatenotice"
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
	q := jobs.Queue{Pool: pool, Policy: store}
	h := notification.Handler{Store: store, Client: httpclient.New(), PlatformKey: key}
	if master, merr := hex.DecodeString(os.Getenv("GOEPAY_MASTER_KEY")); merr == nil && len(master) == 32 {
		if proxyStore, perr := proxyconfig.FromMaster(pool, master); perr == nil {
			// 中转代理: pick direct / proxy server / proxy API per the stored
			// settings at each send. A configured-but-unresolvable proxy fails
			// the delivery closed rather than sending direct.
			h.Planner = proxyPlanner{store: proxyStore}
		}
	}
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
	if key != nil {
		kinds = append(kinds, "merchant.refund-event", "merchant.complaint-event")
	}
	if recoveryFlow != nil {
		kinds = append(kinds, "payment.scan-recover")
	}
	mailMaster195, e := hex.DecodeString(os.Getenv("GOEPAY_MASTER_KEY"))
	if e != nil || len(mailMaster195) != 32 {
		slog.Error("invalid mail account protection configuration")
		os.Exit(1)
	}
	mailAccounts195 := mailaccount195.FromMaster(pool, mailMaster195)
	kinds = append(kinds, "merchant.refund-event-closed-notice")
	noticeRuntime, e := newManagedChannelNoticeRuntime195(ctx, store, os.Getenv, mailAccounts195)
	if e != nil {
		slog.Error("administrator notice configuration rejected")
		os.Exit(1)
	}
	if noticeRuntime != nil {
		kinds = append(kinds, "admin.channel-stopped", "admin.page-template-changed")
	}
	merchantNotices, e := newMerchantNoticeRuntime(pool, os.Getenv)
	if e != nil {
		slog.Error("merchant notice configuration rejected")
		os.Exit(1)
	}
	if merchantNotices != nil {
		kinds = append(kinds, "merchant.notice", "merchant.payment-disabled-notice")
	}
	businessNotices, e := newBusinessNoticeRuntime(store.AdminBusinessNotices(), os.Getenv)
	if e != nil {
		slog.Error("administrator business notice configuration rejected")
		os.Exit(1)
	}
	if businessNotices != nil {
		businessNotices.mailAccounts = mailAccounts195
		kinds = append(kinds, "admin.business-notice")
	}
	coreRiskNotices, e := newCoreRiskNoticeRuntime(store, os.Getenv)
	if e != nil {
		slog.Error("core risk notice configuration rejected")
		os.Exit(1)
	}
	if coreRiskNotices != nil {
		kinds = append(kinds, "core-risk.notice")
	}
	var channels []int64
	if flow != nil {
		kinds = append(kinds, "payment.reconcile", "refund.reconcile", "transfer.reconcile", "transfer.bank_reconcile", "authorization.release.reconcile", "channel-sharing.reconcile", "channel-sharing-return.reconcile", "channel-sharing-finish.reconcile")
		for id := range flow.Providers {
			channels = append(channels, id)
		}
	}
	reconciler := reconciliation.Handler{Pool: pool, Flow: flow}
	// Unclaimed red packets past 24h become 已过期. Claims already enforce the
	// deadline, and nothing is reserved for them, so this only updates status.
	go func() {
		for ctx.Err() == nil {
			if _, err := store.ExpireRedpacks(ctx); err != nil && ctx.Err() == nil {
				slog.Error("red packet expiry failed")
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Minute):
			}
		}
	}()
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
		var notificationFailure string
		if j.Kind == "payment.scan-recover" {
			recoveryDelay, e = recoveryFlow.ExecuteScanRecovery(jobCtx, j.ID, j.Token)
		} else if j.Kind == "admin.channel-stopped" {
			noticeSender, noticeTo, noticeError := noticeRuntime.Resolve(jobCtx)
			if noticeError != nil || noticeSender == nil {
				e = errChannelNoticeConfiguration
			} else {
				e = store.DeliverChannelStopNotice(jobCtx, j.ID, j.Token, noticeTo, noticeSender)
			}
		} else if j.Kind == "admin.page-template-changed" {
			noticeSender, noticeTo, noticeError := noticeRuntime.Resolve(jobCtx)
			changeSender, ok := noticeSender.(templatenotice.ChangeNoticeSender)
			if noticeError != nil || noticeSender == nil || !ok {
				e = errChannelNoticeConfiguration
			} else {
				e = templatenotice.New(pool).Deliver(jobCtx, j.ID, j.Token, noticeTo, changeSender)
			}
		} else if j.Kind == "merchant.notice" {
			e = merchantNotices.Handle(jobCtx, j.ID, j.Token)
		} else if j.Kind == "merchant.payment-disabled-notice" {
			sender, resolveErr := merchantNotices.ResolveMerchantNotice(jobCtx, notice.SMTP, notice.Account{})
			if resolveErr != nil || sender == nil {
				e = errMerchantNoticeConfiguration
			} else {
				e = store.DeliverScheduledPaymentDisabledNotice(jobCtx, j.ID, j.Token, sender)
			}
		} else if j.Kind == "admin.business-notice" {
			e = businessNotices.Handle(jobCtx, j.ID, j.Token)
		} else if j.Kind == "core-risk.notice" {
			e = coreRiskNotices.Handle(jobCtx, j.ID, j.Token)
		} else if j.Kind == "merchant.refund-event" {
			e = store.DeliverMerchantRefundEvent201(jobCtx, j, key, h.Client)
		} else if j.Kind == "merchant.complaint-event" {
			e = store.DeliverMerchantComplaintEvent201(jobCtx, j, key, h.Client)
		} else if j.Kind == "merchant.refund-event-closed-notice" {
			sender, managed, resolveErr := mailAccounts195.ResolveSMTP(jobCtx)
			if resolveErr != nil || !managed || sender == nil {
				e = errors.New("managed refund subscription closure mail unavailable")
			} else {
				e = store.DeliverMerchantRefundEventClosedNotice201(jobCtx, j, sender)
			}
		} else if j.Kind == "payment.notify" {
			e = h.Handle(jobCtx, j)
			if code, ok := notification.FailureCode(e, j); ok {
				notificationFailure = code
			}
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
			} else if j.Kind == "admin.page-template-changed" {
				reason = "administrator template notice requires original claim review"
			} else if j.Kind == "merchant.notice" || j.Kind == "merchant.payment-disabled-notice" {
				reason = "merchant notice requires original configuration or claim review"
			} else if j.Kind == "admin.business-notice" {
				reason = "administrator business notice requires original source or claim review"
			} else if j.Kind == "core-risk.notice" {
				reason = "core risk notice requires original source or claim review"
			} else if j.Kind == "merchant.refund-event" || j.Kind == "merchant.refund-event-closed-notice" {
				reason = "refund event requires original source or unsent configuration review"
			} else if j.Kind != "payment.notify" {
				reason = "provider outcome unresolved; reservation preserved"
			}
			if notificationFailure != "" {
				e = q.RetryNotificationFailure(finishCtx, j, notificationFailure, delay)
			} else {
				e = q.Retry(finishCtx, j, reason, delay)
			}
		}
		finishCancel()
		if e != nil {
			slog.Error("job completion failed", "job_id", j.ID)
		}
	}
}
