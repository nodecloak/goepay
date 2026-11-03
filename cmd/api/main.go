package main

import (
	"context"
	"crypto/rsa"
	"encoding/hex"
	"encoding/json"
	"goepay/internal/backoffice"
	"goepay/internal/bootstrap"
	"goepay/internal/captcha203"
	"goepay/internal/compat"
	"goepay/internal/enrollment"
	"goepay/internal/identity"
	"goepay/internal/merchantnotice"
	"goepay/internal/migration"
	"goepay/internal/ordersubject"
	"goepay/internal/paymentdefend206"
	"goepay/internal/printing"
	"goepay/internal/server"
	"goepay/internal/systemupdate"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == "--build-info" {
		if e := json.NewEncoder(os.Stdout).Encode(systemupdate.RuntimeBuild(0)); e != nil {
			os.Exit(1)
		}
		return
	}
	if len(os.Args) > 1 {
		if e := runPaymentInspection236(context.Background(), os.Args[1:], os.Stdout); e != nil {
			slog.Error("payment admission inspection failed")
			os.Exit(1)
		}
		return
	}
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
	if e = store.RebuildPrivateSearch(ctx); e != nil {
		slog.Error("private search projection unavailable")
		os.Exit(1)
	}
	// A8-D2: seed the original out-of-box collection-code styles on an install
	// that has none (idempotent; never touches an install that already has
	// styles). A failure here must not stop the service — the console still
	// runs, an admin can create styles by hand — so it is logged, not fatal.
	if seeded, err := backoffice.New(pool, store).EnsureDefaultCollectionStyles(ctx); err != nil {
		slog.Warn("default collection styles not seeded", "error", err)
	} else if seeded > 0 {
		slog.Info("seeded default collection styles", "count", seeded)
	}
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
	app.Mail195TestEnabled, e = mailTestGate195(os.Getenv)
	if e != nil {
		slog.Error("invalid SMTP test submission configuration")
		os.Exit(1)
	}
	app.SystemUpdate, e = systemupdate.FromEnvironment(os.Getenv)
	if e != nil {
		slog.Error("invalid trusted local update configuration")
		os.Exit(1)
	}
	defer store.StopSystemUpdates()
	app.Printing, e = printing.New(pool, store)
	if e != nil {
		slog.Error("invalid printing protection configuration")
		os.Exit(1)
	}
	app.FundsActionResolver = app.ResolveFundsAction
	app.BusinessNotice = store.AdminBusinessNotices()
	seed, _ := hex.DecodeString(os.Getenv("GOEPAY_MASTER_KEY"))
	app.MigrationWizard234, e = migration.NewWizard234(pool, seed)
	if e != nil {
		slog.Error("invalid migration protection configuration")
		os.Exit(1)
	}
	app.PaymentDefend206, e = paymentdefend206.NewStore(pool, seed)
	if e != nil {
		slog.Error("invalid payment verification policy protection configuration")
		os.Exit(1)
	}
	app.PaymentDefendCaptcha206, e = captcha203.NewStore(pool, seed)
	if e != nil {
		slog.Error("invalid payment verification captcha protection configuration")
		os.Exit(1)
	}
	app.PaymentDefendVerifier206 = captcha203.NewGeetestVerifier(nil)
	app.OrderAPITradeAlias120, e = server.NewOrderAPITradeAliasStore120ForMaster(app, seed)
	if e != nil {
		slog.Error("invalid order alias protection configuration")
		os.Exit(1)
	}
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
	app.CombinedOrderHistory204, e = migration.NewCombinedOrderHistoryReader(pool, seed)
	if e != nil {
		slog.Error("invalid combined order history protection configuration")
		os.Exit(1)
	}
	app.MFA, e = identity.NewMFA(pool, seed)
	if e != nil {
		slog.Error("invalid authenticator encryption configuration")
		os.Exit(1)
	}
	verificationSender, e := configuredManagedVerification195(pool, seed, os.Getenv)
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
	app.KYC233ConsoleOrigin = consoleOrigin
	app.Enrollment = enrollment.NewHandler(enrollmentService, consoleOrigin)
	if e = configureProviders(app, os.Getenv("GOEPAY_PROVIDER_MODE"), seed, os.Getenv("GOEPAY_PUBLIC_ORIGIN")); e != nil {
		slog.Error("invalid local provider configuration")
		os.Exit(1)
	}
	if app.ProviderFlow != nil {
		app.ProviderFlow.SubjectResolver = ordersubject.New(pool).Resolve
	}
	app.DocsAPIOrigin = os.Getenv("GOEPAY_PUBLIC_ORIGIN")
	// The public test-pay page (/test) is a console page: its browser calls,
	// Origin check and return address belong to the console origin, like the
	// login CAPTCHA below. Single-origin installs are unchanged.
	app.PublicTestPay203, e = server.NewPublicTestPayService203ForMaster(app, captcha203.NewGeetestVerifier(nil), seed, consoleOrigin)
	if e != nil {
		slog.Error("invalid public test payment configuration")
		os.Exit(1)
	}
	app.LoginCaptcha205, e = server.NewLoginCaptchaService205ForMaster(app, captcha203.NewGeetestVerifier(nil), seed, consoleOrigin)
	if e != nil {
		slog.Error("invalid login CAPTCHA configuration")
		os.Exit(1)
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
