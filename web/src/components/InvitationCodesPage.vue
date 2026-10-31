<script lang="ts">
type BatchIntent = { request_id: string; count: number };
type ClearIntent = { request_id: string; scope: "all" | "used"; confirm: true };
const invitationIntents = new WeakMap<
  object,
  Map<string, { batch: BatchIntent | null; clear: ClearIntent | null }>
>();
</script>
<script setup lang="ts">
import {
  computed,
  ref,
  onMounted,
  onBeforeUnmount,
  watch,
  nextTick,
} from "vue";
import { ApiClient, ApiError } from "../api";
import KitPagination from "./KitPagination.vue";
interface Invitation {
  id: string;
  code: string;
  status: "used" | "unused";
  enabled: boolean;
  max_uses: string;
  use_count: number;
  created_at: string;
  used_at: string | null;
  used_by: string;
  uses: { merchant_id: string; used_at: string }[];
}
const props = defineProps<{ client: ApiClient; ownSession: boolean }>();
const items = ref<Invitation[]>([]),
  total = ref(0),
  page = ref(1),
  limit = ref(20),
  search = ref(""),
  status = ref(""),
  loading = ref(false),
  busy = ref(false),
  error = ref(""),
  notice = ref("");
const mode = ref<"" | "generate" | "result" | "clear" | "delete">(""),
  count = ref("1"),
  scope = ref<"all" | "used">("all"),
  selected = ref<Invitation | null>(null),
  result = ref<{ id: string; code: string }[]>([]),
  unknown = ref(false);
const canWrite = computed(
  () => props.client.area === "admin" && props.ownSession,
);
const dialog = ref<HTMLElement | null>(null);
let focusBefore: HTMLElement | null = null,
  controller: AbortController | undefined,
  alive = true;
let sessionIntents = invitationIntents.get(props.client);
if (!sessionIntents) {
  sessionIntents = new Map();
  invitationIntents.set(props.client, sessionIntents);
}
const originalSession = props.client.csrfToken;
const savedIntents = sessionIntents.get(originalSession);
const batch = ref<BatchIntent | null>(savedIntents?.batch || null),
  clearRequest = ref<ClearIntent | null>(savedIntents?.clear || null);
if (batch.value) {
  count.value = String(batch.value.count);
  unknown.value = true;
}
if (clearRequest.value) {
  scope.value = clearRequest.value.scope;
  unknown.value = true;
}
function remember() {
  if (batch.value || clearRequest.value)
    sessionIntents!.set(originalSession, {
      batch: batch.value,
      clear: clearRequest.value,
    });
  else sessionIntents!.delete(originalSession);
}
const title = computed(
  () =>
    ({
      generate: "生成邀请码",
      result: "生成结果",
      clear: scope.value === "used" ? "清空已使用的邀请码" : "清空所有邀请码",
      delete: "删除邀请码",
      "": "",
    })[mode.value],
);
function time(value: string | null) {
  if (!value) return "—";
  const d = new Date(value);
  return Number.isNaN(d.getTime())
    ? "—"
    : d.toLocaleString("zh-CN", { hour12: false });
}
async function load() {
  controller?.abort();
  const c = new AbortController();
  controller = c;
  loading.value = true;
  error.value = "";
  try {
    const q = new URLSearchParams({
      page: String(page.value),
      limit: String(limit.value),
      search: search.value,
      status: status.value,
    });
    const data = await props.client.request<{
      items: Invitation[];
      total: number;
    }>("/invitation-codes?" + q, { signal: c.signal });
    if (!alive || c.signal.aborted) return;
    items.value = data.items;
    total.value = data.total;
  } catch (e) {
    if (alive && !c.signal.aborted)
      error.value = e instanceof Error ? e.message : "读取邀请码失败";
  } finally {
    if (alive && controller === c) loading.value = false;
  }
}
function query() {
  page.value = 1;
  void load();
}
function openGenerate() {
  if (!canWrite.value || busy.value) return;
  if (!batch.value) {
    count.value = "1";
    unknown.value = false;
  }
  error.value = "";
  mode.value = "generate";
}
function openClear(s: "all" | "used") {
  if (!canWrite.value || busy.value) return;
  if (clearRequest.value && clearRequest.value.scope !== s) {
    error.value = "请先核对原清空请求的结果";
    return;
  }
  scope.value = s;
  unknown.value = !!clearRequest.value;
  error.value = "";
  mode.value = "clear";
}
function openDelete(item: Invitation) {
  if (!canWrite.value || busy.value) return;
  selected.value = item;
  unknown.value = false;
  error.value = "";
  mode.value = "delete";
}
function close() {
  if (busy.value) return;
  mode.value = "";
  error.value = "";
}
function failed(e: unknown) {
  unknown.value = !(
    e instanceof ApiError &&
    e.status >= 400 &&
    e.status < 500 &&
    e.status !== 408 &&
    e.status !== 429
  );
  error.value = unknown.value
    ? "结果未知，请使用原请求核对；不要另外生成或重复清空。"
    : e instanceof Error
      ? e.message
      : "操作失败";
}
async function generate() {
  if (!canWrite.value || busy.value) return;
  if (!batch.value) {
    if (!/^[1-9]\d{0,3}$/.test(count.value) || Number(count.value) > 1000) {
      error.value = "生成个数必须是1–1000的整数";
      return;
    }
    batch.value = {
      request_id: crypto.randomUUID(),
      count: Number(count.value),
    };
  }
  remember();
  busy.value = true;
  error.value = "";
  try {
    const data = await props.client.request<{
      items: { id: string; code: string }[];
      replayed: boolean;
    }>("/invitation-codes/generate", { method: "POST", body: batch.value });
    if (!alive) return;
    result.value = data.items;
    batch.value = null;
    remember();
    unknown.value = false;
    mode.value = "result";
    void load();
  } catch (e) {
    if (alive) {
      failed(e);
      if (!unknown.value) batch.value = null;
      remember();
    }
  } finally {
    if (alive) busy.value = false;
  }
}
async function clear() {
  if (!canWrite.value || busy.value) return;
  clearRequest.value ??= {
    request_id: crypto.randomUUID(),
    scope: scope.value,
    confirm: true,
  };
  remember();
  busy.value = true;
  error.value = "";
  try {
    const data = await props.client.request<{
      archived: number;
      remaining: number;
    }>("/invitation-codes/clear", { method: "POST", body: clearRequest.value });
    if (!alive) return;
    notice.value =
      `已清空 ${data.archived} 个邀请码，保留注册凭据。` +
      (data.remaining
        ? ` 当前范围还剩 ${data.remaining} 个，需要再次确认才会处理下一批。`
        : "");
    clearRequest.value = null;
    remember();
    unknown.value = false;
    mode.value = "";
    page.value = 1;
    void load();
  } catch (e) {
    if (alive) {
      failed(e);
      if (!unknown.value) clearRequest.value = null;
      remember();
    }
  } finally {
    if (alive) busy.value = false;
  }
}
async function remove() {
  if (!canWrite.value || busy.value || !selected.value) return;
  busy.value = true;
  error.value = "";
  try {
    await props.client.request(
      "/resources/invitation-codes/" + encodeURIComponent(selected.value.id),
      { method: "DELETE" },
    );
    if (!alive) return;
    notice.value = "邀请码已停用并从列表移除，注册凭据保留。";
    mode.value = "";
    void load();
  } catch (e) {
    if (alive) failed(e);
  } finally {
    if (alive) busy.value = false;
  }
}
async function copy() {
  try {
    await navigator.clipboard.writeText(
      result.value.map((r) => r.code).join("\n"),
    );
    notice.value = "已复制全部邀请码";
  } catch {
    error.value = "剪贴板不可用，请选中生成结果手动复制";
  }
}
function focusTrap(e: KeyboardEvent) {
  if (e.key === "Escape") {
    e.preventDefault();
    close();
    return;
  }
  if (e.key !== "Tab" || !dialog.value) return;
  const controls = Array.from(
    dialog.value.querySelectorAll<HTMLElement>(
      "button:not(:disabled),input:not(:disabled),select:not(:disabled),a[href],textarea:not(:disabled)",
    ),
  ).filter((el) => el.offsetParent !== null);
  if (!controls.length) {
    e.preventDefault();
    dialog.value.focus();
    return;
  }
  const first = controls[0]!,
    last = controls.at(-1)!;
  if (
    e.shiftKey &&
    (document.activeElement === first ||
      document.activeElement === dialog.value)
  ) {
    e.preventDefault();
    last.focus();
  } else if (!e.shiftKey && document.activeElement === last) {
    e.preventDefault();
    first.focus();
  }
}
watch(mode, async (value, prior) => {
  if (value) {
    if (!prior) focusBefore = document.activeElement as HTMLElement;
    await nextTick();
    dialog.value?.focus();
  } else if (focusBefore?.isConnected) focusBefore.focus();
});
onMounted(load);
onBeforeUnmount(() => {
  alive = false;
  controller?.abort();
});
</script>
<template>
  <form
    class="toolbar list-filter-card panel invitation-filters"
    @submit.prevent="query"
  >
    <label class="filter-inline"
      >邀请码<input
        v-model="search"
        aria-label="搜索邀请码"
        placeholder="搜索邀请码"
        maxlength="256"
        :disabled="busy"
    /></label>
    <label class="filter-inline"
      >状态<select v-model="status" aria-label="状态筛选" :disabled="busy">
        <option value="">全部状态</option>
        <option value="unused">未使用</option>
        <option value="used">已使用</option>
      </select></label
    >
    <div class="filter-actions">
      <button
        type="button"
        :disabled="loading || busy"
        @click="
          search = '';
          status = '';
          query();
        "
      >
        重置
      </button>
      <button class="primary" :disabled="loading || busy">查询</button>
    </div>
  </form>
  <section class="panel records invitation-page">
    <div class="table-tools invitation-actions">
      <button
        class="primary"
        :disabled="!canWrite || busy || !!clearRequest"
        @click="openGenerate"
      >
        生成邀请码</button
      ><button
        :disabled="!canWrite || busy || !!batch"
        @click="openClear('all')"
      >
        清空全部</button
      ><button
        :disabled="!canWrite || busy || !!batch"
        @click="openClear('used')"
      >
        清空已使用
      </button>
      <div class="table-tools-end">
        <button :disabled="loading || busy" @click="load">刷新</button>
      </div>
    </div>
    <p v-if="!canWrite" class="table-note">
      当前会话只读，请使用本人管理员会话管理邀请码。
    </p>
    <p v-if="batch && mode !== 'generate'" class="alert" role="status">
      仍有一批生成结果待核对。点击“生成邀请码”会读取同一个原请求。
    </p>
    <p v-if="clearRequest && mode !== 'clear'" class="alert" role="status">
      原清空请求结果待核对，请点击原范围的清空按钮继续核对。
    </p>
    <p v-if="error && !mode" class="alert error" role="alert">{{ error }}</p>
    <p v-if="notice" class="alert success" role="status">{{ notice }}</p>
    <p v-if="loading" role="status">正在读取邀请码…</p>
    <div class="table-wrap">
      <table>
        <thead>
          <tr>
            <th>邀请码</th>
            <th>状态</th>
            <th>添加时间</th>
            <th>使用时间</th>
            <th>使用者</th>
            <th>操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="item in items" :key="item.id">
            <td class="invite-code">{{ item.code }}</td>
            <td>
              <span
                class="status-tag"
                :class="
                  item.status === 'used' ? 'status-danger' : 'status-success'
                "
                >{{ item.status === "used" ? "已使用" : "未使用" }}</span
              ><small v-if="!item.enabled">已停用</small
              ><small v-if="item.max_uses !== '1'"
                >{{ item.use_count }} / {{ item.max_uses }} 次</small
              >
            </td>
            <td>{{ time(item.created_at) }}</td>
            <td>{{ time(item.used_at) }}</td>
            <td>
              <RouterLink
                v-if="item.used_by"
                :to="{
                  path: '/admin/merchant/merchant-list',
                  query: { uid: item.used_by },
                }"
                >{{ item.used_by }}</RouterLink
              ><span v-else>—</span>
              <details v-if="item.uses.length > 1">
                <summary>全部使用记录（{{ item.uses.length }}）</summary>
                <p v-for="use in item.uses" :key="use.merchant_id">
                  {{ use.merchant_id }} · {{ time(use.used_at) }}
                </p>
              </details>
            </td>
            <td>
              <button
                class="danger"
                :disabled="!canWrite || busy || !!batch || !!clearRequest"
                @click="openDelete(item)"
              >
                删除
              </button>
            </td>
          </tr>
          <tr v-if="!items.length && !loading">
            <td colspan="6" class="empty-state">暂无邀请码</td>
          </tr>
        </tbody>
      </table>
    </div>
    <KitPagination
      v-model:page="page"
      v-model:page-size="limit"
      :total="total"
      @change="load"
    />
  </section>
  <Teleport to="body"
    ><div v-if="mode" class="modal-backdrop" @keydown="focusTrap">
      <section
        ref="dialog"
        class="modal panel invitation-dialog"
        role="dialog"
        aria-modal="true"
        :aria-label="title"
        tabindex="-1"
      >
        <h2>{{ title }}</h2>
        <p v-if="error" class="alert error" role="alert">{{ error }}</p>
        <form v-if="mode === 'generate'" @submit.prevent="generate">
          <div class="form-rows">
            <label
              ><span class="field-label">生成个数</span
              ><input
                id="invitation-count"
                v-model="count"
                type="number"
                min="1"
                max="1000"
                step="1"
                required
                :disabled="busy || unknown"
            /></label>
          </div>
          <p class="muted">一次最多生成1000个，每个新邀请码可使用一次。</p>
          <div class="modal-actions">
            <button type="button" :disabled="busy" @click="close">
              {{ unknown ? "关闭并保留原请求" : "取消" }}</button
            ><button class="primary" :disabled="busy || !canWrite">
              {{ busy ? "正在处理…" : unknown ? "核对原批生成结果" : "生成" }}
            </button>
          </div>
        </form>
        <template v-else-if="mode === 'result'"
          ><p class="alert success">成功生成 {{ result.length }} 个邀请码</p>
          <div class="invitation-results" tabindex="0">
            <div v-for="row in result" :key="row.id">{{ row.code }}</div>
          </div>
          <div class="modal-actions">
            <button @click="copy">复制全部</button
            ><button aria-label="关闭结果" @click="close">关闭</button>
          </div></template
        >
        <template v-else-if="mode === 'clear'"
          ><p>
            确认清空{{
              scope === "used" ? "已使用的" : "所有"
            }}邀请码？将停用并移出列表，保留注册凭据和审计历史，原码不能恢复或重复分配。
          </p>
          <p class="muted">每次最多处理1000条，不会自动处理下一批。</p>
          <div class="modal-actions">
            <button :disabled="busy" @click="close">
              {{ unknown ? "关闭并保留原请求" : "取消" }}</button
            ><button
              class="danger"
              :disabled="busy || !canWrite"
              @click="clear"
            >
              {{ busy ? "正在处理…" : unknown ? "核对原清空结果" : "确认清空" }}
            </button>
          </div></template
        >
        <template v-else-if="mode === 'delete' && selected"
          ><p>
            确定删除邀请码
            {{ selected.code }}？将停用并移出列表，保留注册凭据，不能恢复。
          </p>
          <p v-if="unknown" class="muted">
            请关闭后刷新列表核对原记录，不会自动再次删除其它邀请码。
          </p>
          <div class="modal-actions">
            <button :disabled="busy" @click="close">
              {{ unknown ? "关闭后核对列表" : "取消" }}</button
            ><button
              class="danger"
              :disabled="busy || !canWrite || unknown"
              @click="remove"
            >
              确认删除
            </button>
          </div></template
        >
      </section>
    </div></Teleport
  >
</template>
<style scoped>
.invitation-page {
  min-width: 0;
}
.invitation-page .status-tag {
  display: inline-flex;
  padding: 3px 8px;
  border-radius: 4px;
  font-size: 12px;
  border: 1px solid;
  white-space: nowrap;
}
.invitation-page .status-danger {
  background: var(--danger-soft);
  color: var(--danger-text);
  border-color: var(--danger-soft);
}
.invitation-page .status-success {
  background: var(--success-soft);
  color: var(--success-text);
  border-color: var(--success-soft);
}
.invitation-filters label {
  display: flex;
  gap: 8px;
  align-items: center;
  white-space: nowrap;
  font-size: 13px;
}
.invitation-filters input {
  width: min(240px, 100%);
  margin: 0;
}
.invitation-filters select {
  width: auto;
  min-width: 120px;
  margin: 0;
}
.invitation-filters.toolbar {
  align-items: center;
}
.invitation-actions {
  flex-wrap: wrap;
}
.invitation-page table {
  min-width: 850px;
}
.invite-code,
.invitation-results {
  font-family: ui-monospace, monospace;
}
.invitation-page td small {
  display: block;
  margin-top: 5px;
  color: var(--muted);
}
.invitation-dialog {
  width: min(500px, calc(100vw - 32px));
  max-height: calc(100dvh - 32px);
  overflow: auto;
  outline: none;
}
.invitation-dialog .form-rows input {
  width: 100%;
  box-sizing: border-box;
}
/* The generate form holds the sticky foot, which bleeds to the dialog edge
   with negative side margins; let the form carry that bleed on its padding
   so it does not overflow horizontally. */
.invitation-dialog > form {
  margin: 0 calc(-1 * var(--space-5));
  padding: 0 var(--space-5);
}
.invitation-results {
  max-height: 300px;
  overflow: auto;
  border: 1px solid var(--line);
  padding: 12px;
  user-select: text;
  overflow-wrap: anywhere;
}
.invitation-results > div {
  padding: 4px 0;
}
.invitation-page .table-wrap {
  overflow-x: auto;
  max-width: 100%;
}
@media (max-width: 600px) {
  .invitation-filters label {
    width: 100%;
    justify-content: space-between;
  }
  .invitation-filters input,
  .invitation-filters select {
    flex: 1;
    min-width: 0;
  }
  .invitation-filters {
    gap: 12px;
  }
  .invitation-actions button {
    flex: 1;
    white-space: nowrap;
  }
}
</style>
