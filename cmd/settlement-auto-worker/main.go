package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	adapter "goepay/internal/adapters/payment"
	"goepay/internal/bootstrap"
	"goepay/internal/channelconfig"
	"goepay/internal/payment"
	"goepay/internal/platform/jobs"
)

var errConfiguration = errors.New("explicit independent automatic wallet payout configuration required")
var errPayload = errors.New("invalid original automatic payout job")

func parseJob(raw json.RawMessage) (int64, int64, error) {
	if len(raw) == 0 || len(raw) > 1024 {
		return 0, 0, errPayload
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	token, e := d.Token()
	if e != nil || token != json.Delim('{') {
		return 0, 0, errPayload
	}
	fields := map[string]int64{}
	for d.More() {
		k, e := d.Token()
		key, ok := k.(string)
		if e != nil || !ok || (key != "request_id" && key != "authorization_id") || fields[key] != 0 {
			return 0, 0, errPayload
		}
		token, e = d.Token()
		value, ok := token.(string)
		if e != nil || !ok {
			return 0, 0, errPayload
		}
		id, e := strconv.ParseInt(value, 10, 64)
		if e != nil || id <= 0 || strconv.FormatInt(id, 10) != value {
			return 0, 0, errPayload
		}
		fields[key] = id
	}
	token, e = d.Token()
	if e != nil || token != json.Delim('}') || len(fields) != 2 {
		return 0, 0, errPayload
	}
	if _, e = d.Token(); e != io.EOF {
		return 0, 0, errPayload
	}
	return fields["request_id"], fields["authorization_id"], nil
}

func configuredParentImport() (channelconfig.Import, bool, error) {
	autoStop, e := channelAutoStopSetting(os.Getenv("GOEPAY_ENABLE_CHANNEL_AUTO_STOP"))
	if e != nil {
		return channelconfig.Import{}, false, e
	}
	if os.Getenv("GOEPAY_PROVIDER_MODE") != "configured" || os.Getenv("GOEPAY_SANDBOX") != "false" || os.Getenv("GOEPAY_ENABLE_PROVIDER_WRITES") != "true" {
		return channelconfig.Import{}, false, errConfiguration
	}
	master, e := hex.DecodeString(os.Getenv("GOEPAY_MASTER_KEY"))
	path := os.Getenv("GOEPAY_CHANNEL_CONFIG_FILE")
	if e != nil || len(master) != 32 || path == "" {
		return channelconfig.Import{}, false, errConfiguration
	}
	imp, e := channelconfig.Load(path, master)
	if e != nil {
		return channelconfig.Import{}, false, errConfiguration
	}
	return imp, autoStop, nil
}

func configuredFlow(stores ...*payment.Store) (*payment.ProviderFlow, error) {
	if len(stores) > 1 {
		return nil, errConfiguration
	}
	var store *payment.Store
	if len(stores) == 1 {
		store = stores[0]
	}
	imp, autoStop, e := configuredParentImport()
	if e != nil {
		return nil, e
	}
	var parentAccounts []adapter.ParentStartupAccount
	if store != nil {
		parentAccounts, e = store.ParentStartupAccounts(context.Background())
		if e != nil {
			return nil, errConfiguration
		}
	}
	startup, e := channelconfig.PrepareParentStartup(imp, parentAccounts)
	if e != nil {
		return nil, errConfiguration
	}
	providers, e := startup.BuildProviders(channelconfig.BuildOptions{AllowLive: true})
	if e != nil || !startup.HasRegisteredSources() {
		return nil, errConfiguration
	}
	mapping, e := startup.MerchantAllowlist(os.Getenv("GOEPAY_MERCHANT_CHANNELS"))
	if e != nil {
		return nil, errConfiguration
	}
	flow := &payment.ProviderFlow{Store: store, Providers: providers, MerchantChannels: mapping, AllowLive: true, ChannelAutoStopEnabled: autoStop}
	factories, e := startup.BuildFactories(channelconfig.ParentBuildOptions{BuildOptions: channelconfig.BuildOptions{AllowLive: true, RuntimeWriteAllowed: func() bool { return flow.AllowLive }}})
	if e != nil || flow.InstallParentAccountFactories(factories) != nil {
		return nil, errConfiguration
	}
	if len(flow.SettlementAutoWalletChannels()) == 0 && !startup.HasManagedAccounts() {
		return nil, errConfiguration
	}
	return flow, nil
}

type autoExecutor interface {
	ExecuteSettlementAutoAuthorized(context.Context, int64, int64, payment.TransferBatchCheck) (payment.SettlementPayout, error)
}

func handle(ctx context.Context, flow autoExecutor, j jobs.Job) error {
	if gate, ok := flow.(interface {
		VerifyScheduledJobClaim(context.Context, jobs.Job) error
	}); ok {
		if e := gate.VerifyScheduledJobClaim(ctx, j); e != nil {
			return e
		}
	}
	if j.Kind != payment.SettlementAutoJobKind || flow == nil {
		return errPayload
	}
	id, authority, e := parseJob(j.Payload)
	if e != nil {
		return e
	}
	out, e := flow.ExecuteSettlementAutoAuthorized(ctx, id, authority, nil)
	// The original durable reconciliation job owns outcome recovery. The
	// first-send job must not turn a lost provider response into a new send.
	if onlyUnknownOutcome(e) && out.SendState == "claimed" && out.State == "unknown" {
		return nil
	}
	return e
}

func onlyUnknownOutcome(e error) bool {
	if e == nil {
		return false
	}
	if _, ok := e.(*adapter.UnknownOutcome); ok {
		return true
	}
	if joined, ok := e.(interface{ Unwrap() []error }); ok {
		children := joined.Unwrap()
		if len(children) == 0 {
			return false
		}
		for _, child := range children {
			if !onlyUnknownOutcome(child) {
				return false
			}
		}
		return true
	}
	if child := errors.Unwrap(e); child != nil {
		return onlyUnknownOutcome(child)
	}
	return false
}

func run(ctx context.Context) error {
	switch os.Getenv("GOEPAY_SETTLEMENT_AUTO_ENABLED") {
	case "", "false":
		slog.Info("automatic settlement executor disabled; no configuration, database or provider access")
		return nil
	case "true":
	default:
		return errConfiguration
	}
	if _, _, e := configuredParentImport(); e != nil {
		return e
	}
	pool, store, e := bootstrap.Connect(ctx)
	if e != nil {
		return e
	}
	defer pool.Close()
	flow, e := configuredFlow(store)
	if e != nil {
		return e
	}
	q := jobs.Queue{Pool: pool, Policy: store}
	slog.Info("independent automatic wallet settlement executor started")
	nextScan := time.Time{}
	for ctx.Err() == nil {
		if !time.Now().Before(nextScan) {
			work, stop := context.WithTimeout(ctx, 20*time.Second)
			_, e = flow.QueueSettlementAuto(work, 20)
			stop()
			nextScan = time.Now().Add(30 * time.Second)
			if e != nil && ctx.Err() == nil {
				slog.Error("automatic payout scope scan rejected")
			}
		}
		j, e := q.Claim(ctx, []string{payment.SettlementAutoJobKind}, time.Minute)
		if e == nil {
			work, stop := context.WithTimeout(ctx, 20*time.Second)
			e = handle(work, flow, j)
			stop()
			finish, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			if e == nil {
				e = q.Complete(finish, j)
			} else {
				e = q.Retry(finish, j, "automatic payout held; inspect original authority and request", time.Minute)
			}
			cancel()
			if e != nil {
				slog.Error("automatic payout job completion unresolved")
			}
			continue
		}
		if !jobs.IsEmpty(e) && ctx.Err() == nil {
			slog.Error("automatic payout queue unavailable")
		}
		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}
	return nil
}
func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if e := run(ctx); e != nil {
		slog.Error("automatic settlement executor startup or execution failed")
		os.Exit(1)
	}
}
