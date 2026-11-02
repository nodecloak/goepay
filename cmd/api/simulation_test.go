package main

import (
	"bytes"
	"reflect"
	"testing"

	"goepay/internal/server"
)

func TestSimulationMerchantAllowlist(t *testing.T) {
	for _, tc := range []struct {
		name, raw string
		want      map[int64][]int64
	}{
		{"default", "", map[int64][]int64{1000: {1, 2}}},
		{"explicit", "1000,10000,9223372036854775807", map[int64][]int64{1000: {1, 2}, 10000: {1, 2}, 9223372036854775807: {1, 2}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("GOEPAY_SIMULATION_MERCHANT_IDS", tc.raw)
			app := &server.Server{Sandbox: true}
			if err := configureSimulation(app, "simulation", bytes.Repeat([]byte{1}, 32), "http://127.0.0.1:18260"); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(app.MerchantChannels, tc.want) || !app.ProviderFlow.LocalSimulation {
				t.Fatalf("unexpected simulation admission: %v", app.MerchantChannels)
			}
		})
	}
}

func TestSimulationMerchantAllowlistRejectsInvalidWithoutInstallingProviders(t *testing.T) {
	for _, raw := range []string{"*", "0", "-1", "01", "1,1", "1,", ",1", " 1", "1, 2", "9223372036854775808"} {
		t.Run(raw, func(t *testing.T) {
			t.Setenv("GOEPAY_SIMULATION_MERCHANT_IDS", raw)
			app := &server.Server{Sandbox: true}
			if err := configureSimulation(app, "simulation", bytes.Repeat([]byte{1}, 32), "http://127.0.0.1:18260"); err == nil {
				t.Fatal("invalid simulation admission accepted")
			}
			if app.Simulation != nil || app.ProviderFlow != nil || app.MerchantChannels != nil || len(app.CheckoutKey) != 0 {
				t.Fatal("failed configuration partially installed providers")
			}
		})
	}
}

func TestSimulationMerchantAllowlistCannotEnablePublicOrProductionSimulation(t *testing.T) {
	t.Setenv("GOEPAY_SIMULATION_MERCHANT_IDS", "10000")
	for _, tc := range []struct {
		sandbox bool
		origin  string
	}{
		{false, "http://127.0.0.1:18260"},
		{true, "https://pay.example.invalid"},
		{true, "http://0.0.0.0:18260"},
	} {
		app := &server.Server{Sandbox: tc.sandbox}
		if err := configureSimulation(app, "simulation", bytes.Repeat([]byte{1}, 32), tc.origin); err == nil {
			t.Fatal("nonlocal simulation accepted")
		}
		if app.ProviderFlow != nil || app.Simulation != nil {
			t.Fatal("nonlocal simulation installed providers")
		}
	}
}
