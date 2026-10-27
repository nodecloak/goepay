package refundevent201

import (
	"context"
	"crypto/hmac"
	"encoding/hex"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"goepay/internal/platform/httpclient"
)

type View struct {
	EventURL            string   `json:"event_url"`
	EventTypes          []string `json:"event_types"`
	Fingerprint         string   `json:"fingerprint"`
	ConsecutiveFailures int      `json:"consecutive_failures"`
	LastDeliveryState   string   `json:"last_delivery_state"`
	SubscriptionNotice  string   `json:"subscription_notice"`
}

type subscription struct {
	MerchantID int64
	Revision   int64
	Generation int64
	EventURL   string
	EventTypes []string
	Failures   int
	LastState  string
	Notice     string
	Previous   string
	At         time.Time
	EventID    int64
}

func validTypes(types []string) bool {
	return types != nil && (len(types) == 0 || len(types) == 1 && types[0] == "refund")
}
func validState(state string) bool {
	switch state {
	case "none", "pending", "delivering", "delivered", "failed", "unknown", "skipped":
		return true
	}
	return false
}

func (s *Store) readSubscription(ctx context.Context, tx pgx.Tx, mid int64) (subscription, []byte, error) {
	out := subscription{MerchantID: mid, EventTypes: []string{}, LastState: "none"}
	var revision, max, count int64
	var raw, mac []byte
	err := tx.QueryRow(ctx, `SELECT h.revision,r.body,h.mac FROM merchant_event_subscription_heads201 h JOIN merchant_event_subscription_revisions201 r ON (r.merchant_id,r.revision,r.mac)=(h.merchant_id,h.revision,h.mac) WHERE h.merchant_id=$1`, mid).Scan(&revision, &raw, &mac)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return out, nil, err
	}
	if e := tx.QueryRow(ctx, `SELECT count(*),COALESCE(max(revision),0) FROM merchant_event_subscription_revisions201 WHERE merchant_id=$1`, mid).Scan(&count, &max); e != nil {
		return out, nil, e
	}
	if errors.Is(err, pgx.ErrNoRows) {
		var seen bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM backoffice_audit WHERE resource='merchant-refund-events201' AND resource_id=$1 AND operation='subscription-revision') OR EXISTS(SELECT 1 FROM merchant_event_subscription_heads201 WHERE merchant_id=$1)`, mid).Scan(&seen); err != nil {
			return out, nil, err
		}
		if count != 0 || seen {
			return out, nil, ErrSource
		}
		_, proof, e := s.seal("subscription", out)
		return out, proof, e
	}
	if revision <= 0 || revision != max || count != revision || s.open("subscription", raw, mac, &out) != nil || out.Revision != revision || out.MerchantID != mid || !text(out.EventURL, 500) || !validTypes(out.EventTypes) || out.Failures < 0 || out.Failures > 9 || !validState(out.LastState) || !text(out.Notice, 1000) {
		return out, nil, ErrSource
	}
	var witnessed bool
	if err = tx.QueryRow(ctx, `SELECT count(*)=1 FROM backoffice_audit WHERE resource='merchant-refund-events201' AND resource_id=$1 AND operation='subscription-revision' AND actor_role='system' AND actor_id=0 AND actor_name='refund-event201' AND changes=jsonb_build_object('proof',$2::text)`, mid, hex.EncodeToString(mac)).Scan(&witnessed); err != nil {
		return out, nil, err
	}
	if !witnessed {
		return out, nil, ErrSource
	}
	return out, mac, nil
}

func (s *Store) writeSubscription(ctx context.Context, tx pgx.Tx, state subscription, previous []byte) ([]byte, error) {
	state.Revision++
	state.Previous = hex.EncodeToString(previous)
	state.At = time.Now().UTC()
	raw, mac, err := s.seal("subscription", state)
	if err != nil {
		return nil, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO merchant_event_subscription_revisions201(merchant_id,revision,body,mac) VALUES($1,$2,$3,$4)`, state.MerchantID, state.Revision, raw, mac); err != nil {
		return nil, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO merchant_event_subscription_heads201(merchant_id,revision,mac) VALUES($1,$2,$3) ON CONFLICT(merchant_id) DO UPDATE SET revision=EXCLUDED.revision,mac=EXCLUDED.mac`, state.MerchantID, state.Revision, mac); err != nil {
		return nil, err
	}
	if err = audit(ctx, tx, state.MerchantID, "subscription-revision", mac); err != nil {
		return nil, err
	}
	return mac, nil
}

func view(state subscription, proof []byte) View {
	return View{EventURL: state.EventURL, EventTypes: append([]string{}, state.EventTypes...), Fingerprint: hex.EncodeToString(proof), ConsecutiveFailures: state.Failures, LastDeliveryState: state.LastState, SubscriptionNotice: state.Notice}
}

func (s *Store) ViewTx(ctx context.Context, tx pgx.Tx, mid int64) (View, error) {
	state, proof, err := s.readSubscription(ctx, tx, mid)
	out := view(state, proof)
	if err != nil {
		return out, err
	}
	if state.LastState == "delivering" && state.EventID > 0 {
		event, _, e := s.readEvent(ctx, tx, state.EventID)
		if e != nil {
			return out, e
		}
		claim, _, result, _, e := s.lastClaim(ctx, tx, event)
		if e != nil {
			return out, e
		}
		if claim.ID == 0 {
			return out, ErrSource
		}
		if result == nil {
			var live bool
			if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM jobs WHERE id=$1 AND state='running' AND lease_until>clock_timestamp())`, claim.JobID).Scan(&live); e != nil {
				return out, e
			}
			if !live {
				out.LastDeliveryState = "unknown"
			}
		}
	}
	return out, nil
}

// Caller holds and authenticates the original merchant session in this same tx.
func (s *Store) UpdateTx(ctx context.Context, tx pgx.Tx, mid int64, endpoint string, types []string, expected string) (View, error) {
	var empty View
	if !text(endpoint, 500) || !validTypes(types) || len(types) > 0 && endpoint == "" {
		return empty, ErrInvalid
	}
	state, proof, err := s.readSubscription(ctx, tx, mid)
	if err != nil {
		return empty, err
	}
	if len(expected) != 64 || !hmac.Equal([]byte(expected), []byte(hex.EncodeToString(proof))) {
		return empty, ErrConflict
	}
	// A previously authenticated endpoint can always be disabled in place.
	if endpoint != "" && httpclient.ValidateURL(endpoint) != nil && !(len(types) == 0 && endpoint == state.EventURL) {
		return empty, ErrInvalid
	}
	state.EventURL, state.EventTypes = endpoint, append([]string{}, types...)
	state.Generation++
	state.Notice = ""
	proof, err = s.writeSubscription(ctx, tx, state, proof)
	if err != nil {
		return empty, err
	}
	state.Revision++
	return view(state, proof), nil
}
