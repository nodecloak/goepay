// crypto-worker observes configured nodes and dispatches durable USDT intents.
// It receives an mTLS client identity; treasury private keys belong only to the
// separately deployed crypto-signer process.
package main

import (
	"context"
	"crypto/hmac"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"goepay/internal/bootstrap"
	"goepay/internal/compat"
	"goepay/internal/cryptochain"
	"goepay/internal/cryptopay"
	"goepay/internal/cryptosigner"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

type networkConfig struct {
	cryptopay.NetworkConfig
	PrimaryRPC     string      `json:"primary_rpc"`
	WitnessRPC     string      `json:"witness_rpc"`
	PrimaryHeaders http.Header `json:"primary_headers"`
	WitnessHeaders http.Header `json:"witness_headers"`
	StartHeight    uint64      `json:"start_height"`
	FeeLimit       uint64      `json:"fee_limit"`
}
type config struct {
	Networks        map[string]networkConfig `json:"networks"`
	EnableBroadcast bool                     `json:"enable_broadcast"`
	EnableSweeps    bool                     `json:"enable_sweeps"`
	SignerURL       string                   `json:"signer_url"`
	ClientCert      string                   `json:"client_cert"`
	ClientKey       string                   `json:"client_key"`
	SignerCA        string                   `json:"signer_ca"`
}

func parseConfig(r io.Reader) (config, error) {
	var c config
	d := json.NewDecoder(io.LimitReader(r, 1<<20))
	d.DisallowUnknownFields()
	if e := d.Decode(&c); e != nil {
		return c, e
	}
	if e := d.Decode(new(any)); e != io.EOF {
		return c, errors.New("trailing worker configuration")
	}
	if len(c.Networks) == 0 {
		return c, errors.New("explicit network configuration required")
	}
	if c.EnableSweeps && !c.EnableBroadcast {
		return c, errors.New("sweep requires explicit broadcast gate")
	}
	if c.EnableBroadcast && (c.SignerURL == "" || c.ClientCert == "" || c.ClientKey == "" || c.SignerCA == "") {
		return c, errors.New("isolated signer mTLS configuration required")
	}
	for name, n := range c.Networks {
		if name != "tron" && name != "ethereum" || n.StartHeight == 0 || n.FeeBPS < 0 || n.FeeBPS > 10000 || n.PrimaryRPC == "" || n.WitnessRPC == "" || n.PrimaryRPC == n.WitnessRPC {
			return c, errors.New("network requires distinct node endpoints, start height and fee policy")
		}
		if c.EnableBroadcast {
			if n.FeeLimit == 0 {
				return c, errors.New("explicit fee limit required")
			}
			if _, e := cryptochain.NormalizeAddress(cryptochain.Chain(name), n.HotAddress); e != nil {
				return c, e
			}
			if _, e := cryptochain.NormalizeAddress(cryptochain.Chain(name), n.TreasuryAddress); e != nil {
				return c, e
			}
		}
	}
	return c, nil
}
func main() {
	file := flag.String("config", "", "worker JSON configuration")
	auditOnly := flag.Bool("audit", false, "read-only custody and ledger reconciliation; never scan/sign/broadcast")
	once := flag.Bool("once", false, "one bounded observation/dispatch iteration")
	flag.Parse()
	if *file == "" {
		slog.Error("crypto-worker requires --config")
		os.Exit(2)
	}
	f, e := os.Open(*file)
	if e != nil {
		slog.Error("worker configuration unavailable")
		os.Exit(1)
	}
	cfg, e := parseConfig(f)
	f.Close()
	if e != nil {
		slog.Error("worker configuration rejected")
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if e = run(ctx, cfg, *once, *auditOnly); e != nil {
		slog.Error("crypto worker stopped; inspect configured service and original intents")
		os.Exit(1)
	}
}
func run(ctx context.Context, c config, once, auditOnly bool) error {
	if auditOnly {
		c.EnableBroadcast = false
		c.EnableSweeps = false
	}
	pool, store, e := bootstrap.Connect(ctx)
	if e != nil {
		return e
	}
	defer pool.Close()
	master, e := hex.DecodeString(os.Getenv("GOEPAY_MASTER_KEY"))
	if e != nil || len(master) != 32 {
		return cryptopay.ErrUnavailable
	}
	h := hmac.New(sha256.New, master)
	h.Write([]byte("GOEPAY/crypto-token-key/v1"))
	svc := &cryptopay.Service{Pool: pool, TokenKey: h.Sum(nil), Networks: map[string]cryptopay.NetworkConfig{}}
	scanner := &cryptopay.Scanner{Service: svc, Primary: map[string]cryptochain.Adapter{}, Witness: map[string]cryptochain.Adapter{}, StartHeight: map[string]uint64{}}
	executor := &cryptopay.Executor{Service: svc, Adapters: scanner.Primary, Witness: scanner.Witness, FeeLimits: map[string]uint64{}}
	listenerFingerprints := make(map[string]string, len(c.Networks))
	for name, n := range c.Networks {
		if n.HotAddress != "" {
			n.HotAddress, e = cryptochain.NormalizeAddress(cryptochain.Chain(name), n.HotAddress)
			if e != nil {
				return e
			}
		}
		if n.TreasuryAddress != "" {
			n.TreasuryAddress, e = cryptochain.NormalizeAddress(cryptochain.Chain(name), n.TreasuryAddress)
			if e != nil {
				return e
			}
		}
		svc.Networks[name] = n.NetworkConfig
		if n.Enabled && !auditOnly && !once {
			fingerprint, err := cryptopay.ListenerFingerprint(name, n.NetworkConfig)
			if err != nil {
				slog.Warn("USDT listener wallet configuration incomplete", "network", name)
			} else {
				listenerFingerprints[name] = fingerprint
			}
		}
		scanner.StartHeight[name] = n.StartHeight
		executor.FeeLimits[name] = n.FeeLimit
		primary := cryptochain.Config{Endpoint: n.PrimaryRPC, Headers: n.PrimaryHeaders, AllowBroadcast: c.EnableBroadcast}
		witness := cryptochain.Config{Endpoint: n.WitnessRPC, Headers: n.WitnessHeaders}
		if name == "tron" {
			scanner.Primary[name], e = cryptochain.NewTron(primary)
			if e != nil {
				return e
			}
			scanner.Witness[name], e = cryptochain.NewTron(witness)
		} else {
			scanner.Primary[name], e = cryptochain.NewEthereum(primary)
			if e != nil {
				return e
			}
			scanner.Witness[name], e = cryptochain.NewEthereum(witness)
		}
		if e != nil {
			return e
		}
	}
	if auditOnly {
		ledger, e := svc.Reconcile(ctx)
		if e != nil {
			return e
		}
		out := map[string]any{"ledger": ledger, "wallets": []cryptopay.WalletAudit{}}
		wallets := []cryptopay.WalletAudit{}
		good := ledger.ImbalancedJournals == 0 && ledger.BalanceMismatches == 0 && ledger.NegativeBalances == 0
		for name, n := range c.Networks {
			if !n.Enabled {
				continue
			}
			w, e := executor.AuditWallets(ctx, name)
			if e != nil {
				return e
			}
			wallets = append(wallets, w)
			good = good && w.Stable && w.DifferenceAtomic == "0"
		}
		out["wallets"] = wallets
		out["consistent"] = good
		if e = json.NewEncoder(os.Stdout).Encode(out); e != nil {
			return e
		}
		if !good {
			return cryptopay.ErrConflict
		}
		return nil
	}
	if c.EnableBroadcast {
		executor.Signer, e = cryptosigner.NewMTLSClient(c.SignerURL, c.ClientCert, c.ClientKey, c.SignerCA)
		if e != nil {
			return e
		}
	}
	var key *rsa.PrivateKey
	if path := os.Getenv("GOEPAY_PLATFORM_PRIVATE_KEY_FILE"); path != "" {
		raw, e := os.ReadFile(path)
		if e != nil {
			return e
		}
		key, e = compat.ParsePrivateKey(string(raw))
		if e != nil {
			return e
		}
	}
	notifier := &cryptopay.Notifier{Service: svc, Merchants: store, PlatformKey: key}
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return nil
		}
		var iterationError bool
		for name, n := range c.Networks {
			if !n.Enabled {
				continue
			}
			if e = scanListener(ctx, scanner, name, listenerFingerprints[name]); e != nil {
				slog.Warn("USDT scanner paused", "network", name)
				iterationError = true
				continue
			}
			if c.EnableSweeps {
				if e = executor.RunSweeps(ctx, name); e != nil {
					iterationError = true
					slog.Warn("USDT sweep queue pending", "network", name)
				}
			}
			if c.EnableBroadcast {
				if e = executor.RunPendingFor(ctx, name); e != nil {
					iterationError = true
					slog.Warn("USDT outgoing operations require original-transaction reconciliation", "network", name)
				}
			}
		}
		if hide, e := store.NotifyHideName(ctx); e == nil {
			notifier.HideName = hide
		} else {
			return e
		}
		for i := 0; i < 20; i++ {
			did, e := notifier.RunOne(ctx)
			if e != nil {
				iterationError = true
				slog.Warn("USDT callback pending retry")
			}
			if !did {
				break
			}
		}
		if once {
			if iterationError {
				return cryptopay.ErrUnavailable
			}
			return nil
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}
