package backoffice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"
)

// maxBlacklistBatchLines bounds a single bulk-add request, mirroring Pro's
// paste-many add dialog without letting one request create an unbounded number
// of rows in one transaction.
const maxBlacklistBatchLines = 500

// BlacklistBatchResult reports what a bulk add did: the IDs it created and how
// many lines it skipped because an enabled rule with the same kind+value is
// already stored (the management-page single create has no uniqueness, so a
// duplicate would otherwise pile up a second identical row).
type BlacklistBatchResult struct {
	Created   []string `json:"created"`
	Skipped   int      `json:"skipped"`
	ExpiresAt string   `json:"expires_at"`
}

// BatchCreateBlacklist adds many 黑名单 entries from a multi-line textarea in one
// all-or-nothing transaction, the epay_pro way: 拉黑类型 (kind), 拉黑内容 (one entry
// per line, batched), 有效期 天 (days; 0 = 永久 → empty expires_at, else now+days)
// and 备注 (reason). Every line is validated exactly like the single resource
// create (selectField kind, text(value,253), IP/CIDR or lowercased domain via
// validateSpecific); a single bad line rejects the whole request and names it.
// Lines are trimmed, blanks skipped, duplicates within the request collapsed,
// and a line already present as an enabled rule of the same kind+value skipped
// (counted, not duplicated). Each created row is audited like the single create.
// The same admin role / admin-class gate as the blacklist resource create and
// the order-level 拉黑 path applies; CSRF is enforced by the console middleware.
func (s *Service) BatchCreateBlacklist(ctx context.Context, p Principal, kind string, lines []string, days int, reason string) (BlacklistBatchResult, error) {
	var out BlacklistBatchResult
	// Same gate the resource create uses: blacklist is an admin-only resource
	// (not tenant), so a merchant principal is refused; keep the explicit
	// admin-class floor the order-level 拉黑 path uses for this risk write.
	if p.Role != "admin" || p.SubjectID < 1 || (p.AdminClass != "super" && p.AdminClass != "operator") {
		return out, ErrForbidden
	}
	if days < 0 || days > 36500 {
		return out, ErrInvalid
	}
	reason = strings.TrimSpace(reason)
	if len(reason) > 1000 || !validText(reason) {
		return out, ErrInvalid
	}
	// Split, trim, drop blanks, collapse within-request duplicates while keeping
	// the submitted order so the error line number matches what the admin sees.
	seen := map[string]bool{}
	type entry struct {
		value string
		line  int
	}
	var pending []entry
	for i, raw := range lines {
		value := strings.TrimSpace(raw)
		if value == "" {
			continue
		}
		key := kind + "\x00" + strings.ToLower(value)
		if seen[key] {
			continue
		}
		seen[key] = true
		pending = append(pending, entry{value: value, line: i + 1})
	}
	if len(pending) == 0 {
		return out, ErrInvalid
	}
	if len(pending) > maxBlacklistBatchLines {
		return out, fmt.Errorf("%w: 一次最多添加 %d 条", ErrInvalid, maxBlacklistBatchLines)
	}

	expiry := ""
	if days > 0 {
		expiry = time.Now().UTC().Add(time.Duration(days) * 24 * time.Hour).Format(time.RFC3339)
	}
	out.ExpiresAt = expiry

	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(ctx)

	for _, it := range pending {
		fields := map[string]any{"kind": kind, "value": it.value, "reason": reason, "enabled": true, "expires_at": expiry}
		// value length and type coherence exactly as the single create's
		// validate() + validateSpecific("blacklist") would enforce.
		if len(it.value) > 253 || !validText(it.value) {
			return out, fmt.Errorf("%w: 第 %d 行内容无效", ErrInvalid, it.line)
		}
		if !validBlacklistKind(kind) {
			return out, ErrInvalid
		}
		if e = validateSpecific("blacklist", fields); e != nil {
			return out, fmt.Errorf("%w: 第 %d 行内容无效", ErrInvalid, it.line)
		}
		// validateSpecific lowercases a domain value in place; reflect that in the
		// stored row and the skip lookup.
		storedValue := fmt.Sprint(fields["value"])
		var exists bool
		if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM backoffice_records WHERE resource='blacklist' AND body->>'kind'=$1 AND body->>'value'=$2 AND body->>'enabled'='true')`, kind, storedValue).Scan(&exists); e != nil {
			return out, businessError(e)
		}
		if exists {
			out.Skipped++
			continue
		}
		raw, e := json.Marshal(fields)
		if e != nil {
			return out, e
		}
		var id int64
		if e = tx.QueryRow(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('blacklist',$1::jsonb) RETURNING id`, string(raw)).Scan(&id); e != nil {
			return out, businessError(e)
		}
		if e = audit(ctx, tx, p, "blacklist", id, "create", fields); e != nil {
			return out, e
		}
		out.Created = append(out.Created, fmt.Sprint(id))
	}
	if len(out.Created) == 0 {
		// Nothing new to add (every line already present): surface a conflict so
		// the admin is told rather than seeing an empty success.
		return out, ErrConflict
	}
	return out, tx.Commit(ctx)
}

// validText mirrors the single create's implicit guard: a stored string must be
// valid UTF-8 and carry no NUL or newline (the batch value is already a single
// line, but a pasted entry can still smuggle a control character).
func validText(s string) bool {
	return utf8.ValidString(s) && !strings.ContainsAny(s, "\x00\r\n")
}

func validBlacklistKind(kind string) bool {
	return kind == "ip" || kind == "domain" || kind == "buyer"
}

// serveBatchCreateBlacklist handles POST /resources/blacklist/batch-create with
// {"kind","lines"|"content","days","remark"|"reason"}. Pro's 添加 dialog posts a
// textarea; both a "lines" array and a newline-joined "content" string are
// accepted, and either 备注 field name. A validation failure names the first bad
// line; success returns the created IDs and how many duplicate lines were
// skipped.
func (s *Service) serveBatchCreateBlacklist(w http.ResponseWriter, r *http.Request, p Principal, resource string) {
	if resource != "blacklist" {
		write(w, 404, nil, "not found")
		return
	}
	var in struct {
		Kind    string   `json:"kind"`
		Lines   []string `json:"lines"`
		Content string   `json:"content"`
		Days    *int     `json:"days"`
		Remark  string   `json:"remark"`
		Reason  string   `json:"reason"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if e := dec.Decode(&in); e != nil {
		write(w, 400, nil, "invalid JSON")
		return
	}
	lines := in.Lines
	if len(lines) == 0 && in.Content != "" {
		lines = splitBlacklistLines(in.Content)
	}
	days := 0
	if in.Days != nil {
		days = *in.Days
	}
	reason := in.Remark
	if reason == "" {
		reason = in.Reason
	}
	out, e := s.BatchCreateBlacklist(r.Context(), p, in.Kind, lines, days, reason)
	if e != nil {
		// Surface a line-specific invalid message when BatchCreateBlacklist built
		// one (e.g. "第 3 行内容无效"), so the admin sees which pasted line failed.
		if msg := invalidDetail(e); msg != "" {
			write(w, 422, nil, msg)
			return
		}
		writeError(w, e)
		return
	}
	write(w, 201, out, "")
}

// splitBlacklistLines breaks a pasted textarea into entries on any newline
// combination, matching Pro's content.split(/[\r\n]+/).
func splitBlacklistLines(content string) []string {
	return strings.FieldsFunc(content, func(r rune) bool { return r == '\n' || r == '\r' })
}

// invalidDetail returns a wrapped ErrInvalid's own message when it carries more
// than the sentinel text (e.g. "第 3 行内容无效"), else "".
func invalidDetail(e error) string {
	if !errors.Is(e, ErrInvalid) {
		return ""
	}
	base := ErrInvalid.Error()
	msg := e.Error()
	if msg == base || !strings.HasPrefix(msg, base+": ") {
		return ""
	}
	return strings.TrimPrefix(msg, base+": ")
}
