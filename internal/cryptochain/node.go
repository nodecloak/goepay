package cryptochain

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

const maxResponseBytes = 16 << 20

type node struct {
	endpoint       string
	client         *http.Client
	headers        http.Header
	allowBroadcast bool
	maxScanBlocks  uint64
}

func newNode(c Config) (*node, error) {
	u, err := url.Parse(c.Endpoint)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, fmt.Errorf("%w: endpoint", ErrInvalidResponse)
	}
	if u.Scheme != "https" {
		ip := net.ParseIP(u.Hostname())
		if u.Scheme != "http" || !(u.Hostname() == "localhost" || ip != nil && ip.IsLoopback()) {
			return nil, fmt.Errorf("%w: endpoint must use HTTPS or loopback", ErrInvalidResponse)
		}
	}
	client := &http.Client{Timeout: 10 * time.Second}
	if c.Client != nil {
		copyClient := *c.Client
		client = &copyClient
		if client.Timeout <= 0 {
			client.Timeout = 10 * time.Second
		}
	}
	client.CheckRedirect = func(*http.Request, []*http.Request) error {
		return fmt.Errorf("%w: redirect refused", ErrInvalidResponse)
	}
	maxBlocks := c.MaxScanBlocks
	if maxBlocks == 0 {
		maxBlocks = 100
	}
	if maxBlocks > 10000 {
		return nil, fmt.Errorf("%w: scan limit", ErrInvalidResponse)
	}
	return &node{strings.TrimRight(c.Endpoint, "/"), client, c.Headers.Clone(), c.AllowBroadcast, maxBlocks}, nil
}

func (n *node) post(ctx context.Context, path string, input, output any) error {
	data, err := json.Marshal(input)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.endpoint+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header = n.headers.Clone()
	if req.Header == nil {
		req.Header = make(http.Header)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := n.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("cryptochain: node HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return err
	}
	if len(body) > maxResponseBytes {
		return fmt.Errorf("%w: oversized response", ErrInvalidResponse)
	}
	if err := json.Unmarshal(body, output); err != nil {
		return fmt.Errorf("%w: malformed JSON", ErrInvalidResponse)
	}
	return nil
}

func (n *node) rpc(ctx context.Context, method string, params []any, output any) error {
	var resp struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      uint64          `json:"id"`
		Result  json.RawMessage `json:"result"`
		Error   *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := n.post(ctx, "", map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params}, &resp); err != nil {
		return err
	}
	if resp.JSONRPC != "2.0" || resp.ID != 1 {
		return fmt.Errorf("%w: RPC envelope", ErrInvalidResponse)
	}
	if resp.Error != nil {
		return fmt.Errorf("cryptochain: RPC %s error %d", method, resp.Error.Code)
	}
	if len(resp.Result) == 0 {
		return fmt.Errorf("%w: missing RPC result", ErrInvalidResponse)
	}
	if err := json.Unmarshal(resp.Result, output); err != nil {
		return fmt.Errorf("%w: RPC result", ErrInvalidResponse)
	}
	return nil
}

func checkRange(from, to, finalized, limit uint64) error {
	if from > to || to-from >= limit {
		return fmt.Errorf("%w: invalid scan range", ErrInvalidResponse)
	}
	if to > finalized {
		return ErrUnfinalized
	}
	return nil
}

func decodeHex(s string, size int) ([]byte, error) {
	s = strings.TrimPrefix(s, "0x")
	if size >= 0 && len(s) != size*2 {
		return nil, ErrInvalidResponse
	}
	if len(s) == 0 || len(s)%2 != 0 {
		return nil, ErrInvalidResponse
	}
	b, err := hex.DecodeString(s)
	if err != nil {
		return nil, ErrInvalidResponse
	}
	return b, nil
}

func normalizeHash(chain Chain, s string) (string, error) {
	b, err := decodeHex(s, 32)
	if err != nil {
		return "", err
	}
	if chain == Ethereum {
		return "0x" + hex.EncodeToString(b), nil
	}
	return hex.EncodeToString(b), nil
}

func hexQuantity(s string) (uint64, error) {
	if !strings.HasPrefix(s, "0x") || len(s) < 3 || len(s) > 3 && s[2] == '0' {
		return 0, ErrInvalidResponse
	}
	v, err := strconv.ParseUint(s[2:], 16, 64)
	if err != nil {
		return 0, ErrInvalidResponse
	}
	return v, nil
}

func uintQuantity(v uint64) string { return "0x" + strconv.FormatUint(v, 16) }

func uint256Result(s string) (string, error) {
	b, err := decodeHex(s, 32)
	if err != nil {
		return "", err
	}
	return new(big.Int).SetBytes(b).String(), nil
}

func parseTransferEvent(chain Chain, address string, topics []string, data string) (from, to, amount string, match bool, err error) {
	var emitter []byte
	if chain == Tron {
		emitter, err = decodeHex(address, -1)
		if err == nil && len(emitter) == 21 && emitter[0] == 0x41 {
			emitter = emitter[1:]
		}
		if len(emitter) != 20 {
			err = ErrInvalidResponse
		}
	} else {
		emitter, err = addressBytes(Ethereum, address)
	}
	if err != nil {
		return "", "", "", false, nil
	}
	contract := EthereumUSDTContract
	if chain == Tron {
		contract = TronUSDTContract
	}
	expected, _ := addressBytes(chain, contract)
	if !bytes.Equal(emitter, expected) {
		return "", "", "", false, nil
	}
	if len(topics) == 0 || !strings.EqualFold(strings.TrimPrefix(topics[0], "0x"), transferTopic) {
		return "", "", "", false, nil
	}
	if len(topics) != 3 {
		return "", "", "", false, ErrInvalidResponse
	}
	f, err := decodeHex(topics[1], 32)
	if err != nil {
		return "", "", "", false, err
	}
	t, err := decodeHex(topics[2], 32)
	if err != nil {
		return "", "", "", false, err
	}
	if !bytes.Equal(f[:12], make([]byte, 12)) || !bytes.Equal(t[:12], make([]byte, 12)) {
		return "", "", "", false, ErrInvalidResponse
	}
	amount, err = uint256Result(data)
	if err != nil {
		return "", "", "", false, err
	}
	if amount == "0" {
		return "", "", "", false, nil
	}
	return formatAddress(chain, f[12:]), formatAddress(chain, t[12:]), amount, true, nil
}

func normalizedRequest(chain Chain, r TransferRequest) (TransferRequest, error) {
	var err error
	r.From, err = NormalizeAddress(chain, r.From)
	if err != nil {
		return r, err
	}
	r.To, err = NormalizeAddress(chain, r.To)
	if err != nil {
		return r, err
	}
	if r.From == r.To {
		return r, fmt.Errorf("%w: self transfer", ErrInvalidAddress)
	}
	if _, err = ParseAtomicAmount(r.AtomicAmount); err != nil {
		return r, err
	}
	if r.RequestID == "" || len(r.RequestID) > 200 {
		return r, fmt.Errorf("%w: request ID", ErrInvalidResponse)
	}
	return r, nil
}

func sortTransfers(transfers []Transfer) {
	sort.Slice(transfers, func(i, j int) bool {
		a, b := transfers[i], transfers[j]
		if a.BlockHeight != b.BlockHeight {
			return a.BlockHeight < b.BlockHeight
		}
		if a.TxID != b.TxID {
			return a.TxID < b.TxID
		}
		return a.EventIndex < b.EventIndex
	})
}
