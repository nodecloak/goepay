package migration

import (
	"regexp"
	"strings"
	"time"
)

var registrationTimePattern = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(\.[0-9]{1,9})?(Z|[+-]([01][0-9]|2[0-3]):[0-5][0-9])$`)

// New optional merchant birth data needs an explicit known offset. Existing
// legacy order timestamp parsing is intentionally unchanged.
func parseRegistrationTime(value string) (time.Time, error) {
	if !registrationTimePattern.MatchString(value) || strings.HasSuffix(value, "-00:00") {
		return time.Time{}, ErrValidation
	}
	at, e := time.Parse(time.RFC3339Nano, value)
	if e != nil || at.IsZero() || at.Year() < 1 {
		return time.Time{}, ErrValidation
	}
	return at, nil
}
