<script setup lang="ts">
import { ref, computed, onMounted, onBeforeUnmount } from "vue";
import { ApiClient, displayValue } from "../api";
import type { Operation } from "../operations";
import { canRetryJob } from "../operations";
import RuntimeLogPage from "./RuntimeLogPage.vue";
import SystemLogPage from "./SystemLogPage.vue";
const props = defineProps<{
  client: ApiClient;
  operation: Operation;
  ownSession?: boolean;
  adminClass?: string;
}>();
const items = ref<Record<string, unknown>[]>([]),
  total = ref(0),
  page = ref(1),
  loading = ref(false),
  busy = ref(false),
  error = ref(""),
  notice = ref(""),
  cutoffDate = ref(""),
  preview = ref<Record<string, unknown> | null>(null),
  confirm = ref(false);
let controller: AbortController | undefined;
const labels: Record<string, string> = {
  id: "编号",
  username: "账号",
  role: "身份",
  ip: "登录地址",
  success: "成功",
  created_at: "发生时间",
  actor: "操作者",
  resource: "业务资源",
  action: "操作",
  target_id: "目标编号",
  kind: "任务类型",
  status: "状态",
  attempts: "已尝试次数",
  last_error: "最近错误",
  next_run_at: "下次尝试",
  name: "项目",
  value: "当前值",
  description: "说明",
  actor_id: "操作者 ID",
  subject_id: "账户 ID",
  resource_id: "资源编号",
  attempt_count: "尝试次数",
  run_at: "运行时间",
  provider: "通道提供方",
  enabled: "启用状态",
};
const columns = computed(() =>
  [...new Set(items.value.flatMap(Object.keys))]
    .filter((key) => key !== "can_retry")
    .map((key) => ({ key, label: labels[key] || key })),
);
async function load() {
  if (props.operation === "cleanup") return;
  controller?.abort();
  controller = new AbortController();
  const c = controller;
  loading.value = true;
  error.value = "";
  try {
    const data = await props.client.request<{
      items: Record<string, unknown>[];
      total: number;
    }>(`/operations/${props.operation}?page=${page.value}&limit=20`, {
      signal: c.signal,
    });
    if (!c.signal.aborted) {
      items.value = data.items;
      total.value = data.total;
    }
  } catch (e) {
    if (!c.signal.aborted)
      error.value = e instanceof Error ? e.message : "操作信息读取失败";
  } finally {
    if (!c.signal.aborted) loading.value = false;
  }
}
async function retry(row: Record<string, unknown>) {
  busy.value = true;
  error.value = "";
  try {
    await props.client.request(
      `/operations/jobs/${encodeURIComponent(String(row.id))}/retry`,
      { method: "POST", body: {} },
    );
    notice.value = "任务已重新排队，请核实后续执行结果";
    await load();
  } catch (e) {
    error.value = e instanceof Error ? e.message : "任务重试失败";
  } finally {
    busy.value = false;
  }
}
async function inspectCleanup() {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(cutoffDate.value)) {
    error.value = "请选择清理截止日期";
    return;
  }
  busy.value = true;
  error.value = "";
  preview.value = null;
  try {
    preview.value = await props.client.request<Record<string, unknown>>(
      "/operations/cleanup",
      {
        method: "POST",
        body: { preview: true, cutoff: cutoffDate.value + "T00:00:00+08:00" },
      },
    );
  } catch (e) {
    error.value = e instanceof Error ? e.message : "清理预览失败";
  } finally {
    busy.value = false;
  }
}
async function cleanup() {
  if (!preview.value) return;
  busy.value = true;
  error.value = "";
  try {
    await props.client.request("/operations/cleanup", {
      method: "POST",
      body: { preview: false, cutoff: preview.value.cutoff },
    });
    notice.value = "过期登录状态已清理";
    preview.value = null;
    confirm.value = false;
  } catch (e) {
    error.value = e instanceof Error ? e.message : "清理失败";
  } finally {
    busy.value = false;
  }
}
onMounted(() => {
  if (!["runtime-log", "system-log"].includes(props.operation)) void load();
});
onBeforeUnmount(() => controller?.abort());
</script>
<template>
  <SystemLogPage v-if="operation === 'system-log'" :client="client"
    ><template #login-audit
      ><OperationsPage
        :client="client"
        operation="login-log"
        :own-session="ownSession"
        :admin-class="adminClass" /></template
  ></SystemLogPage>
  <RuntimeLogPage
    v-else-if="operation === 'runtime-log'"
    :client="client"
    :own-session="ownSession"
    :admin-class="adminClass"
    ><template #audit
      ><OperationsPage
        :client="client"
        operation="audit-log"
        :own-session="ownSession"
        :admin-class="adminClass" /></template
  ></RuntimeLogPage>
  <section
    v-else
    class="panel records"
    :class="{ 'cleanup-card': operation === 'cleanup' }"
  >
    <div v-if="error" class="alert error" role="alert">{{ error }}</div>
    <div v-if="notice" class="alert success" role="status">{{ notice }}</div>
    <template v-if="operation === 'cleanup'"
      ><div class="account-section">
        <h2>清理过期登录状态</h2>
        <p class="muted">
          仅清理截止日期前已过期的登录会话和登录尝试计数。交易、账本和业务记录不在清理范围内。
        </p>
        <form
          class="report-filter toolbar cleanup-form"
          @submit.prevent="inspectCleanup"
        >
          <label class="filter-inline"
            >清理截止日期（UTC+08:00）<input
              v-model="cutoffDate"
              type="date"
              required
              @change="
                preview = null;
                confirm = false;
              " /></label
          ><button class="primary" :disabled="busy">预览清理范围</button>
        </form>
        <dl v-if="preview" class="record-detail">
          <dt>截止时间</dt>
          <dd>{{ displayValue(preview.cutoff) }}</dd>
          <dt>过期会话</dt>
          <dd>{{ displayValue(preview.expired_sessions) }}</dd>
          <dt>过期尝试计数</dt>
          <dd>{{ displayValue(preview.expired_attempts) }}</dd>
        </dl>
        <button
          v-if="preview"
          class="danger-button"
          :disabled="busy"
          @click="confirm = true"
        >
          清理以上过期登录状态
        </button>
      </div></template
    ><template v-else
      ><div class="toolbar">
        <p v-if="operation === 'system-info'" class="muted">
          当前部署构建与实际加载能力。本页面提供运行信息，不执行 PHP
          插件或在线覆盖更新。
        </p>
        <span v-else class="muted">{{
          operation === "jobs"
            ? "仅允许重新排队支付通知和结果核实任务，不重新提交支付、退款或转账。"
            : "只显示有权访问的运行记录。"
        }}</span
        ><button :disabled="loading" @click="load">刷新</button>
      </div>
      <div v-if="loading" class="loading-row" role="status">
        正在读取运行信息…
      </div>
      <div v-else class="table-scroll">
        <table>
          <thead>
            <tr>
              <th v-for="column in columns" :key="column.key">
                {{ column.label }}
              </th>
              <th v-if="operation === 'jobs'">操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="(row, index) in items" :key="String(row.id ?? index)">
              <td v-for="column in columns" :key="column.key">
                {{ displayValue(row[column.key]) }}
              </td>
              <td v-if="operation === 'jobs'">
                <button
                  v-if="canRetryJob(row)"
                  :disabled="busy"
                  @click="retry(row)"
                >
                  重新排队</button
                ><span v-else class="muted">无需操作</span>
              </td>
            </tr>
            <tr v-if="!items.length">
              <td :colspan="columns.length + 1" class="empty">暂无记录</td>
            </tr>
          </tbody>
        </table>
      </div>
      <div class="pagination">
        <span>共 {{ total }} 条记录</span>
        <div>
          <button
            :disabled="page <= 1 || loading"
            @click="
              page--;
              load();
            "
          >
            上一页</button
          ><span>第 {{ page }} 页</span
          ><button
            :disabled="page * 20 >= total || loading"
            @click="
              page++;
              load();
            "
          >
            下一页
          </button>
        </div>
      </div></template
    >
  </section>
  <div v-if="confirm" class="modal-backdrop">
    <section
      class="modal panel"
      role="dialog"
      aria-modal="true"
      aria-label="确认清理过期状态"
    >
      <h2>确认清理过期状态</h2>
      <p>将按已预览的截止时间清理过期登录状态。请核对范围后执行。</p>
      <div class="modal-actions">
        <button :disabled="busy" @click="confirm = false">取消</button
        ><button class="danger-button" :disabled="busy" @click="cleanup">
          {{ busy ? "正在清理…" : "确认清理" }}
        </button>
      </div>
    </section>
  </div>
</template>
<style scoped>
.cleanup-card {
  max-width: 720px;
}
.cleanup-form {
  align-items: flex-end;
  justify-content: flex-start;
}
.cleanup-form .filter-inline {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 6px;
  font-size: 13px;
  color: var(--ink-muted);
}
.cleanup-form .filter-inline input {
  min-width: 200px;
}
</style>
