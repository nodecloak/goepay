package cryptosigner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"goepay/internal/cryptochain"
)

// CancellationReceipt is a process-local capability proving that the signer
// durably fenced this exact prepared request before any key use. Its fields are
// private: callers cannot mint a proof from JSON, HTTP data or a zero value.
// Only Service.Cancel or a successfully authenticated Client.Cancel can mint it.
// This is not an offline signature or a portable, serialized attestation.
type CancellationReceipt struct {
	requestID string
	binding   string
}

// VerifyCancellation accepts only the capability for the complete prepared
// transaction. Do not persist receipt JSON as proof; obtain it again via Cancel.
func VerifyCancellation(p cryptochain.PreparedTransaction, receipt CancellationReceipt) bool {
	binding, err := preparedBinding(p)
	return err == nil && receipt.requestID == p.RequestID && receipt.binding == binding
}

func preparedBinding(p cryptochain.PreparedTransaction) (string, error) {
	if p.RequestID == "" || len(p.RequestID) > 200 || strings.TrimSpace(p.RequestID) != p.RequestID {
		return "", fmt.Errorf("%w: invalid request ID", ErrPolicy)
	}
	for _, s := range []string{string(p.Chain), p.Network, p.ChainID, p.RequestID, p.From, p.To, p.Contract, p.AtomicAmount, p.UnsignedPayload, p.SigningPayload, p.TxID, p.GasPrice} {
		if !utf8.ValidString(s) {
			return "", fmt.Errorf("%w: invalid prepared UTF-8", ErrPolicy)
		}
	}
	b, err := json.Marshal(p)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(b)
	return hex.EncodeToString(digest[:]), nil
}

// Cancel creates an irrevocable tombstone only if the request has never entered
// pending signing. It deliberately does not ask a key provider, authorize the
// current destination policy, check expiry, or contact a blockchain. An expired
// unaccepted request can therefore be recovered safely. A pending HSM result or
// an existing signature can never be converted to a cancellation proof.
func (s *Service) Cancel(ctx context.Context, p cryptochain.PreparedTransaction) (CancellationReceipt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var zero CancellationReceipt
	if s.closed || s.poisoned != nil {
		return zero, fmt.Errorf("%w: signer journal unavailable", ErrIndeterminate)
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	binding, err := preparedBinding(p)
	if err != nil {
		return zero, err
	}
	if e, exists := s.entries[p.RequestID]; exists {
		if e.Binding != binding {
			return zero, ErrRequestConflict
		}
		if e.State != "cancelled" {
			return zero, ErrIndeterminate
		}
		return CancellationReceipt{requestID: p.RequestID, binding: binding}, nil
	}
	e := entry{RequestID: p.RequestID, Binding: binding, State: "cancelled"}
	if err = s.append(e); err != nil {
		return zero, fmt.Errorf("%w: persist cancellation: %v", ErrIndeterminate, err)
	}
	s.entries[p.RequestID] = e
	return CancellationReceipt{requestID: p.RequestID, binding: binding}, nil
}
