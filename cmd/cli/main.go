package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"goepay/db"
	"goepay/internal/bootstrap"
	"goepay/internal/businessnotice"
	"goepay/internal/channelconfig"
	"goepay/internal/identity"
	notice "goepay/internal/merchantnotice"
	"goepay/internal/migration"
	"goepay/internal/payment"
	"goepay/internal/printing"
	"goepay/internal/systemupdate"
	"os"
	"sort"
	"time"
)

// Keep both independent JSON namespaces; anonymous embedding of two types
// named IntegrityCheck is invalid even when their package qualifiers differ.
type BusinessNoticeIntegrity = businessnotice.IntegrityCheck
type reconciliationReport struct {
	payment.LedgerCheck
	InvalidPrintingRecords int64 `json:"invalid_printing_records"`
	migration.RefundHistoryCheck
	notice.IntegrityCheck
	BusinessNoticeIntegrity
}

func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
func run() error {
	if len(os.Args) != 2 {
		return fmt.Errorf("usage: cli migrate|seed-admin|seed-local|reconcile|check-channels|seal-channels|gen-platform-key|verify-update|build-info")
	}
	if os.Args[1] == "build-info" {
		return json.NewEncoder(os.Stdout).Encode(systemupdate.RuntimeBuild(0))
	}
	if os.Args[1] == "seal-channels" {
		key, e := masterKeyFromEnvironment()
		if e != nil {
			return e
		}
		return sealChannels(os.Stdin, os.Stdout, os.Stderr, key)
	}
	if os.Args[1] == "gen-platform-key" {
		return generatePlatformKey(os.Getenv("GOEPAY_PLATFORM_PRIVATE_KEY_FILE"), os.Stdout)
	}
	if os.Args[1] == "check-channels" {
		key, e := hex.DecodeString(os.Getenv("GOEPAY_MASTER_KEY"))
		if e != nil || len(key) != 32 {
			return fmt.Errorf("GOEPAY_MASTER_KEY must be 64 hex characters")
		}
		file := os.Getenv("GOEPAY_CHANNEL_IMPORT_FILE")
		if file == "" {
			file = ".secrets/imported-channels.enc.json"
		}
		imp, e := channelconfig.Load(file, key)
		if e != nil {
			return e
		}
		clients, e := channelconfig.Build(imp, false)
		if e != nil {
			return e
		}
		ids := make([]int64, 0, len(clients))
		for id := range clients {
			ids = append(ids, id)
		}
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		rows := []map[string]any{}
		for _, id := range ids {
			p := clients[id]
			capabilities := p.Capabilities()
			// Without refund query support an unknown refund outcome cannot be
			// reconciled automatically; it stays reserved until checked upstream.
			reconciliation := "automatic"
			if capabilities.Refund && !capabilities.RefundQuery {
				reconciliation = "manual"
			}
			rows = append(rows, map[string]any{"id": id, "plugin": p.ID(), "capabilities": capabilities, "unknown_refund_reconciliation": reconciliation, "live_operations_enabled": false})
		}
		return json.NewEncoder(os.Stdout).Encode(rows)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, store, e := bootstrap.Connect(ctx)
	if e != nil {
		return e
	}
	defer pool.Close()
	switch os.Args[1] {
	case "verify-update":
		return verifyLocalUpdate183(ctx, pool, os.Stdout)
	case "migrate":
		return db.Migrate(ctx, pool)
	case "seed-admin":
		created, e := seedMigrationAdmin234(ctx, pool, os.Getenv("GOEPAY_SEED_PASSWORD"))
		if e != nil {
			return e
		}
		if created {
			// A new installation starts with merchant API-key console login off:
			// a leaked V1 signing key must not double as a console password.
			// Super administrators can enable it under platform settings.
			if _, e = store.InitializePlatformMerchantAccess(ctx, identity.User{ID: 1, Username: "admin"}, "1", "1"); e != nil {
				return fmt.Errorf("initialize merchant access defaults: %w", e)
			}
			fmt.Println("Migration administrator initialized; no merchant or financial data was created.")
			fmt.Println("Merchant API-key console login is disabled by default; enable it in platform settings only if merchants need it.")
		} else {
			fmt.Println("Existing administrator preserved; credentials were not changed.")
		}
		return nil
	case "seed-local":
		if os.Getenv("GOEPAY_SANDBOX") != "true" {
			return fmt.Errorf("sandbox required")
		}
		password := os.Getenv("GOEPAY_SEED_PASSWORD")
		key := os.Getenv("GOEPAY_SEED_API_KEY")
		hash, e := identity.HashPassword(password)
		if e != nil {
			return e
		}
		if len(key) < 24 {
			return fmt.Errorf("GOEPAY_SEED_API_KEY minimum24 characters")
		}
		if _, e = store.Merchant(ctx, 1000); e != nil {
			if e = store.CreateMerchant(ctx, payment.Merchant{ID: 1000, Name: "Local sandbox merchant", APIKey: key}); e != nil {
				return e
			}
		}
		for _, r := range []string{"admin", "merchant"} {
			id := int64(1)
			if r == "merchant" {
				id = 1000
			}
			class := "operator"
			if r == "admin" {
				class = "super"
			}
			if _, e = pool.Exec(ctx, "INSERT INTO console_users(username,role,subject_id,display_name,password_hash,admin_class) VALUES($1,$1,$2,$1,$3,$4) ON CONFLICT DO NOTHING", r, id, hash, class); e != nil {
				return e
			}
		}
		fmt.Println("Local sandbox identities seeded; existing credentials are not overwritten.")
		return nil
	case "reconcile":
		report, e := store.CheckLedger(ctx)
		if e != nil {
			return e
		}
		printer, e := printing.New(pool, store)
		if e != nil {
			return e
		}
		invalidPrinting, e := printer.Check(ctx)
		if e != nil {
			return e
		}
		master, e := hex.DecodeString(os.Getenv("GOEPAY_MASTER_KEY"))
		if e != nil {
			return e
		}
		history, e := migration.CheckRefundHistory(ctx, pool, master)
		if e != nil {
			return e
		}
		notifications, e := notice.NewStore(pool, master)
		if e != nil {
			return e
		}
		noticeReport, e := notifications.Check(ctx)
		if e != nil {
			return e
		}
		businessReport, e := store.AdminBusinessNotices().Check(ctx)
		if e != nil {
			return e
		}
		result := reconciliationReport{report, invalidPrinting, history, noticeReport, businessReport}
		if e = json.NewEncoder(os.Stdout).Encode(result); e != nil {
			return e
		}
		if report.InvalidCollectionGenerators != 0 || report.InvalidSettlementBatches != 0 || businessReport.InvalidEvents != 0 || businessReport.InvalidDeliveries != 0 {
			return fmt.Errorf("business notice reconciliation failed")
		}
		if noticeReport.InvalidProfiles != 0 || noticeReport.InvalidEvents != 0 || noticeReport.InvalidDeliveries != 0 || report.InvalidScanRecoverySources != 0 || report.InvalidChannelStops != 0 || report.InvalidAuthorizationReleases != 0 || report.InvalidJournals != 0 || report.InvalidAccounts != 0 || report.InvalidSettlementFundings != 0 || report.InvalidChannelSharingFundings != 0 || report.InvalidChannelSharingReturns != 0 || report.InvalidChannelSharingFinishes != 0 || report.InvalidReferralTerms != 0 || report.InvalidReferralRewards != 0 || report.InvalidReferralClawbacks != 0 || report.InvalidProviderRefundResults != 0 || report.InvalidSettlementPlans != 0 || report.InvalidOrderIncomeFundings != 0 || report.InvalidRefundFundings != 0 || invalidPrinting != 0 || history.InvalidLegacyRefundHistory != 0 || history.UnknownLegacyRefunds != 0 {
			return fmt.Errorf("reconciliation failed")
		}
		return nil
	}
	return fmt.Errorf("unknown command")
}
