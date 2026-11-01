package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"goepay/internal/identity"
	"goepay/internal/payment"
)

func weixinApplicationError(w http.ResponseWriter, e error) {
	switch {
	case errors.Is(e, identity.ErrAdminSession), errors.Is(e, identity.ErrFundsAuthentication):
		response(w, 403, -1, "当前账号无权修改应用资料", nil)
	case errors.Is(e, payment.ErrNotFound):
		response(w, 404, -1, "应用或原请求不存在", nil)
	case errors.Is(e, payment.ErrConflict):
		response(w, 409, -1, "应用或授权范围已变更，请重新读取；未知提交请只查询原请求", nil)
	case errors.Is(e, payment.ErrInvalid), errors.Is(e, payment.ErrState):
		response(w, 422, -1, "应用资料或授权范围无效", nil)
	default:
		response(w, 503, -1, "应用资料暂不可用，请只查询原请求结果", nil)
	}
}

func (s *Server) weixinApplicationConsole(w http.ResponseWriter, r *http.Request, role string, session identity.Session, parts []string, token string) bool {
	if len(parts) < 2 || parts[1] != "weixin-applications" {
		return false
	}
	identityHeaders(w)
	if role != "admin" || session.Issuer != nil || s.Store == nil || s.Pool == nil {
		response(w, 403, -1, "需要本人管理员会话", nil)
		return true
	}
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		response(w, 400, -1, "此接口不接受查询参数", nil)
		return true
	}
	actor := payment.ParentChannelActor{User: session.User, Token: token, CSRF: session.CSRF, Delegated: session.Issuer != nil}
	tx, e := s.Pool.Begin(r.Context())
	if e != nil {
		weixinApplicationError(w, e)
		return true
	}
	e = identity.CheckOriginalAdminSessionTx(r.Context(), tx, session.User, token, false)
	canWrite := false
	if e == nil {
		canWrite = s.Store.CheckParentChannelActorTx(r.Context(), tx, actor) == nil
	}
	_ = tx.Rollback(r.Context())
	if e != nil {
		weixinApplicationError(w, e)
		return true
	}
	if r.Method != "GET" && !canWrite {
		response(w, 403, -1, "仅本人超级管理员可修改应用资料", nil)
		return true
	}
	if len(parts) == 3 && parts[2] == "capabilities" && r.Method == "GET" {
		sources := []weixinApplicationSource{}
		if canWrite {
			_, sources, e = s.weixinApplicationScope()
			if e != nil {
				weixinApplicationError(w, e)
				return true
			}
		}
		response(w, 200, 0, "", map[string]any{"can_write": canWrite, "sources": sources})
		return true
	}
	if len(parts) == 4 && parts[2] == "requests" && r.Method == "GET" {
		out, e := s.Store.WeixinApplicationReceipt(r.Context(), actor, parts[3])
		if e != nil {
			weixinApplicationError(w, e)
		} else {
			response(w, 200, 0, "", out)
		}
		return true
	}
	if r.Method == "GET" && len(parts) <= 3 {
		items, invalid, e := s.Store.WeixinApplicationDirectory(r.Context(), actor)
		if e != nil {
			weixinApplicationError(w, e)
			return true
		}
		configurationErrors := []map[string]string{}
		for _, id := range invalid {
			configurationErrors = append(configurationErrors, map[string]string{"id": strconv.FormatInt(id, 10), "message": "应用配置损坏，请检查原记录"})
		}
		scope, _, scopeErr := s.weixinApplicationScope()
		if scopeErr != nil {
			weixinApplicationError(w, scopeErr)
			return true
		}
		for i := range items {
			item := &items[i]
			if item.Archived {
				continue
			}
			reason := ""
			if scope.ReservedIDs[item.ID] {
				reason = "应用编号与导入资料冲突，请归档此受管记录后重新选择"
			} else if item.ExecutionReady {
				allowed := false
				for _, text := range item.MerchantIDs {
					mid, err := strconv.ParseInt(text, 10, 64)
					if err == nil && scope.Allows != nil && scope.Allows(mid, item.SourceChannelID) {
						allowed = true
						break
					}
				}
				if scope.SourceFingerprints[item.SourceChannelID] == "" || !allowed {
					reason = "当前来源或允许商户范围不可用，请重新配置关联"
				}
			}
			if reason != "" {
				item.ExecutionReady = false
				configurationErrors = append(configurationErrors, map[string]string{"id": strconv.FormatInt(item.ID, 10), "message": reason})
			}
		}
		if len(parts) == 3 {
			id, ok := positiveFundsID(parts[2])
			if !ok {
				response(w, 400, -1, "应用编号无效", nil)
				return true
			}
			for _, item := range items {
				if item.ID == id {
					response(w, 200, 0, "", item)
					return true
				}
			}
			for _, bad := range invalid {
				if bad == id {
					response(w, 409, -1, "此应用当前配置损坏，请检查原记录", nil)
					return true
				}
			}
			response(w, 404, -1, "应用不存在", nil)
			return true
		}
		items = append(items, s.weixinApplicationRoot().WeixinImportedApplicationViews...)
		rows, err := s.Pool.Query(r.Context(), `SELECT id,COALESCE(body->>'name',''),COALESCE(body->>'app_id',''),COALESCE(body->>'kind','') FROM backoffice_records WHERE resource='weixin-apps' ORDER BY id`)
		if err != nil {
			weixinApplicationError(w, err)
			return true
		}
		for rows.Next() {
			var item payment.WeixinApplicationView
			if err = rows.Scan(&item.ID, &item.Name, &item.AppID, &item.Kind); err != nil {
				break
			}
			item.Origin = "legacy_metadata"
			item.ReadOnly = true
			item.Draft = true
			item.MerchantIDs = []string{}
			items = append(items, item)
		}
		rows.Close()
		if err == nil {
			err = rows.Err()
		}
		if err != nil {
			weixinApplicationError(w, err)
			return true
		}
		response(w, 200, 0, "", map[string]any{"items": items, "total": len(items), "can_write": canWrite, "configuration_errors": configurationErrors})
		return true
	}
	archive := len(parts) == 4 && parts[3] == "archive" && r.Method == "POST"
	id := int64(0)
	if len(parts) == 3 || archive {
		var ok bool
		id, ok = positiveFundsID(parts[2])
		if !ok {
			response(w, 400, -1, "应用编号无效", nil)
			return true
		}
	}
	if !(len(parts) == 2 && r.Method == "POST" || len(parts) == 3 && r.Method == "PATCH" || archive) {
		response(w, 405, -1, "请求方法不支持", nil)
		return true
	}
	in, ok := readWeixinApplicationInput(w, r, id != 0, archive)
	if !ok {
		return true
	}
	scope, _, e := s.weixinApplicationScope()
	if e != nil {
		weixinApplicationError(w, e)
		return true
	}
	out, e := s.Store.MutateWeixinApplication(r.Context(), actor, id, in, scope, archive)
	if e != nil {
		weixinApplicationError(w, e)
	} else {
		status := 200
		if id == 0 {
			status = 201
		}
		response(w, status, 0, "", out)
	}
	return true
}

func readWeixinApplicationInput(w http.ResponseWriter, r *http.Request, editing, archive bool) (payment.WeixinApplicationInput, bool) {
	var in payment.WeixinApplicationInput
	fail := func() (payment.WeixinApplicationInput, bool) {
		response(w, 400, -1, "应用配置格式无效", nil)
		return in, false
	}
	if strings.Split(r.Header.Get("Content-Type"), ";")[0] != "application/json" {
		return fail()
	}
	raw, e := io.ReadAll(http.MaxBytesReader(w, r.Body, 65536))
	if e != nil {
		return fail()
	}
	fields, e := parentExactObject(raw)
	if e != nil {
		return fail()
	}
	allowed := map[string]bool{"request_id": true, "kind": !archive, "name": !archive, "app_id": !archive, "appsecret": !archive, "enabled": !archive, "source_channel_id": !archive, "merchant_ids": !archive, "path": !archive, "expected_source_fingerprint": !archive, "expected_revision": editing, "expected_fingerprint": editing}
	for key := range fields {
		if !allowed[key] {
			return fail()
		}
	}
	decode := func(key string, target any) bool {
		return fields[key] != nil && string(fields[key]) != "null" && json.Unmarshal(fields[key], target) == nil
	}
	if !decode("request_id", &in.RequestID) || !identityHex(in.RequestID) {
		return fail()
	}
	if editing {
		var revision string
		if !decode("expected_revision", &revision) || !decode("expected_fingerprint", &in.ExpectedFingerprint) || !identityHex(in.ExpectedFingerprint) {
			return fail()
		}
		in.ExpectedRevision, e = strconv.ParseInt(revision, 10, 64)
		if e != nil || in.ExpectedRevision <= 0 || strconv.FormatInt(in.ExpectedRevision, 10) != revision {
			return fail()
		}
	}
	if archive {
		if len(fields) != 3 {
			return fail()
		}
		return in, true
	}
	var source string
	var merchants []string
	if !decode("kind", &in.Kind) || !decode("name", &in.Name) || !decode("app_id", &in.AppID) || !decode("enabled", &in.Enabled) || !decode("source_channel_id", &source) || !decode("merchant_ids", &merchants) || !decode("path", &in.Path) || !decode("expected_source_fingerprint", &in.ExpectedSourceFingerprint) {
		return fail()
	}
	if fields["appsecret"] != nil && !decode("appsecret", &in.AppSecret) {
		return fail()
	}
	in.SourceChannelID, e = strconv.ParseInt(source, 10, 64)
	if e != nil || in.SourceChannelID < 0 || strconv.FormatInt(in.SourceChannelID, 10) != source || len(merchants) > 1000 {
		return fail()
	}
	seen := map[int64]bool{}
	for _, value := range merchants {
		mid, ok := positiveFundsID(value)
		if !ok || seen[mid] {
			return fail()
		}
		seen[mid] = true
		in.MerchantIDs = append(in.MerchantIDs, mid)
	}
	sort.Slice(in.MerchantIDs, func(i, j int) bool { return in.MerchantIDs[i] < in.MerchantIDs[j] })
	return in, true
}
