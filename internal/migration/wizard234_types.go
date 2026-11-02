package migration

import (
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"goepay/internal/identity"
)

const MaxDumpBytes234 int64 = 64 << 20

var (
	ErrWizardInvalid234     = errors.New("invalid migration wizard operation")
	ErrWizardConflict234    = errors.New("migration wizard state changed")
	ErrWizardTarget234      = errors.New("migration target contains business data")
	ErrWizardUnavailable234 = errors.New("migration wizard unavailable")
	ErrWizardCapacity234    = errors.New("migration staging capacity reached")
	ErrWizardBusy234        = errors.New("migration target is busy")
)

type WizardActor234 struct {
	User      identity.User
	Token     string
	Delegated bool
}

type ApplyInput234 struct {
	ExpectedSHA256      string `json:"expected_sha256"`
	SourceFrozen        bool   `json:"source_frozen"`
	ArchiveAcknowledged bool   `json:"archive_acknowledged"`
	Confirmation        string `json:"confirmation"`
}

type WizardView234 struct {
	ID             string         `json:"id"`
	Status         string         `json:"status"`
	SHA256         string         `json:"sha256"`
	Source         string         `json:"source"`
	Prefix         string         `json:"prefix"`
	TableCounts    []DumpTable234 `json:"table_counts"`
	Merchants      int            `json:"merchant_count"`
	Orders         int            `json:"order_count"`
	Refunds        int            `json:"refund_count"`
	Transfers      int            `json:"transfer_count"`
	AvailableTotal string         `json:"available_total"`
	ReservedTotal  string         `json:"reserved_total"`
	Issues         []Issue        `json:"issues"`
	Warnings       []Issue        `json:"warnings"`
	TargetReady    bool           `json:"target_ready"`
	Progress       int            `json:"progress"`
	Report         *Report        `json:"report"`
	ExpiresAt      time.Time      `json:"expires_at"`
}

type Capabilities234 struct {
	Enabled        bool            `json:"enabled"`
	MaxUploadBytes int64           `json:"max_upload_bytes"`
	TargetReady    bool            `json:"target_ready"`
	Issues         []Issue         `json:"issues"`
	Runs           []WizardView234 `json:"runs"`
}

type Wizard234 struct {
	pool *pgxpool.Pool
	key  []byte
}

func NewWizard234(pool *pgxpool.Pool, key []byte) (*Wizard234, error) {
	if pool == nil || len(key) != 32 {
		return nil, ErrWizardUnavailable234
	}
	return &Wizard234{pool: pool, key: append([]byte(nil), key...)}, nil
}
