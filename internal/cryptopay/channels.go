package cryptopay

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

type Channel struct {
	ID          int64     `json:"id,string"`
	Revision    int64     `json:"revision,string"`
	Name        string    `json:"name"`
	Network     string    `json:"network"`
	Enabled     bool      `json:"enabled"`
	Archived    bool      `json:"archived"`
	FeeBPS      int64     `json:"fee_bps"`
	MinAmount   string    `json:"min_amount"`
	MaxAmount   string    `json:"max_amount"`
	DayLimit    string    `json:"day_limit"`
	MerchantIDs []string  `json:"merchant_ids"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type SaveChannelInput struct {
	ID, ExpectedRevision, ActorID                            int64
	RequestID, Name, Network, MinAmount, MaxAmount, DayLimit string
	Enabled, Archived                                        bool
	FeeBPS                                                   int64
	MerchantIDs, ReservedIDs                                 []int64
}

const channelColumns = `id,revision,name,network,enabled,archived,fee_bps,min_amount,max_amount,day_limit,created_at,updated_at`

func scanChannel(row pgx.Row) (Channel, error) {
	var c Channel
	var min, max, day int64
	e := row.Scan(&c.ID, &c.Revision, &c.Name, &c.Network, &c.Enabled, &c.Archived, &c.FeeBPS, &min, &max, &day, &c.CreatedAt, &c.UpdatedAt)
	if errors.Is(e, pgx.ErrNoRows) {
		return c, ErrNotFound
	}
	c.MinAmount, c.MaxAmount, c.DayLimit = FormatAmount(min), FormatAmount(max), FormatAmount(day)
	c.MerchantIDs = []string{}
	return c, e
}

func readChannel(ctx context.Context, tx pgx.Tx, id int64, lock bool) (Channel, error) {
	suffix := ""
	if lock {
		suffix = " FOR UPDATE"
	}
	c, e := scanChannel(tx.QueryRow(ctx, "SELECT "+channelColumns+" FROM crypto_channels WHERE id=$1"+suffix, id))
	if e != nil {
		return c, e
	}
	rows, e := tx.Query(ctx, "SELECT merchant_id FROM crypto_channel_grants WHERE channel_id=$1 ORDER BY merchant_id", id)
	if e != nil {
		return c, e
	}
	defer rows.Close()
	for rows.Next() {
		var mid int64
		if e = rows.Scan(&mid); e != nil {
			return c, e
		}
		c.MerchantIDs = append(c.MerchantIDs, strconv.FormatInt(mid, 10))
	}
	return c, rows.Err()
}

func (s *Service) ListChannels(ctx context.Context) ([]Channel, error) {
	if s == nil || s.Pool == nil {
		return nil, ErrUnavailable
	}
	// Authority readers may hold FOR SHARE on the current session. This is a
	// business read, but PostgreSQL READ ONLY would prohibit those proof locks.
	tx, e := s.Pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	if e = authorize(ctx, tx); e != nil {
		return nil, e
	}
	rows, e := tx.Query(ctx, "SELECT id FROM crypto_channels ORDER BY id LIMIT 1001")
	if e != nil {
		return nil, e
	}
	ids := []int64{}
	for rows.Next() {
		var id int64
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return nil, e
		}
		ids = append(ids, id)
	}
	rows.Close()
	if e = rows.Err(); e != nil {
		return nil, e
	}
	if len(ids) > 1000 {
		return nil, ErrUnavailable
	}
	items := make([]Channel, 0, len(ids))
	for _, id := range ids {
		c, e := readChannel(ctx, tx, id, false)
		if e != nil {
			return nil, e
		}
		items = append(items, c)
	}
	return items, tx.Commit(ctx)
}

func channelLimit(raw string) (int64, error) {
	if raw == "0" {
		return 0, nil
	}
	return ParseAmount(raw)
}
func channelRequestID(s string) bool {
	if len(s) != 32 {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

func (s *Service) SaveChannel(ctx context.Context, in SaveChannelInput) (Channel, error) {
	var out Channel
	if guard, ok := ctx.Value(authorityKey{}).(func(context.Context, pgx.Tx) error); !ok || guard == nil {
		return out, ErrInvalid
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.ActorID <= 0 || in.ID < 0 || in.ExpectedRevision < 0 || in.ID == 0 && in.ExpectedRevision != 0 || in.ID > 0 && in.ExpectedRevision == 0 || in.Name == "" || len(in.Name) > 120 || !utf8.ValidString(in.Name) || in.Network != "tron" && in.Network != "ethereum" || in.FeeBPS < 0 || in.FeeBPS > 10000 || in.Archived && in.Enabled || len(in.MerchantIDs) > 1000 || len(in.ReservedIDs) > 1000 {
		return out, ErrInvalid
	}
	for _, c := range in.Name {
		if unicode.IsControl(c) {
			return out, ErrInvalid
		}
	}
	if in.ID == 0 && !channelRequestID(in.RequestID) || in.RequestID != "" && !channelRequestID(in.RequestID) {
		return out, ErrInvalid
	}
	min, e := channelLimit(in.MinAmount)
	if e != nil {
		return out, ErrInvalid
	}
	max, e := channelLimit(in.MaxAmount)
	if e != nil || max != 0 && max < min {
		return out, ErrInvalid
	}
	day, e := channelLimit(in.DayLimit)
	if e != nil {
		return out, ErrInvalid
	}
	merchants := append([]int64(nil), in.MerchantIDs...)
	sort.Slice(merchants, func(i, j int) bool { return merchants[i] < merchants[j] })
	for i, id := range merchants {
		if id <= 0 || i > 0 && id == merchants[i-1] {
			return out, ErrInvalid
		}
	}
	in.MerchantIDs = merchants
	hashInput := in
	hashInput.ReservedIDs = nil
	raw, e := json.Marshal(hashInput)
	if e != nil {
		return out, e
	}
	digest := sha256.Sum256(raw)
	tx, e := s.begin(ctx)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('goepay/runtime-parent/catalog',0))`); e != nil {
		return out, e
	}
	if in.RequestID != "" {
		var id, revision int64
		var previous []byte
		e = tx.QueryRow(ctx, "SELECT channel_id,revision,request_hash FROM crypto_channel_requests WHERE actor_id=$1 AND request_id=$2", in.ActorID, in.RequestID).Scan(&id, &revision, &previous)
		if e == nil {
			if subtle.ConstantTimeCompare(previous, digest[:]) != 1 {
				return out, ErrConflict
			}
			out, e = readChannel(ctx, tx, id, false)
			if e != nil {
				return out, e
			}
			if out.Revision != revision {
				return out, ErrConflict
			}
			return out, commit(ctx, tx)
		}
		if !errors.Is(e, pgx.ErrNoRows) {
			return out, e
		}
	}
	// Lock merchant rows before the channel, matching invoice admission's order.
	rows, e := tx.Query(ctx, "SELECT id FROM merchants WHERE id=ANY($1::bigint[]) ORDER BY id FOR KEY SHARE", merchants)
	if e != nil {
		return out, e
	}
	count := 0
	for rows.Next() {
		count++
	}
	rows.Close()
	if e = rows.Err(); e != nil {
		return out, e
	}
	if count != len(merchants) {
		return out, ErrInvalid
	}
	if in.ID == 0 {
		id, e := allocateChannelID(ctx, tx, in.ReservedIDs)
		if e != nil {
			return out, e
		}
		_, e = tx.Exec(ctx, `INSERT INTO crypto_channels(id,name,network,enabled,archived,revision,fee_bps,min_amount,max_amount,day_limit) VALUES($1,$2,$3,$4,$5,1,$6,$7,$8,$9)`, id, in.Name, in.Network, in.Enabled, in.Archived, in.FeeBPS, min, max, day)
		if e != nil {
			return out, e
		}
		in.ID = id
	} else {
		old, e := readChannel(ctx, tx, in.ID, true)
		if e != nil {
			return out, e
		}
		if old.Revision != in.ExpectedRevision || old.Network != in.Network || old.Archived || old.Revision == math.MaxInt64 {
			return out, ErrConflict
		}
		_, e = tx.Exec(ctx, `UPDATE crypto_channels SET name=$2,enabled=$3,archived=$4,fee_bps=$5,min_amount=$6,max_amount=$7,day_limit=$8,revision=revision+1,updated_at=clock_timestamp() WHERE id=$1`, in.ID, in.Name, in.Enabled, in.Archived, in.FeeBPS, min, max, day)
		if e != nil {
			return out, e
		}
	}
	if _, e = tx.Exec(ctx, "DELETE FROM crypto_channel_grants WHERE channel_id=$1", in.ID); e != nil {
		return out, e
	}
	for _, mid := range merchants {
		if _, e = tx.Exec(ctx, "INSERT INTO crypto_channel_grants(channel_id,merchant_id) VALUES($1,$2)", in.ID, mid); e != nil {
			return out, e
		}
	}
	out, e = readChannel(ctx, tx, in.ID, false)
	if e != nil {
		return out, e
	}
	if in.RequestID != "" {
		if _, e = tx.Exec(ctx, "INSERT INTO crypto_channel_requests(actor_id,request_id,request_hash,channel_id,revision) VALUES($1,$2,$3,$4,$5)", in.ActorID, in.RequestID, digest[:], out.ID, out.Revision); e != nil {
			return out, e
		}
	}
	if e = audit(ctx, tx, in.ActorID, "channel_save", strconv.FormatInt(out.ID, 10)+":"+strconv.FormatInt(out.Revision, 10)); e != nil {
		return out, e
	}
	return out, commit(ctx, tx)
}

func allocateChannelID(ctx context.Context, tx pgx.Tx, reserved []int64) (int64, error) {
	var highest, last int64
	var called, parentExists bool
	if e := tx.QueryRow(ctx, "SELECT COALESCE(max(id),0) FROM crypto_channels").Scan(&highest); e != nil {
		return 0, e
	}
	if e := tx.QueryRow(ctx, "SELECT to_regclass('runtime_parent_channels') IS NOT NULL").Scan(&parentExists); e != nil {
		return 0, e
	}
	if parentExists {
		var n int64
		if e := tx.QueryRow(ctx, "SELECT COALESCE(max(id),0) FROM runtime_parent_channels").Scan(&n); e != nil {
			return 0, e
		}
		if n > highest {
			highest = n
		}
	}
	for _, id := range reserved {
		if id <= 0 {
			return 0, ErrInvalid
		}
		if id > highest {
			highest = id
		}
	}
	if e := tx.QueryRow(ctx, "SELECT last_value,is_called FROM runtime_parent_channel_ids").Scan(&last, &called); e != nil {
		return 0, e
	}
	if !called {
		last--
	}
	if last > highest {
		highest = last
	}
	if highest == math.MaxInt64 {
		return 0, ErrInvalid
	}
	if _, e := tx.Exec(ctx, "SELECT setval('runtime_parent_channel_ids',$1,false)", highest+1); e != nil {
		return 0, e
	}
	var id int64
	e := tx.QueryRow(ctx, "SELECT nextval('runtime_parent_channel_ids')").Scan(&id)
	return id, e
}
