package payment

import (
	"context"
	"crypto/hmac"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5"
	"goepay/internal/identity"
	"goepay/internal/riskpolicy"
	"sort"
	"strings"
	"time"
)

type CoreRiskCheckResult struct {
	ExternalNo     string    `json:"external_no"`
	PolicyRevision int64     `json:"policy_revision,string"`
	Scanned        int       `json:"scanned"`
	Actions        int       `json:"actions"`
	At             time.Time `json:"at"`
}
type coreRiskCheckOrder struct {
	id, merchant, channel, child int64
	multi                        bool
	created                      time.Time
	paid                         bool
	ips                          []string
	buyers                       []string
}
type coreRiskCheckStat struct {
	all, success []int64
	oldest       time.Time
}

func splitCoreRiskIDs(v string) []string { return strings.Split(v, ",") }
func (s *Store) RunCoreRiskCheck(ctx context.Context, external string) (CoreRiskCheckResult, error) {
	return s.runCoreRiskCheck(ctx, nil, external, "")
}
func (s *Store) RunCoreRiskCheckAsAdmin(ctx context.Context, a MerchantCredentialActor, external, expected string) (CoreRiskCheckResult, error) {
	return s.runCoreRiskCheck(ctx, &a, external, expected)
}
func (s *Store) runCoreRiskCheck(ctx context.Context, actor *MerchantCredentialActor, external, expected string) (CoreRiskCheckResult, error) {
	var out CoreRiskCheckResult
	if len(external) < 8 || len(external) > 96 {
		return out, ErrInvalid
	}
	for _, c := range external {
		if c != '-' && c != '_' && c != ':' && (c < '0' || c > '9') && (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') {
			return out, ErrInvalid
		}
	}
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if e != nil {
		return out, e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('goepay/core-risk-check',0))`); e != nil {
		return out, e
	}
	if actor != nil {
		if e = identity.CheckOriginalSuperAdminSessionTx(ctx, tx, actor.User, actor.SessionToken, actor.Delegated); e != nil {
			return out, e
		}
	}
	if e = checkCoreRiskExecutionGuard(ctx, tx); e != nil {
		return out, e
	}
	p, configured, e := s.coreRiskPolicyTx(ctx, tx, false)
	if e != nil {
		return out, e
	}
	if !configured {
		return out, ErrState
	}
	view, e := s.coreRiskView(p, true)
	if e != nil {
		return out, e
	}
	if actor != nil && !hmac.Equal([]byte(expected), []byte(view.Fingerprint)) {
		return out, ErrConflict
	}
	var raw, proof []byte
	e = tx.QueryRow(ctx, `SELECT facts,proof_hash FROM core_risk_check_runs WHERE external_no=$1`, external).Scan(&raw, &proof)
	if e == nil {
		if decodeCoreRiskFacts(raw, &out) != nil || out.ExternalNo != external {
			return out, ErrConflict
		}
		hash, e := s.referralDigest("risk102/check", out)
		if e != nil || !hmac.Equal(hash, proof) {
			return out, ErrConflict
		}
		var audit bool
		if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM backoffice_audit WHERE resource='core-risk-check' AND operation='complete' AND changes->>'external_no'=$1 AND changes->>'source_hash'=$2)`, external, hex.EncodeToString(hash)).Scan(&audit); e != nil || !audit {
			return out, ErrConflict
		}
		if actor != nil {
			if e = identity.CheckOriginalSuperAdminSessionTx(ctx, tx, actor.User, actor.SessionToken, actor.Delegated); e != nil {
				return out, e
			}
		}
		if e = checkCoreRiskExecutionGuard(ctx, tx); e != nil {
			return out, e
		}
		return out, tx.Commit(ctx)
	}
	if e != pgx.ErrNoRows {
		return out, e
	}
	var lost bool
	if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM backoffice_audit WHERE resource='core-risk-check' AND changes->>'external_no'=$1)`, external).Scan(&lost); e != nil {
		return out, e
	}
	if lost {
		return out, ErrConflict
	}
	now, e := coreRiskDBNow(ctx, tx)
	if e != nil {
		return out, e
	}
	out = CoreRiskCheckResult{ExternalNo: external, PolicyRevision: p.Revision, At: now}
	var observationUntil time.Time
	seconds := int64(0)
	for _, key := range []string{"check_payip_second", "check_payspeed_second", "check_sucrate_second", "check_channel_second"} {
		if v := riskpolicy.Integer(p.Settings, key); v > seconds {
			seconds = v
		}
	}
	if seconds > 0 {
		orders, e := s.coreRiskCheckOrders(ctx, tx, now.Add(-time.Duration(seconds)*time.Second))
		if e != nil {
			return out, e
		}
		out.Scanned = len(orders)
		if e = s.applyCoreRiskCheck(ctx, tx, p, orders, now, &out, &observationUntil); e != nil {
			return out, e
		}
	}
	proof, e = s.referralDigest("risk102/check", out)
	if e != nil {
		return out, e
	}
	raw, e = json.Marshal(out)
	if e != nil {
		return out, e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO core_risk_check_runs(external_no,facts,proof_hash)VALUES($1,$2::jsonb,$3)`, external, string(raw), proof); e != nil {
		return out, e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes)VALUES('system',0,'core-risk','core-risk-check',0,'complete',jsonb_build_object('external_no',$1::text,'source_hash',$2::text))`, external, hex.EncodeToString(proof)); e != nil {
		return out, e
	}
	if actor != nil {
		if e = identity.CheckOriginalSuperAdminSessionTx(ctx, tx, actor.User, actor.SessionToken, actor.Delegated); e != nil {
			return out, e
		}
	}
	if e = checkCoreRiskExecutionGuard(ctx, tx); e != nil {
		return out, e
	}
	if !observationUntil.IsZero() {
		final, e := coreRiskDBNow(ctx, tx)
		if e != nil {
			return out, e
		}
		if !final.Before(observationUntil) {
			return out, ErrState
		}
	}
	return out, tx.Commit(ctx)
}
func (s *Store) coreRiskCheckOrders(ctx context.Context, tx pgx.Tx, after time.Time) ([]coreRiskCheckOrder, error) {
	rows, e := tx.Query(ctx, `SELECT o.id,o.merchant_id,COALESCE(a.channel_id,route.channel_id,0),o.created_at,(o.paid_at IS NOT NULL AND EXISTS(SELECT 1 FROM verified_payment_receipts v WHERE v.order_id=o.id)),COALESCE(r.client_ip,''),COALESCE(r.buyer,''),o.type,EXISTS(SELECT 1 FROM core_risk_origins k WHERE k.order_id=o.id)FROM orders o LEFT JOIN provider_attempts a ON a.order_id=o.id LEFT JOIN order_reporting r ON r.order_id=o.id LEFT JOIN provider_route_assignments route ON route.merchant_id=o.merchant_id AND route.out_trade_no=o.out_trade_no WHERE o.created_at >= $1 ORDER BY o.trade_no,o.id LIMIT 100001`, after)
	if e != nil {
		return nil, e
	}
	type input struct {
		o               coreRiskCheckOrder
		ip, buyer, kind string
		origin          bool
	}
	inputs := []input{}
	for rows.Next() {
		var in input
		if e = rows.Scan(&in.o.id, &in.o.merchant, &in.o.channel, &in.o.created, &in.o.paid, &in.ip, &in.buyer, &in.kind, &in.origin); e != nil {
			rows.Close()
			return nil, e
		}
		inputs = append(inputs, in)
		if len(inputs) > 100000 {
			rows.Close()
			return nil, ErrState
		}
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	out := []coreRiskCheckOrder{}
	childState := map[int64]coreRiskChildCheckState{}
	for _, in := range inputs {
		if in.o.channel > 0 {
			var e error
			in.o.child, in.o.multi, e = s.coreRiskChildCheckSourceTx(ctx, tx, in.o.id, in.o.channel, childState)
			if e != nil {
				return nil, e
			}
		}
		if in.origin {
			origin, proof, e := s.readCoreRiskOrigin(ctx, tx, in.o.id)
			if e != nil {
				return nil, e
			}
			in.o.ips = origin.IPs
			if success, e := s.readCoreRiskSuccess(ctx, tx, in.o.id, proof); e != nil {
				return nil, e
			} else if success != nil {
				in.o.buyers = success.BuyerKeys
			} else if in.o.paid {
				return nil, ErrConflict
			}
		} else if ip, e := riskpolicy.CanonicalIP(in.ip); e == nil {
			in.o.ips = []string{ip.String()}
		}
		if len(in.o.buyers) == 0 && in.o.paid {
			key, e := s.coreRiskBuyerKey(in.kind, in.buyer)
			if e != nil {
				return nil, e
			}
			if key != "" {
				in.o.buyers = []string{key}
			}
		}
		out = append(out, in.o)
	}
	return out, nil
}
func (s *Store) applyCoreRiskCheck(ctx context.Context, tx pgx.Tx, p coreRiskPolicy, orders []coreRiskCheckOrder, now time.Time, out *CoreRiskCheckResult, observationUntil *time.Time) error {
	type pendingAction struct {
		action       coreRiskAction
		noticeReason string
	}
	pending := []pendingAction{}
	type rule struct{ flag, seconds, count, kind string }
	rules := []rule{{"auto_check_payip", "check_payip_second", "check_payip_count", "ip"}, {"auto_check_payspeed", "check_payspeed_second", "check_payspeed_count", "buyer"}, {"auto_check_sucrate", "check_sucrate_second", "check_sucrate_count", "merchant"}, {"auto_check_channel", "check_channel_second", "check_channel_failcount", "channel"}, {"auto_check_channel", "check_channel_second", "check_channel_failcount", "subchannel"}}
	for _, r := range rules {
		if p.Settings[r.flag] != "1" {
			continue
		}
		seconds, count := riskpolicy.Integer(p.Settings, r.seconds), riskpolicy.Integer(p.Settings, r.count)
		cutoff := now.Add(-time.Duration(seconds) * time.Second)
		stats := map[string]*coreRiskCheckStat{}
		targets := map[string]int64{}
		for _, o := range orders {
			if o.created.Before(cutoff) {
				continue
			}
			keys := []string{}
			switch r.kind {
			case "ip":
				for _, ip := range o.ips {
					k, e := s.coreRiskIPKey(ip)
					if e != nil {
						return e
					}
					keys = append(keys, k)
				}
			case "buyer":
				if o.paid {
					keys = o.buyers
				}
			case "merchant":
				keys = []string{fmt.Sprint(o.merchant)}
				targets[keys[0]] = o.merchant
			case "channel", "subchannel":
				if r.kind == "channel" && o.multi || r.kind == "subchannel" && o.child == 0 {
					continue
				}
				if o.channel > 0 {
					if selected := p.Settings["check_channel_ids"]; selected != "" {
						allowed := false
						for _, id := range splitCoreRiskIDs(selected) {
							allowed = allowed || id == fmt.Sprint(o.channel)
						}
						if !allowed {
							continue
						}
					}
					target := o.channel
					if r.kind == "subchannel" {
						target = o.child
					}
					keys = []string{fmt.Sprint(target)}
					targets[keys[0]] = target
				}
			}
			for _, key := range keys {
				stat := stats[key]
				if stat == nil {
					stat = &coreRiskCheckStat{}
					stats[key] = stat
				}
				stat.all = append(stat.all, o.id)
				if o.paid {
					stat.success = append(stat.success, o.id)
				}
				if stat.oldest.IsZero() || o.created.Before(stat.oldest) {
					stat.oldest = o.created
				}
			}
		}
		keys := []string{}
		for key := range stats {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			stat := stats[key]
			if int64(len(stat.all)) < count {
				continue
			}
			eligible := false
			sources := stat.all
			switch r.kind {
			case "ip":
				eligible = len(stat.success) == 0
			case "buyer":
				eligible = true
			case "merchant": // Integer half-up rounding matches original round(rate,2).
				total, success := int64(len(stat.all)), int64(len(stat.success))
				rounded := (success*10000 + total/2) / total
				eligible = rounded < riskpolicy.Amount(p.Settings, "check_sucrate_value")
			case "channel", "subchannel":
				latest := stat.all[len(stat.all)-int(count):]
				sources = latest
				eligible = true
				for _, id := range latest {
					for _, paid := range stat.success {
						if id == paid {
							eligible = false
						}
					}
				}
			}
			if !eligible {
				continue
			}
			var until *time.Time
			days := int64(5)
			if r.kind == "buyer" {
				days = riskpolicy.Integer(p.Settings, "check_payspeed_days")
			}
			if r.kind == "ip" || r.kind == "buyer" {
				if days > 0 {
					v := now.Add(time.Duration(days) * 24 * time.Hour)
					until = &v
				}
			}
			reason := ""
			if r.kind == "merchant" && p.Settings["check_sucrate_notice"] == "1" || (r.kind == "channel" || r.kind == "subchannel") && p.Settings["check_channel_notice"] == "1" {
				reason = fmt.Sprintf("%d 秒内统计 %d 笔订单，成功 %d 笔，触发已保存规则。", seconds, len(stat.all), len(stat.success))
			}
			deadline := stat.oldest.Add(time.Duration(seconds) * time.Second)
			if observationUntil.IsZero() || deadline.Before(*observationUntil) {
				*observationUntil = deadline
			}
			pending = append(pending, pendingAction{coreRiskAction{Kind: r.kind, SubjectKey: key, Action: "block", Rule: r.flag, TargetID: targets[key], PolicyRevision: p.Revision, At: now, Until: until, Sources: sources}, reason})
		}
	}
	keys := make([][2]string, len(pending))
	for i, v := range pending {
		keys[i] = [2]string{v.action.Kind, v.action.SubjectKey}
	}
	if e := lockCoreRiskActionKeys(ctx, tx, keys); e != nil {
		return e
	}
	for _, v := range pending {
		changed, e := s.writeCoreRiskAction(ctx, tx, v.action)
		if e != nil {
			return e
		}
		if changed {
			out.Actions++
			if v.noticeReason != "" {
				if e = s.captureCoreRiskNotice(ctx, tx, v.action.Kind, v.action.SubjectKey, v.noticeReason); e != nil {
					return e
				}
			}
		}
	}
	return nil
}
