package notification

import (
	"crypto/sha256"
	"encoding/json"
	"errors"

	"goepay/internal/platform/jobs"
)

type deliveryFailure struct {
	code, message string
	identity      [32]byte
}

func (e *deliveryFailure) Error() string { return e.message }
func deliveryIdentity(j jobs.Job) [32]byte {
	token, payload := sha256.Sum256([]byte(j.Token)), sha256.Sum256(j.Payload)
	raw, _ := json.Marshal([]any{j.ID, j.Kind, j.EventKey, j.Attempts, token, payload})
	return sha256.Sum256(raw)
}
func observedDeliveryFailure(j jobs.Job, code, message string) error {
	return &deliveryFailure{code: code, message: message, identity: deliveryIdentity(j)}
}

// FailureCode identifies an observed delivery failure for this exact original
// job lease, rather than a permission, proof, payload or configuration failure.
func FailureCode(err error, j jobs.Job) (string, bool) {
	var failure *deliveryFailure
	if !errors.As(err, &failure) || failure.identity != deliveryIdentity(j) {
		return "", false
	}
	return failure.code, true
}
