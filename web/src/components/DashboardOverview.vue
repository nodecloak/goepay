<script setup lang="ts">
import { computed, ref } from "vue";
import ShellIcon from "./ShellIcon.vue";
import RichContent from "./RichContent.vue";
import DailyWelcome from "./DailyWelcome.vue";
import type { Area } from "../menu";
import { displayValue } from "../api";
import { beijingTimestamp as consoleDateTime } from "../console-datetime";
const props = defineProps<{
  data: Record<string, unknown>;
  area: Area;
  adminClass?: string;
  merchantId?: string;
  ownSession?: boolean;
}>();
const metric = ref("amount"),
  payment = ref("all"),
  details = ref(false);
const record = (value: unknown): Record<string, any> =>
  value && typeof value === "object" && !Array.isArray(value)
    ? (value as Record<string, any>)
    : {};
const list = (value: unknown): Record<string, any>[] =>
  Array.isArray(value) ? value.map(record) : [];
const summary = computed(() => record(props.data.summary)),
  trend = computed(() => list(props.data.trend)),
  mix = computed(() => list(props.data.payment_mix)),
  fees = computed(() => list(props.data.fee_trend)),
  todos = computed(() => record(props.data.todos)),
  merchant = computed(() => record(props.data.merchant)),
  runtime = computed(() => record(props.data.runtime)),
  announcements = computed(() => list(props.data.announcements)),
  settlements = computed(() => list(props.data.recent_settlements));
const feeSchedule = computed(() => record(props.data.fee_schedule)),
  channels = computed(() => list(props.data.payment_channels));
function feePercent(value: unknown) {
  const s = String(value ?? "");
  if (!/^(0|[1-9][0-9]{0,4})$/.test(s) || BigInt(s) > 10000n)
    return "暂不可确认";
  const n = BigInt(s);
  return `${n / 100n}.${(n % 100n).toString().padStart(2, "0")}%`;
}
const text = (value: unknown) =>
  value === null || value === undefined ? "—" : displayValue(value);
const amount = (key: string) => text(summary.value[key]);
const cards = computed(() =>
  props.area === "admin"
    ? [
        {
          title: "今日注册",
          value: amount("today_register"),
          sub: "用户总数",
          extra: amount("total_users"),
          icon: "merchant",
        },
        {
          title: "今日订单",
          value: amount("today_orders"),
          sub: "订单总数",
          extra: text(props.data.orders),
          icon: "trade",
        },
        {
          title: "今日交易额(元)",
          value: amount("today_money"),
          sub: "昨日交易额",
          extra: amount("yesterday_money"),
          icon: "wallet",
        },
        {
          title: "今日结算金额(元)",
          value: amount("today_settled_money"),
          sub: "昨日结算金额",
          extra: amount("yesterday_settled_money"),
          icon: "wallet",
        },
      ]
    : [
        {
          title: "商户当前余额",
          value: text(props.data.available_money),
          sub: "已结算余额",
          extra: amount("settled_money"),
          icon: "wallet",
        },
        {
          title: "今日订单数",
          value: amount("today_orders"),
          sub: "成功订单数",
          extra: amount("today_paid_orders"),
          icon: "trade",
        },
        {
          title: "今日交易额",
          value: amount("today_money"),
          sub: "昨日交易额",
          extra: amount("yesterday_money"),
          icon: "wallet",
        },
        {
          title: "今日转账支出",
          value: amount("today_transfer_money"),
          sub: "昨日转账支出",
          extra: amount("yesterday_transfer_money"),
          icon: "wallet",
        },
      ],
);
const methods = [
  { id: "all", label: "全部方式" },
  { id: "alipay", label: "支付宝" },
  { id: "wxpay", label: "微信支付" },
];
const methodName = (value: unknown) =>
  methods.find((v) => v.id === value)?.label || text(value);
function minor(value: unknown): bigint {
  const s = String(value ?? "0");
  if (!/^[0-9]{1,38}(\.[0-9]{1,2})?$/.test(s)) return 0n;
  const [whole, decimal = ""] = s.split(".");
  return BigInt(whole) * 100n + BigInt(decimal.padEnd(2, "0"));
}
const trendValue = (row: Record<string, any>) =>
  metric.value === "orders"
    ? String(row.orders ?? "0")
    : String(row[payment.value === "all" ? "amount" : payment.value] ?? "0.00");
const maximum = computed(() =>
  trend.value.reduce((max, row) => {
    const value = minor(trendValue(row));
    return value > max ? value : max;
  }, 1n),
);
const coordinates = computed(() =>
  trend.value.map((row, i) => ({
    x: 48 + i * 92,
    y: 210 - Number((minor(trendValue(row)) * 17000n) / maximum.value) / 100,
    row,
    value: trendValue(row),
  })),
);
const graphLabel = computed(() =>
  metric.value === "orders" ? "近7日订单数趋势图" : "近7日交易额趋势图",
);
const mixTotal = computed(() =>
  mix.value.reduce((n, row) => n + minor(row.amount), 0n),
);
const mixSegments = computed(() => {
  let offset = 0;
  return mix.value.map((row, i) => {
    const percentage =
      mixTotal.value > 0n
        ? Number((minor(row.amount) * 100000n) / mixTotal.value) / 1000
        : 0;
    const segment = {
      row,
      percentage,
      offset,
      color:
        row.type === "wxpay"
          ? "var(--chart-2)"
          : row.type === "alipay"
            ? "var(--chart-1)"
            : ["var(--chart-3)", "var(--chart-4)", "var(--chart-6)"][i % 3],
    };
    offset += percentage;
    return segment;
  });
});
function mixWidth(row: Record<string, any>) {
  return mixTotal.value > 0n
    ? Number((minor(row.amount) * 10000n) / mixTotal.value) / 100
    : 0;
}
function settlementState(row: Record<string, any>) {
  if (row.bank_state === "reversed") return "已退回";
  if (row.bank_state === "reversal_pending") return "退回待核对";
  if (row.payout_status === "succeeded") return "已完成";
  if (row.payout_status === "failed" || row.status === "rejected")
    return "结算失败";
  if (row.send_state === "cancelled") return "已取消";
  if (row.payout_status === "pending" || row.payout_status === "unknown")
    return "正在结算";
  return row.status === "approved" ? "待出款" : "待结算";
}
const taskLinks = computed(() => [
  {
    label: "未处理投诉",
    count: todos.value.complaints,
    path: "/complain/list",
  },
  {
    label: "待审核商户",
    count: todos.value.merchant_audits,
    path: "/merchant/merchant-list?pay=2",
  },
  { label: "待处理结算", count: todos.value.settlements, path: "/settle/list" },
  { label: "待处理付款", count: todos.value.transfers, path: "/transfer/list" },
]);
const quickLinks = computed(() =>
  props.area === "admin"
    ? [
        { label: "订单管理", path: "/trade/order" },
        { label: "商户管理", path: "/merchant/merchant-list" },
        ...(props.adminClass === "super"
          ? [{ label: "系统设置", path: "/system/settings" }]
          : []),
        { label: "返回首页", path: "/" },
        { label: "修改密码", path: "/system/change-password" },
      ]
    : [
        { label: "订单管理", path: "/trade/order" },
        { label: "结算管理", path: "/settle/list" },
        { label: "账户设置", path: "/account/settings" },
        { label: "API 信息", path: "/account/api-info" },
      ],
);
</script>
<template>
  <div class="dashboard-overview">
    <DailyWelcome
      v-if="area === 'merchant'"
      :content="String(data.welcome_notice ?? '')"
      :business-date="String(data.business_date ?? '')"
      :merchant-id="merchantId ?? ''"
      :data-merchant-id="String(merchant.id ?? '')"
      :own-session="ownSession === true"
    />
    <section v-if="area === 'merchant'" class="panel welcome">
      <h2>欢迎回来，{{ text(merchant.name) }}</h2>
      <p>
        商户号：{{ text(merchant.id) }}
        <span
          >商户状态：{{
            merchant.enabled === false
              ? "已停用"
              : !merchant.id || !["0", "1", "2"].includes(merchant.pay)
                ? "待确认"
                : merchant.pay === "2"
                  ? "待审核"
                  : merchant.pay === "0"
                    ? "支付受限"
                    : "正常"
          }}</span
        >
      </p>
    </section>
    <div class="stat-grid">
      <section v-for="card in cards" :key="card.title" class="panel stat">
        <span class="stat-icon"><ShellIcon :name="card.icon" /></span>
        <p>{{ card.title }}</p>
        <strong>{{ card.value }}</strong>
        <p class="card-sub">
          {{ card.sub }} <b>{{ card.extra }}</b>
        </p>
      </section>
    </div>
    <div class="dashboard-columns">
      <section class="panel dashboard-trend">
        <div class="chart-header">
          <div>
            <h2>
              {{ area === "admin" ? "支付方式收入统计" : "近7日订单数据统计" }}
            </h2>
            <p class="muted">
              {{
                metric === "orders"
                  ? "订单量按创建时间统计"
                  : "交易额按核实支付时间统计"
              }}
              · 北京时间 · 近7日
            </p>
          </div>
          <div class="chart-controls">
            <label
              >趋势查看方式<select v-model="metric" aria-label="趋势查看方式">
                <option value="amount">交易额（元）</option>
                <option value="orders">订单数</option>
              </select></label
            ><label v-if="metric === 'amount'"
              >支付方式<select v-model="payment" aria-label="趋势支付方式">
                <option
                  v-for="method in methods"
                  :key="method.id"
                  :value="method.id"
                >
                  {{ method.label }}
                </option>
              </select></label
            >
          </div>
        </div>
        <svg
          v-if="trend.length"
          class="trend-svg"
          viewBox="0 0 650 260"
          role="img"
          :aria-label="graphLabel"
        >
          <g class="grid-lines">
            <line
              v-for="y in [40, 125, 210]"
              :key="y"
              x1="40"
              :y1="y"
              x2="610"
              :y2="y"
            />
          </g>
          <polyline
            :points="coordinates.map((p) => p.x + ',' + p.y).join(' ')"
            class="trend-line"
          />
          <g v-for="point in coordinates" :key="point.row.date">
            <circle :cx="point.x" :cy="point.y" r="5" class="trend-dot">
              <title>
                {{ point.row.date }}：{{ point.value
                }}{{ metric === "amount" ? "元" : "笔" }}
              </title>
            </circle>
            <text :x="point.x" y="240" text-anchor="middle">
              {{ String(point.row.date).slice(5) }}
            </text>
          </g>
        </svg>
        <p v-else class="empty">暂无数据</p>
        <button
          type="button"
          class="link-button"
          :aria-expanded="details"
          @click="details = !details"
        >
          {{ details ? "收起趋势明细" : "查看趋势明细" }}
        </button>
        <div v-if="details" class="table-scroll">
          <table aria-label="近7日趋势明细">
            <thead>
              <tr>
                <th>日期</th>
                <th>订单数</th>
                <th>成功订单数</th>
                <th>交易额（元）</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="row in trend" :key="row.date">
                <td>{{ row.date }}</td>
                <td>{{ text(row.orders) }}</td>
                <td>{{ text(row.paid_orders) }}</td>
                <td>
                  {{ text(payment === "all" ? row.amount : row[payment]) }}
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>
      <section class="panel">
        <h2>{{ area === "admin" ? "今日交易统计" : "今日交易额占比" }}</h2>
        <p class="muted">各支付方式的实际交易额占比</p>
        <div v-if="mix.length" class="payment-mix">
          <svg
            class="mix-ring"
            viewBox="0 0 160 160"
            role="img"
            aria-label="今日交易额占比图"
          >
            <circle
              cx="80"
              cy="80"
              r="56"
              fill="none"
              style="stroke: var(--chart-grid)"
              stroke-width="18"
            />
            <circle
              v-for="segment in mixSegments"
              :key="segment.row.type"
              cx="80"
              cy="80"
              r="56"
              fill="none"
              :style="{ stroke: segment.color }"
              stroke-width="18"
              pathLength="100"
              :stroke-dasharray="
                segment.percentage + ' ' + (100 - segment.percentage)
              "
              :stroke-dashoffset="-segment.offset"
              transform="rotate(-90 80 80)"
            >
              <title>
                {{ methodName(segment.row.type) }}：{{
                  text(segment.row.amount)
                }}
                元
              </title>
            </circle>
            <text x="80" y="84" text-anchor="middle">数据占比</text>
          </svg>
          <div v-for="row in mix" :key="row.type" class="mix-row">
            <div>
              <span>{{ methodName(row.type) }}</span
              ><strong>{{ text(row.amount) }} 元</strong>
            </div>
            <div
              class="mix-track"
              role="img"
              :aria-label="
                methodName(row.type) + '交易额 ' + text(row.amount) + ' 元'
              "
            >
              <span
                :class="row.type === 'wxpay' ? 'wechat' : 'alipay'"
                :style="{ width: mixWidth(row) + '%' }"
              />
            </div>
            <p class="muted">
              {{ mixWidth(row) }}% · {{ text(row.orders) }} 笔
            </p>
          </div>
        </div>
        <p v-else class="empty">暂无数据</p>
      </section>
    </div>
    <div v-if="area === 'admin'" class="dashboard-columns">
      <section class="panel">
        <h2>支付方式手续费利润</h2>
        <p class="muted">
          近7日平台手续费及按下单通道成本快照估算的利润；缺少成本来源时保留未知。
        </p>
        <div v-if="fees.length" class="table-scroll">
          <table aria-label="近7日手续费利润">
            <thead>
              <tr>
                <th>日期</th>
                <th>支付方式</th>
                <th>手续费（元）</th>
                <th>估算利润（元）</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="row in fees" :key="row.date + '/' + row.type">
                <td>{{ row.date }}</td>
                <td>{{ methodName(row.type) }}</td>
                <td>{{ text(row.fee) }}</td>
                <td>
                  <span v-if="row.estimated_profit === null" class="muted"
                    >成本来源不完整（{{
                      text(row.missing_cost_orders)
                    }}
                    笔）</span
                  ><span v-else>{{ text(row.estimated_profit) }}</span>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
        <p v-else class="empty">暂无数据</p>
      </section>
      <section class="panel">
        <h2>待办事项</h2>
        <div class="dashboard-todos">
          <RouterLink
            v-for="task in taskLinks"
            :key="task.path"
            :to="'/admin' + task.path"
            >{{ task.label }}
            <strong>{{ text(task.count) }}</strong></RouterLink
          >
        </div>
      </section>
    </div>
    <div class="dashboard-columns">
      <section class="panel dashboard-shortcuts">
        <h2>快捷操作</h2>
        <div>
          <template v-for="link in quickLinks" :key="link.path"
            ><a v-if="link.path === '/'" href="/">{{ link.label }}</a
            ><RouterLink v-else :to="'/' + area + link.path">{{
              link.label
            }}</RouterLink></template
          >
        </div>
        <p v-if="data.frozen_money !== undefined" class="muted">
          冻结金额：{{ text(data.frozen_money) }} 元 ·
          余额与成功出款按账务事实显示
        </p>
      </section>
      <section class="panel">
        <h2>公告通知</h2>
        <article
          v-for="item in announcements"
          :key="item.id"
          class="dashboard-announcement"
        >
          <h3 v-if="item.title">{{ text(item.title) }}</h3>
          <RichContent
            :content="String(item.content ?? '')"
            :format="String(item.content_format ?? 'text')"
            :color="String(item.color ?? '')"
          />
          <time>{{ consoleDateTime(item.created_at) }}</time>
        </article>
        <p v-if="!announcements.length" class="empty">暂无公告</p>
      </section>
    </div>
    <section v-if="area === 'merchant'" class="panel">
      <h2>通道费率</h2>
      <p class="muted">
        当前用户组：{{ text(feeSchedule.group_name) }}；{{
          feeSchedule.state === "disabled"
            ? "支付手续费未启用，当前费率为0.00%"
            : feeSchedule.state === "ready"
              ? "各通道费率按当前政策逐行显示，仅适用于新订单"
              : "费率配置暂不可确认，请联系平台核对"
        }}。
      </p>
      <div v-if="channels.length" class="table-scroll">
        <table aria-label="通道费率">
          <thead>
            <tr>
              <th>通道ID</th>
              <th>支付方式</th>
              <th>商户费率</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="channel in channels" :key="channel.channel_id">
              <td>{{ text(channel.channel_id) }}</td>
              <td>{{ methodName(channel.type) }}</td>
              <td>{{ feePercent(channel.fee_rate_bps) }}</td>
            </tr>
          </tbody>
        </table>
      </div>
      <p v-else class="empty">暂无通道</p>
    </section>
    <section v-if="area === 'merchant'" class="panel">
      <h2>最近结算记录</h2>
      <div class="table-scroll">
        <table aria-label="最近结算记录">
          <thead>
            <tr>
              <th>申请号</th>
              <th>金额（元）</th>
              <th>状态</th>
              <th>申请时间</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="row in settlements" :key="row.id">
              <td>
                <RouterLink to="/merchant/settle/list">{{
                  text(row.external_no)
                }}</RouterLink>
              </td>
              <td>{{ text(row.amount) }}</td>
              <td>{{ settlementState(row) }}</td>
              <td>{{ consoleDateTime(row.created_at) }}</td>
            </tr>
            <tr v-if="!settlements.length">
              <td colspan="4" class="empty">暂无结算记录</td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>
    <section v-if="area === 'admin'" class="panel">
      <h2>服务器信息</h2>
      <dl class="runtime-grid">
        <dt>Go版本</dt>
        <dd>{{ text(runtime.go) }}</dd>
        <dt>PostgreSQL版本</dt>
        <dd>{{ text(runtime.postgresql) }}</dd>
        <dt>Web服务器</dt>
        <dd>{{ text(runtime.web) }}</dd>
        <dt>服务器时间</dt>
        <dd>{{ consoleDateTime(runtime.server_time) }}</dd>
      </dl>
    </section>
  </div>
</template>
<style scoped>
.dashboard-overview {
  display: grid;
  gap: 20px;
  min-width: 0;
}
.dashboard-overview .panel {
  min-width: 0;
  padding: 20px;
}
.dashboard-overview h2 {
  font-size: 16px;
  margin: 0 0 16px;
  font-weight: 600;
}
.dashboard-overview p {
  line-height: 1.7;
}
.dashboard-overview .stat-grid {
  margin: 0;
}
.dashboard-overview .stat .card-sub {
  margin: 12px 0 0;
  display: flex;
  justify-content: flex-start;
  align-items: baseline;
  gap: 6px;
  font-size: 12px;
  color: var(--ink-muted);
}
.dashboard-overview .stat .card-sub b {
  color: var(--primary-text);
}
.card-sub b {
  color: var(--ink);
  font-weight: 500;
}
.dashboard-columns {
  display: grid;
  grid-template-columns: minmax(0, 1.6fr) minmax(0, 1fr);
  gap: 20px;
  align-items: start;
}
.chart-header {
  display: flex;
  gap: 12px;
  flex-wrap: wrap;
  justify-content: space-between;
}
.chart-controls {
  display: flex;
  gap: 10px;
  flex-wrap: wrap;
}
.chart-controls label {
  display: grid;
  gap: 6px;
  margin: 0;
  font-size: 12px;
}
.chart-controls select {
  max-width: 150px;
  min-width: 0;
}
.trend-svg {
  display: block;
  width: 100%;
  max-height: 320px;
}
.grid-lines line {
  stroke: var(--line);
  stroke-dasharray: 4;
}
.trend-line {
  fill: none;
  stroke: var(--accent);
  stroke-width: 3;
  stroke-linecap: round;
  stroke-linejoin: round;
}
.trend-dot {
  fill: var(--paper);
  stroke: var(--accent);
  stroke-width: 3;
}
.trend-svg text {
  font-size: 12px;
  fill: var(--muted);
}
.payment-mix {
  display: grid;
  gap: 18px;
  margin-top: 24px;
}
.mix-ring {
  display: block;
  width: 220px;
  max-width: 100%;
  margin: 0 auto;
}
.mix-ring text {
  font-size: 13px;
  fill: var(--muted);
}
.mix-row > div:first-child {
  display: flex;
  justify-content: space-between;
  gap: 12px;
  flex-wrap: wrap;
}
.mix-row strong {
  font-size: 14px;
  font-weight: 500;
}
.mix-track {
  height: 12px;
  background: var(--bg-raised);
  border-radius: 6px;
  overflow: hidden;
  margin-top: 12px;
}
.mix-track > span {
  display: block;
  height: 100%;
  border-radius: 6px;
}
.alipay {
  background: var(--primary);
}
.wechat {
  background: var(--success);
}
.mix-row p {
  font-size: 12px;
  margin: 6px 0;
}
.dashboard-todos {
  display: grid;
  gap: 14px;
}
.dashboard-todos a {
  display: flex;
  justify-content: space-between;
  gap: 12px;
  border-bottom: 1px solid var(--line);
  padding: 12px 0;
}
.dashboard-todos strong {
  color: var(--accent);
}
.dashboard-shortcuts > div {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
}
.dashboard-shortcuts a {
  padding: 12px 18px;
  background: var(--primary-soft);
  border-radius: 6px;
}
.dashboard-announcement {
  border-bottom: 1px solid var(--line);
  padding: 12px 0;
}
.dashboard-announcement h3 {
  font-size: 14px;
  font-weight: 500;
  margin: 0;
}
.dashboard-announcement p {
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}
.dashboard-announcement time {
  font-size: 12px;
  color: var(--muted);
}
.runtime-grid {
  display: grid;
  grid-template-columns: max-content minmax(0, 1fr);
  gap: 14px 24px;
  font-size: 13px;
}
.runtime-grid dd {
  margin: 0;
  overflow-wrap: anywhere;
}
.welcome span {
  margin-left: 20px;
}
@media (max-width: 1100px) {
  .dashboard-columns {
    grid-template-columns: minmax(0, 1fr);
  }
}
@media (max-width: 600px) {
  .chart-controls {
    width: 100%;
  }
  .runtime-grid {
    gap: 10px;
  }
  .welcome span {
    display: block;
    margin-left: 0;
  }
  .dashboard-overview {
    gap: 14px;
  }
}
</style>
