package cryptopay

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func unassignedChannel(t *testing.T, s *Service, name string) Channel {
	t.Helper()
	c, err := s.SaveChannel(channelAuthority(t.Context()), SaveChannelInput{ActorID: 1, RequestID: strings.Repeat(name, 32), Name: name, Network: "tron", Enabled: true, FeeBPS: 100, MinAmount: "0", MaxAmount: "0", DayLimit: "0"})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func setChannelGroup(t *testing.T, s *Service, id int64, body string) {
	t.Helper()
	if _, err := s.Pool.Exec(t.Context(), `INSERT INTO backoffice_records(id,resource,body) VALUES($1,'groups',$2::jsonb) ON CONFLICT(resource,id) DO UPDATE SET body=excluded.body`, id, body); err != nil {
		t.Fatal(err)
	}
}

func TestCryptoChannelDefinitionHasNoMerchantAuthorization(t *testing.T) {
	s := channelTestService(t)
	c := unassignedChannel(t, s, "a")
	raw, err := json.Marshal(c)
	if err != nil || strings.Contains(string(raw), "merchant_ids") {
		t.Fatalf("channel definition still exposes a separate merchant list: %s %v", raw, err)
	}
	var grants int
	if err := s.Pool.QueryRow(t.Context(), "SELECT count(*) FROM crypto_channel_grants").Scan(&grants); err != nil || grants != 0 {
		t.Fatalf("definition wrote redundant grants: %d %v", grants, err)
	}
}

func TestCryptoFixedGroupAssignsChannelWithoutSeparateGrant(t *testing.T) {
	s := channelTestService(t)
	c := unassignedChannel(t, s, "a")
	setChannelGroup(t, s, 0, fmt.Sprintf(`{"enabled":true,"info":{"usdt":{"channel":"%d","type":"channel"}}}`, c.ID))
	if err := s.AddAddress(t.Context(), "tron", depositOne, "deposit"); err != nil {
		t.Fatal(err)
	}
	choices, err := s.AvailableChannels(t.Context(), 2, "")
	if err != nil || len(choices) != 1 || choices[0].ID != c.ID {
		t.Fatalf("group assignment was blocked by the removed grant: %+v %v", choices, err)
	}
	v, err := s.CreateInvoice(t.Context(), CreateInvoiceInput{MerchantID: 2, ChannelID: c.ID, OutTradeNo: "group-only", Amount: "10"})
	if err != nil || v.ChannelID != c.ID || v.Fee != "0.1" {
		t.Fatalf("group assigned collection failed: %+v %v", v, err)
	}
}

func TestCryptoInheritedDefaultRoutePreservesEffectiveGroupFee(t *testing.T) {
	s := channelTestService(t)
	c := unassignedChannel(t, s, "a")
	setChannelGroup(t, s, 0, fmt.Sprintf(`{"enabled":true,"info":{"usdt":{"channel":"%d","type":"channel","rate":"90"}}}`, c.ID))
	setChannelGroup(t, s, 3, `{"enabled":true,"info":{"usdt":{"rate":"98"}}}`)
	if _, err := s.Pool.Exec(t.Context(), "UPDATE merchants SET base_group_id=3 WHERE id=1"); err != nil {
		t.Fatal(err)
	}
	if err := s.AddAddress(t.Context(), "tron", depositOne, "deposit"); err != nil {
		t.Fatal(err)
	}
	choices, err := s.AvailableChannels(t.Context(), 1, "")
	if err != nil || len(choices) != 1 || choices[0].FeeBPS != 200 {
		t.Fatalf("inherited route lost the effective fee: %+v %v", choices, err)
	}
	v, err := s.CreateInvoice(t.Context(), CreateInvoiceInput{MerchantID: 1, OutTradeNo: "default-route-own-rate", Amount: "10"})
	if err != nil || v.ChannelID != c.ID || v.Fee != "0.2" || v.FeeSource != "group" {
		t.Fatalf("default routing changed effective group fee: %+v %v", v, err)
	}
	var frozen map[string]any
	if err := json.Unmarshal(v.ChannelSnapshot, &frozen); err != nil || frozen["group_id"] != "3" || frozen["routing_group_id"] != "0" {
		t.Fatalf("inherited route provenance missing: %s %v", v.ChannelSnapshot, err)
	}
}

func TestCryptoUnassignedDefaultNeverOpensCatalogThroughExplicitIDOrOldGrant(t *testing.T) {
	for _, body := range []string{`{"enabled":true,"info":{}}`, `{"enabled":true,"info":{"usdt":{"channel":"0","type":"channel"}}}`} {
		t.Run(body, func(t *testing.T) {
			s := channelTestService(t)
			c := unassignedChannel(t, s, "a")
			setChannelGroup(t, s, 0, body)
			if _, err := s.Pool.Exec(t.Context(), "INSERT INTO crypto_channel_grants(channel_id,merchant_id) VALUES($1,1)", c.ID); err != nil {
				t.Fatal(err)
			}
			if err := s.AddAddress(t.Context(), "tron", depositOne, "deposit"); err != nil {
				t.Fatal(err)
			}
			choices, err := s.AvailableChannels(t.Context(), 1, "")
			if err != nil || len(choices) != 0 {
				t.Fatalf("unassigned merchant sees global channels: %+v %v", choices, err)
			}
			if _, err = s.CreateInvoice(t.Context(), CreateInvoiceInput{MerchantID: 1, ChannelID: c.ID, OutTradeNo: "unassigned-explicit", Amount: "1"}); err == nil {
				t.Fatal("explicit ID or historical grant invented a group assignment")
			}
			var count int
			if err := s.Pool.QueryRow(t.Context(), "SELECT count(*) FROM crypto_addresses WHERE used").Scan(&count); err != nil || count != 0 {
				t.Fatalf("rejected assignment allocated an address: %d %v", count, err)
			}
		})
	}
}

func TestCryptoGroupReassignmentRejectsOldChannelButKeepsOriginalInvoice(t *testing.T) {
	s := channelTestService(t)
	a, b := unassignedChannel(t, s, "a"), unassignedChannel(t, s, "b")
	setChannelGroup(t, s, 0, fmt.Sprintf(`{"enabled":true,"info":{"usdt":{"channel":"%d","type":"channel"}}}`, a.ID))
	for _, address := range []string{depositOne, depositTwo} {
		if err := s.AddAddress(t.Context(), "tron", address, "deposit"); err != nil {
			t.Fatal(err)
		}
	}
	in := CreateInvoiceInput{MerchantID: 1, ChannelID: a.ID, OutTradeNo: "before-reassignment", Amount: "1"}
	original, err := s.CreateInvoice(t.Context(), in)
	if err != nil {
		t.Fatal(err)
	}
	setChannelGroup(t, s, 0, fmt.Sprintf(`{"enabled":true,"info":{"usdt":{"channel":"%d","type":"channel"}}}`, b.ID))
	replay, err := s.CreateInvoice(t.Context(), in)
	if err != nil || replay.ID != original.ID || replay.ChannelID != a.ID {
		t.Fatalf("group change rebound original invoice: %+v %v", replay, err)
	}
	in.OutTradeNo = "after-reassignment"
	if _, err := s.CreateInvoice(t.Context(), in); err == nil {
		t.Fatal("old explicit channel bypassed the new group assignment")
	}
	in.ChannelID = 0
	v, err := s.CreateInvoice(t.Context(), in)
	if err != nil || v.ChannelID != b.ID {
		t.Fatalf("new group channel not applied: %+v %v", v, err)
	}
	in.ChannelID = a.ID
	if _, err := s.CreateInvoice(t.Context(), in); !errors.Is(err, ErrConflict) {
		t.Fatalf("replay allowed a different assigned channel: %v", err)
	}
}

func TestCryptoInheritedDefaultWithoutOwnRateUsesChannelFee(t *testing.T) {
	s := channelTestService(t)
	c := unassignedChannel(t, s, "a")
	setChannelGroup(t, s, 0, fmt.Sprintf(`{"enabled":true,"info":{"usdt":{"channel":"%d","type":"channel","rate":"90"}}}`, c.ID))
	setChannelGroup(t, s, 3, `{"enabled":true,"info":{}}`)
	if _, err := s.Pool.Exec(t.Context(), "UPDATE merchants SET base_group_id=3 WHERE id=1"); err != nil {
		t.Fatal(err)
	}
	if err := s.AddAddress(t.Context(), "tron", depositOne, "deposit"); err != nil {
		t.Fatal(err)
	}
	choices, err := s.AvailableChannels(t.Context(), 1, "")
	if err != nil || len(choices) != 1 || choices[0].FeeBPS != c.FeeBPS {
		t.Fatalf("default routing unexpectedly inherited its rate: %+v %v", choices, err)
	}
	v, err := s.CreateInvoice(t.Context(), CreateInvoiceInput{MerchantID: 1, OutTradeNo: "default-route-channel-rate", Amount: "10"})
	if err != nil || v.Fee != "0.1" || v.FeeSource != "channel" {
		t.Fatalf("route-only inheritance changed channel fallback fee: %+v %v", v, err)
	}
}

func TestCryptoExplicitFirstUsesEnabledPoolWithoutExtraGrants(t *testing.T) {
	s := channelTestService(t)
	a, b := unassignedChannel(t, s, "a"), unassignedChannel(t, s, "b")
	eth := saveTestChannel(t, s, "Ethereum", "ethereum")
	for _, address := range []string{depositOne, depositTwo} {
		if err := s.AddAddress(t.Context(), "tron", address, "deposit"); err != nil {
			t.Fatal(err)
		}
	}
	choices, err := s.AvailableChannels(t.Context(), 1, "")
	if err != nil || len(choices) != 3 || choices[0].ID != a.ID || choices[1].ID != b.ID || choices[2].ID != eth.ID {
		t.Fatalf("First pool incomplete or nondeterministic: %+v %v", choices, err)
	}
	s.Networks["ethereum"] = NetworkConfig{Enabled: false}
	choices, err = s.AvailableChannels(t.Context(), 1, "")
	if err != nil || len(choices) != 2 {
		t.Fatalf("disabled runtime in candidate pool: %+v %v", choices, err)
	}
	v, err := s.CreateInvoice(t.Context(), CreateInvoiceInput{MerchantID: 1, ChannelID: b.ID, OutTradeNo: "first-explicit", Amount: "1"})
	if err != nil || v.ChannelID != b.ID {
		t.Fatalf("explicit enabled pool member rejected: %+v %v", v, err)
	}
	v, err = s.CreateInvoice(t.Context(), CreateInvoiceInput{MerchantID: 1, OutTradeNo: "first-automatic", Amount: "1"})
	if err != nil || v.ChannelID != a.ID {
		t.Fatalf("First did not select the lowest available ID: %+v %v", v, err)
	}
}

func TestCryptoInheritedDefaultRevocationIsCurrentButReplayStaysFrozen(t *testing.T) {
	for _, revocation := range []string{
		`UPDATE backoffice_records SET body='{"enabled":true,"info":{}}' WHERE resource='groups' AND id=0`,
		`UPDATE backoffice_records SET body='{"enabled":true,"info":{"usdt":{"channel":"0","type":"channel"}}}' WHERE resource='groups' AND id=0`,
		`UPDATE backoffice_records SET body='{"enabled":false,"info":{"usdt":{"channel":"-5","type":"channel"}}}' WHERE resource='groups' AND id=0`,
		`DELETE FROM backoffice_records WHERE resource='groups' AND id=0`,
	} {
		t.Run(revocation, func(t *testing.T) {
			s := channelTestService(t)
			c := unassignedChannel(t, s, "a")
			setChannelGroup(t, s, 3, `{"enabled":true,"info":{}}`)
			if _, err := s.Pool.Exec(t.Context(), "UPDATE merchants SET base_group_id=3 WHERE id=1"); err != nil {
				t.Fatal(err)
			}
			if err := s.AddAddress(t.Context(), "tron", depositOne, "deposit"); err != nil {
				t.Fatal(err)
			}
			in := CreateInvoiceInput{MerchantID: 1, ChannelID: c.ID, OutTradeNo: "before-default-revoke", Amount: "1"}
			original, err := s.CreateInvoice(t.Context(), in)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = s.Pool.Exec(t.Context(), revocation); err != nil {
				t.Fatal(err)
			}
			choices, err := s.AvailableChannels(t.Context(), 1, "")
			if err != nil || len(choices) != 0 {
				t.Fatalf("revoked default still advertised: %+v %v", choices, err)
			}
			replayed, err := s.CreateInvoice(t.Context(), in)
			if err != nil || replayed.ID != original.ID || replayed.ChannelID != c.ID {
				t.Fatalf("revocation changed original invoice: %+v %v", replayed, err)
			}
			in.OutTradeNo = "after-default-revoke"
			if _, err = s.CreateInvoice(t.Context(), in); err == nil {
				t.Fatal("explicit ID bypassed the revoked inherited default")
			}
		})
	}
}

func TestCryptoInheritedDefaultAssignmentLockedThroughFinalAuthority(t *testing.T) {
	s := channelTestService(t)
	c := unassignedChannel(t, s, "a")
	setChannelGroup(t, s, 3, `{"enabled":true,"info":{}}`)
	if _, err := s.Pool.Exec(t.Context(), "UPDATE merchants SET base_group_id=3 WHERE id=1"); err != nil {
		t.Fatal(err)
	}
	if err := s.AddAddress(t.Context(), "tron", depositOne, "deposit"); err != nil {
		t.Fatal(err)
	}
	checks, blocked := 0, false
	ctx := WithAuthority(t.Context(), func(ctx context.Context, tx pgx.Tx) error {
		checks++
		if checks != 3 {
			return nil
		}
		other, err := s.Pool.Begin(ctx)
		if err != nil {
			return err
		}
		defer other.Rollback(ctx)
		if _, err = other.Exec(ctx, `SET LOCAL lock_timeout='100ms'`); err != nil {
			return err
		}
		_, err = other.Exec(ctx, `UPDATE backoffice_records SET body='{"enabled":true,"info":{"usdt":{"channel":"0","type":"channel"}}}' WHERE resource='groups' AND id=0`)
		var pgErr *pgconn.PgError
		blocked = errors.As(err, &pgErr) && pgErr.Code == "55P03"
		if !blocked {
			return fmt.Errorf("default routing update did not wait for invoice commit: %v", err)
		}
		return nil
	})
	v, err := s.CreateInvoice(ctx, CreateInvoiceInput{MerchantID: 1, ChannelID: c.ID, OutTradeNo: "locked-default", Amount: "1"})
	if err != nil || !blocked || checks != 3 || v.ChannelID != c.ID {
		t.Fatalf("inherited default was not held through final proof: %+v blocked=%v checks=%d %v", v, blocked, checks, err)
	}
	setChannelGroup(t, s, 0, `{"enabled":true,"info":{"usdt":{"channel":"0","type":"channel"}}}`)
	if _, err := s.CreateInvoice(t.Context(), CreateInvoiceInput{MerchantID: 1, ChannelID: c.ID, OutTradeNo: "after-locked-default-revoke", Amount: "1"}); err == nil {
		t.Fatal("committed default revocation was not re-read")
	}
}
