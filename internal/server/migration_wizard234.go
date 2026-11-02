package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"

	"goepay/internal/identity"
	"goepay/internal/migration"
)

var errMigrationUpload234 = errors.New("invalid migration backup upload")

const migrationUploadBodyLimit234 = int64(64<<20) + (128 << 10)

func readMigrationDump234(r *http.Request) ([]byte, string, string, error) {
	const max = int64(64 << 20)
	if r.ContentLength > migrationUploadBodyLimit234 {
		return nil, "", "", errMigrationUpload234
	}
	r.Body = http.MaxBytesReader(nil, r.Body, migrationUploadBodyLimit234)
	m, e := r.MultipartReader()
	if e != nil {
		return nil, "", "", errMigrationUpload234
	}
	seen := map[string]bool{}
	var raw []byte
	var source, prefix string
	for {
		part, e := m.NextPart()
		if e == io.EOF {
			break
		}
		if e != nil {
			return nil, "", "", errMigrationUpload234
		}
		name := part.FormName()
		if seen[name] || (name != "source" && name != "prefix" && name != "backup") {
			return nil, "", "", errMigrationUpload234
		}
		seen[name] = true
		limit := int64(128)
		if name == "backup" {
			limit = max
			if part.FileName() == "" {
				return nil, "", "", errMigrationUpload234
			}
		} else if part.FileName() != "" {
			return nil, "", "", errMigrationUpload234
		}
		v, e := io.ReadAll(io.LimitReader(part, limit+1))
		if e != nil || int64(len(v)) > limit {
			return nil, "", "", errMigrationUpload234
		}
		if e = part.Close(); e != nil {
			return nil, "", "", errMigrationUpload234
		}
		switch name {
		case "source":
			source = string(v)
		case "prefix":
			prefix = string(v)
		case "backup":
			raw = v
		}
	}
	if len(raw) == 0 || (source != "epay_pro" && source != "epay_release") || len(prefix) > 59 {
		return nil, "", "", errMigrationUpload234
	}
	return raw, source, prefix, nil
}

// Migration uploads have their own bounded memory budget and never reach the
// filesystem or an SQL executor. Rejected concurrent requests do not read bodies.
var migrationUploadAdmission234 = make(chan struct{}, 1)
var migrationID234 = regexp.MustCompile(`^[a-f0-9]{32}$`)

func migrationError234(w http.ResponseWriter, e error) {
	status, msg := 503, "迁移结果尚未确认，请读取当前任务状态，不要重复提交"
	switch {
	case errors.Is(e, migration.ErrWizardCapacity234):
		status, msg = 429, "迁移暂存次数或空间达到上限，请联系管理员处理暂存后重试"
		w.Header().Set("Retry-After", "60")
	case errors.Is(e, migration.ErrWizardBusy234):
		status, msg = 409, "目标数据库正被其它操作占用，请暂停写入后重新准备迁移"
	case errors.Is(e, identity.ErrAdminSession):
		status, msg = 403, "请使用本人有效的超级管理员会话"
	case errors.Is(e, migration.ErrWizardInvalid234), errors.Is(e, migration.ErrValidation):
		status, msg = 422, "备份格式、确认信息或迁移参数不正确，请重新核对"
	case errors.Is(e, migration.ErrWizardTarget234):
		status, msg = 409, "目标数据库已有业务数据，不能覆盖迁移；请在全新安装中操作"
	case errors.Is(e, migration.ErrWizardConflict234), errors.Is(e, migration.ErrConflict):
		status, msg = 409, "迁移任务状态已变化，请读取当前状态，不要重复提交"
	}
	response(w, status, -1, msg, nil)
}

func (s *Server) migrationWizardConsole234(w http.ResponseWriter, r *http.Request, role string, session identity.Session, parts []string, token string) bool {
	if len(parts) < 3 || parts[1] != "migration" || parts[2] != "epay" {
		return false
	}
	identityHeaders(w)
	if role != "admin" || session.User.AdminClass != "super" || session.Issuer != nil {
		response(w, 403, -1, "只有本人超级管理员可以使用迁移向导", nil)
		return true
	}
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		response(w, 400, -1, "迁移接口不接受URL参数", nil)
		return true
	}
	// The native legacy importer uses the business master key. CheckoutKey is
	// purpose-derived and must never encrypt merchant credentials or history.
	service := s.MigrationWizard234
	if service == nil {
		migrationError234(w, migration.ErrWizardUnavailable234)
		return true
	}
	var e error
	a := migration.WizardActor234{User: session.User, Token: token, Delegated: session.Issuer != nil}
	var out any
	switch {
	case len(parts) == 3:
		if r.Method != "GET" {
			w.Header().Set("Allow", "GET")
			response(w, 405, -1, "method not allowed", nil)
			return true
		}
		out, e = service.Capabilities(r.Context(), a)
	case len(parts) == 4 && parts[3] == "prepare":
		if r.Method != "POST" {
			w.Header().Set("Allow", "POST")
			response(w, 405, -1, "method not allowed", nil)
			return true
		}
		select {
		case migrationUploadAdmission234 <- struct{}{}:
			defer func() { <-migrationUploadAdmission234 }()
		default:
			w.Header().Set("Retry-After", "5")
			response(w, 429, -1, "已有迁移备份正在处理，请稍后手动重试", nil)
			return true
		}
		var raw []byte
		var source, prefix string
		raw, source, prefix, e = readMigrationDump234(r)
		if e != nil {
			response(w, 422, -1, "请上传单个有效的SQL备份（最大64MiB），并选择原系统版本", nil)
			return true
		}
		out, e = service.Prepare(r.Context(), a, raw, source, prefix)
	case len(parts) == 4 && migrationID234.MatchString(parts[3]):
		if r.Method != "GET" {
			w.Header().Set("Allow", "GET")
			response(w, 405, -1, "method not allowed", nil)
			return true
		}
		out, e = service.Status(r.Context(), a, parts[3])
	case len(parts) == 5 && migrationID234.MatchString(parts[3]) && parts[4] == "apply":
		if r.Method != "POST" {
			w.Header().Set("Allow", "POST")
			response(w, 405, -1, "method not allowed", nil)
			return true
		}
		var fields map[string]json.RawMessage
		if !decodeFundsJSON(w, r, &fields) {
			return true
		}
		keys := []string{"expected_sha256", "source_frozen", "archive_acknowledged", "confirmation"}
		valid := len(fields) == len(keys)
		for _, k := range keys {
			v, ok := fields[k]
			if !ok || strings.TrimSpace(string(v)) == "null" {
				valid = false
			}
		}
		var in migration.ApplyInput234
		raw, _ := json.Marshal(fields)
		if !valid || json.Unmarshal(raw, &in) != nil {
			response(w, 400, -1, "请完整填写迁移确认信息", nil)
			return true
		}
		out, e = service.Apply(r.Context(), a, parts[3], in)
	default:
		response(w, 404, -1, "unknown migration operation", nil)
		return true
	}
	if e != nil {
		migrationError234(w, e)
	} else {
		response(w, 200, 0, "", out)
	}
	return true
}
