package server

import (
	"net/http"
	"net/url"
	"strconv"

	"goepay/internal/identity"
	"goepay/internal/payment"
)

// Admin overview has its own GET-only endpoints. Existing merchant-scoped
// reads and financial endpoints never interpret a missing owner as all owners.
func (s *Server) channelSharingOverviewConsole(w http.ResponseWriter, r *http.Request, role string, session identity.Session, parts []string) {
	if role != "admin" {
		response(w, 403, -1, "administrator sharing overview required", nil)
		return
	}
	if r.Method != "GET" {
		w.Header().Set("Allow", "GET")
		response(w, 405, -1, "sharing overview is read-only", nil)
		return
	}
	rules := parts[2] == "rule-overview"
	statistics := !rules && len(parts) == 4 && parts[3] == "statistics"
	if len(parts) != 3 && !statistics {
		response(w, 404, -1, "sharing overview operation not found", nil)
		return
	}
	params, err := url.ParseQuery(r.URL.RawQuery)
	allowed := []string{"merchant_id", "channel_id"}
	if !statistics {
		allowed = append(allowed, "limit", "offset")
	}
	if rules {
		allowed = append(allowed, "rule_id", "receiver_id", "enabled")
	} else {
		allowed = append(allowed, "subchannel_id", "field", "keyword", "status", "rule_id", "from", "to")
		if !statistics {
			allowed = append(allowed, "format")
		}
	}
	invalid := func() { response(w, 400, -1, "invalid sharing overview query", nil) }
	if err != nil || !sharingConsoleQuery(r, allowed...) {
		invalid()
		return
	}
	id := func(key string) (int64, bool) {
		if !params.Has(key) {
			return 0, true
		}
		value, ok := identityPositiveID(params.Get(key))
		return value, ok
	}
	mid, valid := id("merchant_id")
	if !valid {
		invalid()
		return
	}
	channel, valid := id("channel_id")
	if !valid {
		invalid()
		return
	}
	actor := payment.TransferBatchActor{Role: "admin", Class: session.User.AdminClass, ID: session.User.ID, Name: session.User.Username}
	var ruleQuery payment.ChannelSharingRuleOverviewQuery
	var orderQuery payment.ChannelSharingOrderQuery
	if rules {
		ruleQuery = payment.ChannelSharingRuleOverviewQuery{MerchantID: mid, ChannelID: channel, Limit: 20}
		ruleQuery.RuleID, valid = id("rule_id")
		if !valid {
			invalid()
			return
		}
		ruleQuery.ReceiverID, valid = id("receiver_id")
		if !valid {
			invalid()
			return
		}
		for _, key := range []string{"limit", "offset"} {
			if !params.Has(key) {
				continue
			}
			n, e := strconv.Atoi(params.Get(key))
			if e != nil || strconv.Itoa(n) != params.Get(key) || n < 0 || (key == "limit" && (n < 1 || n > 100)) || (key == "offset" && n > 1_000_000) {
				invalid()
				return
			}
			if key == "limit" {
				ruleQuery.Limit = n
			} else {
				ruleQuery.Offset = n
			}
		}
		if params.Has("enabled") {
			value := params.Get("enabled")
			if value != "true" && value != "false" {
				invalid()
				return
			}
			flag := value == "true"
			ruleQuery.Enabled = &flag
		}
	} else {
		for _, key := range []string{"limit", "offset", "rule_id"} {
			if params.Has(key) && params.Get(key) == "" {
				invalid()
				return
			}
		}
		orderQuery, valid = sharingOrderQueryFromRequest(r, mid)
		if !valid {
			invalid()
			return
		}
		orderQuery.ChannelID = channel
		orderQuery.SubchannelID, valid = id("subchannel_id")
		if !valid {
			invalid()
			return
		}
		// Building the fixed query validates fields, exact money, states and dates
		// before touching the store. It never executes a statement or writes data.
		if _, _, valid = orderQuery.AdminChannelSharingOrderCSVQuery(actor); !valid || orderQuery.Offset > 1_000_000 {
			invalid()
			return
		}
		if params.Has("format") && params.Get("format") != "csv" {
			invalid()
			return
		}
	}
	if s.Store == nil || s.Pool == nil {
		response(w, 503, -1, "sharing overview unavailable", nil)
		return
	}
	if rules {
		out, e := s.Store.ListAdminChannelSharingRules(r.Context(), ruleQuery, actor)
		if e != nil {
			s.fail(w, e)
		} else {
			response(w, 200, 0, "", out)
		}
		return
	}
	if statistics {
		out, e := s.Store.AdminChannelSharingOrderStatistics(r.Context(), orderQuery, actor)
		if e != nil {
			s.fail(w, e)
		} else {
			response(w, 200, 0, "", out)
		}
		return
	}
	if params.Get("format") == "csv" {
		query, args, ok := orderQuery.AdminChannelSharingOrderCSVQuery(actor)
		if !ok {
			invalid()
			return
		}
		s.exportCSV(w, r, session, "channel-sharing-order-overview", query, args)
		return
	}
	out, e := s.Store.ListAdminChannelSharingOrders(r.Context(), orderQuery, actor)
	if e != nil {
		s.fail(w, e)
	} else {
		response(w, 200, 0, "", out)
	}
}
