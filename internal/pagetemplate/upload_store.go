package pagetemplate

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Uploaded templates in the store. Every change locks the kind's head row
// and moves its revision (the same compare-and-set as every other template
// change), and writes an audit row; uploads, restores, removals and
// selections of an uploaded template also append an event with one
// administrator notice job, in the same transaction.

// UploadNoticeKind is the job the worker turns into an administrator notice.
const UploadNoticeKind = "admin.page-template-notice"

type querier interface {
	reader
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

// UploadReport is what checking a package found.
type UploadReport struct {
	Files    int            `json:"files"`
	Size     int64          `json:"size"`
	Warnings []string       `json:"warnings"`
	Removed  map[string]int `json:"removed"`
	Ignored  []string       `json:"ignored"`
}

// UploadCheck tells the console what uploading a package would do.
type UploadCheck struct {
	Type        string `json:"type"`
	Name        string `json:"name"`
	Title       string `json:"title"`
	Exists      bool   `json:"exists"`
	Selected    bool   `json:"selected"`
	NextVersion int    `json:"next_version"`
	Identical   bool   `json:"identical"`
	UploadReport
}

// UploadSaved is a stored upload or restore.
type UploadSaved struct {
	Type     string `json:"type"`
	Name     string `json:"name"`
	Title    string `json:"title"`
	Version  int    `json:"version"`
	Revision int64  `json:"revision,string"`
	Created  bool   `json:"created"`
	Selected bool   `json:"selected"`
	UploadReport
}

// UploadVersionItem is one version in the console's version list.
type UploadVersionItem struct {
	Version        int    `json:"version"`
	Operation      string `json:"operation"`
	RestoredFrom   int    `json:"restored_from"`
	Title          string `json:"title"`
	PackageVersion string `json:"package_version"`
	Author         string `json:"author"`
	Files          int    `json:"files"`
	Size           int64  `json:"size"`
	CreatedAt      string `json:"created_at"`
	Actor          string `json:"actor"`
	Notice         string `json:"notice"`
}

// TemplateFile is a public package file.
type TemplateFile struct {
	MIME, Digest string
	Content      []byte
}

type preparedPackage struct {
	pkg      *Package
	key      string
	compiled *compiledTemplate
	digests  map[string]string
	digest   string
	report   UploadReport
}

func newFileKey() (string, error) {
	var b [16]byte
	if _, e := rand.Read(b[:]); e != nil {
		return "", e
	}
	return hex.EncodeToString(b[:]), nil
}

func newReport(files map[string][]byte, warn *warnList, removed map[string]int, ignored []string) UploadReport {
	r := UploadReport{Files: len(files), Warnings: warn.list(), Removed: map[string]int{}, Ignored: []string{}}
	for _, b := range files {
		r.Size += int64(len(b))
	}
	for k, v := range removed {
		r.Removed[k] = v
	}
	r.Ignored = append(r.Ignored, ignored...)
	return r
}

func preparePackage(kind string, raw []byte) (*preparedPackage, error) {
	pkg, e := ParsePackage(kind, raw)
	if e != nil {
		return nil, e
	}
	key, e := newFileKey()
	if e != nil {
		return nil, e
	}
	warn := &warnList{}
	c, e := compilePackage(kind, key, pkg.Files, pathSet(pkg.Files), true, warn)
	if e != nil {
		return nil, e
	}
	digests := map[string]string{}
	for p, b := range pkg.Files {
		digests[p] = digestHex(b)
	}
	return &preparedPackage{pkg: pkg, key: key, compiled: c, digests: digests, digest: packageDigest(digests), report: newReport(pkg.Files, warn, c.removed, pkg.Ignored)}, nil
}

type uploadStatus struct {
	id             int64
	exists, active bool
	newest         int
	newestID       int64
	digest, title  string
}

const latestOperation = `COALESCE((SELECT e.operation FROM page_template_upload_events e WHERE e.upload_id=u.id ORDER BY e.id DESC LIMIT 1),'remove')`

func uploadState(ctx context.Context, q reader, kind, name string) (uploadStatus, error) {
	var st uploadStatus
	var operation string
	e := q.QueryRow(ctx, `SELECT u.id,`+latestOperation+`,v.id,v.version,v.digest,v.title FROM page_template_uploads u JOIN LATERAL (SELECT id,version,digest,title FROM page_template_upload_versions v WHERE v.upload_id=u.id ORDER BY v.version DESC LIMIT 1) v ON true WHERE u.kind=$1 AND u.name=$2`, kind, name).Scan(&st.id, &operation, &st.newestID, &st.newest, &st.digest, &st.title)
	if errors.Is(e, pgx.ErrNoRows) {
		return uploadStatus{}, nil
	}
	if e != nil {
		return uploadStatus{}, e
	}
	st.exists, st.active = true, operation != "remove"
	return st, nil
}

func uploadActive(ctx context.Context, q reader, kind, name string) (bool, error) {
	st, e := uploadState(ctx, q, kind, name)
	return st.active, e
}

func lockHead(ctx context.Context, tx pgx.Tx, kind string, expected int64) (int64, string, error) {
	var revision int64
	var selected string
	if e := tx.QueryRow(ctx, "SELECT revision,selected_name FROM page_template_heads WHERE kind=$1 FOR UPDATE", kind).Scan(&revision, &selected); e != nil {
		return 0, "", e
	}
	if revision != expected {
		return 0, "", ErrConflict
	}
	if revision+1 <= revision {
		return 0, "", ErrInvalid
	}
	return revision, selected, nil
}

func bumpHead(ctx context.Context, tx pgx.Tx, kind string, next int64) error {
	_, e := tx.Exec(ctx, "UPDATE page_template_heads SET revision=$2,updated_at=now() WHERE kind=$1", kind, next)
	return e
}

func uploadAudit(ctx context.Context, tx pgx.Tx, a Actor, kind, operation string, changes map[string]any) error {
	id := map[string]int64{"home": 1, "pay": 2, "qrpay": 3}[kind]
	raw, _ := json.Marshal(changes)
	_, e := tx.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes)VALUES($1,$2,$3,'page-templates',$4,$5,$6::jsonb)`, a.Role, a.ID, a.Name, id, operation, string(raw))
	return e
}

// uploadEvent appends an event of an uploaded template and its notice job.
func uploadEvent(ctx context.Context, tx pgx.Tx, uploadID int64, operation string, version, restoredFrom int, a Actor) error {
	var restored any
	if restoredFrom > 0 {
		restored = restoredFrom
	}
	var id int64
	if e := tx.QueryRow(ctx, `INSERT INTO page_template_upload_events(upload_id,operation,version,restored_from,actor_id,actor_name)VALUES($1,$2,$3,$4,$5,$6) RETURNING id`, uploadID, operation, version, restored, a.ID, clip(a.Name, 64)).Scan(&id); e != nil {
		return e
	}
	eid := strconv.FormatInt(id, 10)
	_, e := tx.Exec(ctx, `INSERT INTO jobs(event_key,kind,payload,run_at)VALUES($1,$2,jsonb_build_object('event_id',$3::text),clock_timestamp())`, "page-template-event:"+eid, UploadNoticeKind, eid)
	return e
}

type versionRow struct {
	title, author, description, packageVersion, digest string
	files                                              int
	size                                               int64
}

func insertVersion(ctx context.Context, tx pgx.Tx, uploadID int64, version int, key string, v versionRow, restoredFrom int, a Actor) (int64, error) {
	var restored any
	if restoredFrom > 0 {
		restored = restoredFrom
	}
	var id int64
	e := tx.QueryRow(ctx, `INSERT INTO page_template_upload_versions(upload_id,version,file_key,title,author,description,package_version,digest,files,size_bytes,restored_from,actor_id,actor_name)VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13) RETURNING id`, uploadID, version, key, v.title, v.author, v.description, v.packageVersion, v.digest, v.files, v.size, restored, a.ID, clip(a.Name, 64)).Scan(&id)
	return id, e
}

func insertFiles(ctx context.Context, tx pgx.Tx, versionID int64, files map[string][]byte, digests map[string]string) error {
	b := &pgx.Batch{}
	paths := sortedPaths(files)
	for _, p := range paths {
		b.Queue(`INSERT INTO page_template_blobs(digest,content)VALUES($1,$2)ON CONFLICT(digest)DO NOTHING`, digests[p], files[p])
	}
	for _, p := range paths {
		b.Queue(`INSERT INTO page_template_upload_files(version_id,path,digest)VALUES($1,$2,$3)`, versionID, p, digests[p])
	}
	r := tx.SendBatch(ctx, b)
	for range 2 * len(paths) {
		if _, e := r.Exec(); e != nil {
			r.Close()
			return e
		}
	}
	return r.Close()
}

// CheckUpload checks a package and reports what uploading it would do.
func (s *Store) CheckUpload(ctx context.Context, kind string, raw []byte, a Actor) (UploadCheck, error) {
	if !allowed(a) {
		return UploadCheck{}, ErrForbidden
	}
	if !validKind(kind) {
		return UploadCheck{}, ErrInvalid
	}
	if s.pool == nil {
		return UploadCheck{}, ErrNotFound
	}
	p, e := preparePackage(kind, raw)
	if e != nil {
		return UploadCheck{}, e
	}
	m := p.pkg.Manifest
	out := UploadCheck{Type: kind, Name: m.Name, Title: m.Title, NextVersion: 1, UploadReport: p.report}
	st, e := uploadState(ctx, s.pool, kind, m.Name)
	if e != nil {
		return UploadCheck{}, e
	}
	var selected string
	if e = s.pool.QueryRow(ctx, "SELECT selected_name FROM page_template_heads WHERE kind=$1", kind).Scan(&selected); e != nil {
		return UploadCheck{}, e
	}
	out.Exists, out.Selected = st.active, selected == m.Name
	out.NextVersion = st.newest + 1
	out.Identical = st.active && st.digest == p.digest
	return out, nil
}

// UploadTemplate stores a package as a new template, or as the next version
// of the template with the same name.
func (s *Store) UploadTemplate(ctx context.Context, kind string, expected int64, raw []byte, a Actor) (UploadSaved, error) {
	if !allowed(a) {
		return UploadSaved{}, ErrForbidden
	}
	if !validKind(kind) || expected < 0 {
		return UploadSaved{}, ErrInvalid
	}
	if s.pool == nil {
		return UploadSaved{}, ErrNotFound
	}
	p, e := preparePackage(kind, raw)
	if e != nil {
		return UploadSaved{}, e
	}
	m := p.pkg.Manifest
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return UploadSaved{}, e
	}
	defer tx.Rollback(ctx)
	revision, selected, e := lockHead(ctx, tx, kind, expected)
	if e != nil {
		return UploadSaved{}, e
	}
	st, e := uploadState(ctx, tx, kind, m.Name)
	if e != nil {
		return UploadSaved{}, e
	}
	if st.active && st.digest == p.digest {
		return UploadSaved{}, refuse("和第 %d 版完全相同，没有生成新版本", st.newest)
	}
	uploadID := st.id
	if !st.exists {
		if e = tx.QueryRow(ctx, `INSERT INTO page_template_uploads(kind,name)VALUES($1,$2) RETURNING id`, kind, m.Name).Scan(&uploadID); e != nil {
			return UploadSaved{}, e
		}
	}
	version := st.newest + 1
	row := versionRow{title: m.Title, author: m.Author, description: m.Description, packageVersion: m.Version, digest: p.digest, files: p.report.Files, size: p.report.Size}
	versionID, e := insertVersion(ctx, tx, uploadID, version, p.key, row, 0, a)
	if e != nil {
		return UploadSaved{}, e
	}
	if e = insertFiles(ctx, tx, versionID, p.pkg.Files, p.digests); e != nil {
		return UploadSaved{}, e
	}
	if e = uploadEvent(ctx, tx, uploadID, "upload", version, 0, a); e != nil {
		return UploadSaved{}, e
	}
	next := revision + 1
	if e = bumpHead(ctx, tx, kind, next); e != nil {
		return UploadSaved{}, e
	}
	if e = uploadAudit(ctx, tx, a, kind, "upload-template", map[string]any{"type": kind, "name": m.Name, "revision": next, "version": version, "digest": p.digest, "files": row.files, "size": row.size}); e != nil {
		return UploadSaved{}, e
	}
	if e = tx.Commit(ctx); e != nil {
		return UploadSaved{}, e
	}
	cacheCompiled(p.compiled)
	return UploadSaved{Type: kind, Name: m.Name, Title: m.Title, Version: version, Revision: next, Created: !st.exists, Selected: selected == m.Name, UploadReport: p.report}, nil
}

// loadFiles reads the files of a version and checks their digests.
func loadFiles(ctx context.Context, q querier, versionID int64) (map[string][]byte, error) {
	rows, e := q.Query(ctx, `SELECT f.path,f.digest,b.content FROM page_template_upload_files f JOIN page_template_blobs b ON b.digest=f.digest WHERE f.version_id=$1`, versionID)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	files := map[string][]byte{}
	for rows.Next() {
		var p, d string
		var b []byte
		if e = rows.Scan(&p, &d, &b); e != nil {
			return nil, e
		}
		if digestHex(b) != d {
			return nil, ErrInvalid
		}
		files[p] = b
	}
	return files, rows.Err()
}

func versionID(ctx context.Context, q reader, uploadID int64, version int) (int64, versionRow, error) {
	var id int64
	var v versionRow
	e := q.QueryRow(ctx, `SELECT id,title,author,description,package_version,digest,files,size_bytes FROM page_template_upload_versions WHERE upload_id=$1 AND version=$2`, uploadID, version).Scan(&id, &v.title, &v.author, &v.description, &v.packageVersion, &v.digest, &v.files, &v.size)
	if errors.Is(e, pgx.ErrNoRows) {
		return 0, v, ErrNotFound
	}
	return id, v, e
}

// RestoreUpload appends a new version with the files of an older one.
func (s *Store) RestoreUpload(ctx context.Context, kind, name string, expected int64, version int, a Actor) (UploadSaved, error) {
	if !allowed(a) {
		return UploadSaved{}, ErrForbidden
	}
	if !validKind(kind) || !ValidUploadName(name) || expected < 0 || version < 1 {
		return UploadSaved{}, ErrInvalid
	}
	if s.pool == nil {
		return UploadSaved{}, ErrNotFound
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return UploadSaved{}, e
	}
	defer tx.Rollback(ctx)
	revision, selected, e := lockHead(ctx, tx, kind, expected)
	if e != nil {
		return UploadSaved{}, e
	}
	st, e := uploadState(ctx, tx, kind, name)
	if e != nil {
		return UploadSaved{}, e
	}
	if !st.active || version > st.newest {
		return UploadSaved{}, ErrNotFound
	}
	if version == st.newest {
		return UploadSaved{}, refuse("第 %d 版已经是最新版本", version)
	}
	oldID, row, e := versionID(ctx, tx, st.id, version)
	if e != nil {
		return UploadSaved{}, e
	}
	files, e := loadFiles(ctx, tx, oldID)
	if e != nil {
		return UploadSaved{}, e
	}
	if e = checkFiles(kind, files); e != nil {
		return UploadSaved{}, e
	}
	key, e := newFileKey()
	if e != nil {
		return UploadSaved{}, e
	}
	warn := &warnList{}
	compiled, e := compilePackage(kind, key, files, pathSet(files), true, warn)
	if e != nil {
		return UploadSaved{}, e
	}
	next := st.newest + 1
	newID, e := insertVersion(ctx, tx, st.id, next, key, row, version, a)
	if e != nil {
		return UploadSaved{}, e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO page_template_upload_files(version_id,path,digest) SELECT $1,path,digest FROM page_template_upload_files WHERE version_id=$2`, newID, oldID); e != nil {
		return UploadSaved{}, e
	}
	if e = uploadEvent(ctx, tx, st.id, "restore", next, version, a); e != nil {
		return UploadSaved{}, e
	}
	nextRevision := revision + 1
	if e = bumpHead(ctx, tx, kind, nextRevision); e != nil {
		return UploadSaved{}, e
	}
	if e = uploadAudit(ctx, tx, a, kind, "restore-template", map[string]any{"type": kind, "name": name, "revision": nextRevision, "version": next, "restored_from": version, "digest": row.digest}); e != nil {
		return UploadSaved{}, e
	}
	if e = tx.Commit(ctx); e != nil {
		return UploadSaved{}, e
	}
	cacheCompiled(compiled)
	return UploadSaved{Type: kind, Name: name, Title: row.title, Version: next, Revision: nextRevision, Selected: selected == name, UploadReport: newReport(files, warn, compiled.removed, nil)}, nil
}

// RemoveUpload removes an uploaded template that is not selected. Its
// versions stay in the history; uploading the same name again revives it.
func (s *Store) RemoveUpload(ctx context.Context, kind, name string, expected int64, a Actor) (int64, error) {
	if !allowed(a) {
		return 0, ErrForbidden
	}
	if !validKind(kind) || !ValidUploadName(name) || expected < 0 {
		return 0, ErrInvalid
	}
	if s.pool == nil {
		return 0, ErrNotFound
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return 0, e
	}
	defer tx.Rollback(ctx)
	revision, selected, e := lockHead(ctx, tx, kind, expected)
	if e != nil {
		return 0, e
	}
	st, e := uploadState(ctx, tx, kind, name)
	if e != nil {
		return 0, e
	}
	if !st.active {
		return 0, ErrNotFound
	}
	if selected == name {
		return 0, refuse("这个模板正在使用，请先切换到其他模板再删除")
	}
	if e = uploadEvent(ctx, tx, st.id, "remove", st.newest, 0, a); e != nil {
		return 0, e
	}
	next := revision + 1
	if e = bumpHead(ctx, tx, kind, next); e != nil {
		return 0, e
	}
	if e = uploadAudit(ctx, tx, a, kind, "remove-template", map[string]any{"type": kind, "name": name, "revision": next, "version": st.newest}); e != nil {
		return 0, e
	}
	if e = tx.Commit(ctx); e != nil {
		return 0, e
	}
	return next, nil
}

// UploadVersions lists the newest 50 versions of an uploaded template with
// the state of their notice: pending (not yet attempted), accepted, or
// unknown (attempted without a recorded answer).
func (s *Store) UploadVersions(ctx context.Context, kind, name string, a Actor) ([]UploadVersionItem, error) {
	if !allowed(a) {
		return nil, ErrForbidden
	}
	if !validKind(kind) || !ValidUploadName(name) {
		return nil, ErrInvalid
	}
	if s.pool == nil {
		return nil, ErrNotFound
	}
	st, e := uploadState(ctx, s.pool, kind, name)
	if e != nil {
		return nil, e
	}
	if !st.active {
		return nil, ErrNotFound
	}
	rows, e := s.pool.Query(ctx, `SELECT v.version,COALESCE(v.restored_from,0),v.title,v.package_version,v.author,v.files,v.size_bytes,v.created_at,v.actor_name,
 CASE WHEN r.state IS NOT NULL THEN r.state WHEN n.event_id IS NOT NULL AND n.claimed_at<clock_timestamp()-interval '1 minute' THEN 'unknown' ELSE 'pending' END
 FROM page_template_upload_versions v
 LEFT JOIN page_template_upload_events e ON e.upload_id=v.upload_id AND e.version=v.version AND e.operation IN('upload','restore')
 LEFT JOIN page_template_notices n ON n.event_id=e.id LEFT JOIN page_template_notice_results r ON r.event_id=e.id
 WHERE v.upload_id=$1 ORDER BY v.version DESC LIMIT 50`, st.id)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	items := []UploadVersionItem{}
	for rows.Next() {
		var it UploadVersionItem
		var at time.Time
		if e = rows.Scan(&it.Version, &it.RestoredFrom, &it.Title, &it.PackageVersion, &it.Author, &it.Files, &it.Size, &at, &it.Actor, &it.Notice); e != nil {
			return nil, e
		}
		it.Operation = "upload"
		if it.RestoredFrom > 0 {
			it.Operation = "restore"
		}
		it.CreatedAt = at.UTC().Format(time.RFC3339)
		items = append(items, it)
	}
	return items, rows.Err()
}

// UploadPackage is a version of an uploaded template as a zip file.
func (s *Store) UploadPackage(ctx context.Context, kind, name string, version int, a Actor) ([]byte, error) {
	if !allowed(a) {
		return nil, ErrForbidden
	}
	if !validKind(kind) || !ValidUploadName(name) || version < 1 {
		return nil, ErrInvalid
	}
	if s.pool == nil {
		return nil, ErrNotFound
	}
	st, e := uploadState(ctx, s.pool, kind, name)
	if e != nil {
		return nil, e
	}
	if !st.active {
		return nil, ErrNotFound
	}
	id, _, e := versionID(ctx, s.pool, st.id, version)
	if e != nil {
		return nil, e
	}
	files, e := loadFiles(ctx, s.pool, id)
	if e != nil {
		return nil, e
	}
	return zipFiles(files)
}

// UploadPreview is a version of an uploaded template for the console preview
// (0 is the newest).
func (s *Store) UploadPreview(ctx context.Context, kind, name string, version int, a Actor) (*UploadedTemplate, error) {
	if !allowed(a) {
		return nil, ErrForbidden
	}
	if !validKind(kind) || !ValidUploadName(name) || version < 0 {
		return nil, ErrInvalid
	}
	if s.pool == nil {
		return nil, ErrNotFound
	}
	st, e := uploadState(ctx, s.pool, kind, name)
	if e != nil {
		return nil, e
	}
	if !st.active || version > st.newest {
		return nil, ErrNotFound
	}
	if version == 0 {
		version = st.newest
	}
	var id int64
	var key, title string
	e = s.pool.QueryRow(ctx, `SELECT id,file_key,title FROM page_template_upload_versions WHERE upload_id=$1 AND version=$2`, st.id, version).Scan(&id, &key, &title)
	if errors.Is(e, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if e != nil {
		return nil, e
	}
	c, e := compiledVersion(ctx, s.pool, kind, key, id)
	if e != nil {
		return nil, e
	}
	return &UploadedTemplate{Name: name, Title: title, Version: version, Key: key, compiled: c}, nil
}

// compiledVersion compiles a stored version, or takes it from the cache.
func compiledVersion(ctx context.Context, q querier, kind, key string, id int64) (*compiledTemplate, error) {
	if c := cachedCompiled(key); c != nil && c.kind == kind {
		return c, nil
	}
	rows, e := q.Query(ctx, `SELECT path FROM page_template_upload_files WHERE version_id=$1`, id)
	if e != nil {
		return nil, e
	}
	paths := map[string]bool{}
	for rows.Next() {
		var p string
		if e = rows.Scan(&p); e != nil {
			rows.Close()
			return nil, e
		}
		paths[p] = true
	}
	rows.Close()
	if e = rows.Err(); e != nil {
		return nil, e
	}
	var digest string
	var index []byte
	if e = q.QueryRow(ctx, `SELECT f.digest,b.content FROM page_template_upload_files f JOIN page_template_blobs b ON b.digest=f.digest WHERE f.version_id=$1 AND f.path='index.html'`, id).Scan(&digest, &index); e != nil {
		return nil, ErrInvalid
	}
	if digestHex(index) != digest || len(index) > maxPackageIndex || !textFile(index) {
		return nil, ErrInvalid
	}
	c, e := compilePackage(kind, key, map[string][]byte{"index.html": index}, paths, false, &warnList{})
	if e != nil {
		return nil, ErrInvalid
	}
	cacheCompiled(c)
	return c, nil
}

// resolveUpload loads the version that an uploaded template shows; a reason
// is returned when the page must fall back to the default template.
func resolveUpload(ctx context.Context, q querier, kind, name string) (*UploadedTemplate, string) {
	var id int64
	var version int
	var key, title string
	e := q.QueryRow(ctx, `SELECT v.id,v.version,v.file_key,v.title FROM page_template_uploads u JOIN LATERAL (SELECT id,version,file_key,title FROM page_template_upload_versions v WHERE v.upload_id=u.id ORDER BY v.version DESC LIMIT 1) v ON true WHERE u.kind=$1 AND u.name=$2 AND `+latestOperation+`<>'remove'`, kind, name).Scan(&id, &version, &key, &title)
	if errors.Is(e, pgx.ErrNoRows) {
		return nil, "upload_unavailable"
	}
	if e != nil {
		return nil, "store_unavailable"
	}
	c, e := compiledVersion(ctx, q, kind, key, id)
	if errors.Is(e, ErrInvalid) {
		return nil, "invalid_upload"
	}
	if e != nil {
		return nil, "store_unavailable"
	}
	return &UploadedTemplate{Name: name, Title: title, Version: version, Key: key, compiled: c}, ""
}

// listUploads are the uploaded templates of kind that are not removed, the
// newest first, as console list items.
func listUploads(ctx context.Context, q querier, kind string) ([]Template, error) {
	rows, e := q.Query(ctx, `SELECT u.name,v.version,v.title,v.author,v.description,v.package_version,v.file_key,v.files,v.size_bytes,v.created_at,v.actor_name,
 COALESCE((SELECT f.path FROM page_template_upload_files f WHERE f.version_id=v.id AND f.path IN('preview.png','preview.jpg','preview.jpeg','preview.webp') ORDER BY f.path LIMIT 1),'')
 FROM page_template_uploads u JOIN LATERAL (SELECT id,version,title,author,description,package_version,file_key,files,size_bytes,created_at,actor_name FROM page_template_upload_versions v WHERE v.upload_id=u.id ORDER BY v.version DESC LIMIT 1) v ON true
 WHERE u.kind=$1 AND `+latestOperation+`<>'remove' ORDER BY v.created_at DESC,u.id DESC LIMIT 200`, kind)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var out []Template
	for rows.Next() {
		t := Template{Type: kind, HasConfig: true, Source: "upload"}
		var key, picture string
		var at time.Time
		if e = rows.Scan(&t.Name, &t.UploadVersion, &t.Title, &t.Author, &t.Description, &t.Version, &key, &t.Files, &t.Size, &at, &t.UpdatedBy, &picture); e != nil {
			return nil, e
		}
		t.PreviewURL = "/console/v1/admin/templates/" + kind + "/" + t.Name + "/preview"
		if picture != "" {
			t.PictureURL = fileURL(key, picture)
		}
		t.UpdatedAt = at.UTC().Format(time.RFC3339)
		out = append(out, t)
	}
	return out, rows.Err()
}

// PublicFile is a file of an uploaded version that is not removed.
func (s *Store) PublicFile(ctx context.Context, key, p string) (TemplateFile, error) {
	mime, ok := FileMIME(p)
	if !ok || !uploadKeyPattern.MatchString(key) || s.pool == nil {
		return TemplateFile{}, ErrNotFound
	}
	segments := strings.Split(p, "/")
	if len(p) > 200 || len(segments) > 8 {
		return TemplateFile{}, ErrNotFound
	}
	for _, seg := range segments {
		if !packageSegment.MatchString(seg) {
			return TemplateFile{}, ErrNotFound
		}
	}
	f := TemplateFile{MIME: mime}
	e := s.pool.QueryRow(ctx, `SELECT f.digest,b.content FROM page_template_upload_versions v JOIN page_template_uploads u ON u.id=v.upload_id JOIN page_template_upload_files f ON f.version_id=v.id AND f.path=$2 JOIN page_template_blobs b ON b.digest=f.digest WHERE v.file_key=$1 AND `+latestOperation+`<>'remove'`, key, p).Scan(&f.Digest, &f.Content)
	if errors.Is(e, pgx.ErrNoRows) {
		return TemplateFile{}, ErrNotFound
	}
	if e != nil {
		return TemplateFile{}, e
	}
	if digestHex(f.Content) != f.Digest {
		return TemplateFile{}, ErrNotFound
	}
	return f, nil
}

//go:embed samples
var sampleFiles embed.FS

// SamplePackage is the starter package of kind as a zip file.
func SamplePackage(kind string) ([]byte, error) {
	if !validKind(kind) {
		return nil, ErrInvalid
	}
	files := map[string][]byte{}
	root := "samples/" + kind
	e := fs.WalkDir(sampleFiles, root, func(p string, d fs.DirEntry, e error) error {
		if e != nil || d.IsDir() {
			return e
		}
		b, e := sampleFiles.ReadFile(p)
		if e != nil {
			return e
		}
		files[strings.TrimPrefix(p, root+"/")] = b
		return nil
	})
	if e != nil {
		return nil, e
	}
	return zipFiles(files)
}
