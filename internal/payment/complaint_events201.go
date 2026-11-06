package payment

import (
	"context"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/jackc/pgx/v5"
	"goepay/internal/platform/jobs"
	"goepay/internal/refundevent"
)

// captureComplaintEvent201 appends one merchant-subscription event for a
// complaint at its current coarse state, inside ux-complain's own upsert
// transaction. It is idempotent per (complaint, state) in refundevent201, so a
// re-sync at the same state enqueues nothing; a forward state change enqueues
// one event. The webhook content mirrors the complaint facts a merchant sees.
func (s *Store) captureComplaintEvent201(ctx context.Context, tx pgx.Tx, complaintID, merchantID int64, state string, rec complaintRecordView) error {
	content := struct {
		Event       string `json:"event"`
		ComplaintID string `json:"complaint_id"`
		State       string `json:"state"`
		PayType     string `json:"pay_type"`
		ThirdID     string `json:"third_id"`
		UID         string `json:"uid"`
	}{Event: "complain", ComplaintID: fmt.Sprint(complaintID), State: state, PayType: rec.PayType, ThirdID: rec.ThirdID, UID: fmt.Sprint(merchantID)}
	raw, err := complaintEventJSON201(content)
	if err != nil {
		return err
	}
	return s.refundEvents201().CaptureComplaint(ctx, tx, refundevent.ComplaintCapture{
		ComplaintID: complaintID, MerchantID: merchantID, State: state, Content: raw,
	})
}

// complaintRecordView is the subset of the provider complaint record the event
// content needs; it decouples the capture from the adapter package's type.
type complaintRecordView struct{ PayType, ThirdID string }

func complaintEventJSON201(value any) ([]byte, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return raw, nil
}

// verifyComplaintEvent201 re-proves the event against the live complaint row:
// the complaint must exist, be owned by the event's merchant, and still be at or
// past the event's reported state (the local_state never moves backward).
func (s *Store) verifyComplaintEvent201(ctx context.Context, tx pgx.Tx, event refundevent.ComplaintEvent) error {
	var owner *int64
	var liveState string
	err := tx.QueryRow(ctx, `SELECT owner_id,local_state FROM complaints133 WHERE id=$1`, event.ComplaintID).Scan(&owner, &liveState)
	if err != nil {
		return err
	}
	if owner == nil || *owner != event.MerchantID {
		return refundevent.ErrSource
	}
	rank := map[string]int{"open": 0, "processing": 1, "done": 2}
	live, ok1 := rank[liveState]
	ev, ok2 := rank[event.State]
	if !ok1 || !ok2 || live < ev {
		return refundevent.ErrSource
	}
	return nil
}

func (s *Store) DeliverMerchantComplaintEvent201(ctx context.Context, j jobs.Job, key *rsa.PrivateKey, client *http.Client) error {
	if s == nil || s.pool == nil {
		return refundevent.ErrSource
	}
	return s.refundEvents201().DeliverComplaint(ctx, j, key, client, s.verifyComplaintEvent201)
}
