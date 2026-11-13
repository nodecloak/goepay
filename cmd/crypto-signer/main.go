// crypto-signer runs on a separate service account/host from the API. It owns
// treasury key files and a durable journal, and accepts only mTLS USDT requests.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"goepay/internal/cryptosigner"
)

type commandConfig struct {
	cryptosigner.Config
	Listen            string            `json:"listen"`
	ServerCertificate string            `json:"server_certificate"`
	ServerKey         string            `json:"server_key"`
	ClientCA          string            `json:"client_ca"`
	AllowedClientSANs []string          `json:"allowed_client_sans"`
	Keys              map[string]string `json:"keys"`
}

func loadConfig(path string) (commandConfig, error) {
	var cfg commandConfig
	f, err := os.Open(path)
	if err != nil {
		return cfg, err
	}
	defer f.Close()
	d := json.NewDecoder(io.LimitReader(f, 1<<20))
	d.DisallowUnknownFields()
	if err = d.Decode(&cfg); err != nil {
		return cfg, err
	}
	if err = d.Decode(new(any)); err != io.EOF {
		return cfg, fmt.Errorf("trailing signer configuration")
	}
	if cfg.Listen == "" || cfg.ServerCertificate == "" || cfg.ServerKey == "" || cfg.ClientCA == "" || len(cfg.AllowedClientSANs) == 0 || len(cfg.Keys) == 0 || cfg.StatePath == "" || len(cfg.Rules) == 0 {
		return cfg, fmt.Errorf("signer requires explicit listen, mTLS identity/CA/client SANs, key file handles, journal and policy rules")
	}
	if _, _, err = net.SplitHostPort(cfg.Listen); err != nil {
		return cfg, fmt.Errorf("invalid signer listen address: %w", err)
	}
	for _, san := range cfg.AllowedClientSANs {
		if san == "" || strings.Contains(san, "*") {
			return cfg, fmt.Errorf("signer client SANs must be exact nonempty identities")
		}
	}
	for key, path := range cfg.Keys {
		if key == "" || path == "" {
			return cfg, fmt.Errorf("empty signer key file handle")
		}
	}
	return cfg, nil
}

func run(ctx context.Context, cfg commandConfig) error {
	tlsConfig, err := cryptosigner.ServerTLSConfig(cfg.ServerCertificate, cfg.ServerKey, cfg.ClientCA)
	if err != nil {
		return fmt.Errorf("load signer mTLS: %w", err)
	}
	keys, err := cryptosigner.NewFileKeys(cfg.Keys)
	if err != nil {
		return fmt.Errorf("load signer key files: %w", err)
	}
	service, err := cryptosigner.NewService(cfg.Config, keys)
	if err != nil {
		return err
	}
	defer service.Close()
	server := &http.Server{Addr: cfg.Listen, TLSConfig: tlsConfig, Handler: cryptosigner.NewHandler(service, cfg.AllowedClientSANs), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 45 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10}
	done := make(chan error, 1)
	go func() { done <- server.ListenAndServeTLS("", "") }()
	select {
	case err := <-done:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil {
			server.Close()
			return err
		}
		err := <-done
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func main() {
	path := flag.String("config", "", "signer-only JSON configuration path")
	flag.Parse()
	if *path == "" {
		fmt.Fprintln(os.Stderr, "crypto-signer requires --config with signer-only mTLS/key/policy configuration")
		os.Exit(2)
	}
	cfg, err := loadConfig(*path)
	if err != nil {
		log.Printf("crypto-signer configuration rejected: %v", err)
		os.Exit(1)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err = run(ctx, cfg); err != nil {
		log.Printf("crypto-signer stopped: %v", err)
		os.Exit(1)
	}
}
