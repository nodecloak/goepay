package server

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"goepay/internal/channelconfig"
	"goepay/internal/identity"
	"goepay/internal/payment"
)

type scanJSOption202 struct {
	ID        string `json:"id"`
	Revision  string `json:"revision"`
	ChannelID string `json:"channel_id"`
	AppID     string `json:"app_id"`
}
type scanJSView202 struct {
	payment.ScanJSSettings202
	Available bool              `json:"available"`
	Profiles  []scanJSOption202 `json:"profiles"`
}

func scanJSBinding202(p channelconfig.ScanJSProfile202) payment.ScanJSBinding202 {
	return payment.ScanJSBinding202{ProfileID: p.Profile.ID, ProfileRevision: p.Profile.Revision, ProfileFingerprint: p.Profile.Fingerprint}
}
func validScanJSProfile202(id int64, p channelconfig.ScanJSProfile202) bool {
	v := p.Profile
	return id > 0 && v.ID == id && v.Revision > 0 && v.ChannelID > 0 && v.Provider == "wxpay" && v.Kind == "web" && v.Path == "" && v.AppID != "" && identityHex(v.Fingerprint) && p.Signer != nil
}
func scanJSViewFor202(raw payment.ScanJSSettings202, profiles map[int64]channelconfig.ScanJSProfile202, origin string) scanJSView202 {
	out := scanJSView202{ScanJSSettings202: raw, Profiles: []scanJSOption202{}}
	ids := []int64{}
	for id, p := range profiles {
		if validScanJSProfile202(id, p) {
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	bindings := []payment.ScanJSBinding202{}
	for _, id := range ids {
		p := profiles[id].Profile
		out.Profiles = append(out.Profiles, scanJSOption202{ID: strconv.FormatInt(id, 10), Revision: strconv.FormatInt(p.Revision, 10), ChannelID: strconv.FormatInt(p.ChannelID, 10), AppID: p.AppID})
		bindings = append(bindings, scanJSBinding202(profiles[id]))
	}
	// Confirmation includes the current catalogue, so a rotated encrypted source
	// cannot be selected using a read taken before the rotation.
	data, _ := json.Marshal([]any{raw.Fingerprint, bindings, origin})
	digest := sha256.Sum256(data)
	out.Fingerprint = hex.EncodeToString(digest[:])
	if p, ok := profiles[raw.Binding.ProfileID]; ok && raw.Enabled && validScanJSProfile202(raw.Binding.ProfileID, p) && scanJSBinding202(p) == raw.Binding {
		out.Available = true
	}
	return out
}
func scanJSError202(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, identity.ErrAdminSession), errors.Is(err, identity.ErrOriginalMerchantSession):
		response(w, 403, -1, "original owner session required", nil)
	case errors.Is(err, payment.ErrInvalid):
		response(w, 422, -1, "invalid native scan application selection", nil)
	case errors.Is(err, payment.ErrConflict):
		response(w, 409, -1, "native scan source changed; reload and explicitly confirm current selection", nil)
	default:
		response(w, 503, -1, "native scan configuration unavailable", nil)
	}
}
func (s *Server) scanJSSettingsConsole202(w http.ResponseWriter, r *http.Request, role string, session identity.Session, parts []string, token string) bool {
	if len(parts) != 3 || parts[1] != "settings" || parts[2] != "scanpay-js" {
		return false
	}
	identityHeaders(w)
	if role != "admin" || session.User.AdminClass != "super" || session.Issuer != nil {
		response(w, 403, -1, "original super administrator session required", nil)
		return true
	}
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		response(w, 400, -1, "query parameters not accepted", nil)
		return true
	}
	if r.Method != "GET" && r.Method != "PATCH" {
		w.Header().Set("Allow", "GET, PATCH")
		response(w, 405, -1, "method not allowed", nil)
		return true
	}
	if r.Method == "PATCH" && (session.CSRF == "" || !hmac.Equal([]byte(session.CSRF), []byte(r.Header.Get("X-CSRF-Token")))) {
		response(w, 403, -1, "invalid CSRF token", nil)
		return true
	}
	if s.Store == nil {
		response(w, 503, -1, "native scan settings unavailable", nil)
		return true
	}
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()
	a := payment.ParentChannelActor{User: session.User, Token: token, CSRF: session.CSRF, Delegated: session.Issuer != nil}
	raw, err := s.Store.ScanJSSettings202(ctx, a)
	if err != nil {
		scanJSError202(w, err)
		return true
	}
	profiles := s.ScanJSProfiles202
	view := scanJSViewFor202(raw, profiles, s.ScanJSSiteOrigin202)
	if r.Method == "GET" {
		response(w, 200, 0, "", view)
		return true
	}
	var in struct {
		Enabled         bool   `json:"enabled"`
		ProfileID       string `json:"profile_id"`
		ProfileRevision string `json:"profile_revision"`
		Expected        string `json:"expected_fingerprint"`
	}
	if !decodeBuyerFeeInput104(w, r, &in, "enabled", "profile_id", "profile_revision", "expected_fingerprint") {
		return true
	}
	if !identityHex(in.Expected) {
		response(w, 400, -1, "current native scan confirmation required", nil)
		return true
	}
	if !hmac.Equal([]byte(in.Expected), []byte(view.Fingerprint)) {
		scanJSError202(w, payment.ErrConflict)
		return true
	}
	var binding payment.ScanJSBinding202
	if in.Enabled {
		id, ok := positiveFundsID(in.ProfileID)
		revision, valid := positiveFundsID(in.ProfileRevision)
		p, exists := profiles[id]
		if !ok || !valid || !exists || !validScanJSProfile202(id, p) {
			scanJSError202(w, payment.ErrInvalid)
			return true
		}
		if revision != p.Profile.Revision {
			scanJSError202(w, payment.ErrConflict)
			return true
		}
		binding = scanJSBinding202(p)
	} else if in.ProfileID != "" || in.ProfileRevision != "" {
		scanJSError202(w, payment.ErrInvalid)
		return true
	}
	out, err := s.Store.UpdateScanJSSettings202(ctx, a, in.Enabled, binding, raw.Fingerprint)
	if err != nil {
		scanJSError202(w, err)
	} else {
		response(w, 200, 0, "", scanJSViewFor202(out, profiles, s.ScanJSSiteOrigin202))
	}
	return true
}

func scanJSPageURL202(origin, page string) bool {
	if len(page) < 1 || len(page) > 2048 {
		return false
	}
	for _, r := range page {
		if r <= 32 || r == 127 {
			return false
		}
	}
	base, err := url.Parse(origin)
	if err != nil || base.Scheme != "https" || base.Host == "" || base.User != nil || base.RawQuery != "" || base.ForceQuery || base.Fragment != "" || base.Opaque != "" || base.Path != "" && base.Path != "/" {
		return false
	}
	u, err := url.Parse(page)
	if err != nil || u.Scheme != base.Scheme || !strings.EqualFold(u.Host, base.Host) || u.User != nil || u.Fragment != "" || strings.Contains(page, "#") || u.Opaque != "" || u.Path != "/merchant/scanpay" || u.EscapedPath() != "/merchant/scanpay" {
		return false
	}
	decoded, err := url.QueryUnescape(u.RawQuery)
	if err != nil {
		return false
	}
	for _, r := range decoded {
		if r < 32 || r == 127 {
			return false
		}
	}
	return true
}
func (s *Server) scanJSRuntime202(ctx context.Context, mid int64, p channelconfig.ScanJSProfile202) bool {
	if !p.Profile.AllowsMerchant(mid) {
		return false
	}
	view, err := s.withParentRuntime(ctx, mid, "")
	if err != nil || view.ProviderFlow == nil {
		return false
	}
	allowed := false
	for _, id := range view.MerchantChannels[mid] {
		if id == p.Profile.ChannelID {
			allowed = true
		}
	}
	provider := view.ProviderFlow.Providers[p.Profile.ChannelID]
	return allowed && provider != nil && provider.ID() == "wxpay"
}
func (s *Server) merchantScanJS202(w http.ResponseWriter, r *http.Request, role string, session identity.Session, parts []string, token string) bool {
	if len(parts) != 3 || parts[1] != "scanpay" || parts[2] != "jsconfig" {
		return false
	}
	identityHeaders(w)
	if role != "merchant" || session.Issuer != nil {
		response(w, 403, -1, "original merchant session required", nil)
		return true
	}
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		response(w, 400, -1, "query parameters not accepted", nil)
		return true
	}
	if r.Method != "POST" {
		w.Header().Set("Allow", "POST")
		response(w, 405, -1, "method not allowed", nil)
		return true
	}
	if session.CSRF == "" || !hmac.Equal([]byte(session.CSRF), []byte(r.Header.Get("X-CSRF-Token"))) {
		response(w, 403, -1, "invalid CSRF token", nil)
		return true
	}
	var in struct {
		URL string `json:"url"`
	}
	if !decodeBuyerFeeInput104(w, r, &in, "url") {
		return true
	}
	if s.Store == nil || s.Pool == nil || s.Sessions == nil {
		response(w, 503, -1, "native scan configuration unavailable", nil)
		return true
	}
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	a := payment.MerchantRSAActor{User: session.User, Token: token, CSRF: session.CSRF}
	state, err := s.Store.MerchantScanJSSettings202(ctx, a)
	if err != nil {
		scanJSError202(w, err)
		return true
	}
	p, ok := s.ScanJSProfiles202[state.Binding.ProfileID]
	if !state.Enabled || !ok || !validScanJSProfile202(state.Binding.ProfileID, p) || scanJSBinding202(p) != state.Binding {
		response(w, 503, -1, "native scan application requires explicit current authorization", nil)
		return true
	}
	if !scanJSPageURL202(s.ScanJSSiteOrigin202, in.URL) {
		response(w, 422, -1, "native scan URL must be the configured cashier page", nil)
		return true
	}
	open, enabled, err := s.Store.MerchantScanAvailability106(ctx, a)
	if err != nil {
		scanJSError202(w, err)
		return true
	}
	if !open || !enabled || !s.scanJSRuntime202(ctx, a.User.ID, p) {
		response(w, 503, -1, "native scan application unavailable for this merchant", nil)
		return true
	}
	config, err := p.Signer.Sign(ctx, in.URL)
	if err != nil {
		response(w, 503, -1, "native scan signature could not be confirmed", nil)
		return true
	}
	if config.AppID != p.Profile.AppID || config.Debug || len(config.JSAPIList) != 1 || config.JSAPIList[0] != "scanQRCode" {
		response(w, 503, -1, "native scan signature profile could not be confirmed", nil)
		return true
	}
	current, err := s.Store.MerchantScanJSSettings202(ctx, a)
	if err != nil {
		scanJSError202(w, err)
		return true
	}
	open, enabled, err = s.Store.MerchantScanAvailability106(ctx, a)
	if err != nil {
		scanJSError202(w, err)
		return true
	}
	if !hmac.Equal([]byte(current.Fingerprint), []byte(state.Fingerprint)) || !open || !enabled || !s.scanJSRuntime202(ctx, a.User.ID, p) {
		response(w, 503, -1, "native scan authority changed; reload before scanning", nil)
		return true
	}
	response(w, 200, 0, "", config)
	return true
}
