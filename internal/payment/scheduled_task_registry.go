package payment

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"math"
	"time"
)

type ScheduledTaskSchedule struct {
	Frequency string    `json:"frequency"`
	Interval  int64     `json:"interval"`
	ExecuteAt time.Time `json:"execute_at"`
}
type ScheduledTaskParams map[string]int
type ScheduledTaskParameter struct {
	Name     string `json:"name"`
	Label    string `json:"label"`
	Type     string `json:"type"`
	Min      int    `json:"min"`
	Max      *int   `json:"max"`
	Default  int    `json:"default"`
	Required bool   `json:"required"`
}

func scheduledBound(value int) *int { return &value }
func scheduledParameters(key string) []ScheduledTaskParameter {
	switch key {
	case "referral_daily", "settlement_generate":
		return []ScheduledTaskParameter{{"limit", "每轮最多处理数量", "number", 1, scheduledBound(100), 20, true}}
	case "daily_report":
		return []ScheduledTaskParameter{{"send_hour", "发送时间点（小时，0~23）", "number", 0, scheduledBound(23), 9, true}}
	case "applyrefresh":
		return []ScheduledTaskParameter{{"refresh_minutes", "商户状态刷新频率（分钟）", "number", 15, nil, 60, true}}
	case "order_query":
		return []ScheduledTaskParameter{{"unpaid_minutes", "未支付订单超过多少分钟后进行查单", "number", 10, nil, 60, true}}
	case "profitsharing":
		return []ScheduledTaskParameter{{"profitsharing_limit", "每次分账的订单数量", "number", 1, nil, 10, true}, {"settle_query_limit", "每次处理延迟结算的订单数量", "number", 1, nil, 10, true}}
	default:
		return []ScheduledTaskParameter{}
	}
}

func ScheduledTaskParameters(key string) ([]ScheduledTaskParameter, bool) {
	if _, ok := scheduledDefinition(key); !ok {
		return nil, false
	}
	return scheduledParameters(key), true
}
func scheduledDefaults(key string) ScheduledTaskParams {
	out := ScheduledTaskParams{}
	for _, p := range scheduledParameters(key) {
		out[p.Name] = p.Default
	}
	return out
}
func validScheduledParams(key string, values ScheduledTaskParams) bool {
	definitions := scheduledParameters(key)
	if values == nil || len(values) != len(definitions) {
		return false
	}
	for _, p := range definitions {
		value, ok := values[p.Name]
		if !ok || value < p.Min || p.Max != nil && value > *p.Max {
			return false
		}
	}
	return true
}

type scheduledTaskDefinition struct{ key, name, description, unavailable string }

var scheduledTaskRegistry = []scheduledTaskDefinition{
	{"referral_daily", "每日邀请返现结算", "处理原已入账、已足额预留且已到期的每日邀请奖励；原退款和账务证明仍须通过。", ""},
	{"settlement_generate", "自动生成结算列表（待审核）", "只执行原generate_only有限授权，生成待审核申请；不审批、不出款。", ""},
	{"order", "系统每日数据维护", "登记本人已验证会员到期通知，并结算原已足额预留的每日邀请返现（独立返现策略已接管时由其处理）；到期组、黑名单及通道日额度由原请求路径实时判定。保留订单与账务历史，不执行插件日志清理和未知每日钩子。", ""},
	{"notify", "订单异步通知重试", "登记原自动通知重试的有限准入，由现有worker发送；首次通知与已授权人工通知独立，调度完成不代表送达。", ""},
	{"order_query", "未支付订单查询补单", "按原未支付时间窗口登记原支付查询job的有限准入；不生成支付意图，不控制退款、代付或预授权操作查询。", ""},
	{"transfer", "商户余额自动转账", "按原余额阈值与本人自动结算设置生成冻结申请，必须另有匹配的有限生成/自动出款授权及受控运行配置；仅登记原出款job准入，由原worker执行。", ""},
	{"profitsharing", "订单分账与延迟结算", "分别按原两项数量参数登记现有分账/延迟结算查询job准入（每类每轮最多100）；只恢复已授权原操作，自动首次发送的加密插件规则暂缓。", ""},
	{"daily_report", "收款日报发送", "按本人原设置登记当日通知源；实际发送由现有merchant.notice worker验证并消费，调度完成不代表送达。", ""},
	{"balance_notice", "商户余额不足提醒", "按本人原余额阈值登记当日通知源；实际发送由现有merchant.notice worker验证并消费，调度完成不代表送达。", ""},
	{"check", "支付风控检测", "按原风控规则生成可审计新付款限制；独立受控通知不会直接改人工收款配置。", ""},
	{"complain", "交易投诉同步", "原渠道投诉同步任务。", "相关外部业务插件暂缓。"},
	{"applyrefresh", "进件商户状态刷新", "原渠道进件资料刷新任务。", "相关外部业务插件暂缓。"},
	{"settle", "自动生成结算列表", "按原有限计划生成冻结待审核申请；只有另有匹配自动出款授权及受控运行配置才登记原自动出款job准入，生成和调度成功均不代表付款。", ""},
}

func scheduledDefinition(key string) (scheduledTaskDefinition, bool) {
	for _, d := range scheduledTaskRegistry {
		if d.key == key {
			return d, true
		}
	}
	return scheduledTaskDefinition{}, false
}
func decodeScheduledJSON(raw []byte, v any) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if e := d.Decode(v); e != nil {
		return ErrConflict
	}
	if e := d.Decode(new(any)); !errors.Is(e, io.EOF) {
		return ErrConflict
	}
	return nil
}
func (v ScheduledTaskSchedule) validate() error {
	if v.ExecuteAt.IsZero() || v.ExecuteAt.Year() < 1 || v.ExecuteAt.Year() > 9999 || v.ExecuteAt.Nanosecond() != 0 {
		return ErrInvalid
	}
	maximum := int64(0)
	switch v.Frequency {
	case "second":
		maximum = 3600
	case "minute":
		maximum = 1440
	case "hour":
		maximum = 168
	case "day":
		maximum = 366
	case "once":
		maximum = 1
	default:
		return ErrInvalid
	}
	if v.Interval < 1 || v.Interval > maximum {
		return ErrInvalid
	}
	return nil
}
func (v ScheduledTaskSchedule) step() int64 {
	switch v.Frequency {
	case "minute":
		return v.Interval * 60
	case "hour":
		return v.Interval * 3600
	case "day":
		return v.Interval * 86400
	}
	return v.Interval
}
func (v ScheduledTaskSchedule) due(now time.Time, last int64) (int64, time.Time, error) {
	if e := v.validate(); e != nil {
		return 0, time.Time{}, e
	}
	if now.Before(v.ExecuteAt) {
		return 0, time.Time{}, ErrState
	}
	slot := int64(0)
	if v.Frequency != "once" {
		slot = (now.Unix() - v.ExecuteAt.Unix()) / v.step()
	}
	if slot <= last {
		return 0, time.Time{}, ErrState
	}
	return slot, time.Unix(v.ExecuteAt.Unix()+slot*v.step(), 0).UTC(), nil
}
func (v ScheduledTaskSchedule) next(now time.Time, last int64) *time.Time {
	if v.validate() != nil {
		return nil
	}
	if last < 0 && now.Before(v.ExecuteAt) {
		x := v.ExecuteAt
		return &x
	}
	if v.Frequency == "once" {
		if last < 0 {
			x := v.ExecuteAt
			return &x
		}
		return nil
	}
	slot := last + 1
	if now.Unix() >= v.ExecuteAt.Unix() {
		current := (now.Unix() - v.ExecuteAt.Unix()) / v.step()
		if current > slot {
			slot = current
		}
	}
	if slot < 0 || slot > math.MaxInt64/v.step() {
		return nil
	}
	x := time.Unix(v.ExecuteAt.Unix()+slot*v.step(), 0).UTC()
	if x.Year() > 9999 {
		return nil
	}
	return &x
}

type ScheduledTaskView struct {
	Task                 string                   `json:"task"`
	Name                 string                   `json:"name"`
	Description          string                   `json:"description"`
	Available            bool                     `json:"available"`
	UnavailableReason    string                   `json:"unavailable_reason"`
	ParameterDefinitions []ScheduledTaskParameter `json:"parameter_definitions"`
	Configured           bool                     `json:"configured"`
	ControlMode          string                   `json:"control_mode"`
	Enabled              bool                     `json:"enabled"`
	Schedule             *ScheduledTaskSchedule   `json:"schedule"`
	Params               map[string]int           `json:"params"`
	Revision             int64                    `json:"revision,string"`
	Fingerprint          string                   `json:"fingerprint"`
	CanUpdate            bool                     `json:"can_update"`
	CanRun               bool                     `json:"can_run"`
	Running              bool                     `json:"running"`
	LastRunTime          *time.Time               `json:"last_run_time"`
	NextRunTime          *time.Time               `json:"next_run_time"`
	LastStatus           string                   `json:"last_status"`
	LastError            string                   `json:"last_error"`
}
type ScheduledTaskInput struct {
	Enabled             bool                  `json:"enabled"`
	Schedule            ScheduledTaskSchedule `json:"schedule"`
	Params              ScheduledTaskParams   `json:"params"`
	ExpectedFingerprint string                `json:"expected_fingerprint"`
}
type ScheduledTaskCounts struct {
	Examined              int `json:"examined"`
	Awarded               int `json:"awarded"`
	Generated             int `json:"generated"`
	Skipped               int `json:"skipped"`
	Enqueued              int `json:"enqueued"`
	AwaitingAuthorization int `json:"awaiting_authorization"`
}
type ScheduledTaskRunView struct {
	ID           int64                `json:"id,string"`
	Task         string               `json:"task"`
	Trigger      string               `json:"trigger"`
	Status       string               `json:"status"`
	ScheduledFor time.Time            `json:"scheduled_for"`
	CreatedAt    time.Time            `json:"created_at"`
	StartedAt    *time.Time           `json:"started_at"`
	CompletedAt  *time.Time           `json:"completed_at"`
	Counts       *ScheduledTaskCounts `json:"counts"`
	ErrorCode    string               `json:"error_code"`
}
type ScheduledTaskRunsPage struct {
	Items      []ScheduledTaskRunView `json:"items"`
	Total      int64                  `json:"total"`
	NextCursor int64                  `json:"next_cursor,string"`
}
