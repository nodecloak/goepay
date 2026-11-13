package cryptosigner

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"goepay/internal/cryptochain"
)

func TestCancelPreparedPersistsBindingAndReleasesUnusedNonceBudget(t *testing.T) {
	cfg := testConfig(filepath.Join(t.TempDir(), "journal.jsonl"))
	keys := testKeys(t)
	s, err := NewService(cfg, keys)
	if err != nil {
		t.Fatal(err)
	}
	p := ethereumPrepared("cancelled-before-signing", 0, "900000")
	receipt, err := s.Cancel(t.Context(), p)
	if err != nil || !VerifyCancellation(p, receipt) {
		t.Fatalf("missing cancellation proof: %+v %v", receipt, err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = NewService(cfg, keys)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	again, err := s.Cancel(t.Context(), p)
	if err != nil || !VerifyCancellation(p, again) {
		t.Fatalf("cancel replay after restart: %v", err)
	}
	if _, err = s.Sign(t.Context(), p); !errors.Is(err, ErrCancelled) {
		t.Fatalf("cancelled request signed: %v", err)
	}
	changed := ethereumPrepared(p.RequestID, 0, "800000")
	if VerifyCancellation(changed, receipt) {
		t.Fatal("proof authorized another prepared amount")
	}
	for _, mutation := range []func(*cryptochain.PreparedTransaction){
		func(p *cryptochain.PreparedTransaction) { p.RequestID = "another" },
		func(p *cryptochain.PreparedTransaction) { p.Network = "another" },
		func(p *cryptochain.PreparedTransaction) { p.From = "0x3333333333333333333333333333333333333333" },
		func(p *cryptochain.PreparedTransaction) { p.To = "0x4444444444444444444444444444444444444444" },
		func(p *cryptochain.PreparedTransaction) { p.UnsignedPayload += "00" },
		func(p *cryptochain.PreparedTransaction) { p.Nonce++ },
		func(p *cryptochain.PreparedTransaction) { p.FeeLimit++ },
		func(p *cryptochain.PreparedTransaction) { p.Expiration++ },
	} {
		other := p
		mutation(&other)
		if VerifyCancellation(other, receipt) {
			t.Fatal("proof did not bind complete prepared transaction")
		}
	}
	if _, err = s.Cancel(t.Context(), changed); !errors.Is(err, ErrRequestConflict) {
		t.Fatalf("changed cancellation binding accepted: %v", err)
	}
	if _, err = s.Sign(t.Context(), changed); !errors.Is(err, ErrRequestConflict) {
		t.Fatalf("changed signature binding accepted: %v", err)
	}
	if _, err = s.Sign(t.Context(), ethereumPrepared("replacement-after-proof", 0, "1000000")); err != nil {
		t.Fatalf("unused nonce/budget remained occupied: %v", err)
	}
}

func TestCancelledJournalCannotTransitionBackToPendingSigning(t *testing.T) {
	cfg := testConfig(filepath.Join(t.TempDir(), "journal.jsonl"))
	keys := testKeys(t)
	s, err := NewService(cfg, keys)
	if err != nil {
		t.Fatal(err)
	}
	p := ethereumPrepared("terminal-cancellation", 0, "100000")
	if _, err = s.Cancel(t.Context(), p); err != nil {
		t.Fatal(err)
	}
	s.Close()
	binding, _ := preparedBinding(p)
	illegal := entry{RequestID: p.RequestID, Binding: binding, Day: "2026-11-13", RuleID: ruleID(cfg.Rules[0]), Amount: p.AtomicAmount, FeeBudget: "1300000000000000", NonceKey: walletID(p.Chain, p.Network, p.From) + "/0", State: "pending"}
	raw, _ := json.Marshal(illegal)
	f, err := os.OpenFile(cfg.StatePath, os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		t.Fatal(err)
	}
	f.Write(append(raw, '\n'))
	f.Close()
	if recovered, err := NewService(cfg, keys); err == nil {
		recovered.Close()
		t.Fatal("cancelled journal request reentered signing")
	}
}

type noKeyAccess struct{}

func (noKeyAccess) PublicKey(context.Context, string) ([]byte, error) {
	panic("cancellation must not load a key")
}
func (noKeyAccess) SignDigest(context.Context, string, []byte) ([]byte, error) {
	panic("cancellation must not invoke an HSM")
}

func TestCancelExpiredTRONRequiresNeitherCurrentPolicyNorKeyAccess(t *testing.T) {
	s, err := NewService(testConfig(filepath.Join(t.TempDir(), "journal.jsonl")), noKeyAccess{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	p := tronPrepared("expired-before-signer-acceptance", time.Now().Add(-2*time.Hour))
	receipt, err := s.Cancel(t.Context(), p)
	if err != nil || !VerifyCancellation(p, receipt) {
		t.Fatalf("expired unaccepted transfer not cancelled: %v", err)
	}
	if _, err = s.Sign(t.Context(), p); !errors.Is(err, ErrCancelled) {
		t.Fatalf("expired cancellation did not fence signing: %v", err)
	}
	for _, id := range []string{"", " invalid", "invalid "} {
		bad := p
		bad.RequestID = id
		if proof, err := s.Cancel(t.Context(), bad); err == nil || VerifyCancellation(bad, proof) {
			t.Fatalf("invalid request ID cancelled: %q", id)
		}
	}
}

func TestCancelCannotProveSignedOrUnknownHSMRequestWasUnsigned(t *testing.T) {
	for _, unknown := range []bool{false, true} {
		t.Run(map[bool]string{false: "signed", true: "unknown-hsm"}[unknown], func(t *testing.T) {
			cfg := testConfig(filepath.Join(t.TempDir(), "journal.jsonl"))
			keys := testKeys(t)
			provider := keys
			if unknown {
				provider = unavailableHSM{keys}
			}
			s, err := NewService(cfg, provider)
			if err != nil {
				t.Fatal(err)
			}
			p := ethereumPrepared("original", 0, "700000")
			_, signErr := s.Sign(t.Context(), p)
			if unknown && !errors.Is(signErr, ErrIndeterminate) || !unknown && signErr != nil {
				t.Fatalf("sign setup: %v", signErr)
			}
			for i := 0; i < 2; i++ {
				proof, err := s.Cancel(t.Context(), p)
				if !errors.Is(err, ErrIndeterminate) || VerifyCancellation(p, proof) {
					t.Fatalf("signing request falsely proved unsigned: %v", err)
				}
				if i == 0 {
					s.Close()
					s, err = NewService(cfg, keys)
					if err != nil {
						t.Fatal(err)
					}
				}
			}
			defer s.Close()
			if _, err = s.Sign(t.Context(), ethereumPrepared("unsafe-replacement", 0, "700000")); !errors.Is(err, ErrRequestConflict) {
				t.Fatalf("cancellation released used nonce: %v", err)
			}
		})
	}
}

func TestCancelAndSignHaveOneDurableWinner(t *testing.T) {
	for iteration := 0; iteration < 8; iteration++ {
		s, err := NewService(testConfig(filepath.Join(t.TempDir(), "journal.jsonl")), testKeys(t))
		if err != nil {
			t.Fatal(err)
		}
		p := ethereumPrepared("race", 0, "100000")
		start := make(chan struct{})
		var wg sync.WaitGroup
		var signed cryptochain.SignedTransaction
		var signErr, cancelErr error
		var proof CancellationReceipt
		wg.Add(2)
		go func() { defer wg.Done(); <-start; signed, signErr = s.Sign(context.Background(), p) }()
		go func() { defer wg.Done(); <-start; proof, cancelErr = s.Cancel(context.Background(), p) }()
		close(start)
		wg.Wait()
		if VerifyCancellation(p, proof) {
			if cancelErr != nil || !errors.Is(signErr, ErrCancelled) || signed.RawTransaction != "" {
				t.Fatalf("both cancellation and signature won: %v %v", cancelErr, signErr)
			}
		} else {
			if !errors.Is(cancelErr, ErrIndeterminate) || signErr != nil || VerifySigned(p, signed) != nil {
				t.Fatalf("race lost original result: %v %v", cancelErr, signErr)
			}
		}
		if err = s.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

type gatedHSM struct {
	KeyProvider
	entered, release chan struct{}
}

func (k gatedHSM) SignDigest(ctx context.Context, id string, digest []byte) ([]byte, error) {
	close(k.entered)
	<-k.release
	return k.KeyProvider.SignDigest(ctx, id, digest)
}

func TestCancelWaitsForInProgressHSMAndCannotClaimUnsigned(t *testing.T) {
	keys := gatedHSM{KeyProvider: testKeys(t), entered: make(chan struct{}), release: make(chan struct{})}
	s, err := NewService(testConfig(filepath.Join(t.TempDir(), "journal.jsonl")), keys)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	p := ethereumPrepared("hsm-in-progress", 0, "100000")
	signed := make(chan error, 1)
	go func() { _, err := s.Sign(context.Background(), p); signed <- err }()
	select {
	case <-keys.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("HSM was not entered")
	}
	type cancellationResult struct {
		proof CancellationReceipt
		err   error
	}
	cancelled := make(chan cancellationResult, 1)
	go func() { proof, err := s.Cancel(context.Background(), p); cancelled <- cancellationResult{proof, err} }()
	select {
	case result := <-cancelled:
		close(keys.release)
		t.Fatalf("cancellation crossed active key use: %v", result.err)
	case <-time.After(20 * time.Millisecond):
	}
	close(keys.release)
	if err := <-signed; err != nil {
		t.Fatal(err)
	}
	result := <-cancelled
	if !errors.Is(result.err, ErrIndeterminate) || VerifyCancellation(p, result.proof) {
		t.Fatalf("active HSM falsely reported unsigned: %v", result.err)
	}
}

func TestCancellationFailureReturnsNoProofAndJSONCannotForgeReceipt(t *testing.T) {
	s, err := NewService(testConfig(filepath.Join(t.TempDir(), "journal.jsonl")), testKeys(t))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	p := ethereumPrepared("cancel-storage-failure", 0, "100000")
	s.file.Close()
	proof, err := s.Cancel(t.Context(), p)
	if !errors.Is(err, ErrIndeterminate) || VerifyCancellation(p, proof) {
		t.Fatalf("unpersisted cancellation proved: %v", err)
	}
	var forged CancellationReceipt
	if err = json.Unmarshal([]byte(`{"request_id":"cancel-storage-failure","binding":"anything","valid":true}`), &forged); err != nil {
		t.Fatal(err)
	}
	if VerifyCancellation(p, forged) {
		t.Fatal("caller JSON forged a proof")
	}
	if _, err = os.Stat(s.cfg.StatePath); err != nil {
		t.Fatal(err)
	}
}
