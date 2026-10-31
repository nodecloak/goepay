package pagetemplate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
)

// Versions of the custom home page. Every save or restore appends an
// immutable row under the home head lock (kind-wide CAS, like every other
// template change), writes an audit row and enqueues one administrator notice
// job in the same transaction. The newest version is what the page shows.

// CustomNoticeKind is the job the worker turns into an administrator notice.
const CustomNoticeKind = "admin.page-template-changed"

type CustomInput struct {
	HTML  string
	CSS   string
	Frame bool
}

type CustomView struct {
	Version      int    `json:"version"`
	Revision     int64  `json:"revision,string"`
	HTML         string `json:"html"`
	CSS          string `json:"css"`
	Frame        bool   `json:"frame"`
	CreatedAt    string `json:"created_at"`
	Actor        string `json:"actor"`
	RestoredFrom int    `json:"restored_from"`
}

type CustomSaved struct {
	Version  int            `json:"version"`
	Revision int64          `json:"revision,string"`
	Removed  map[string]int `json:"removed"`
}

type CustomVersionItem struct {
	Version      int    `json:"version"`
	CreatedAt    string `json:"created_at"`
	Actor        string `json:"actor"`
	HTMLBytes    int    `json:"html_bytes"`
	CSSBytes     int    `json:"css_bytes"`
	Frame        bool   `json:"frame"`
	RestoredFrom int    `json:"restored_from"`
	Notice       string `json:"notice"`
}

func customDigest(c CustomContent) string {
	raw, _ := json.Marshal(map[string]any{"html": c.HTML, "css": c.CSS, "frame": c.Frame})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// readCustom reads one version (0 = the newest); ok is false when none exists.
func readCustom(ctx context.Context, q reader, version int) (CustomContent, bool, error) {
	var c CustomContent
	e := q.QueryRow(ctx, `SELECT version,html,css,frame FROM page_template_custom_versions WHERE kind='home' AND ($1::int=0 OR version=$1::int) ORDER BY version DESC LIMIT 1`, version).Scan(&c.Version, &c.HTML, &c.CSS, &c.Frame)
	if errors.Is(e, pgx.ErrNoRows) {
		return CustomContent{}, false, nil
	}
	if e != nil {
		return CustomContent{}, false, e
	}
	return c, true, nil
}

// CustomCurrent is the newest version for the editor (version 0 when none).
func (s *Store) CustomCurrent(ctx context.Context, a Actor) (CustomView, error) {
	return s.customView(ctx, 0, a)
}

// CustomVersion is one saved version for the editor.
func (s *Store) CustomVersion(ctx context.Context, version int, a Actor) (CustomView, error) {
	if version < 1 {
		return CustomView{}, ErrInvalid
	}
	return s.customView(ctx, version, a)
}

func (s *Store) customView(ctx context.Context, version int, a Actor) (CustomView, error) {
	v := CustomView{Frame: true}
	if !allowed(a) {
		return CustomView{}, ErrForbidden
	}
	if s.pool == nil {
		return CustomView{}, ErrNotFound
	}
	if e := s.pool.QueryRow(ctx, "SELECT revision FROM page_template_heads WHERE kind='home'").Scan(&v.Revision); e != nil {
		return CustomView{}, e
	}
	var at time.Time
	e := s.pool.QueryRow(ctx, `SELECT version,html,css,frame,COALESCE(restored_from,0),actor_name,created_at FROM page_template_custom_versions WHERE kind='home' AND ($1::int=0 OR version=$1::int) ORDER BY version DESC LIMIT 1`, version).Scan(&v.Version, &v.HTML, &v.CSS, &v.Frame, &v.RestoredFrom, &v.Actor, &at)
	if errors.Is(e, pgx.ErrNoRows) {
		if version > 0 {
			return CustomView{}, ErrNotFound
		}
		return v, nil
	}
	if e != nil {
		return CustomView{}, e
	}
	v.CreatedAt = at.UTC().Format(time.RFC3339)
	return v, nil
}

// CustomVersions lists the newest 50 versions with their notice state:
// pending (not yet attempted), accepted, or unknown (attempted without a
// recorded answer).
func (s *Store) CustomVersions(ctx context.Context, a Actor) ([]CustomVersionItem, error) {
	if !allowed(a) {
		return nil, ErrForbidden
	}
	if s.pool == nil {
		return nil, ErrNotFound
	}
	rows, e := s.pool.Query(ctx, `SELECT v.version,v.created_at,v.actor_name,octet_length(v.html),octet_length(v.css),v.frame,COALESCE(v.restored_from,0),
 CASE WHEN r.state IS NOT NULL THEN r.state WHEN n.version_id IS NOT NULL AND n.claimed_at<clock_timestamp()-interval '1 minute' THEN 'unknown' ELSE 'pending' END
 FROM page_template_custom_versions v LEFT JOIN page_template_custom_notices n ON n.version_id=v.id LEFT JOIN page_template_custom_notice_results r ON r.version_id=v.id
 WHERE v.kind='home' ORDER BY v.version DESC LIMIT 50`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	items := []CustomVersionItem{}
	for rows.Next() {
		var it CustomVersionItem
		var at time.Time
		if e = rows.Scan(&it.Version, &at, &it.Actor, &it.HTMLBytes, &it.CSSBytes, &it.Frame, &it.RestoredFrom, &it.Notice); e != nil {
			return nil, e
		}
		it.CreatedAt = at.UTC().Format(time.RFC3339)
		items = append(items, it)
	}
	return items, rows.Err()
}

// CustomPreview is the content the console preview shows: the given version,
// or the newest (nil when none is saved yet, which shows the sample).
func (s *Store) CustomPreview(ctx context.Context, version int, a Actor) (*CustomContent, error) {
	if !allowed(a) {
		return nil, ErrForbidden
	}
	if s.pool == nil {
		return nil, ErrNotFound
	}
	if version < 0 {
		return nil, ErrInvalid
	}
	c, ok, e := readCustom(ctx, s.pool, version)
	if e != nil {
		return nil, e
	}
	if !ok {
		if version > 0 {
			return nil, ErrNotFound
		}
		return nil, nil
	}
	return &c, nil
}

// SaveCustom stores the administrator's HTML and CSS as a new version.
func (s *Store) SaveCustom(ctx context.Context, expected int64, in CustomInput, a Actor) (CustomSaved, error) {
	if !allowed(a) {
		return CustomSaved{}, ErrForbidden
	}
	if expected < 0 {
		return CustomSaved{}, ErrInvalid
	}
	c := CustomContent{HTML: in.HTML, CSS: in.CSS, Frame: in.Frame}
	clean, e := checkCustom(c)
	if e != nil {
		return CustomSaved{}, e
	}
	if s.pool == nil {
		return CustomSaved{}, ErrNotFound
	}
	return s.appendCustom(ctx, expected, c, clean, 0, a)
}

// RestoreCustom appends a new version with the content of an older one.
func (s *Store) RestoreCustom(ctx context.Context, expected int64, version int, a Actor) (CustomSaved, error) {
	if !allowed(a) {
		return CustomSaved{}, ErrForbidden
	}
	if expected < 0 || version < 1 {
		return CustomSaved{}, ErrInvalid
	}
	if s.pool == nil {
		return CustomSaved{}, ErrNotFound
	}
	c, ok, e := readCustom(ctx, s.pool, version)
	if e != nil {
		return CustomSaved{}, e
	}
	if !ok {
		return CustomSaved{}, ErrNotFound
	}
	clean, e := checkCustom(c)
	if e != nil {
		return CustomSaved{}, e
	}
	return s.appendCustom(ctx, expected, c, clean, version, a)
}

func (s *Store) appendCustom(ctx context.Context, expected int64, c CustomContent, clean SanitizedHTML, restoredFrom int, a Actor) (CustomSaved, error) {
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return CustomSaved{}, e
	}
	defer tx.Rollback(ctx)
	var revision int64
	if e = tx.QueryRow(ctx, "SELECT revision FROM page_template_heads WHERE kind='home' FOR UPDATE").Scan(&revision); e != nil {
		return CustomSaved{}, e
	}
	if revision != expected {
		return CustomSaved{}, ErrConflict
	}
	for _, id := range clean.Assets {
		if _, e = readAsset(ctx, tx, id); e != nil {
			return CustomSaved{}, &CustomError{"HTML 引用的模板图片 " + id[:12] + "… 不存在或无效，请先上传"}
		}
	}
	var version int
	if e = tx.QueryRow(ctx, "SELECT COALESCE(max(version),0)+1 FROM page_template_custom_versions WHERE kind='home'").Scan(&version); e != nil {
		return CustomSaved{}, e
	}
	next := revision + 1
	if next <= revision || version <= restoredFrom {
		return CustomSaved{}, ErrInvalid
	}
	digest := customDigest(c)
	var restored any
	if restoredFrom > 0 {
		restored = restoredFrom
	}
	var id int64
	if e = tx.QueryRow(ctx, `INSERT INTO page_template_custom_versions(kind,version,html,css,frame,restored_from,digest,actor_id,actor_name)VALUES('home',$1,$2,$3,$4,$5,$6,$7,$8) RETURNING id`, version, c.HTML, c.CSS, c.Frame, restored, digest, a.ID, clip(a.Name, 64)).Scan(&id); e != nil {
		return CustomSaved{}, e
	}
	if _, e = tx.Exec(ctx, "UPDATE page_template_heads SET revision=$2,updated_at=now() WHERE kind=$1", "home", next); e != nil {
		return CustomSaved{}, e
	}
	operation := "save-html"
	if restoredFrom > 0 {
		operation = "restore-html"
	}
	changes, _ := json.Marshal(map[string]any{"type": "home", "name": "custom", "revision": next, "version": version, "restored_from": restoredFrom, "digest": digest, "html_bytes": len(c.HTML), "css_bytes": len(c.CSS), "frame": c.Frame, "removed": clean.Removed})
	if _, e = tx.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes)VALUES($1,$2,$3,'page-templates',1,$4,$5::jsonb)`, a.Role, a.ID, a.Name, operation, string(changes)); e != nil {
		return CustomSaved{}, e
	}
	vid := strconv.FormatInt(id, 10)
	if _, e = tx.Exec(ctx, `INSERT INTO jobs(event_key,kind,payload,run_at)VALUES($1,$2,jsonb_build_object('version_id',$3::text),clock_timestamp())`, "page-template-custom:"+vid, CustomNoticeKind, vid); e != nil {
		return CustomSaved{}, e
	}
	if e = tx.Commit(ctx); e != nil {
		return CustomSaved{}, e
	}
	return CustomSaved{Version: version, Revision: next, Removed: clean.Removed}, nil
}

// resolveCustom loads and checks the newest version for the public page; a
// reason is returned when the page must fall back to the default template.
func resolveCustom(ctx context.Context, q reader) (*CustomContent, string) {
	c, ok, e := readCustom(ctx, q, 0)
	if e != nil {
		return nil, "store_unavailable"
	}
	if !ok {
		return nil, "custom_unavailable"
	}
	clean, e := checkCustom(c)
	if e != nil {
		return nil, "invalid_custom_html"
	}
	for _, id := range clean.Assets {
		if _, e = readAsset(ctx, q, id); e != nil {
			return nil, "asset_unavailable"
		}
	}
	c.Assets = clean.Assets
	c.clean = &clean
	return &c, ""
}
