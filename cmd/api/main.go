package main

import (
	"context"
	"crypto/rsa"
	"encoding/hex"
	"goepay/internal/bootstrap"
	"goepay/internal/compat"
	"goepay/internal/enrollment"
	"goepay/internal/identity"
	"goepay/internal/merchantnotice"
	"goepay/internal/migration"
	"goepay/internal/ordersubject"
	"goepay/internal/printing"
	"goepay/internal/server"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	proxyWrapper, e := server.NewTrustedProxy(os.Getenv("GOEPAY_TRUSTED_PROXY_CIDRS"))
	if e != nil {
		slog.Error("invalid trusted proxy configuration")
		os.Exit(1)
	}
	pool, store, e := bootstrap.Connect(ctx)
	if e != nil {
		slog.Error("startup failed")
		os.Exit(1)
	}
	defer pool.Close()
	var key *rsa.PrivateKey
	if raw := os.Getenv("GOEPAY_PLATFORM_PRIVATE_KEY_FILE"); raw != "" {
		b, err := os.ReadFile(raw)
		if err != nil {
			slog.Error("cannot read platform key")
			os.Exit(1)
		}
		key, e = compat.ParsePrivateKey(string(b))
		if e != nil {
			slog.Error("invalid platform key")
			os.Exit(1)
		}
	}
	app := &server.Server{Pool: pool, Store: store, Sessions: identity.NewPersistentSessions(pool, os.Getenv("COOKIE_SECURE") != "false"), PlatformKey: key, Sandbox: os.Getenv("GOEPAY_SANDBOX") == "true"}
	app.Printing, e = printing.New(pool, store)
	if e != nil {
		slog.Error("invalid printing protection configuration")
		os.Exit(1)
	}
	app.FundsActionResolver = app.ResolveFundsAction
	app.BusinessNotice = store.AdminBusinessNotices()
	seed, _ := hex.DecodeString(os.Getenv("GOEPAY_MASTER_KEY"))
	app.MerchantNotice, e = merchantnotice.NewStore(pool, seed)
	if e != nil {
		slog.Error("invalid merchant notice protection configuration")
		os.Exit(1)
	}
	app.LegacyRefundHistory, e = migration.NewRefundHistoryReader(pool, seed)
	if e != nil {
		slog.Error("invalid legacy history protection configuration")
		os.Exit(1)
	}
	app.MFA, e = identity.NewMFA(pool, seed)
	if e != nil {
		slog.Error("invalid authenticator encryption configuration")
		os.Exit(1)
	}
	verificationSender, e := configuredVerificationSender(os.Getenv)
	if e != nil {
		slog.Error("invalid verification delivery configuration")
		os.Exit(1)
	}
	enrollmentService, e := enrollment.New(pool, store, seed, verificationSender)
	if e != nil {
		slog.Error("invalid verification configuration")
		os.Exit(1)
	}
	consoleOrigin := os.Getenv("GOEPAY_CONSOLE_ORIGIN")
	if consoleOrigin == "" {
		consoleOrigin = os.Getenv("GOEPAY_PUBLIC_ORIGIN")
	}
	app.Enrollment = enrollment.NewHandler(enrollmentService, consoleOrigin)
	if e = configureProviders(app, os.Getenv("GOEPAY_PROVIDER_MODE"), seed, os.Getenv("GOEPAY_PUBLIC_ORIGIN")); e != nil {
		slog.Error("invalid local provider configuration")
		os.Exit(1)
	}
	if app.ProviderFlow != nil {
		app.ProviderFlow.SubjectResolver = ordersubject.New(pool).Resolve
	}
	addr := os.Getenv("HTTP_ADDR")
	if addr == "" {
		addr = "127.0.0.1:8080"
	}
	srv := &http.Server{Addr: addr, Handler: proxyWrapper(app.Handler()), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 1 << 16}
	go func() {
		<-ctx.Done()
		closeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		srv.Shutdown(closeCtx)
	}()
	slog.Info("api listening", "address", addr, "sandbox", app.Sandbox)
	if e = srv.ListenAndServe(); e != nil && e != http.ErrServerClosed {
		slog.Error("server failed")
		os.Exit(1)
	}
}
