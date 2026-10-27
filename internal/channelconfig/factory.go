package channelconfig

import (
	"context"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"errors"
	provider "goepay/internal/adapters/payment"
	"goepay/internal/compat"
	"goepay/internal/platform/httpclient"
	"net/http"
	"time"
)

var ErrLiveDisabled = errors.New("live financial operations disabled")

// Build validates credentials offline. It performs no network requests. Writes
// remain gated unless explicitly enabled by the application operator.
func Build(imp Import, allowLive bool) (map[int64]provider.Provider, error) {
	return BuildWithOptions(imp, BuildOptions{AllowLive: allowLive})
}

type BuildOptions struct {
	// Only child factories consume this independently validated OAuth gate.
	EnableSubchannelOAuth bool
	AllowLive             bool
	ScanRecovery          bool
	// Reference to the original runtime policy, shared by restored providers.
	RuntimeWriteAllowed func() bool
}

// BuildWithOptions retains all ordinary write guards. ScanRecovery adds the
// separately enabled original barcode cancellation capability, never a retry.
func BuildWithOptions(imp Import, options BuildOptions) (map[int64]provider.Provider, error) {
	allowLive := options.AllowLive
	out := map[int64]provider.Provider{}
	for _, c := range imp.Channels {
		if c.ID <= 0 || out[c.ID] != nil || !supportedCollectionMode(c) {
			return nil, ErrImport
		}
		if !directIdentityChannel(c) {
			return nil, ErrImport
		}
		var p provider.Provider
		var e error
		sharing, sharingReturn, se := sharingOptions(c)
		if se != nil {
			return nil, se
		}
		switch c.Plugin {
		case "wxpay":
			if c.Config["wechat_api_version"] == "v3" {
				p, e = buildWechatV3(c)
				break
			}
			if version := c.Config["wechat_api_version"]; version != "" && version != "v2" {
				return nil, ErrImport
			}
			methods := []string{}
			for _, v := range c.AppTypes {
				switch v {
				case "1":
					methods = append(methods, "native")
				case "2":
					methods = append(methods, "jsapi")
				case "3":
					methods = append(methods, "h5")
				case "5":
					methods = append(methods, "app")
				default:
					return nil, ErrImport
				}
			}
			if len(methods) == 0 || len(c.Config["appkey"]) != 32 {
				return nil, ErrImport
			}
			switch c.Config["enable_scan_pay"] {
			case "true":
				methods = append(methods, "scan")
			case "", "false":
			default:
				return nil, ErrImport
			}
			client := httpclient.New()
			var refundClient *http.Client
			var bankPublicKey *rsa.PublicKey
			certPEM, keyPEM := c.Certificates["sslcert_path"], c.Certificates["sslkey_path"]
			if certPEM != "" || keyPEM != "" {
				cert, ce := tls.X509KeyPair([]byte(certPEM), []byte(keyPEM))
				if ce != nil {
					return nil, ErrImport
				}
				leaf, ce := x509.ParseCertificate(cert.Certificate[0])
				if ce != nil || time.Now().Before(leaf.NotBefore) || !time.Now().Before(leaf.NotAfter) {
					return nil, ErrImport
				}
				refundClient = httpclient.NewMutualTLS(cert)
			}
			if sharing && refundClient == nil {
				return nil, ErrImport
			}
			switch c.Config["enable_bank_transfer"] {
			case "", "false":
			case "true":
				if refundClient == nil {
					return nil, ErrImport
				}
				keyText := c.Config["bank_public_key"]
				certificateKey := c.Certificates["publickey_path"]
				if keyText != "" && certificateKey != "" && keyText != certificateKey {
					return nil, ErrImport
				}
				if keyText == "" {
					keyText = certificateKey
				}
				bankPublicKey, e = compat.ParsePublicKey(keyText)
				if e != nil || bankPublicKey.N.BitLen() < 2048 {
					return nil, ErrImport
				}
			default:
				return nil, ErrImport
			}
			mini, me := miniProgramConfig(c)
			if me != nil {
				return nil, me
			}
			p, e = provider.NewWechatV2(provider.WechatV2Config{EnableChannelSharing: sharing, EnableScanCancellation: options.ScanRecovery, AppID: c.Config["appid"], MerchantID: c.Config["appmchid"], APIKey: c.Config["appkey"], MiniProgram: mini, Methods: methods, H5SiteURL: c.Config["h5_site_url"], H5SiteName: c.Config["h5_site_name"], HTTPClient: client, RefundClient: refundClient, BankPublicKey: bankPublicKey})
		case "alipay":
			if c.Config["sign_type"] != "" && c.Config["sign_type"] != "0" && c.Config["sign_type"] != "1" {
				return nil, provider.ErrUnsupported
			}
			methods := []string{}
			native, orderCode := false, false
			for _, v := range c.AppTypes {
				switch v {
				case "1":
					methods = append(methods, "page")
				case "2":
					methods = append(methods, "wap")
				case "3", "8":
					if v == "3" {
						native = true
					} else {
						orderCode = true
					}
					found := false
					for _, method := range methods {
						found = found || method == "native"
					}
					if !found {
						methods = append(methods, "native")
					}
				case "6":
					methods = append(methods, "app")
				case "4":
					methods = append(methods, "jsapi")
				case "7":
					methods = append(methods, "mini")
				case "5", "9", "10":
					found := false
					for _, method := range methods {
						if method == "preauth" {
							found = true
						}
					}
					if !found {
						methods = append(methods, "preauth")
					}
				default:
					return nil, ErrImport
				}
			}
			if len(methods) == 0 {
				return nil, ErrImport
			}
			switch c.Config["enable_scan_pay"] {
			case "true":
				methods = append(methods, "scan")
			case "", "false":
			default:
				return nil, ErrImport
			}
			private, pe := compat.ParsePrivateKey(c.Config["appsecret"])
			var public *rsa.PublicKey
			var certificates *provider.AlipayCertificateConfig
			if c.Config["sign_type"] == "1" {
				certificates = &provider.AlipayCertificateConfig{ApplicationPEM: c.Certificates["app_cert_path"], PlatformPEM: c.Certificates["alipay_cert_path"], RootPEM: c.Certificates["root_cert_path"]}
			} else {
				public, e = compat.ParsePublicKey(c.Config["appkey"])
				if e != nil || public.N.BitLen() < 2048 {
					return nil, ErrImport
				}
			}
			if pe != nil || private.N.BitLen() < 2048 {
				return nil, ErrImport
			}
			mini, me := miniProgramConfig(c)
			if me != nil {
				return nil, me
			}
			bank, scene, reports, be := alipayBankConfig(c)
			if be != nil {
				return nil, be
			}
			p, e = provider.NewAlipay(provider.AlipayConfig{EnableScanCancellation: options.ScanRecovery, EnableChannelSharing: sharing, EnableChannelSharingReturn: sharingReturn, AppID: c.Config["appid"], SellerID: c.Config["appmchid"], PrivateKey: private, PublicKey: public, CertificateMode: certificates, EnableBankTransfer: bank, BankTransferScene: scene, BankTransferReports: reports, PreauthVoucher: preauthVoucherOnly(c.AppTypes), PreauthVoucherProductCode: c.Config["preauth_voucher_product_code"], NativeOrderCode: orderCode && !native, MiniProgram: mini, Methods: methods, HTTPClient: httpclient.New()})
		default:
			return nil, provider.ErrUnsupported
		}
		if e != nil {
			return nil, ErrImport
		}
		if options.ScanRecovery {
			for _, method := range p.Capabilities().Methods {
				if method == "scan" {
					cancel, ok := p.(provider.ScanCancellationProvider)
					if !ok || !cancel.CanCancelScan() {
						return nil, ErrImport
					}
					if e := cancel.ValidateScanCancellation(provider.ScanCancellationRequest{TradeNo: "preflight78", Amount: 1}); e != nil {
						return nil, ErrImport
					}
				}
			}
		}
		g := guarded{Provider: p, allowLive: allowLive, runtimeWriteAllowed: options.RuntimeWriteAllowed}
		if raw, present := c.Config["costrate"]; present {
			cost, err := provider.NewPaymentCostEstimate(p, raw)
			if err != nil {
				return nil, ErrImport
			}
			g.cost = &cost
		}
		out[c.ID] = g
	}
	return out, nil
}

// Explicit direct settlement cannot be silently executed by the platform-net
// ledger. Existing imports with no collection mode retain their old behavior.
func supportedCollectionMode(c Channel) bool {
	return (c.Mode == nil || *c.Mode == 0) && (c.Config["mode"] == "" || c.Config["mode"] == "0")
}

type guarded struct {
	provider.Provider
	allowLive           bool
	cost                *provider.PaymentCostEstimate
	runtimeWriteAllowed func() bool
}

func (g guarded) writesEnabled() bool {
	return g.allowLive && (g.runtimeWriteAllowed == nil || g.runtimeWriteAllowed())
}

func (g guarded) PaymentCostEstimate() (provider.PaymentCostEstimate, bool) {
	if g.cost == nil {
		return provider.PaymentCostEstimate{}, false
	}
	return *g.cost, true
}

func (g guarded) TransferAccountIdentity() string {
	if p, ok := g.Provider.(provider.TransferAccountIdentity); ok {
		return p.TransferAccountIdentity()
	}
	return ""
}
func (g guarded) TransferProductIdentity() string {
	if p, ok := g.Provider.(provider.TransferProductIdentity); ok {
		return p.TransferProductIdentity()
	}
	return ""
}
func (g guarded) PaymentProductIdentity() string {
	value, _ := provider.PaymentProductScope(g.Provider)
	return value
}
func (g guarded) NotificationFormat() string {
	if p, ok := g.Provider.(interface{ NotificationFormat() string }); ok {
		return p.NotificationFormat()
	}
	return ""
}

func (g guarded) ApplicationID() string {
	if p, ok := g.Provider.(provider.ApplicationIdentity); ok {
		return p.ApplicationID()
	}
	return ""
}

func (g guarded) ValidateCreate(r provider.CreateRequest) error {
	if !g.writesEnabled() {
		return ErrLiveDisabled
	}
	if p, ok := g.Provider.(provider.CreateValidator); ok {
		return p.ValidateCreate(r)
	}
	return provider.ErrUnsupported
}

func (g guarded) Create(ctx context.Context, r provider.CreateRequest) (provider.CreateResult, error) {
	if !g.writesEnabled() {
		return provider.CreateResult{}, ErrLiveDisabled
	}
	return g.Provider.Create(ctx, r)
}
func (g guarded) Close(ctx context.Context, r provider.QueryRequest) error {
	if !g.writesEnabled() {
		return ErrLiveDisabled
	}
	return g.Provider.Close(ctx, r)
}
func (g guarded) Refund(ctx context.Context, r provider.RefundRequest) (provider.RefundResult, error) {
	if !g.writesEnabled() {
		return provider.RefundResult{}, ErrLiveDisabled
	}
	return g.Provider.Refund(ctx, r)
}
func (g guarded) VerifyNotification(ctx context.Context, h http.Header, raw []byte, expected provider.ExpectedPayment) (provider.PaymentEvent, error) {
	v, ok := g.Provider.(provider.NotificationVerifier)
	if !ok {
		return provider.PaymentEvent{}, provider.ErrUnsupported
	}
	return v.VerifyNotification(ctx, h, raw, expected)
}

func (g guarded) TransferTypes() []string {
	if p, ok := g.Provider.(provider.TransferCapabilities); ok {
		return append([]string{}, p.TransferTypes()...)
	}
	return nil
}

func (g guarded) ValidateTransfer(r provider.TransferRequest) error {
	if !g.writesEnabled() {
		return ErrLiveDisabled
	}
	if p, ok := g.Provider.(provider.TransferValidator); ok {
		return p.ValidateTransfer(r)
	}
	return provider.ErrUnsupported
}
func (g guarded) Transfer(ctx context.Context, r provider.TransferRequest) (provider.TransferResult, error) {
	if !g.writesEnabled() {
		return provider.TransferResult{}, ErrLiveDisabled
	}
	p, ok := g.Provider.(provider.TransferProvider)
	if !ok {
		return provider.TransferResult{}, provider.ErrUnsupported
	}
	return p.Transfer(ctx, r)
}
func (g guarded) QueryTransfer(ctx context.Context, r provider.TransferRequest) (provider.TransferResult, error) {
	p, ok := g.Provider.(provider.TransferProvider)
	if !ok {
		return provider.TransferResult{}, provider.ErrUnsupported
	}
	return p.QueryTransfer(ctx, r)
}

func (g guarded) ApplyTransferProof(ctx context.Context, id string) (provider.TransferProofResult, error) {
	if !g.writesEnabled() {
		return provider.TransferProofResult{}, ErrLiveDisabled
	}
	p, ok := g.Provider.(provider.TransferProofProvider)
	if !ok {
		return provider.TransferProofResult{}, provider.ErrUnsupported
	}
	return p.ApplyTransferProof(ctx, id)
}
func (g guarded) QueryTransferProof(ctx context.Context, id string) (provider.TransferProofResult, error) {
	p, ok := g.Provider.(provider.TransferProofProvider)
	if !ok {
		return provider.TransferProofResult{}, provider.ErrUnsupported
	}
	return p.QueryTransferProof(ctx, id)
}
func (g guarded) ChannelBalance(ctx context.Context, id string) (provider.ChannelBalanceResult, error) {
	p, ok := g.Provider.(provider.ChannelBalanceProvider)
	if !ok {
		return provider.ChannelBalanceResult{}, provider.ErrUnsupported
	}
	return p.ChannelBalance(ctx, id)
}
