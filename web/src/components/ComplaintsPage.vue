<script setup lang="ts">
import { computed, onMounted, ref, watch } from "vue";
import { ApiError, type ApiClient } from "../api";
import type { Area } from "../menu";
import { money, datetime } from "../pro-console-fields";
import {
  fetchComplaints,
  fetchComplaint,
  providerReply,
  providerComplete,
  providerSupplement,
  refundOnComplaint,
  fetchComplaintSummary,
  fetchComplaintTasks,
  fetchComplaintChannels,
  saveComplaintTask,
  setComplaintTaskStatus,
  deleteComplaintTask,
  deleteComplaintTasks,
  exportComplaints,
  transitionComplaint,
  complaintStatusWords,
  complaintStatusTone,
  complaintSearchFields,
  type ComplaintRow,
  type ComplaintDetailRow,
  type ComplaintSummary,
  type ComplaintTaskRow,
  type ComplaintChannelOption,
} from "../complaints";
import KitFilterBar from "./KitFilterBar.vue";
import KitField from "./KitField.vue";
import KitInput from "./KitInput.vue";
import KitSelect from "./KitSelect.vue";
import KitStatusTag from "./KitStatusTag.vue";
import KitPayMethod from "./KitPayMethod.vue";
import KitEmpty from "./KitEmpty.vue";
import KitModal from "./KitModal.vue";
import KitPagination from "./KitPagination.vue";
import ShellIcon from "./ShellIcon.vue";

// GOEPAY 交易投诉 on Pro's model. One component serves all four Pro views
// (admin 投诉记录/投诉统计/同步任务, merchant 投诉记录) selected by `path`, built
// from the design kit. Data reads/writes run against the existing backoffice
// `complaints` resource + cases endpoints and the `complaints` report; the
// provider sync task state is read-only here. Everything is reachable only when
// complain_open is on (merchant menu entry gated by the server, admin pages
// under the super-administrator 交易投诉 menu).
const props = defineProps<{
  client: ApiClient;
  area: Area;
  path: string;
  canConfigure?: boolean;
}>();

type View = "list" | "charts" | "task";
const view = computed<View>(() => {
  if (props.path === "/complain/charts") return "charts";
  if (props.path === "/complain/task") return "task";
  return "list"; // /complain/list (admin) and /complain (merchant)
});

const loading = ref(false);
const error = ref("");
const notice = ref("");
const featureDisabled = ref(false);
function readFailure(e: unknown, fallback: string) {
  featureDisabled.value =
    e instanceof ApiError &&
    e.responseDecoded &&
    e.status === 403 &&
    e.code === -1 &&
    e.serverMessage === "complaint feature disabled";
  error.value = featureDisabled.value
    ? ""
    : e instanceof Error
      ? e.message
      : fallback;
}

// ---- 投诉记录 (list) ----
const searchField = ref("");
const keyword = ref("");
const statusFilter = ref("");
const payTypeFilter = ref("");
const channelFilter = ref("");
const dateStart = ref("");
const dateEnd = ref("");
const exporting = ref(false);
const page = ref(1);
const pageSize = ref(20);
const total = ref(0);
const rows = ref<ComplaintRow[]>([]);
const loaded = ref(false);

const statusOptions = [
  { value: "", label: "全部状态" },
  { value: "open", label: "待处理" },
  { value: "processing", label: "处理中" },
  { value: "done", label: "已处理" },
];
const payTypeOptions = [
  { value: "", label: "全部方式" },
  { value: "alipay", label: "支付宝" },
  { value: "wxpay", label: "微信" },
  { value: "qqpay", label: "QQ钱包" },
  { value: "bank", label: "网银" },
];

function filterQuery(q: URLSearchParams): URLSearchParams {
  const text = keyword.value.trim();
  if (text) {
    q.set("q", text);
    if (searchField.value) q.set("field", searchField.value);
  }
  if (statusFilter.value) q.set("status", statusFilter.value);
  if (payTypeFilter.value) q.set("pay_type", payTypeFilter.value);
  if (props.area === "admin" && channelFilter.value.trim())
    q.set("channel_id", channelFilter.value.trim());
  if (dateStart.value) q.set("starttime", dateStart.value);
  if (dateEnd.value) q.set("endtime", dateEnd.value);
  return q;
}

function listQuery(): string {
  const q = new URLSearchParams({
    page: String(page.value),
    limit: String(pageSize.value),
  });
  return filterQuery(q).toString();
}

async function loadList() {
  if (view.value !== "list") return;
  loading.value = true;
  error.value = "";
  try {
    const res = await fetchComplaints(props.client, listQuery());
    rows.value = res.items;
    total.value = res.total;
    loaded.value = true;
    featureDisabled.value = false;
    // Summary banner (今日/昨日), best-effort; absence never blocks the list.
    try {
      listSummary.value = await fetchComplaintSummary(props.client);
    } catch {
      listSummary.value = null;
    }
  } catch (e) {
    readFailure(e, "读取投诉记录失败");
  } finally {
    loading.value = false;
  }
}
const listSummary = ref<ComplaintSummary | null>(null);

function onQuery() {
  page.value = 1;
  void loadList();
}
function onReset() {
  searchField.value = "";
  keyword.value = "";
  statusFilter.value = "";
  payTypeFilter.value = "";
  channelFilter.value = "";
  dateStart.value = "";
  dateEnd.value = "";
  page.value = 1;
  void loadList();
}

// 导出: download the filtered list as CSV (same filters, no paging).
async function onExport() {
  if (exporting.value) return;
  exporting.value = true;
  error.value = "";
  try {
    await exportComplaints(
      props.client,
      filterQuery(new URLSearchParams()).toString(),
    );
    notice.value = "已导出当前筛选的投诉记录";
  } catch (e) {
    error.value = e instanceof Error ? e.message : "导出失败";
  } finally {
    exporting.value = false;
  }
}

const amountCell = (a: string) => (/^\d+$/.test(a) ? "¥" + money(a) : "-");

// ---- detail + reply + complete dialog ----
const detail = ref<ComplaintDetailRow | null>(null);
const detailBusy = ref(false);
const replyText = ref("");
const nextStatus = ref("processing");
async function openDetail(row: ComplaintRow) {
  detail.value = { ...row, images: [], negotiation: [] };
  replyText.value = "";
  nextStatus.value = row.status === "open" ? "processing" : "resolved";
  error.value = "";
  try {
    detail.value = await fetchComplaint(props.client, row.id);
  } catch (e) {
    if (!(e instanceof ApiError && e.status === 404))
      error.value = e instanceof Error ? e.message : "读取投诉详情失败";
  }
}
// ---- Pro 处理投诉 dialog: provider actions + refund-with-password ----
// 处理结果 presets (回复用户 content shortcuts).
const resultPresets = [
  "已为用户办理退款",
  "已与用户协商一致",
  "非平台责任，已向用户说明",
  "订单正常，已向用户解释",
];
const refundAmount = ref("");

// 回复用户 through the channel (provider) API.
async function submitProviderReply() {
  if (!detail.value || detailBusy.value) return;
  const text = replyText.value.trim();
  if (!text) {
    error.value = "请输入回复内容";
    return;
  }
  detailBusy.value = true;
  error.value = "";
  try {
    await providerReply(props.client, detail.value.id, text, []);
    notice.value = "已通过渠道回复投诉人";
    replyText.value = "";
    await loadList();
  } catch (e) {
    error.value = e instanceof Error ? e.message : "渠道回复失败";
  } finally {
    detailBusy.value = false;
  }
}
// 补充凭证 (Alipay only).
async function submitSupplement() {
  if (!detail.value || detailBusy.value) return;
  const text = replyText.value.trim();
  if (!text) {
    error.value = "请输入补充说明";
    return;
  }
  detailBusy.value = true;
  error.value = "";
  try {
    await providerSupplement(props.client, detail.value.id, text, []);
    notice.value = "已补充凭证";
  } catch (e) {
    error.value = e instanceof Error ? e.message : "补充凭证失败";
  } finally {
    detailBusy.value = false;
  }
}
// 处理 through the provider (微信反馈完成 / 支付宝处理) then advance local state.
async function submitProviderComplete() {
  if (!detail.value || detailBusy.value) return;
  detailBusy.value = true;
  error.value = "";
  try {
    await providerComplete(
      props.client,
      detail.value.id,
      replyText.value.trim(),
      "",
      [],
    );
    await transitionComplaint(props.client, detail.value.id, "resolved", "");
    notice.value = "已通过渠道处理并标记完成";
    detail.value = null;
    await loadList();
  } catch (e) {
    error.value = e instanceof Error ? e.message : "渠道处理失败";
  } finally {
    detailBusy.value = false;
  }
}
// 退款审批: refund-on-complaint goes through GOEPAY's EXISTING order refund flow,
// which prompts for the admin funds password (funds authorization) itself.
async function submitRefund() {
  if (!detail.value || detailBusy.value) return;
  const trade = detail.value.trade_no;
  if (!trade) {
    error.value = "该投诉未关联订单，无法退款";
    return;
  }
  if (!/^\d+(\.\d{1,2})?$/.test(refundAmount.value)) {
    error.value = "请输入退款金额（元）";
    return;
  }
  detailBusy.value = true;
  error.value = "";
  try {
    await refundOnComplaint(
      props.client,
      trade,
      refundAmount.value,
      crypto.randomUUID(),
    );
    notice.value = "退款已提交（经订单退款流程，资金授权照常生效）";
    refundAmount.value = "";
    await loadList();
  } catch (e) {
    error.value = e instanceof Error ? e.message : "退款失败";
  } finally {
    detailBusy.value = false;
  }
}

// ---- 投诉统计 (charts) ----
type ChartSummary = {
  today_count: string;
  today_alipay: string;
  today_wxpay: string;
  yesterday_count: string;
  yesterday_alipay: string;
  yesterday_wxpay: string;
  days7_rate: string;
};
const summary = ref<ChartSummary | null>(null);
const chartRows = ref<Record<string, string>[]>([]);
const chartColumns = ref<{ key: string; label: string }[]>([]);
const moneyColumns = new Set(["today_money", "yesterday_money"]);
function chartCell(key: string, value: string | undefined): string {
  if (value == null || value === "") return "-";
  if (moneyColumns.has(key)) return "¥" + value;
  return value;
}
async function loadCharts() {
  if (view.value !== "charts") return;
  loading.value = true;
  error.value = "";
  try {
    const data = await props.client.request<{
      items?: Record<string, string>[];
      summary?: ChartSummary;
      columns?: { key: string; label: string }[];
    }>(`/complaints-charts`);
    summary.value = data.summary || null;
    chartRows.value = data.items || [];
    chartColumns.value = data.columns || [];
    loaded.value = true;
    featureDisabled.value = false;
  } catch (e) {
    readFailure(e, "读取投诉统计失败");
  } finally {
    loading.value = false;
  }
}

// ---- 同步任务 (task, read-only state) ----
const tasks = ref<ComplaintTaskRow[]>([]);
const taskDraft = ref({ channelId: "", enabled: "" });
const taskApplied = ref({ channelId: "", enabled: "" });
const taskStatusOptions = [
  { value: "", label: "全部状态" },
  { value: "enabled", label: "已启用" },
  { value: "disabled", label: "已禁用" },
];
const visibleTasks = computed(() =>
  tasks.value.filter(
    (t) =>
      (!taskApplied.value.channelId ||
        t.channel_id === taskApplied.value.channelId) &&
      (!taskApplied.value.enabled ||
        t.enabled === (taskApplied.value.enabled === "enabled")),
  ),
);
function queryTasks() {
  if (loading.value || taskBusy.value) return;
  const id = taskDraft.value.channelId.trim();
  if (
    id &&
    (!/^[1-9]\d{0,18}$/.test(id) || BigInt(id) > 9223372036854775807n)
  ) {
    error.value = "请输入有效通道ID";
    return;
  }
  taskApplied.value = { channelId: id, enabled: taskDraft.value.enabled };
  selectedTasks.value = new Set();
  void loadTasks();
}
function resetTaskFilters() {
  if (loading.value || taskBusy.value) return;
  taskDraft.value = { channelId: "", enabled: "" };
  taskApplied.value = { ...taskDraft.value };
  selectedTasks.value = new Set();
  void loadTasks();
}
async function loadTasks() {
  if (view.value !== "task") return;
  loading.value = true;
  error.value = "";
  try {
    tasks.value = await fetchComplaintTasks(props.client);
    selectedTasks.value = new Set();
    loaded.value = true;
    featureDisabled.value = false;
  } catch (e) {
    readFailure(e, "读取同步任务失败");
  } finally {
    loading.value = false;
  }
}

// ---- 同步任务 (task) editing ----
const taskBusy = ref(false);
const selectedTasks = ref<Set<string>>(new Set());
const allTasksSelected = computed(
  () =>
    visibleTasks.value.length > 0 &&
    visibleTasks.value.every((t) => selectedTasks.value.has(t.channel_id)),
);
function toggleTaskSelection(channelId: string) {
  const next = new Set(selectedTasks.value);
  if (next.has(channelId)) next.delete(channelId);
  else next.add(channelId);
  selectedTasks.value = next;
}
function toggleAllTasks() {
  if (allTasksSelected.value) selectedTasks.value = new Set();
  else
    selectedTasks.value = new Set(visibleTasks.value.map((t) => t.channel_id));
}

// Edit dialog (获取条件 days / 获取频率 frequency / 每次同步获取数量 num).
const editTask = ref<ComplaintTaskRow | null>(null);
const editDays = ref("0");
const editFrequency = ref("6");
const editNum = ref("20");
function openTaskEdit(t: ComplaintTaskRow) {
  editTask.value = t;
  editDays.value = String(t.days);
  editFrequency.value = String(t.frequency || 6);
  editNum.value = String(t.num || 20);
  error.value = "";
}
async function submitTaskEdit() {
  if (!editTask.value || taskBusy.value) return;
  const freq = Math.trunc(Number(editFrequency.value));
  const num = Math.trunc(Number(editNum.value));
  const days = Math.trunc(Number(editDays.value));
  if (!Number.isFinite(freq) || freq < 1 || freq > 1440) {
    error.value = "获取频率需在 1 到 1440 分钟之间";
    return;
  }
  if (!Number.isFinite(num) || num < 20 || num > 200) {
    error.value = "每次同步获取数量需在 20 到 200 之间";
    return;
  }
  if (!Number.isFinite(days) || days < 0) {
    error.value = "获取条件（天数）不能小于 0";
    return;
  }
  taskBusy.value = true;
  error.value = "";
  try {
    await saveComplaintTask(
      props.client,
      editTask.value.channel_id,
      editTask.value.enabled,
      freq,
      num,
      days,
    );
    notice.value = "已保存同步任务";
    editTask.value = null;
    await loadTasks();
  } catch (e) {
    error.value = e instanceof Error ? e.message : "保存同步任务失败";
  } finally {
    taskBusy.value = false;
  }
}
async function toggleTaskStatus(t: ComplaintTaskRow) {
  if (taskBusy.value) return;
  taskBusy.value = true;
  error.value = "";
  try {
    await setComplaintTaskStatus(props.client, t.channel_id, !t.enabled);
    notice.value = t.enabled ? "已停用该同步任务" : "已启用该同步任务";
    await loadTasks();
  } catch (e) {
    error.value = e instanceof Error ? e.message : "更新状态失败";
  } finally {
    taskBusy.value = false;
  }
}
async function removeTask(t: ComplaintTaskRow) {
  if (taskBusy.value) return;
  if (!window.confirm(`确定删除通道 ${t.channel_id} 的同步任务？`)) return;
  taskBusy.value = true;
  error.value = "";
  try {
    await deleteComplaintTask(props.client, t.channel_id);
    notice.value = "已删除同步任务";
    selectedTasks.value.delete(t.channel_id);
    await loadTasks();
  } catch (e) {
    error.value = e instanceof Error ? e.message : "删除同步任务失败";
  } finally {
    taskBusy.value = false;
  }
}
async function removeSelectedTasks() {
  if (taskBusy.value || selectedTasks.value.size === 0) return;
  const ids = [...selectedTasks.value];
  if (!window.confirm(`确定批量删除选中的 ${ids.length} 个同步任务？`)) return;
  taskBusy.value = true;
  error.value = "";
  try {
    const n = await deleteComplaintTasks(props.client, ids);
    notice.value = `已批量删除 ${n} 个同步任务`;
    selectedTasks.value = new Set();
    await loadTasks();
  } catch (e) {
    error.value = e instanceof Error ? e.message : "批量删除失败";
  } finally {
    taskBusy.value = false;
  }
}
const channels = ref<ComplaintChannelOption[]>([]);
const autoChannel = ref("");
const autoBusy = ref(false);
async function loadChannels() {
  if (props.area !== "admin") return;
  try {
    channels.value = await fetchComplaintChannels(props.client);
  } catch {
    channels.value = [];
  }
}
const autoChannelEnabled = computed(
  () => channels.value.find((c) => c.channel_id === autoChannel.value)?.enabled,
);
async function toggleAutoFetch() {
  if (!autoChannel.value || autoBusy.value) return;
  autoBusy.value = true;
  error.value = "";
  try {
    const next = !autoChannelEnabled.value;
    await saveComplaintTask(props.client, autoChannel.value, next);
    notice.value = next ? "已为该通道开启自动获取" : "已关闭该通道自动获取";
    await loadChannels();
  } catch (e) {
    error.value = e instanceof Error ? e.message : "自动获取设置失败";
  } finally {
    autoBusy.value = false;
  }
}

function reload() {
  loaded.value = false;
  if (view.value === "list") {
    void loadList();
    void loadChannels();
  } else if (view.value === "charts") void loadCharts();
  else void loadTasks();
}

watch(view, reload);
onMounted(reload);
</script>

<template>
  <div
    v-if="featureDisabled"
    class="list-page complain-page"
    aria-label="交易投诉"
  >
    <section class="panel records" aria-label="交易投诉未启用">
      <h2>交易投诉尚未启用</h2>
      <p class="muted">
        平台已关闭交易投诉功能，当前不能获取、处理投诉或维护同步任务。
      </p>
      <p v-if="!canConfigure || area !== 'admin'" class="muted">
        如需使用，请联系平台管理员。
      </p>
      <div class="toolbar-actions">
        <a
          v-if="canConfigure && area === 'admin'"
          class="pa-btn pa-btn--primary"
          href="/admin/system/settings?tab=complaint"
          >前往投诉设置</a
        >
        <button
          type="button"
          class="pa-btn"
          :disabled="loading"
          @click="reload"
        >
          重新检查功能状态
        </button>
      </div>
    </section>
  </div>
  <div v-else class="list-page complain-page" aria-label="交易投诉">
    <p v-if="error" class="alert error" role="alert">{{ error }}</p>
    <p v-if="notice" class="alert success" role="status">{{ notice }}</p>

    <!-- 投诉记录 -->
    <template v-if="view === 'list'">
      <KitFilterBar :loading="loading" @query="onQuery" @reset="onReset">
        <KitField label="搜索字段">
          <KitSelect
            v-model="searchField"
            :options="complaintSearchFields"
            placeholder="综合搜索"
            aria-label="搜索字段"
          />
        </KitField>
        <KitField label="搜索内容">
          <KitInput
            v-model="keyword"
            placeholder="输入关键字"
            aria-label="搜索内容"
          />
        </KitField>
        <KitField label="状态">
          <KitSelect
            v-model="statusFilter"
            :options="statusOptions"
            placeholder="全部状态"
            aria-label="状态"
          />
        </KitField>
        <template #extra>
          <KitField label="支付方式">
            <KitSelect
              v-model="payTypeFilter"
              :options="payTypeOptions"
              placeholder="全部方式"
              aria-label="支付方式"
            />
          </KitField>
          <KitField v-if="area === 'admin'" label="通道ID">
            <KitInput
              v-model="channelFilter"
              inputmode="numeric"
              placeholder="通道ID"
              aria-label="通道ID"
            />
          </KitField>
          <KitField label="开始日期">
            <KitInput v-model="dateStart" type="date" aria-label="开始日期" />
          </KitField>
          <KitField label="结束日期">
            <KitInput v-model="dateEnd" type="date" aria-label="结束日期" />
          </KitField>
        </template>
      </KitFilterBar>

      <div
        v-if="listSummary"
        class="stat-grid complain-stat-grid"
        data-test="complain-summary"
      >
        <section class="panel stat" aria-label="今日投诉">
          <span class="stat-icon"><ShellIcon name="shield" /></span>
          <p>今日投诉</p>
          <strong>{{ listSummary.today_count }}</strong>
          <span
            >支付宝 {{ listSummary.today_alipay }} · 微信
            {{ listSummary.today_wxpay }}</span
          >
        </section>
        <section class="panel stat" aria-label="昨日投诉">
          <span class="stat-icon"><ShellIcon name="shield" /></span>
          <p>昨日投诉</p>
          <strong>{{ listSummary.yesterday_count }}</strong>
          <span
            >支付宝 {{ listSummary.yesterday_alipay }} · 微信
            {{ listSummary.yesterday_wxpay }}</span
          >
        </section>
      </div>

      <section class="panel records" aria-label="投诉记录">
        <div class="table-tools">
          <button
            type="button"
            class="pa-btn"
            data-action="refresh"
            :disabled="loading"
            @click="reload"
          >
            获取最新数据
          </button>
          <button
            type="button"
            class="pa-btn"
            data-action="export"
            :disabled="exporting"
            @click="onExport"
          >
            {{ exporting ? "导出中…" : "导出" }}
          </button>
          <template v-if="area === 'admin'">
            <KitSelect
              v-model="autoChannel"
              class="complain-channel-select"
              :options="
                channels.map((c) => ({
                  value: c.channel_id,
                  label: `${c.name} · ${c.provider === 'wxpay' ? '微信' : '支付宝'}${c.enabled ? '（自动获取中）' : ''}`,
                }))
              "
              placeholder="选择支付通道"
              aria-label="自动获取通道"
            />
            <button
              type="button"
              class="pa-btn"
              :class="autoChannelEnabled ? 'pa-btn--danger' : 'pa-btn--success'"
              data-action="auto-fetch"
              :disabled="!autoChannel || autoBusy"
              @click="toggleAutoFetch"
            >
              {{ autoChannelEnabled ? "关闭自动获取" : "开启自动获取" }}
            </button>
          </template>
        </div>

        <div class="table-scroll">
          <table class="pa-table">
            <thead>
              <tr>
                <th>ID</th>
                <th>商户号</th>
                <th>关联订单号</th>
                <th>问题类型</th>
                <th class="pa-c-num">订单金额</th>
                <th>投诉详情</th>
                <th>创建时间</th>
                <th>状态</th>
                <th class="pa-c-num">投诉次数</th>
                <th>操作</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="row in rows" :key="row.id">
                <td class="pa-c-num">{{ row.id }}</td>
                <td>{{ row.merchant_id || "-" }}</td>
                <td>
                  <span class="pa-c-mono">{{ row.trade_no || "-" }}</span>
                </td>
                <td>
                  <KitPayMethod v-if="row.pay_type" :type="row.pay_type" />
                  <span v-else>-</span>
                  <small class="muted">{{ row.subject || "-" }}</small>
                </td>
                <td class="pa-c-num">{{ amountCell(row.amount) }}</td>
                <td class="complain-detail">{{ row.content || "-" }}</td>
                <td>{{ row.created_at ? datetime(row.created_at) : "-" }}</td>
                <td>
                  <KitStatusTag
                    :text="complaintStatusWords[row.status] || row.status"
                    :tone="complaintStatusTone[row.status] || 'neutral'"
                  />
                </td>
                <td class="pa-c-num">{{ row.times || "1" }}</td>
                <td>
                  <button
                    type="button"
                    class="pa-btn"
                    data-action="detail"
                    @click="openDetail(row)"
                  >
                    处理投诉
                  </button>
                </td>
              </tr>
              <tr v-if="loaded && !rows.length">
                <td colspan="10"><KitEmpty text="暂无数据" /></td>
              </tr>
              <tr v-if="!loaded && !error">
                <td colspan="10"><KitEmpty text="正在加载…" /></td>
              </tr>
            </tbody>
          </table>
        </div>

        <KitPagination
          v-model:page="page"
          v-model:page-size="pageSize"
          :total="total"
          @change="loadList"
        />
      </section>
    </template>

    <!-- 投诉统计 -->
    <template v-else-if="view === 'charts'">
      <div
        class="stat-grid complain-stat-grid"
        data-test="complain-charts-summary"
      >
        <section class="panel stat" aria-label="今日投诉">
          <span class="stat-icon"><ShellIcon name="shield" /></span>
          <p>今日投诉</p>
          <strong>{{ summary?.today_count ?? "0" }}</strong>
          <span
            >支付宝 {{ summary?.today_alipay ?? "0" }} · 微信
            {{ summary?.today_wxpay ?? "0" }}</span
          >
        </section>
        <section class="panel stat" aria-label="昨日投诉">
          <span class="stat-icon"><ShellIcon name="shield" /></span>
          <p>昨日投诉</p>
          <strong>{{ summary?.yesterday_count ?? "0" }}</strong>
          <span
            >支付宝 {{ summary?.yesterday_alipay ?? "0" }} · 微信
            {{ summary?.yesterday_wxpay ?? "0" }}</span
          >
        </section>
        <section class="panel stat" aria-label="近7日投诉率">
          <span class="stat-icon"><ShellIcon name="chart" /></span>
          <p>近7日投诉率</p>
          <strong>{{ summary?.days7_rate ?? "-" }}</strong>
          <span>近7日投诉数 / 近7日支付订单数</span>
        </section>
      </div>
      <section class="panel records" aria-labelledby="complain-rank-title">
        <div class="table-tools">
          <h3 id="complain-rank-title" class="complain-table-title">
            投诉数据统计&排行榜
          </h3>
          <div class="table-tools-end">
            <button
              type="button"
              class="pa-btn"
              data-action="reload"
              :disabled="loading"
              @click="reload"
            >
              <ShellIcon name="refresh" />刷新
            </button>
          </div>
        </div>
        <div class="table-scroll">
          <table class="pa-table">
            <thead>
              <tr>
                <th v-for="c in chartColumns" :key="c.key">{{ c.label }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="(r, i) in chartRows" :key="i">
                <td
                  v-for="c in chartColumns"
                  :key="c.key"
                  :class="{ 'pa-c-num': c.key !== 'dimension' }"
                >
                  {{ chartCell(c.key, r[c.key]) }}
                </td>
              </tr>
              <tr v-if="loaded && !chartRows.length">
                <td :colspan="chartColumns.length || 1">
                  <KitEmpty text="暂无数据" />
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>
    </template>

    <!-- 同步任务 -->
    <template v-else>
      <KitFilterBar
        :loading="loading"
        :blocked="taskBusy"
        @query="queryTasks"
        @reset="resetTaskFilters"
      >
        <KitField label="通道ID"
          ><KitInput
            v-model="taskDraft.channelId"
            aria-label="通道ID"
            placeholder="请输入通道ID"
            inputmode="numeric"
            :disabled="loading || taskBusy"
        /></KitField>
        <KitField label="状态"
          ><KitSelect
            v-model="taskDraft.enabled"
            aria-label="状态"
            :options="taskStatusOptions"
            :disabled="loading || taskBusy"
        /></KitField>
      </KitFilterBar>
      <p class="alert info" role="note">
        在投诉记录页面选中某个支付通道后开启自动获取即可创建同步任务；在此可编辑每个通道的获取条件/频率/数量、启停或删除任务。另需在计划任务页面启动投诉同步任务。
      </p>
      <section class="panel records" aria-label="同步任务">
        <div class="table-tools">
          <button
            type="button"
            class="pa-btn pa-btn--danger"
            data-action="batch-delete"
            :disabled="taskBusy || selectedTasks.size === 0"
            @click="removeSelectedTasks"
          >
            批量删除{{ selectedTasks.size ? `（${selectedTasks.size}）` : "" }}
          </button>
          <div class="table-tools-end">
            <button
              type="button"
              class="pa-btn"
              data-action="reload"
              :disabled="loading || taskBusy"
              @click="reload"
            >
              <ShellIcon name="refresh" />刷新
            </button>
          </div>
        </div>
        <div class="table-scroll">
          <table class="pa-table">
            <thead>
              <tr>
                <th class="pa-c-sel">
                  <input
                    type="checkbox"
                    aria-label="全选"
                    :checked="allTasksSelected"
                    @change="toggleAllTasks"
                  />
                </th>
                <th>通道ID</th>
                <th>通道名称</th>
                <th>状态</th>
                <th>上次同步时间</th>
                <th>同步状态</th>
                <th>下次同步时间</th>
                <th>获取条件</th>
                <th>获取频率</th>
                <th>操作</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="t in visibleTasks" :key="t.channel_id">
                <td class="pa-c-sel">
                  <input
                    type="checkbox"
                    :aria-label="`选择通道 ${t.channel_id}`"
                    :checked="selectedTasks.has(t.channel_id)"
                    @change="toggleTaskSelection(t.channel_id)"
                  />
                </td>
                <td class="pa-c-num">{{ t.channel_id }}</td>
                <td>{{ t.name || "-" }}</td>
                <td>
                  <button
                    type="button"
                    class="pa-btn pa-btn--text"
                    data-action="toggle-status"
                    :disabled="taskBusy"
                    @click="toggleTaskStatus(t)"
                  >
                    <KitStatusTag
                      :text="t.enabled ? '已启用' : '已停用'"
                      :tone="t.enabled ? 'success' : 'neutral'"
                    />
                  </button>
                </td>
                <td>{{ t.last_run_at ? datetime(t.last_run_at) : "-" }}</td>
                <td>
                  <KitStatusTag
                    v-if="t.last_status"
                    :text="t.last_status === 'ok' ? '成功' : '失败'"
                    :tone="t.last_status === 'ok' ? 'success' : 'danger'"
                  />
                  <span v-else>-</span>
                  <small
                    v-if="t.last_status === 'failed' && t.consecutive_failures"
                    class="muted"
                    >连续失败 {{ t.consecutive_failures }} 次{{
                      t.last_reason ? "：" + t.last_reason : ""
                    }}</small
                  >
                </td>
                <td>{{ t.next_run_at ? datetime(t.next_run_at) : "-" }}</td>
                <td>{{ t.days > 0 ? t.days + " 天内有订单" : "不限" }}</td>
                <td>
                  {{
                    t.frequency ? t.frequency + " 分钟 / " + t.num + " 条" : "-"
                  }}
                </td>
                <td>
                  <button
                    type="button"
                    class="pa-btn"
                    data-action="edit-task"
                    :disabled="taskBusy"
                    @click="openTaskEdit(t)"
                  >
                    编辑
                  </button>
                  <button
                    type="button"
                    class="pa-btn pa-btn--danger"
                    data-action="delete-task"
                    :disabled="taskBusy"
                    @click="removeTask(t)"
                  >
                    删除
                  </button>
                </td>
              </tr>
              <tr v-if="loaded && !visibleTasks.length">
                <td colspan="10"><KitEmpty text="暂无数据" /></td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>
    </template>

    <!-- 处理投诉 -->
    <KitModal
      v-if="detail"
      title="处理投诉"
      :loading="detailBusy"
      submit-text="渠道处理并完成"
      drawer
      width="600px"
      @close="detail = null"
      @submit="submitProviderComplete"
    >
      <KitField label="投诉单号">
        <div class="pa-c-mono">{{ detail.id }}</div>
      </KitField>
      <KitField label="关联订单号">
        <div class="pa-c-mono">{{ detail.trade_no || "-" }}</div>
      </KitField>
      <KitField label="渠道子商户号">
        <div class="pa-c-mono">{{ detail.third_mch_id || "-" }}</div>
      </KitField>
      <KitField label="支付方式">
        <KitPayMethod v-if="detail.pay_type" :type="detail.pay_type" />
        <span v-else>-</span>
      </KitField>
      <KitField label="订单金额">
        <div>{{ amountCell(detail.amount) }}</div>
      </KitField>
      <KitField label="投诉次数">
        <div>{{ detail.times || "1" }}</div>
      </KitField>
      <KitField label="投诉原因">
        <div>{{ detail.subject || "-" }}</div>
      </KitField>
      <KitField label="投诉详情">
        <div class="complain-detail-full">{{ detail.content || "-" }}</div>
      </KitField>
      <KitField label="当前状态">
        <KitStatusTag
          :text="complaintStatusWords[detail.status] || detail.status"
          :tone="complaintStatusTone[detail.status] || 'neutral'"
        />
      </KitField>
      <KitField v-if="detail.images && detail.images.length" label="投诉图片">
        <div class="complain-images">
          <a
            v-for="(img, i) in detail.images"
            :key="i"
            :href="img"
            target="_blank"
            rel="noopener noreferrer"
            class="complain-image-link"
          >
            <img :src="img" :alt="`投诉图片 ${i + 1}`" loading="lazy" />
          </a>
        </div>
      </KitField>
      <KitField
        v-if="detail.negotiation && detail.negotiation.length"
        label="协商历史"
      >
        <ol class="complain-history">
          <li
            v-for="(turn, i) in detail.negotiation"
            :key="i"
            class="complain-history-turn"
          >
            <div class="complain-history-head">
              <b>{{ turn.operator || turn.role || "操作" }}</b>
              <small v-if="turn.created_at" class="muted">{{
                turn.created_at
              }}</small>
            </div>
            <div class="complain-history-body">{{ turn.content || "-" }}</div>
            <div
              v-if="turn.images && turn.images.length"
              class="complain-images complain-images--sm"
            >
              <a
                v-for="(img, j) in turn.images"
                :key="j"
                :href="img"
                target="_blank"
                rel="noopener noreferrer"
                class="complain-image-link"
              >
                <img :src="img" :alt="`协商图片 ${j + 1}`" loading="lazy" />
              </a>
            </div>
          </li>
        </ol>
      </KitField>
      <KitField label="处理结果">
        <div class="complain-presets">
          <button
            v-for="p in resultPresets"
            :key="p"
            type="button"
            class="pa-btn pa-btn--text"
            data-action="preset"
            @click="replyText = p"
          >
            {{ p }}
          </button>
        </div>
      </KitField>
      <KitField label="回复用户">
        <KitInput
          v-model="replyText"
          placeholder="回复投诉人/处理说明"
          aria-label="回复内容"
        />
      </KitField>
      <KitField label="退款审批（元）">
        <KitInput
          v-model="refundAmount"
          inputmode="decimal"
          placeholder="留空则不退款；退款经订单退款流程并需资金授权"
          aria-label="退款金额"
        />
      </KitField>
      <p class="muted">
        退款审批经 GOEPAY
        订单退款流程执行，管理员退款照常要求资金密码授权；修改状态本身不触发资金变更。
      </p>
      <template #footer-extra>
        <button
          type="button"
          class="pa-btn pa-btn--success"
          data-action="reply"
          :disabled="detailBusy"
          @click="submitProviderReply"
        >
          回复用户
        </button>
        <button
          v-if="detail.pay_type === 'alipay'"
          type="button"
          class="pa-btn"
          data-action="supplement"
          :disabled="detailBusy"
          @click="submitSupplement"
        >
          补充凭证
        </button>
        <button
          type="button"
          class="pa-btn pa-btn--danger"
          data-action="refund"
          :disabled="detailBusy || !detail.trade_no"
          @click="submitRefund"
        >
          退款审批
        </button>
      </template>
    </KitModal>

    <!-- 编辑同步任务 -->
    <KitModal
      v-if="editTask"
      title="编辑同步任务"
      :loading="taskBusy"
      submit-text="保存"
      @close="editTask = null"
      @submit="submitTaskEdit"
    >
      <KitField label="通道">
        <div>{{ editTask.channel_id }} · {{ editTask.name || "-" }}</div>
      </KitField>
      <KitField label="获取条件（天内有订单，0为不限）">
        <KitInput
          v-model="editDays"
          type="number"
          inputmode="numeric"
          aria-label="获取条件天数"
        />
      </KitField>
      <KitField label="获取频率（分钟，≥1）">
        <KitInput
          v-model="editFrequency"
          type="number"
          inputmode="numeric"
          aria-label="获取频率"
        />
      </KitField>
      <KitField label="每次同步获取数量（20-200）">
        <KitInput
          v-model="editNum"
          type="number"
          inputmode="numeric"
          aria-label="每次同步获取数量"
        />
      </KitField>
      <p class="muted">
        若投诉数量过多可适当增加每次获取数量；因投诉获取为循环翻页获取，数量设置过大可能会增加资源占用。
      </p>
    </KitModal>
  </div>
</template>

<style scoped>
/* The page's own notes sit in the 16px column gap like its cards. */
.complain-page > .alert {
  margin: 0;
}
/* Today / yesterday (and 7-day rate) as the dashboard's stat cards, one row. */
.complain-stat-grid {
  grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
}
.complain-presets {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}
/* The channel picker keeps a normal field width in the toolbar. */
.complain-channel-select {
  flex: 0 1 280px;
}
.complain-table-title {
  margin: 0;
  font-size: 14px;
  line-height: 32px;
}
/* No pager under these tables: the card edge closes the last row. */
.records > .table-scroll:last-child tbody tr:last-child td {
  border-bottom: 0;
}
.complain-detail {
  max-width: 260px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.complain-detail-full {
  white-space: pre-wrap;
  word-break: break-word;
}
td small.muted {
  display: block;
}
.pa-c-sel {
  width: 36px;
  text-align: center;
}
.complain-images {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}
.complain-image-link img {
  width: 84px;
  height: 84px;
  object-fit: cover;
  border: 1px solid var(--line, #e5e7eb);
  border-radius: 8px;
}
.complain-images--sm .complain-image-link img {
  width: 56px;
  height: 56px;
}
.complain-history {
  list-style: none;
  margin: 0;
  padding: 0;
  display: grid;
  gap: 10px;
}
.complain-history-turn {
  border: 1px solid var(--line, #e5e7eb);
  border-radius: 8px;
  padding: 8px 10px;
  display: grid;
  gap: 4px;
}
.complain-history-head {
  display: flex;
  justify-content: space-between;
  align-items: baseline;
  gap: 8px;
}
.complain-history-body {
  white-space: pre-wrap;
  word-break: break-word;
}
</style>
