package cryptopay

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"

	"github.com/jackc/pgx/v5"
)

var ErrNetworkNotReady = errors.New("USDT network prerequisites not ready")

type NetworkControl struct {
	Network                 string `json:"network"`
	Enabled                 bool   `json:"enabled"`
	EffectiveEnabled        bool   `json:"effective_enabled"`
	Revision                int64  `json:"revision,string"`
	WalletsConfigured       bool   `json:"wallets_configured"`
	DepositAddressAvailable bool   `json:"deposit_address_available"`
	ListenerReady           bool   `json:"listener_ready"`
	ReadyToEnable           bool   `json:"ready_to_enable"`
}

// ListenerFingerprint matches a worker's public custody configuration to the
// provisioned API configuration without exposing either wallet in a status DTO.
func ListenerFingerprint(network string, cfg NetworkConfig) (string, error) {
	for _, address := range []string{cfg.TreasuryAddress, cfg.HotAddress} {
		canonical, err := normalize(network, address)
		if err != nil || address != canonical {
			return "", ErrInvalid
		}
	}
	hash := sha256.Sum256([]byte("GOEPAY/USDT/listener/v1\x00" + network + "\x00" + cfg.TreasuryAddress + "\x00" + cfg.HotAddress))
	return hex.EncodeToString(hash[:]), nil
}

const freshListener = `EXISTS (
	SELECT 1 FROM crypto_listener_health h
	WHERE h.network=c.network AND h.fingerprint=$2
	  AND h.checked_at<=clock_timestamp()
	  AND h.checked_at>clock_timestamp()-interval '60 seconds'
)`

func (s *Service) NetworkControls(ctx context.Context) ([]NetworkControl, error) {
	tx, err := s.begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	items := make([]NetworkControl, 0, 2)
	for _, network := range []string{"tron", "ethereum"} {
		item, err := s.readNetworkControl(ctx, tx, network, "")
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, commit(ctx, tx)
}

func (s *Service) readNetworkControl(ctx context.Context, q queryer, network, lock string) (NetworkControl, error) {
	cfg := s.Networks[network]
	fingerprint, err := ListenerFingerprint(network, cfg)
	out := NetworkControl{Network: network, WalletsConfigured: cfg.Enabled && err == nil}
	var walletApproved bool
	err = q.QueryRow(ctx, `
		SELECT enabled, revision, approved_wallet_fingerprint=$2, `+freshListener+`, EXISTS(
			SELECT 1 FROM crypto_addresses a
			WHERE a.network=c.network AND a.role='deposit' AND NOT a.used
			  AND a.address<>$3 AND a.address<>$4
		)
		FROM crypto_network_controls c WHERE network=$1`+lock,
		network, fingerprint, cfg.HotAddress, cfg.TreasuryAddress).
		Scan(&out.Enabled, &out.Revision, &walletApproved, &out.ListenerReady, &out.DepositAddressAvailable)
	out.ListenerReady = out.WalletsConfigured && out.ListenerReady
	out.ReadyToEnable = out.WalletsConfigured && out.ListenerReady && out.DepositAddressAvailable
	out.EffectiveEnabled = out.Enabled && walletApproved && out.ListenerReady
	return out, err
}

func (s *Service) SetNetworkEnabled(ctx context.Context, network string, enabled bool, expectedRevision, actorID int64) (NetworkControl, error) {
	if s == nil || !s.ManageNetworks {
		return NetworkControl{}, ErrUnavailable
	}
	if network != "tron" && network != "ethereum" || expectedRevision <= 0 || actorID <= 0 {
		return NetworkControl{}, ErrInvalid
	}
	if guard, ok := ctx.Value(authorityKey{}).(func(context.Context, pgx.Tx) error); !ok || guard == nil {
		return NetworkControl{}, ErrUnavailable
	}
	tx, err := s.begin(ctx)
	if err != nil {
		return NetworkControl{}, err
	}
	defer tx.Rollback(ctx)
	current, err := s.readNetworkControl(ctx, tx, network, " FOR UPDATE OF c")
	if err != nil {
		return NetworkControl{}, err
	}
	if current.Revision != expectedRevision {
		return NetworkControl{}, ErrConflict
	}
	if enabled && !current.ReadyToEnable {
		return NetworkControl{}, ErrNetworkNotReady
	}
	if enabled && current.Enabled && !current.EffectiveEnabled {
		return NetworkControl{}, ErrConflict
	}
	if current.Enabled != enabled {
		fingerprint := ""
		event := "disable-network"
		if enabled {
			fingerprint, err = ListenerFingerprint(network, s.Networks[network])
			if err != nil {
				return NetworkControl{}, err
			}
			event = "enable-network"
		}
		if _, err = tx.Exec(ctx, "UPDATE crypto_network_controls SET enabled=$2,revision=revision+1,approved_wallet_fingerprint=$3 WHERE network=$1", network, enabled, fingerprint); err != nil {
			return NetworkControl{}, err
		}
		if err := audit(ctx, tx, actorID, event, network); err != nil {
			return NetworkControl{}, err
		}
	}
	if err = authorize(ctx, tx); err != nil {
		return NetworkControl{}, err
	}
	out, err := s.readNetworkControl(ctx, tx, network, "")
	if err != nil {
		return NetworkControl{}, err
	}
	if enabled && (!out.ReadyToEnable || !out.EffectiveEnabled) {
		return NetworkControl{}, ErrNetworkNotReady
	}
	return out, tx.Commit(ctx)
}

func (s *Service) RuntimeNetworkEnabled(ctx context.Context, network string) (bool, error) {
	if s == nil {
		return false, nil
	}
	if s.ManageNetworks && s.Pool == nil {
		return false, ErrUnavailable
	}
	return s.networkEnabled(ctx, s.Pool, network, false)
}

func (s *Service) networkEnabled(ctx context.Context, q queryer, network string, lock bool) (bool, error) {
	cfg := s.Networks[network]
	if !s.ManageNetworks || !cfg.Enabled {
		return cfg.Enabled, nil
	}
	fingerprint, err := ListenerFingerprint(network, cfg)
	if err != nil {
		return false, nil
	}
	suffix := ""
	if lock {
		// The shared switch lock remains held through the operation commit.
		suffix = " FOR SHARE OF c"
	}
	var enabled bool
	err = q.QueryRow(ctx, "SELECT enabled AND approved_wallet_fingerprint=$2 AND "+freshListener+" FROM crypto_network_controls c WHERE network=$1"+suffix, network, fingerprint).Scan(&enabled)
	return enabled, err
}

func (s *Service) requireNetworkEnabled(ctx context.Context, tx pgx.Tx, network string, lock bool) error {
	enabled, err := s.networkEnabled(ctx, tx, network, lock)
	if err != nil {
		return err
	}
	if !enabled {
		return ErrUnavailable
	}
	return nil
}

func (s *Service) commitNetworkOperation(ctx context.Context, tx pgx.Tx, network string) error {
	if err := authorize(ctx, tx); err != nil {
		return err
	}
	if s.ManageNetworks {
		if err := s.requireNetworkEnabled(ctx, tx, network, false); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
