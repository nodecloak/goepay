package server

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	kycadapter "goepay/internal/adapters/kyc"
	"goepay/internal/identity"
	"goepay/internal/kyc"
)

func (s *Server) certification233Service() (*kyc.Service, error) {
	p := s.KYC233Provider
	if p == nil {
		p = kycadapter.New(nil)
	}
	var resolve kyc.ChannelResolver
	if s.ProviderFlow != nil {
		resolve = s.ProviderFlow.ResolveKYCChannel233
	}
	base := s.DocsAPIOrigin
	if s.KYC233BaseURL != "" {
		base = s.KYC233BaseURL
	}
	return kyc.New(s.Pool, s.CheckoutKey, p, resolve, base)
}
func exactKYC233Fields(fields map[string]json.RawMessage, keys ...string) bool {
	if len(fields) != len(keys) {
		return false
	}
	for _, k := range keys {
		v, ok := fields[k]
		if !ok || strings.TrimSpace(string(v)) == "null" {
			return false
		}
	}
	return true
}
func decodeKYC233(w http.ResponseWriter, r *http.Request, out any, keys ...string) bool {
	var fields map[string]json.RawMessage
	if !decodeFundsJSON(w, r, &fields) {
		return false
	}
	if !exactKYC233Fields(fields, keys...) {
		response(w, 400, -1, "请完整填写当前操作所需字段", nil)
		return false
	}
	raw, _ := json.Marshal(fields)
	if json.Unmarshal(raw, out) != nil {
		response(w, 400, -1, "字段类型不正确", nil)
		return false
	}
	return true
}
func kyc233Error(w http.ResponseWriter, e error) {
	status, msg := 503, "服务结果尚未确认，请读取当前状态；不要重复提交"
	switch {
	case errors.Is(e, kyc.ErrInvalid):
		status = 422
		msg = "请检查认证方式、必填凭据、证件资料和已验证的绑定手机"
	case errors.Is(e, kyc.ErrConflict), errors.Is(e, pgx.ErrNoRows):
		status = 409
		msg = "状态已变化或操作已使用，请重新读取当前状态后处理"
	case errors.Is(e, kyc.ErrUnavailable):
		status = 503
		msg = "当前服务未开启或配置不完整，请联系管理员"
	case errors.Is(e, kyc.ErrInsufficient):
		status = 422
		msg = "可用余额不足，认证成功前不会扣费，请充值后继续"
	case errors.Is(e, identity.ErrAdminSession), errors.Is(e, identity.ErrOriginalMerchantSession):
		status = 403
		msg = "请使用本人有效会话操作；管理员代登录不能提交证件或识别请求"
	}
	response(w, status, -1, msg, nil)
}
func (s *Server) certification233Console(w http.ResponseWriter, r *http.Request, role string, session identity.Session, parts []string, token string) bool {
	settings := len(parts) == 3 && parts[1] == "settings" && (parts[2] == "certification" || parts[2] == "ocr")
	merchant := len(parts) == 4 && parts[1] == "account" && parts[2] == "certificate"
	ocr := len(parts) == 3 && parts[1] == "account" && parts[2] == "ocr"
	if !settings && !merchant && !ocr {
		return false
	}
	identityHeaders(w)
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		response(w, 400, -1, "操作接口不接受URL参数", nil)
		return true
	}
	if session.Issuer != nil || settings && (role != "admin" || session.User.AdminClass != "super") || merchant && role != "merchant" || ocr && role == "admin" && session.User.AdminClass != "super" {
		response(w, 403, -1, "请使用有权限的本人会话操作", nil)
		return true
	}
	service, e := s.certification233Service()
	if e != nil {
		kyc233Error(w, e)
		return true
	}
	a := kyc.Actor{User: session.User, Token: token, CSRF: session.CSRF, Delegated: session.Issuer != nil}
	var out any
	if settings {
		switch r.Method {
		case "GET":
			out, e = service.Config(r.Context(), a)
		case "PUT":
			var fields map[string]json.RawMessage
			if !decodeFundsJSON(w, r, &fields) {
				return true
			}
			if !exactKYC233Fields(fields, "expected_revision", "cert_open", "cert_channel", "cert_corpopen", "cert_force", "cert_money", "ocr_type", "credentials") {
				response(w, 400, -1, "请完整提交当前配置", nil)
				return true
			}
			var creds map[string]json.RawMessage
			if json.Unmarshal(fields["credentials"], &creds) != nil || len(creds) != len(kyc.CredentialKeys()) {
				response(w, 400, -1, "请明确每项凭据的保留、替换或清除操作", nil)
				return true
			}
			for _, k := range kyc.CredentialKeys() {
				var item map[string]json.RawMessage
				if json.Unmarshal(creds[k], &item) != nil || !exactKYC233Fields(item, "action", "value") {
					response(w, 400, -1, "凭据操作字段不完整", nil)
					return true
				}
			}
			raw, _ := json.Marshal(fields)
			var in kyc.ConfigInput
			if json.Unmarshal(raw, &in) != nil {
				response(w, 400, -1, "配置字段类型不正确", nil)
				return true
			}
			out, e = service.UpdateConfig(r.Context(), a, in)
		default:
			w.Header().Set("Allow", "GET, PUT")
			response(w, 405, -1, "method not allowed", nil)
			return true
		}
	} else if ocr {
		if r.Method != "POST" {
			w.Header().Set("Allow", "POST")
			response(w, 405, -1, "method not allowed", nil)
			return true
		}
		select {
		case ocrAdmission233 <- struct{}{}:
			defer func() { <-ocrAdmission233 }()
		default:
			w.Header().Set("Retry-After", "5")
			response(w, 429, -1, "证照识别正在处理其它请求，请稍后手动重试", nil)
			return true
		}
		kind, id, expectedProvider, expectedRevision, raw, err := readOCR233(r)
		if err != nil {
			response(w, 422, -1, "请上传单张有效的JPEG或PNG图片（最大4MB），并选择证照类型", nil)
			return true
		}
		out, e = service.Recognize(r.Context(), a, id, kind, expectedProvider, expectedRevision, raw)
		if errors.Is(e, kyc.ErrInvalid) {
			response(w, 422, -1, "图片格式、大小或证照类型不符合当前OCR服务要求", nil)
			return true
		}
	} else {
		if parts[3] == "info" && r.Method == "GET" {
			out, e = service.Info(r.Context(), a)
		} else if r.Method == "POST" {
			switch parts[3] {
			case "submit":
				var in kyc.Submission
				if !decodeKYC233(w, r, &in, "expected_revision", "certtype", "certcardtype", "certname", "certno", "certcorpname", "certcorpno", "consent") {
					return true
				}
				out, e = service.Submit(r.Context(), a, in)
			case "geturl", "query", "reset":
				var in kyc.Operation
				if !decodeKYC233(w, r, &in, "attempt_id", "state", "auth_code") {
					return true
				}
				switch parts[3] {
				case "geturl":
					out, e = service.GetURL(r.Context(), a, in)
				case "query":
					out, e = service.Query(r.Context(), a, in)
				case "reset":
					out, e = service.Reset(r.Context(), a, in)
				}
			default:
				response(w, 404, -1, "unknown certification operation", nil)
				return true
			}
		} else {
			w.Header().Set("Allow", "GET, POST")
			response(w, 405, -1, "method not allowed", nil)
			return true
		}
	}
	if e != nil {
		kyc233Error(w, e)
	} else {
		response(w, 200, 0, "", out)
	}
	return true
}

// Keep uploads entirely in bounded memory: MultipartReader never creates
// temporary image files. Remote URLs and host paths are not accepted fields.
func readOCR233(r *http.Request) (kind, id, expectedProvider string, expectedRevision int64, image []byte, err error) {
	media, _, e := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if e != nil || media != "multipart/form-data" {
		return "", "", "", 0, nil, kyc.ErrInvalid
	}
	reader, e := r.MultipartReader()
	if e != nil {
		return "", "", "", 0, nil, kyc.ErrInvalid
	}
	seen := map[string]bool{}
	for {
		part, e := reader.NextPart()
		if e == io.EOF {
			break
		}
		if e != nil {
			return "", "", "", 0, nil, kyc.ErrInvalid
		}
		name := part.FormName()
		if seen[name] || (name != "image" && name != "document_type" && name != "operation_id" && name != "expected_provider" && name != "expected_revision") {
			part.Close()
			return "", "", "", 0, nil, kyc.ErrInvalid
		}
		seen[name] = true
		limit := int64(128)
		if name == "image" {
			limit = kyc.MaxImageBytes
		}
		data, e := io.ReadAll(io.LimitReader(part, limit+1))
		part.Close()
		if e != nil || int64(len(data)) > limit {
			return "", "", "", 0, nil, kyc.ErrInvalid
		}
		switch name {
		case "image":
			image = data
		case "document_type":
			kind = string(data)
		case "operation_id":
			id = string(data)
		case "expected_provider":
			expectedProvider = string(data)
		case "expected_revision":
			expectedRevision, e = strconv.ParseInt(string(data), 10, 64)
			if e != nil || expectedRevision < 0 || strconv.FormatInt(expectedRevision, 10) != string(data) {
				return "", "", "", 0, nil, kyc.ErrInvalid
			}
		}
	}
	if len(seen) != 5 {
		return "", "", "", 0, nil, kyc.ErrInvalid
	}
	return kind, id, expectedProvider, expectedRevision, image, nil
}
