package printing

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"time"
)

const excelsecuGateway = "https://saas.excelsecu.com"

var errPrinterOutcomeUnknown = errors.New("printer gateway outcome unknown; inspect original print intent")

// ExcelsecuConnector sends only the original PJ request to the fixed gateway.
// It has no lookup or retry API: gateway acceptance does not prove paper output.
type ExcelsecuConnector struct{ client *http.Client }

func NewExcelsecuConnector() *ExcelsecuConnector {
	transport := &http.Transport{
		Proxy:                  nil,
		DialContext:            (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		TLSClientConfig:        &tls.Config{MinVersion: tls.VersionTLS12},
		TLSHandshakeTimeout:    5 * time.Second,
		ResponseHeaderTimeout:  8 * time.Second,
		IdleConnTimeout:        30 * time.Second,
		MaxIdleConns:           4,
		MaxIdleConnsPerHost:    4,
		MaxConnsPerHost:        4,
		MaxResponseHeaderBytes: 16 << 10,
	}
	return newExcelsecuConnector(&http.Client{Transport: transport})
}

// The package-private constructor permits a local TLS transport in tests while
// leaving the destination and no-redirect policy identical to production.
func newExcelsecuConnector(client *http.Client) *ExcelsecuConnector {
	copy := *client
	copy.Timeout = 10 * time.Second
	copy.Jar = nil
	copy.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &ExcelsecuConnector{client: &copy}
}

func (*ExcelsecuConnector) LocalOnly() bool         { return false }
func (*ExcelsecuConnector) printerProvider() string { return "excelsecu" }

func (c *ExcelsecuConnector) Send(ctx context.Context, command PrintCommand) (Outcome, error) {
	if err := ctx.Err(); err != nil {
		return Outcome{}, err
	}
	if c == nil || c.client == nil {
		return Outcome{}, ErrUnsupported
	}
	raw, err := BuildExcelsecuRequest(command, time.Now())
	if err != nil {
		return Outcome{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, excelsecuGateway+ExcelsecuPrintPath, bytes.NewReader(raw))
	if err != nil {
		return Outcome{}, ErrInvalid
	}
	// Never expose a replayable body or an idempotency header to Transport's
	// connection-retry logic. Only the durable printing claim may send once.
	req.GetBody = nil
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return Outcome{}, errPrinterOutcomeUnknown
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4097))
	if err != nil || len(body) > 4096 {
		return Outcome{}, errPrinterOutcomeUnknown
	}
	out, err := ParseExcelsecuResponse(resp.StatusCode, body)
	if err != nil {
		return Outcome{}, errPrinterOutcomeUnknown
	}
	return out, nil
}
