package adminnotice

import (
	"crypto/tls"
	"net/http"
)

func strictMerchantHTTPClient(client *http.Client) (*http.Client, error) {
	configured, err := newHTTPClient(client)
	if err != nil {
		return nil, ErrMerchantDelivery
	}
	// Default uses the private public-DNS guard. Explicit local/PKI clients
	// require a cloned transport, with immutable roots and TLS1.2 or newer.
	if client == nil {
		return configured, nil
	}
	transport, ok := configured.Transport.(*http.Transport)
	if !ok {
		return nil, ErrMerchantDelivery
	}
	transport = transport.Clone()
	if transport.TLSClientConfig == nil {
		transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	} else {
		transport.TLSClientConfig = transport.TLSClientConfig.Clone()
		cfg := transport.TLSClientConfig
		if cfg.InsecureSkipVerify || cfg.MinVersion != 0 && cfg.MinVersion < tls.VersionTLS12 || cfg.MaxVersion != 0 && cfg.MaxVersion < tls.VersionTLS12 {
			return nil, ErrMerchantDelivery
		}
		if cfg.MinVersion == 0 {
			cfg.MinVersion = tls.VersionTLS12
		}
		if cfg.RootCAs != nil {
			cfg.RootCAs = cfg.RootCAs.Clone()
		}
	}
	configured.Transport = transport
	return configured, nil
}
