<script setup lang="ts">
import {
  computed,
  nextTick,
  onBeforeUnmount,
  onMounted,
  ref,
  watch,
} from "vue";
import { ApiClient, ApiError } from "../api";
import {
  readScheduledCatalog,
  readScheduledTask,
  readScheduledRun,
  readScheduledHistory,
  scheduledTaskPath,
  scheduledTaskModeLabel,
  scheduledTaskChange,
  scheduledUTC8Input,
  type ScheduledTask,
  type ScheduledDraft,
  type ScheduledHistory,
} from "../scheduled-tasks";
import ResourcePage from "./ResourcePage.vue";
import OperationsPage from "./OperationsPage.vue";
import KitSwitch from "./KitSwitch.vue";
import KitSelect from "./KitSelect.vue";
import KitStatusTag from "./KitStatusTag.vue";
import { operationPages } from "../operations";
const props = defineProps<{ client: ApiClient; allowed: boolean }>();
const permitted = computed(
  () => props.allowed && props.client.area === "admin",
);
const items = ref<ScheduledTask[]>([]),
  loading = ref(false),
  catalogError = ref(""),
  notice = ref("");
const modal = ref<"config" | "run" | "history">(),
  key = ref(""),
  target = ref<ScheduledTask>(),
  detailLoading = ref(false),
  busy = ref(false),
  detailError = ref(""),
  uncertain = ref(false),
  confirmation = ref<ReturnType<typeof scheduledTaskChange>>(),
  runScheduled = ref(false);
const draft = ref<ScheduledDraft>({
    enabled: false,
    frequency: "minute",
    interval: "1",
    executeAt: "",
    params: {},
  }),
  history = ref<ScheduledHistory>(),
  before = ref("0"),
  previous = ref<string[]>([]);
const dialog = ref<HTMLElement>(),
  refresh = ref<HTMLButtonElement>(),
  legacyOpen = ref(false),
  queueOpen = ref(false);
let active = true,
  epoch = 0,
  controller: AbortController | undefined,
  opener: HTMLElement | undefined,
  contextClient = props.client,
  contextCSRF = props.client.csrfToken;
function reset() {
  ++epoch;
  controller?.abort();
  controller = undefined;
  items.value = [];
  modal.value = undefined;
  key.value = "";
  target.value = undefined;
  history.value = undefined;
  confirmation.value = undefined;
  busy.value = false;
  loading.value = false;
  detailLoading.value = false;
  uncertain.value = false;
  notice.value = "";
  detailError.value = "";
  catalogError.value = "";
  opener = undefined;
  legacyOpen.value = false;
  queueOpen.value = false;
  contextClient = props.client;
  contextCSRF = props.client.csrfToken;
}
function ensure() {
  if (
    props.client === contextClient &&
    props.client.csrfToken === contextCSRF &&
    permitted.value
  )
    return true;
  reset();
  catalogError.value = "会话已变更，请刷新任务列表后重新核对。";
  void nextTick(() => refresh.value?.focus());
  return false;
}
function begin() {
  controller?.abort();
  const c = new AbortController();
  controller = c;
  return { c, current: ++epoch, client: props.client };
}
function valid(current: number, c: AbortController) {
  return active && current === epoch && !c.signal.aborted && ensure();
}
const labelStatus = (s: string) =>
  ({
    never_run: "未执行",
    pending: "待运行",
    running: "执行中",
    lease_expired: "待恢复",
    success: "本轮已完成",
    failed: "执行失败",
    cancelled: "已取消",
  })[s] || "状态待核实";
const labelFrequency = (s: string) =>
  ({ second: "秒", minute: "分钟", hour: "小时", day: "天", once: "仅一次" })[
    s
  ] || s;
function displayTime(v: string | null) {
  if (!v) return "—";
  return scheduledUTC8Input(v).replace("T", " ");
}
function scheduleLabel(t: ScheduledTask) {
  if (!t.schedule) return "未配置";
  return t.schedule.frequency === "once"
    ? "仅一次"
    : "每 " + t.schedule.interval + " " + labelFrequency(t.schedule.frequency);
}
function taskStateShort(t: ScheduledTask) {
  if (!t.available) return "不可用";
  if (t.control_mode === "legacy_managed") return "未接管";
  if (!t.configured) return "未配置";
  return t.enabled ? "已开启" : "已关闭";
}
function taskStateBadge(t: ScheduledTask) {
  if (!t.available) return "none";
  if (t.control_mode === "legacy_managed") return "warn";
  if (!t.configured) return "none";
  return t.enabled ? "on" : "off";
}
function taskStateTone(
  t: ScheduledTask,
): "success" | "warning" | "danger" | "info" | "neutral" {
  const s = taskStateBadge(t);
  return s === "on" ? "success" : s === "warn" ? "warning" : "neutral";
}
async function load() {
  if (!ensure() || busy.value || modal.value) return;
  const { c, current, client } = begin();
  loading.value = true;
  catalogError.value = "";
  items.value = [];
  try {
    const raw = await client.request<unknown>("/scheduled-tasks", {
      signal: c.signal,
    });
    if (valid(current, c)) items.value = readScheduledCatalog(raw);
  } catch {
    if (valid(current, c))
      catalogError.value = "任务列表读取失败，请刷新重试。";
  } finally {
    if (valid(current, c)) loading.value = false;
  }
}
function initializeDraft(t: ScheduledTask) {
  draft.value = {
    enabled: t.enabled,
    frequency: t.schedule?.frequency || "minute",
    interval: String(t.schedule?.interval || 1),
    executeAt: scheduledUTC8Input(
      t.schedule?.execute_at ||
        new Date(Math.floor(Date.now() / 1000) * 1000 + 60000).toISOString(),
    ),
    params: Object.fromEntries(
      t.parameter_definitions.map((p) => [p.name, String(t.params[p.name])]),
    ),
  };
}
async function fresh() {
  if (!ensure() || !modal.value || busy.value) return false;
  const currentKey = key.value,
    { c, current, client } = begin();
  detailLoading.value = true;
  detailError.value = "";
  target.value = undefined;
  confirmation.value = undefined;
  uncertain.value = false;
  notice.value = "";
  runScheduled.value = false;
  try {
    const value = readScheduledTask(
      await client.request<unknown>(scheduledTaskPath(currentKey), {
        signal: c.signal,
      }),
    );
    if (!valid(current, c)) return false;
    if (value.task !== currentKey) throw Error("target mismatch");
    target.value = value;
    initializeDraft(value);
    return true;
  } catch {
    if (valid(current, c)) detailError.value = "任务详情读取失败，请刷新核对。";
    return false;
  } finally {
    if (valid(current, c)) {
      detailLoading.value = false;
      void nextTick(() => focusFirst());
    }
  }
}
async function open(
  type: "config" | "run" | "history",
  task: ScheduledTask,
  event: MouseEvent,
) {
  if (!ensure() || busy.value || modal.value) return;
  opener = event.currentTarget as HTMLElement;
  key.value = task.task;
  modal.value = type;
  detailError.value = "";
  notice.value = "";
  history.value = undefined;
  previous.value = [];
  before.value = "0";
  if (await fresh()) {
    if (type === "history") await loadHistory("0");
    void nextTick(() => focusFirst());
  }
}
function close() {
  if (busy.value) return;
  controller?.abort();
  ++epoch;
  modal.value = undefined;
  target.value = undefined;
  confirmation.value = undefined;
  detailError.value = "";
  notice.value = "";
  history.value = undefined;
  uncertain.value = false;
  const saved = opener;
  opener = undefined;
  void nextTick(() => {
    if (active && saved?.isConnected) saved.focus();
  });
}
function focusFirst() {
  const node = dialog.value;
  if (node && !node.contains(document.activeElement))
    node
      .querySelector<HTMLElement>(
        'input:not([disabled]),select:not([disabled]),button:not([disabled]),[tabindex="0"]',
      )
      ?.focus();
}
function keyboard(event: KeyboardEvent) {
  if (event.key === "Escape") {
    event.preventDefault();
    close();
    return;
  }
  if (event.key !== "Tab") return;
  const controls = Array.from(
    dialog.value?.querySelectorAll<HTMLElement>(
      'input:not([disabled]),select:not([disabled]),button:not([disabled]),[tabindex="0"]',
    ) || [],
  );
  if (!controls.length) {
    event.preventDefault();
    dialog.value?.focus();
    return;
  }
  const first = controls[0],
    last = controls.at(-1)!;
  if (event.shiftKey && document.activeElement === first) {
    event.preventDefault();
    last.focus();
  } else if (!event.shiftKey && document.activeElement === last) {
    event.preventDefault();
    first.focus();
  }
}
function review() {
  if (!ensure() || busy.value || uncertain.value || !target.value) return;
  detailError.value = "";
  try {
    confirmation.value = scheduledTaskChange(target.value, draft.value);
    void nextTick(() =>
      dialog.value?.querySelector<HTMLElement>("[data-confirm]")?.focus(),
    );
  } catch {
    detailError.value = "请核对频率、正整数间隔、UTC+8执行时间及各项参数范围。";
  }
}
function blocked() {
  return (
    busy.value || detailLoading.value || uncertain.value || runScheduled.value
  );
}
async function save() {
  if (
    !ensure() ||
    !target.value ||
    !confirmation.value ||
    blocked() ||
    !target.value.can_update
  )
    return;
  const original = target.value,
    body = JSON.parse(JSON.stringify(confirmation.value)) as ReturnType<
      typeof scheduledTaskChange
    >,
    { c, current, client } = begin();
  busy.value = true;
  detailError.value = "";
  try {
    const value = readScheduledTask(
      await client.request<unknown>(scheduledTaskPath(original.task), {
        method: "PATCH",
        body,
        signal: c.signal,
      }),
    );
    if (!valid(current, c)) return;
    if (
      value.task !== original.task ||
      !value.configured ||
      BigInt(value.revision) <= BigInt(original.revision) ||
      value.enabled !== body.enabled ||
      !value.schedule ||
      value.schedule.frequency !== body.schedule.frequency ||
      value.schedule.interval !== body.schedule.interval ||
      Date.parse(value.schedule.execute_at) !==
        Date.parse(body.schedule.execute_at) ||
      JSON.stringify(value.params) !== JSON.stringify(body.params)
    )
      throw Error("result mismatch");
    target.value = value;
    items.value = items.value.map((t) => (t.task === value.task ? value : t));
    confirmation.value = undefined;
    initializeDraft(value);
    notice.value = "任务配置已保存。";
  } catch (error) {
    if (valid(current, c)) {
      uncertain.value = true;
      confirmation.value = undefined;
      detailError.value =
        error instanceof ApiError && error.status === 409
          ? "资料已变更，请刷新核对后重新确认。"
          : "保存结果尚未确认，请刷新核对；不会自动再次提交。";
    }
  } finally {
    if (valid(current, c)) {
      busy.value = false;
      void nextTick(() => focusFirst());
    }
  }
}
async function run() {
  if (!ensure() || blocked() || !target.value?.can_run) return;
  const original = target.value,
    { c, current, client } = begin();
  busy.value = true;
  detailError.value = "";
  try {
    const value = readScheduledRun(
      await client.request<unknown>(scheduledTaskPath(original.task) + "/run", {
        method: "POST",
        body: { expected_fingerprint: original.fingerprint },
        signal: c.signal,
      }),
      original.task,
    );
    if (!valid(current, c)) return;
    if (value.trigger !== "manual" || value.status !== "pending")
      throw Error("not pending");
    runScheduled.value = true;
    notice.value = "已安排，等待调度器执行（编号 " + value.id + "）。";
  } catch (error) {
    if (valid(current, c)) {
      uncertain.value = true;
      detailError.value =
        error instanceof ApiError && error.status === 409
          ? "任务状态已变更，请刷新核对执行记录后重新确认。"
          : "安排结果尚未确认，请刷新核对执行记录；不会自动再次安排。";
    }
  } finally {
    if (valid(current, c)) {
      busy.value = false;
      void nextTick(() => focusFirst());
    }
  }
}
async function loadHistory(cursor: string) {
  if (!ensure() || !target.value || busy.value) return;
  const currentKey = key.value,
    { c, current, client } = begin();
  detailLoading.value = true;
  history.value = undefined;
  detailError.value = "";
  try {
    const value = readScheduledHistory(
      await client.request<unknown>(
        scheduledTaskPath(currentKey) + "/runs?limit=20&before_id=" + cursor,
        { signal: c.signal },
      ),
      currentKey,
    );
    if (valid(current, c)) {
      history.value = value;
      before.value = cursor;
    }
  } catch {
    if (valid(current, c)) detailError.value = "执行记录读取失败，请刷新核对。";
  } finally {
    if (valid(current, c)) detailLoading.value = false;
  }
}
async function nextPage() {
  if (
    !history.value ||
    history.value.next_cursor === "0" ||
    history.value.total <= history.value.items.length
  )
    return;
  const cursor = history.value.next_cursor;
  previous.value.push(before.value);
  await loadHistory(cursor);
}
async function previousPage() {
  const cursor = previous.value.pop();
  if (cursor !== undefined) await loadHistory(cursor);
}
async function refreshDetail() {
  if (await fresh()) {
    if (modal.value === "history") await loadHistory(before.value);
  }
}
watch(
  () => [props.client, props.client.csrfToken, props.allowed],
  () => {
    reset();
    if (permitted.value) void load();
  },
  { flush: "sync" },
);
onMounted(() => {
  if (permitted.value) void load();
});
onBeforeUnmount(() => {
  active = false;
  ++epoch;
  controller?.abort();
});
</script>

<template>
  <section class="task-scheduler">
    <h2>任务列表</h2>
    <p v-if="!permitted" role="status">
      仅本人原超级管理员会话可以核对和配置计划任务。
    </p>
    <template v-else>
      <div class="task-toolbar">
        <p>
          时间均为 UTC+8。立即运行只安排一次待执行任务，完成结果以执行记录为准。
        </p>
        <button ref="refresh" :disabled="loading || !!modal" @click="load">
          <ShellIcon name="refresh" />刷新任务列表
        </button>
      </div>
      <p class="muted">
        未接管的任务仍由原 worker
        及其部署设置管理。首次保存后，开关和频率接管对应的定时业务；已领取的原业务可能继续完成。
      </p>
      <p v-if="loading" role="status">正在读取计划任务…</p>
      <p v-if="catalogError" role="alert" class="task-error">
        {{ catalogError }}
      </p>
      <div v-if="items.length" class="job-cards">
        <article v-for="item in items" :key="item.task" class="job-card">
          <header class="job-card-head">
            <strong class="job-name">{{ item.name }}</strong>
            <KitStatusTag
              :text="taskStateShort(item)"
              :tone="taskStateTone(item)"
            />
          </header>
          <p class="job-key muted">{{ item.task }}</p>
          <p class="job-mode muted">{{ scheduledTaskModeLabel(item) }}</p>
          <p v-if="!item.available" class="job-unavailable muted">
            {{ item.unavailable_reason }}
          </p>
          <dl class="job-meta">
            <dt>执行频率</dt>
            <dd>{{ scheduleLabel(item) }}</dd>
            <dt>上次执行</dt>
            <dd>{{ displayTime(item.last_run_time) }}</dd>
            <dt>下次执行</dt>
            <dd>{{ displayTime(item.next_run_time) }}</dd>
            <dt>最近状态</dt>
            <dd>{{ labelStatus(item.last_status) }}</dd>
          </dl>
          <footer class="job-card-actions">
            <button
              :disabled="!item.can_update"
              @click="open('config', item, $event)"
            >
              配置</button
            ><button
              :disabled="!item.can_run"
              @click="open('run', item, $event)"
            >
              立即运行</button
            ><button @click="open('history', item, $event)">执行记录</button>
          </footer>
        </article>
      </div>
      <div class="task-diagnostics table-tools">
        <button class="soft-button" @click="legacyOpen = !legacyOpen">
          {{ legacyOpen ? "收起旧计划资料" : "旧计划资料（只读）" }}</button
        ><button class="soft-button" @click="queueOpen = !queueOpen">
          {{ queueOpen ? "收起任务队列" : "查看任务队列（诊断）" }}
        </button>
      </div>
      <template v-if="legacyOpen"
        ><h3>旧计划资料（只读）</h3>
        <p>仅保留原资料用于核查，不作为当前任务开关或执行结果。</p>
        <ResourcePage :client="client" resource="cron-tasks"
      /></template>
      <template v-if="queueOpen"
        ><h3>任务队列诊断</h3>
        <p>队列状态不等同于计划任务执行状态。</p>
        <OperationsPage
          :client="client"
          :operation="operationPages['/system/cron']"
          :own-session="true"
          admin-class="super"
      /></template>
    </template>
    <div v-if="modal" class="task-backdrop">
      <section
        ref="dialog"
        class="task-dialog"
        role="dialog"
        aria-modal="true"
        aria-labelledby="scheduler171-dialog-title"
        tabindex="-1"
        @keydown="keyboard"
      >
        <header>
          <h3 id="scheduler171-dialog-title">
            {{
              modal === "config"
                ? "配置任务"
                : modal === "run"
                  ? "安排立即运行"
                  : "执行记录"
            }}<span v-if="target"> · {{ target.name }}</span>
          </h3>
          <button :disabled="busy" @click="close">关闭</button>
        </header>
        <p v-if="detailLoading" role="status">正在核对当前任务…</p>
        <p v-if="detailError" role="alert" class="task-error">
          {{ detailError }}
        </p>
        <p v-if="notice" role="status" class="task-notice">{{ notice }}</p>
        <template v-if="target && !detailLoading">
          <p>{{ target.description }}</p>
          <p v-if="!target.available">{{ target.unavailable_reason }}</p>
          <template v-if="modal === 'config'">
            <p>
              当前：{{
                scheduledTaskModeLabel(target)
              }}。保存开关不授予资金或对外发送权限，仍须满足原业务授权及运行配置。
            </p>
            <form v-if="!confirmation" @submit.prevent="review">
              <fieldset :disabled="blocked() || !target.can_update">
                <div class="task-fields">
                  <div class="task-check">
                    <KitSwitch v-model="draft.enabled" label="开启此任务" />
                  </div>
                  <label for="scheduler171-frequency"
                    >执行频率<KitSelect
                      v-model="draft.frequency"
                      :options="[
                        { value: 'second', label: '秒' },
                        { value: 'minute', label: '分钟' },
                        { value: 'hour', label: '小时' },
                        { value: 'day', label: '天' },
                        { value: 'once', label: '仅一次' },
                      ]"
                      aria-label="执行频率"
                  /></label>
                  <label for="scheduler171-interval"
                    >间隔<input
                      id="scheduler171-interval"
                      v-model="draft.interval"
                      type="number"
                      min="1"
                      step="1"
                      inputmode="numeric"
                  /></label>
                  <label for="scheduler171-at"
                    >首次执行时间（UTC+8）<input
                      id="scheduler171-at"
                      v-model="draft.executeAt"
                      type="datetime-local"
                      step="1"
                      required
                  /></label>
                  <label
                    v-for="p in target.parameter_definitions"
                    :key="p.name"
                    :for="'scheduler171-param-' + p.name"
                    >{{ p.label
                    }}<input
                      :id="'scheduler171-param-' + p.name"
                      v-model="draft.params[p.name]"
                      type="number"
                      :min="p.min"
                      :max="p.max ?? undefined"
                      step="1"
                      :required="p.required"
                      inputmode="numeric"
                    /><small
                      >默认 {{ p.default }}；最小 {{ p.min
                      }}<template v-if="p.max !== null"
                        >；最大 {{ p.max }}</template
                      ></small
                    ></label
                  >
                </div>
              </fieldset>
              <p>
                按首次时间计算下一次安排；错过的旧周期不会突发补跑。人工安排不推进自动周期。
              </p>
              <button type="submit" :disabled="blocked() || !target.can_update">
                核对配置
              </button>
            </form>
            <div v-else class="task-confirm">
              <h4>确认保存配置</h4>
              <p>
                {{ confirmation.enabled ? "开启" : "关闭" }} ·
                {{
                  confirmation.schedule.frequency === "once"
                    ? "仅一次"
                    : "每 " +
                      confirmation.schedule.interval +
                      " " +
                      labelFrequency(confirmation.schedule.frequency)
                }}
                · 首次 {{ displayTime(confirmation.schedule.execute_at) }}
              </p>
              <p v-for="p in target.parameter_definitions" :key="p.name">
                {{ p.label }}：{{ confirmation.params[p.name] }}
              </p>
              <p>保存后取消尚未领取的旧安排，正在执行的原业务不会因此回滚。</p>
              <button data-confirm :disabled="blocked()" @click="save">
                确认保存</button
              ><button :disabled="busy" @click="confirmation = undefined">
                返回编辑</button
              ><button :disabled="busy" @click="close">取消</button>
            </div>
          </template>
          <template v-if="modal === 'run'">
            <p>
              确认安排本任务的一次运行。此操作不会直接执行付款或发送通知，也不代表业务已完成。
            </p>
            <p v-if="!target.can_run && !runScheduled">
              当前不能安排：请先核对任务开关、已有待执行安排及执行中状态。
            </p>
            <button
              v-if="!runScheduled"
              data-confirm
              :disabled="blocked() || !target.can_run"
              @click="run"
            >
              确认安排</button
            ><button :disabled="busy" @click="close">取消</button>
          </template>
          <template v-if="modal === 'history' && history"
            ><p>当前页范围共 {{ history.total }} 条；记录编号保持完整。</p>
            <p v-if="!history.items.length">暂无执行记录。</p>
            <ol class="task-history">
              <li v-for="row in history.items" :key="row.id">
                <strong
                  >{{ labelStatus(row.status) }} ·
                  {{
                    row.trigger === "manual" ? "人工安排" : "自动周期"
                  }}</strong
                ><code>{{ row.id }}</code>
                <dl>
                  <dt>计划时间</dt>
                  <dd>{{ displayTime(row.scheduled_for) }}</dd>
                  <dt>真实开始</dt>
                  <dd>{{ displayTime(row.started_at) }}</dd>
                  <dt>真实完成</dt>
                  <dd>{{ displayTime(row.completed_at) }}</dd>
                </dl>
                <p v-if="row.counts">
                  核对 {{ row.counts.examined }} · 奖励
                  {{ row.counts.awarded }} · 生成 {{ row.counts.generated }} ·
                  登记 {{ row.counts.enqueued }} · 跳过
                  {{ row.counts.skipped }} · 待独立授权
                  {{ row.counts.awaiting_authorization }}
                </p>
                <p v-if="row.error_code">
                  {{
                    row.error_code === "business_state_changed"
                      ? "原业务条件已变化，请核对原业务记录。"
                      : row.error_code === "config_changed"
                        ? "配置变化，旧安排已取消。"
                        : "执行失败，请核对原业务及运行配置。"
                  }}
                </p>
              </li>
            </ol>
            <nav aria-label="执行记录分页">
              <button
                :disabled="!previous.length || detailLoading"
                @click="previousPage"
              >
                上一页</button
              ><button
                :disabled="
                  history.next_cursor === '0' ||
                  history.total <= history.items.length ||
                  detailLoading
                "
                @click="nextPage"
              >
                下一页
              </button>
            </nav></template
          >
        </template>
        <footer>
          <button :disabled="busy || detailLoading" @click="refreshDetail">
            刷新核对</button
          ><button
            v-if="!confirmation && modal === 'config'"
            :disabled="busy"
            @click="close"
          >
            取消
          </button>
        </footer>
      </section>
    </div>
  </section>
</template>

<style scoped>
.task-toolbar,
.task-dialog header,
.task-actions,
.task-diagnostics,
.task-dialog nav,
.task-dialog footer {
  display: flex;
  align-items: center;
  gap: 0.65rem;
  flex-wrap: wrap;
}
.task-toolbar {
  justify-content: space-between;
}
.task-toolbar p {
  margin: 0;
}
.muted,
.task-fields small {
  color: var(--ink-muted);
}
.job-cards {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(320px, 1fr));
  gap: var(--space-4);
  margin-top: var(--space-4);
}
.job-card {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
  border: 1px solid var(--line);
  border-radius: var(--radius-sm);
  background: var(--bg-card);
  padding: var(--space-4);
  min-width: 0;
}
.job-card-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: var(--space-2);
}
.job-name {
  min-width: 0;
  overflow-wrap: anywhere;
}
.job-key,
.job-mode,
.job-unavailable {
  margin: 0;
  font-size: 12px;
  overflow-wrap: anywhere;
}
.job-meta {
  display: grid;
  grid-template-columns: auto 1fr;
  gap: 4px var(--space-3);
  margin: var(--space-2) 0 0;
  font-size: 0.85rem;
}
.job-meta dt {
  color: var(--ink-muted);
  white-space: nowrap;
}
.job-meta dd {
  margin: 0;
  overflow-wrap: anywhere;
}
.job-card-actions {
  display: flex;
  gap: 0.5rem;
  flex-wrap: wrap;
  margin-top: var(--space-3);
  padding-top: var(--space-3);
  border-top: 1px solid var(--line);
}
.task-scheduler button {
  cursor: pointer;
  min-height: 2.3rem;
  border: 1px solid var(--line-control);
  border-radius: 0.35rem;
  background: var(--bg-card);
  color: var(--ink);
  padding: 0.4rem 0.7rem;
}
.task-scheduler button:hover:not(:disabled) {
  background: var(--primary-soft);
  border-color: var(--primary-text);
}
.task-scheduler button:focus-visible,
.task-fields input:focus-visible,
.task-fields select:focus-visible {
  outline: 3px solid var(--primary-text);
  outline-offset: 2px;
}
.task-scheduler button:disabled {
  cursor: default;
  opacity: 0.55;
}
.task-diagnostics {
  margin-top: 1.2rem;
}
.task-error {
  color: var(--danger-text);
  overflow-wrap: anywhere;
}
.task-notice {
  color: var(--success-text);
  overflow-wrap: anywhere;
}
.task-backdrop {
  position: fixed;
  inset: 0;
  background: var(--scrim);
  z-index: 1300;
  display: grid;
  place-items: center;
  padding: 1rem;
}
.task-dialog {
  width: min(46rem, 100%);
  max-height: 88vh;
  overflow: auto;
  background: var(--bg-card);
  border-radius: 0.55rem;
  padding: 1.2rem;
  box-shadow: var(--shadow-modal);
  overflow-wrap: anywhere;
}
.task-dialog header {
  justify-content: space-between;
  align-items: flex-start;
}
.task-dialog header h3 {
  margin: 0.2rem 0;
  flex: 1;
  min-width: 0;
}
.task-dialog footer {
  border-top: 1px solid var(--line);
  padding-top: 1rem;
  margin-top: 1rem;
  justify-content: flex-end;
}
.task-fields {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 1rem;
}
.task-fields label {
  display: flex;
  flex-direction: column;
  gap: 0.35rem;
  min-width: 0;
}
.task-fields input,
.task-fields select {
  width: 100%;
  min-width: 0;
  box-sizing: border-box;
  padding: 0.5rem;
  border: 1px solid var(--line-control);
  border-radius: 0.3rem;
  background: var(--bg-card);
}
.task-fields .task-check {
  flex-direction: row;
  align-items: center;
  grid-column: 1/-1;
}
.task-check input {
  width: auto;
}
.task-dialog fieldset {
  padding: 0;
  border: 0;
  margin: 1rem 0;
}
.task-confirm {
  padding: 1rem;
  border: 1px solid var(--line-control);
  background: var(--bg-raised);
  border-radius: 0.35rem;
}
.task-confirm button {
  margin: 0.25rem;
}
.task-history {
  list-style: none;
  padding: 0;
}
.task-history li {
  padding: 1rem;
  border: 1px solid var(--line);
  border-radius: 0.35rem;
  margin: 0.8rem 0;
}
.task-history code {
  display: block;
  overflow-wrap: anywhere;
  margin-top: 0.35rem;
}
.task-history dl {
  display: grid;
  grid-template-columns: 6rem 1fr;
  gap: 0.3rem;
  margin: 0.7rem 0;
}
.task-history dd {
  margin: 0;
}
.task-history p {
  font-size: 0.9rem;
  margin-bottom: 0;
}
@media (max-width: 680px) {
  .task-fields {
    grid-template-columns: 1fr;
  }
  .task-dialog {
    padding: 0.9rem;
    max-height: 90vh;
  }
  .task-backdrop {
    padding: 0.5rem;
  }
  .task-history dl {
    grid-template-columns: 1fr;
  }
  .task-history dd {
    margin-bottom: 0.3rem;
  }
}
</style>
