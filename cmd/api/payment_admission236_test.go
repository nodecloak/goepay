package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"goepay/db"
	"goepay/internal/payment"
	"goepay/internal/server"
)

func TestPaymentAdmission236ReadOnlyHostBootstrap(t *testing.T) {
	dsn := os.Getenv("GOEPAY_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("GOEPAY_TEST_DATABASE_URL required")
	}
	ctx := context.Background()
	root, e := pgxpool.New(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	defer root.Close()
	entropy := make([]byte, 12)
	if _, e = rand.Read(entropy); e != nil {
		t.Fatal(e)
	}
	schema := "inspect236_" + hex.EncodeToString(entropy)
	if _, e = root.Exec(ctx, "CREATE SCHEMA "+schema); e != nil {
		t.Fatal(e)
	}
	defer root.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE")
	u, e := url.Parse(dsn)
	if e != nil {
		t.Fatal(e)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	pool, e := pgxpool.New(ctx, u.String())
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	if e = db.Migrate(ctx, pool); e != nil {
		t.Fatal(e)
	}
	key := bytes.Repeat([]byte{23}, 32)
	store, e := payment.NewWithKey(pool, key)
	if e != nil {
		t.Fatal(e)
	}
	if e = store.CreateMerchant(ctx, payment.Merchant{ID: 10003, Name: "Synthetic inspection", APIKey: "synthetic-inspection-key-236"}); e != nil {
		t.Fatal(e)
	}
	snapshot := func() string {
		var v string
		if e := pool.QueryRow(ctx, `SELECT json_build_array((SELECT count(*) FROM merchants),(SELECT count(*) FROM orders),(SELECT count(*) FROM journals),(SELECT count(*) FROM accounts),(SELECT count(*) FROM jobs),(SELECT count(*) FROM backoffice_records),(SELECT count(*) FROM backoffice_audit))::text`).Scan(&v); e != nil {
			t.Fatal(e)
		}
		return v
	}
	before := snapshot()
	// The DSN is independently read-only as well as the diagnostic's own pool.
	q.Set("default_transaction_read_only", "on")
	u.RawQuery = q.Encode()
	t.Setenv("DATABASE_URL", u.String())
	t.Setenv("GOEPAY_MASTER_KEY", hex.EncodeToString(key))
	t.Setenv("GOEPAY_SANDBOX", "true")
	t.Setenv("GOEPAY_PROVIDER_MODE", "simulation")
	t.Setenv("GOEPAY_PUBLIC_ORIGIN", "http://127.0.0.1:18260")
	t.Setenv("GOEPAY_SIMULATION_MERCHANT_IDS", "1000")
	t.Setenv("GOEPAY_DEFAULT_PAYMENT_CHANNELS", `[1,2]`)
	var output bytes.Buffer
	if e = runPaymentInspection236(ctx, []string{"check-payment-admission", "10003"}, &output); e != nil {
		t.Fatal("read-only host inspection failed", e)
	}
	var result struct {
		ReadOnly bool                               `json:"read_only"`
		Mer      []server.PaymentAdmissionReport236 `json:"merchants"`
	}
	if e = json.Unmarshal(output.Bytes(), &result); e != nil || !result.ReadOnly || len(result.Mer) != 1 || result.Mer[0].Source != "default" || len(result.Mer[0].PaymentChannels) != 2 || len(result.Mer[0].OperatorChannels) != 0 {
		t.Fatal("inspection did not reflect collection-only defaults", output.String(), e)
	}
	if snapshot() != before {
		t.Fatal("read-only inspection wrote startup projection/styles/audit/business data")
	}
}

func TestPaymentAdmission236HostArguments(t *testing.T) {
	ids, e := inspectionMerchantIDs236([]string{"check-payment-admission", "10000,10003,9223372036854775807"})
	if e != nil || !reflect.DeepEqual(ids, []int64{10000, 10003, 9223372036854775807}) {
		t.Fatal(ids, e)
	}
	for _, args := range [][]string{nil, {"other", "1"}, {"check-payment-admission"}, {"check-payment-admission", ""}, {"check-payment-admission", "0"}, {"check-payment-admission", "01"}, {"check-payment-admission", "1,1"}, {"check-payment-admission", "1,"}, {"check-payment-admission", "1, 2"}, {"check-payment-admission", "9223372036854775808"}, {"check-payment-admission", strings.Repeat("1,", 51) + "2"}} {
		if _, e := inspectionMerchantIDs236(args); e == nil {
			t.Fatal("ambiguous inspection arguments accepted", args)
		}
	}
}

func TestSimulationDefaultPayment236DoesNotExpandGenericMerchantMap(t *testing.T) {
	t.Setenv("GOEPAY_DEFAULT_PAYMENT_CHANNELS", `[2,1]`)
	t.Setenv("GOEPAY_SIMULATION_MERCHANT_IDS", "1000")
	app := &server.Server{Sandbox: true}
	if e := configureSimulation(app, "simulation", bytes.Repeat([]byte{1}, 32), "http://127.0.0.1:18260"); e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(app.DefaultPaymentChannels, []int64{1, 2}) || len(app.MerchantChannels) != 1 || len(app.MerchantChannels[10003]) != 0 || len(app.ProviderFlow.MerchantChannels[10003]) != 0 {
		t.Fatal("defaults changed generic merchant grant")
	}
	t.Setenv("GOEPAY_DEFAULT_PAYMENT_CHANNELS", `[3]`)
	bad := &server.Server{Sandbox: true}
	if e := configureSimulation(bad, "simulation", bytes.Repeat([]byte{1}, 32), "http://127.0.0.1:18260"); e == nil || bad.ProviderFlow != nil {
		t.Fatal("unregistered default source accepted")
	}
}
