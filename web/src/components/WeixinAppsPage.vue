<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, watch } from "vue";
import type { ApiClient } from "../api";
import KitStatusTag from "./KitStatusTag.vue";
import KitPagination from "./KitPagination.vue";
import ShellIcon from "./ShellIcon.vue";
import {
  emptyWeixinDraft,
  knownWeixinRejection,
  newWeixinRequestID,
  readWeixinApplication,
  readWeixinApplications,
  readWeixinCapabilities,
  weixinDraftError,
  weixinKindName,
  weixinKinds,
  type WeixinApplication,
  type WeixinDraft,
  type WeixinSource,
  type WeixinConfigurationError,
} from "../weixin-applications";
const props = defineProps<{ client: ApiClient }>();
const items = ref<WeixinApplication[]>([]),
  configurationErrors = ref<WeixinConfigurationError[]>([]),
  sources = ref<WeixinSource[]>([]),
  loading = ref(false),
  busy = ref(false),
  listWritable = ref(false),
  scopeWritable = ref(false),
  listReady = ref(false),
  scopeReady = ref(false),
  error = ref(""),
  notice = ref("");
const mode = ref<"create" | "edit" | "archive" | null>(null),
  selected = ref<WeixinApplication | null>(null),
  draft = ref<WeixinDraft>(emptyWeixinDraft()),
  dialog = ref<HTMLElement>(),
  secretInput = ref<HTMLInputElement>(),
  refreshButton = ref<HTMLButtonElement>(),
  unknown = ref(false),
  rotationPending = ref(false);
type Pending = {
  requestID: string;
  targetID?: string;
  action: "create" | "edit" | "archive";
  name: string;
  kind: string;
  appID: string;
  revision?: string;
  enabled: boolean;
  sourceID: string;
  merchants: string[];
  path: string;
};
const pending = ref<Pending | null>(null);
const search = ref(""),
  category = ref(""),
  appliedSearch = ref(""),
  appliedCategory = ref(""),
  page = ref(1),
  pageSize = ref(20);
const filtered = computed(() =>
  items.value.filter(
    (row) =>
      (!appliedCategory.value || row.kind === appliedCategory.value) &&
      (!appliedSearch.value ||
        [row.id, row.name, row.app_id].some((value) =>
          value.toLowerCase().includes(appliedSearch.value),
        )),
  ),
);
const visibleRows = computed(() =>
  filtered.value.slice(
    (page.value - 1) * pageSize.value,
    page.value * pageSize.value,
  ),
);
watch([() => filtered.value.length, pageSize], () => {
  page.value = Math.max(
    1,
    Math.min(
      page.value,
      Math.ceil(filtered.value.length / pageSize.value) || 1,
    ),
  );
});
function applyFilters() {
  if (busy.value || mode.value) return;
  appliedSearch.value = search.value.trim().toLowerCase();
  appliedCategory.value = category.value;
  page.value = 1;
}
function resetFilters() {
  if (busy.value || mode.value) return;
  search.value = "";
  category.value = "";
  appliedSearch.value = "";
  appliedCategory.value = "";
  page.value = 1;
}
let epoch = 0,
  readEpoch = 0,
  active = true,
  controller: AbortController | undefined,
  opener: HTMLElement | undefined;
const canWrite = computed(
  () =>
    props.client.area === "admin" &&
    listReady.value &&
    scopeReady.value &&
    listWritable.value &&
    scopeWritable.value,
);
const source = computed(() =>
  sources.value.find((s) => s.id === draft.value.source_channel_id),
);
const unavailableMIDs = computed(() =>
  draft.value.merchant_ids.filter(
    (id) => !source.value?.merchant_ids.includes(id),
  ),
);
const title = computed(() =>
  mode.value === "archive"
    ? "归档应用"
    : mode.value === "edit"
      ? "编辑应用"
      : "新增应用",
);
const endpoint = "/weixin-applications";
function context() {
  return {
    epoch,
    client: props.client,
    csrf: props.client.csrfToken,
    area: props.client.area,
  };
}
type Context = ReturnType<typeof context>;
function current(c: Context) {
  return (
    active &&
    c.epoch === epoch &&
    c.client === props.client &&
    c.csrf === props.client.csrfToken &&
    c.area === props.client.area
  );
}
function clearSecret() {
  draft.value.appsecret = "";
  if (secretInput.value) secretInput.value.value = "";
}
function clearForm() {
  clearSecret();
  draft.value = emptyWeixinDraft();
  mode.value = null;
  selected.value = null;
  unknown.value = false;
  pending.value = null;
  rotationPending.value = false;
}
function reset() {
  epoch++;
  readEpoch++;
  controller?.abort();
  clearForm();
  items.value = [];
  configurationErrors.value = [];
  sources.value = [];
  listReady.value = false;
  scopeReady.value = false;
  listWritable.value = false;
  scopeWritable.value = false;
  loading.value = false;
  busy.value = false;
  error.value = "";
  notice.value = "";
  search.value = "";
  category.value = "";
  appliedSearch.value = "";
  appliedCategory.value = "";
  page.value = 1;
  pageSize.value = 20;
  opener = undefined;
}
async function load() {
  const c = context(),
    serial = ++readEpoch;
  controller?.abort();
  controller = new AbortController();
  loading.value = true;
  listReady.value = false;
  scopeReady.value = false;
  listWritable.value = false;
  scopeWritable.value = false;
  error.value = "";
  try {
    if (c.area !== "admin") throw Error("需要管理员会话读取应用资料");
    const [rows, capabilities] = await Promise.allSettled([
      c.client.request(endpoint, { signal: controller.signal }),
      c.client.request(endpoint + "/capabilities", {
        signal: controller.signal,
      }),
    ]);
    if (!current(c) || serial !== readEpoch) return;
    if (rows.status === "fulfilled") {
      const page = readWeixinApplications(rows.value);
      items.value = page.items;
      configurationErrors.value = page.configuration_errors;
      listWritable.value = page.can_write;
      listReady.value = true;
    } else throw rows.reason;
    if (capabilities.status !== "fulfilled") throw capabilities.reason;
    const scope = readWeixinCapabilities(capabilities.value);
    sources.value = scope.sources;
    scopeWritable.value = scope.can_write;
    scopeReady.value = true;
  } catch (e) {
    if (current(c) && serial === readEpoch)
      error.value = e instanceof Error ? e.message : "应用资料读取失败";
  } finally {
    if (current(c) && serial === readEpoch) loading.value = false;
  }
}
watch(
  () => [props.client, props.client.area, props.client.csrfToken],
  () => {
    reset();
    void load();
  },
  { immediate: true, flush: "sync" },
);
async function focusForm() {
  const c = context();
  await nextTick();
  if (!current(c)) return;
  dialog.value
    ?.querySelector<HTMLElement>(
      "form input:not(:disabled),form select:not(:disabled),form button:not(:disabled)",
    )
    ?.focus({ preventScroll: true });
}
function remember() {
  opener =
    document.activeElement instanceof HTMLElement
      ? document.activeElement
      : undefined;
  error.value = "";
  notice.value = "";
  unknown.value = false;
  pending.value = null;
}
function create(row?: WeixinApplication) {
  if (!canWrite.value || loading.value || busy.value || mode.value) return;
  remember();
  selected.value = null;
  draft.value = emptyWeixinDraft();
  if (row?.origin === "legacy_metadata") {
    draft.value.name = row.name;
    draft.value.app_id = row.app_id;
    draft.value.kind =
      row.kind === "mini_program" ? "mini_program" : "official_account";
    notice.value = "旧资料仅用于预填新建应用，请重新填写凭证与关联范围。";
  }
  mode.value = "create";
  void focusForm();
}
function edit(row: WeixinApplication) {
  if (
    !canWrite.value ||
    busy.value ||
    loading.value ||
    mode.value ||
    row.origin !== "managed" ||
    row.read_only ||
    row.archived
  )
    return;
  remember();
  selected.value = row;
  draft.value = {
    kind: row.kind as WeixinDraft["kind"],
    name: row.name,
    app_id: row.app_id,
    appsecret: "",
    enabled: row.enabled,
    source_channel_id: row.source_channel_id,
    merchant_ids: [...row.merchant_ids],
    path: row.path,
  };
  mode.value = "edit";
  void focusForm();
}
function archive(row: WeixinApplication) {
  if (
    !canWrite.value ||
    busy.value ||
    loading.value ||
    mode.value ||
    row.origin !== "managed" ||
    row.read_only ||
    row.archived
  )
    return;
  remember();
  selected.value = row;
  draft.value = emptyWeixinDraft();
  mode.value = "archive";
  void focusForm();
}
async function close() {
  if (busy.value || unknown.value) return;
  const target = opener,
    c = context();
  clearForm();
  await nextTick();
  if (current(c)) (target?.isConnected ? target : refreshButton.value)?.focus();
}
function changedSource() {
  draft.value.merchant_ids = [];
}
function changedKind() {
  if (draft.value.kind === "official_account") draft.value.path = "";
}
function abandonRotation() {
  if (busy.value || unknown.value || mode.value !== "edit") return;
  clearSecret();
  rotationPending.value = false;
  error.value = "";
}
function chooseMerchant(id: string, checked: boolean) {
  if (busy.value || unknown.value || !source.value?.merchant_ids.includes(id))
    return;
  draft.value.merchant_ids = checked
    ? [...new Set([...draft.value.merchant_ids, id])]
    : draft.value.merchant_ids.filter((v) => v !== id);
}
function receiptMatches(row: WeixinApplication, p: Pending) {
  return (
    row.origin === "managed" &&
    row.secret_configured &&
    (!p.targetID || row.id === p.targetID) &&
    row.name === p.name &&
    row.kind === p.kind &&
    row.app_id === p.appID &&
    row.enabled === p.enabled &&
    row.source_channel_id === p.sourceID &&
    row.path === p.path &&
    row.merchant_ids.length === p.merchants.length &&
    row.merchant_ids.every((id) => p.merchants.includes(id)) &&
    (p.action !== "archive" || row.archived) &&
    (p.revision === undefined
      ? row.revision === "1"
      : BigInt(row.revision) === BigInt(p.revision) + 1n)
  );
}
async function confirmed(row: WeixinApplication, c: Context) {
  if (!current(c)) return;
  const target = opener;
  clearForm();
  notice.value = "应用资料已保存，请核对当前列表。";
  await load();
  await nextTick();
  if (current(c)) (target?.isConnected ? target : refreshButton.value)?.focus();
}
async function mutate() {
  if (
    !canWrite.value ||
    loading.value ||
    busy.value ||
    unknown.value ||
    !mode.value
  )
    return;
  const c = context(),
    action = mode.value,
    target = selected.value;
  if (action === "edit" && rotationPending.value && !draft.value.appsecret) {
    error.value =
      "本次凭证尚未保存，请重新输入APPSECRET，或明确放弃本次凭证轮换。";
    return;
  }
  if (action !== "archive") {
    const problem = weixinDraftError(
      draft.value,
      sources.value,
      action === "edit" ? target || undefined : undefined,
    );
    if (problem) {
      error.value = problem;
      return;
    }
  }
  const replacementAttempt = action === "edit" && draft.value.appsecret !== "";
  const requestID = newWeixinRequestID(),
    p: Pending = {
      requestID,
      targetID: action === "create" ? undefined : target!.id,
      action,
      name: action === "archive" ? target!.name : draft.value.name.trim(),
      kind: action === "archive" ? target!.kind : draft.value.kind,
      appID: action === "archive" ? target!.app_id : draft.value.app_id,
      revision: action === "create" ? undefined : target!.revision,
      enabled: action === "archive" ? false : draft.value.enabled,
      sourceID:
        action === "archive"
          ? target!.source_channel_id
          : draft.value.source_channel_id,
      merchants:
        action === "archive"
          ? [...target!.merchant_ids]
          : [...draft.value.merchant_ids],
      path:
        action === "archive"
          ? target!.path
          : draft.value.kind === "mini_program"
            ? draft.value.path
            : "",
    };
  const body: Record<string, unknown> = { request_id: requestID };
  if (action !== "create") {
    body.expected_revision = target!.revision;
    body.expected_fingerprint = target!.fingerprint;
  }
  if (action !== "archive")
    Object.assign(body, {
      kind: draft.value.kind,
      name: draft.value.name.trim(),
      app_id: draft.value.app_id,
      appsecret: draft.value.appsecret,
      enabled: draft.value.enabled,
      source_channel_id: draft.value.source_channel_id,
      merchant_ids: [...draft.value.merchant_ids].sort((a, b) =>
        BigInt(a) < BigInt(b) ? -1 : 1,
      ),
      path: draft.value.kind === "mini_program" ? draft.value.path : "",
      expected_source_fingerprint:
        draft.value.source_channel_id === "0" ? "" : source.value!.fingerprint,
    });
  const path =
    endpoint +
    (p.targetID ? "/" + p.targetID : "") +
    (action === "archive" ? "/archive" : "");
  busy.value = true;
  error.value = "";
  try {
    const raw = await c.client.request(path, {
      method: action === "edit" ? "PATCH" : "POST",
      body,
    });
    if (!current(c)) return;
    const row = readWeixinApplication(raw);
    if (!receiptMatches(row, p)) throw Error("保存回执无法核对");
    await confirmed(row, c);
  } catch (e) {
    if (!current(c)) return;
    if (knownWeixinRejection(e) && [400, 422].includes(e.status)) {
      if (replacementAttempt) rotationPending.value = true;
      error.value = e.message;
    } else if (knownWeixinRejection(e) && e.status === 409) {
      clearSecret();
      clearForm();
      await load();
      if (current(c))
        error.value = "配置版本已变化，已尝试读取最新资料，请重新核对后编辑。";
    } else if (knownWeixinRejection(e)) {
      clearSecret();
      clearForm();
      listWritable.value = false;
      error.value = e.message;
    } else {
      clearSecret();
      unknown.value = true;
      pending.value = p;
      error.value = "";
    }
  } finally {
    if (current(c)) {
      clearSecret();
      busy.value = false;
    }
  }
}
async function queryReceipt() {
  const p = pending.value;
  if (!p || busy.value || !unknown.value) return;
  const c = context();
  busy.value = true;
  error.value = "";
  try {
    const row = readWeixinApplication(
      await c.client.request(endpoint + "/requests/" + p.requestID),
    );
    if (!current(c) || pending.value !== p) return;
    if (!receiptMatches(row, p)) throw Error("原请求回执无法核对");
    await confirmed(row, c);
  } catch {
    if (current(c))
      error.value =
        "尚未核对到原请求结果，请稍后再次查询；结果仍未确认，不会重新提交。";
  } finally {
    if (current(c)) busy.value = false;
  }
}
function trap(event: KeyboardEvent) {
  const nodes = Array.from(
    dialog.value?.querySelectorAll<HTMLElement>(
      'button:not(:disabled),input:not(:disabled),select:not(:disabled),textarea:not(:disabled),[tabindex="0"]',
    ) || [],
  ).filter((n) => n.getClientRects().length);
  const first = nodes[0],
    last = nodes.at(-1);
  if (
    first &&
    last &&
    (event.shiftKey
      ? document.activeElement === first
      : document.activeElement === last)
  ) {
    event.preventDefault();
    (event.shiftKey ? last : first).focus();
  }
}
onBeforeUnmount(() => {
  active = false;
  reset();
});
</script>
<template>
  <section class="panel weixin-apps-page">
    <div class="weixin-toolbar">
      <button
        v-if="canWrite"
        type="button"
        class="primary"
        :disabled="loading || busy || !!mode"
        @click="create()"
      >
        <ShellIcon name="plus" />新增应用</button
      ><span>共 {{ items.length }} 个应用</span
      ><button
        ref="refreshButton"
        type="button"
        :disabled="loading || busy || !!mode"
        @click="load"
      >
        刷新
      </button>
    </div>
    <p v-if="!loading && !canWrite" class="muted">
      当前资料只读。修改权限以当前管理员会话及服务端授权范围为准。
    </p>
    <p v-if="error && !mode" role="alert" class="weixin-error">{{ error }}</p>
    <p v-if="notice" role="status">{{ notice }}</p>
    <div v-if="configurationErrors.length" role="alert" class="weixin-error">
      <p v-for="entry in configurationErrors" :key="entry.id">
        应用 #{{ entry.id }}：{{ entry.message }}
      </p>
    </div>
    <form class="weixin-filters" @submit.prevent="applyFilters">
      <label
        >搜索<input
          v-model="search"
          aria-label="搜索应用"
          placeholder="ID、名称或APPID"
          maxlength="256"
          :disabled="loading || busy || !!mode" /></label
      ><label
        >类别<select
          v-model="category"
          aria-label="筛选类别"
          :disabled="loading || busy || !!mode"
        >
          <option value="">全部类别</option>
          <option
            v-for="kind in weixinKinds"
            :key="kind.value"
            :value="kind.value"
          >
            {{ kind.label }}
          </option>
        </select></label
      >
      <div>
        <button
          type="button"
          :disabled="loading || busy || !!mode"
          @click="resetFilters"
        >
          重置</button
        ><button
          type="submit"
          class="primary"
          :disabled="loading || busy || !!mode"
        >
          查询
        </button>
      </div>
    </form>
    <div class="weixin-table-wrap">
      <table>
        <thead>
          <tr>
            <th>ID</th>
            <th>类别</th>
            <th>名称</th>
            <th>APPID</th>
            <th>操作</th>
            <th>凭证</th>
            <th>状态</th>
            <th>来源</th>
          </tr>
        </thead>
        <tbody>
          <tr v-if="loading">
            <td colspan="8">正在读取应用资料…</td>
          </tr>
          <tr v-else-if="!filtered.length">
            <td colspan="8">
              {{ items.length ? "没有符合条件的应用" : "暂无应用资料" }}
            </td>
          </tr>
          <tr
            v-for="row in visibleRows"
            v-else
            :key="row.origin + ':' + row.id"
            :data-app-origin="row.origin"
            :data-app-id="row.id"
          >
            <td>{{ row.id }}</td>
            <td>{{ weixinKindName(row.kind) }}</td>
            <td>
              {{ row.name || "未命名旧资料"
              }}<small>版本 {{ row.revision }}</small>
            </td>
            <td class="weixin-appid">{{ row.app_id || "未配置" }}</td>
            <td class="weixin-row-actions">
              <template
                v-if="
                  canWrite &&
                  row.origin === 'managed' &&
                  !row.read_only &&
                  !row.archived
                "
                ><button
                  type="button"
                  :disabled="busy || loading || !!mode"
                  @click="edit(row)"
                >
                  编辑</button
                ><button
                  type="button"
                  :disabled="busy || loading || !!mode"
                  @click="archive(row)"
                >
                  归档
                </button></template
              ><button
                v-else-if="canWrite && row.origin === 'legacy_metadata'"
                type="button"
                :disabled="busy || loading || !!mode"
                @click="create(row)"
              >
                配置为新应用</button
              ><span v-else class="muted">只读</span>
            </td>
            <td>{{ row.secret_configured ? "已配置（不回显）" : "未配置" }}</td>
            <td>
              <KitStatusTag
                :text="
                  row.archived
                    ? '已归档'
                    : row.draft
                      ? '待关联草稿'
                      : row.execution_ready
                        ? '执行就绪'
                        : row.enabled
                          ? '资料已启用'
                          : '已停用'
                "
                :tone="
                  row.execution_ready
                    ? 'success'
                    : row.draft
                      ? 'warning'
                      : 'neutral'
                "
              />
            </td>
            <td>
              {{
                row.origin === "managed"
                  ? "后台受管"
                  : row.origin === "imported"
                    ? "配置文件导入 · 只读"
                    : "未接入资料 · 只读"
              }}
              <details
                v-if="
                  row.source_channel_id !== '0' ||
                  row.merchant_ids.length ||
                  row.path
                "
              >
                <summary>关联范围</summary>
                <p>来源通道 #{{ row.source_channel_id }}</p>
                <p>允许商户：{{ row.merchant_ids.join("、") || "未设置" }}</p>
                <p v-if="row.path">路径：{{ row.path }}</p>
              </details>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
    <KitPagination
      v-if="filtered.length"
      v-model:page="page"
      v-model:page-size="pageSize"
      :total="filtered.length"
    />
  </section>
  <div
    v-if="mode"
    class="modal-backdrop"
    @keydown.esc.prevent="close"
    @keydown.tab="trap"
  >
    <section
      ref="dialog"
      class="modal panel weixin-app-dialog"
      role="dialog"
      aria-modal="true"
      :aria-label="title"
      tabindex="-1"
    >
      <header>
        <h2>{{ title }}</h2>
        <button
          type="button"
          aria-label="关闭"
          :disabled="busy || unknown"
          @click="close"
        >
          ×
        </button>
      </header>
      <form @submit.prevent="mutate">
        <p v-if="mode === 'archive'">
          确认归档“{{
            selected?.name
          }}”？归档会关闭这份应用资料，已保存的历史记录继续保留。
        </p>
        <fieldset v-else :disabled="busy || unknown || !canWrite">
          <label
            >类别<select
              v-model="draft.kind"
              aria-label="类别"
              @change="changedKind"
            >
              <option
                v-for="kind in weixinKinds"
                :key="kind.value"
                :value="kind.value"
              >
                {{ kind.label }}
              </option>
            </select></label
          ><label
            >名称<input
              v-model="draft.name"
              aria-label="名称"
              maxlength="100"
              autocomplete="off" /></label
          ><label
            >APPID<input
              v-model="draft.app_id"
              aria-label="APPID"
              maxlength="128"
              autocomplete="off" /></label
          ><label
            >APPSECRET<input
              ref="secretInput"
              v-model="draft.appsecret"
              aria-label="APPSECRET"
              type="password"
              maxlength="512"
              autocomplete="new-password"
              spellcheck="false" /></label
          ><small class="muted">{{
            mode === "edit"
              ? "留空保留原凭证；更改类别或APPID时必须填写新的APPSECRET。"
              : "凭证加密保存，成功后不再回显。"
          }}</small>
          <div v-if="rotationPending" role="status" class="weixin-error">
            本次凭证尚未保存。请重新输入APPSECRET后再提交，或明确放弃本次凭证轮换。<button
              type="button"
              :disabled="busy || unknown"
              @click="abandonRotation"
            >
              放弃本次凭证轮换
            </button>
          </div>
          <label class="weixin-check"
            ><input
              v-model="draft.enabled"
              type="checkbox"
              aria-label="启用资料"
            />启用资料</label
          >
          <p class="muted">
            新应用默认停用。尚未完成关联的资料会作为草稿保存。
          </p>
          <details class="weixin-bindings">
            <summary>关联与授权范围</summary>
            <label
              >关联来源通道<select
                v-model="draft.source_channel_id"
                aria-label="关联来源通道"
                @change="changedSource"
              >
                <option value="0">暂不关联（草稿）</option>
                <option
                  v-if="draft.source_channel_id !== '0' && !source"
                  :value="draft.source_channel_id"
                >
                  当前来源 #{{ draft.source_channel_id }}（不可用）
                </option>
                <option v-for="item in sources" :key="item.id" :value="item.id">
                  通道 #{{ item.id }}
                </option>
              </select></label
            >
            <p v-if="!sources.length" class="muted">
              当前没有可关联的来源通道，可先保存草稿。
            </p>
            <fieldset v-if="source" class="weixin-merchants">
              <legend>允许商户编号</legend>
              <label
                v-for="id in source.merchant_ids"
                :key="id"
                class="weixin-check"
                ><input
                  type="checkbox"
                  :aria-label="`允许商户 ${id}`"
                  :checked="draft.merchant_ids.includes(id)"
                  @change="
                    chooseMerchant(
                      id,
                      ($event.target as HTMLInputElement).checked,
                    )
                  "
                />{{ id }}</label
              ><small v-if="!source.merchant_ids.length"
                >当前来源未授权任何商户。</small
              >
            </fieldset>
            <p v-for="id in unavailableMIDs" :key="id" class="weixin-error">
              原商户 {{ id }} 已不在当前授权范围。<button
                type="button"
                @click="
                  draft.merchant_ids = draft.merchant_ids.filter(
                    (v) => v !== id,
                  )
                "
              >
                移除此商户
              </button>
            </p>
            <label v-if="draft.kind === 'mini_program'"
              >小程序路径<input
                v-model="draft.path"
                aria-label="小程序路径"
                maxlength="256"
                placeholder="pages/index/index"
            /></label>
          </details>
        </fieldset>
        <p v-if="unknown" role="alert" class="weixin-error">
          结果尚未确认。凭证草稿已清除，请只查询原请求；不会自动再次提交。<small
            >请求编号：{{ pending?.requestID }}</small
          >
        </p>
        <p v-if="error" role="alert" class="weixin-error">{{ error }}</p>
        <div class="weixin-modal-actions">
          <button
            v-if="unknown"
            type="button"
            :disabled="busy"
            @click="queryReceipt"
          >
            查询原请求</button
          ><button type="button" :disabled="busy || unknown" @click="close">
            取消</button
          ><button
            type="submit"
            class="primary"
            :disabled="busy || unknown || !canWrite || loading"
          >
            {{
              busy ? "正在处理…" : mode === "archive" ? "确认归档" : "保存应用"
            }}
          </button>
        </div>
      </form>
    </section>
  </div>
</template>
<style scoped>
.weixin-apps-page {
  min-width: 0;
}
.weixin-toolbar {
  display: flex;
  align-items: center;
  gap: 12px;
  flex-wrap: wrap;
  margin-bottom: 16px;
}
.weixin-toolbar button:last-child {
  margin-left: auto;
}
.weixin-table-wrap {
  max-width: 100%;
  overflow: auto;
}
.weixin-filters {
  display: flex;
  flex-wrap: wrap;
  align-items: end;
  gap: 14px;
  padding: 16px 0;
}
.weixin-filters label {
  display: grid;
  gap: 7px;
  min-width: 160px;
  flex: 1;
}
.weixin-filters input,
.weixin-filters select {
  width: 100%;
  min-width: 0;
  box-sizing: border-box;
}
.weixin-filters > div {
  display: flex;
  gap: 8px;
}
.weixin-table-wrap table {
  width: 100%;
  border-collapse: collapse;
  min-width: 740px;
}
.weixin-table-wrap th,
.weixin-table-wrap td {
  text-align: left;
  padding: 12px;
  border-bottom: 1px solid var(--border, #e2e8f0);
  vertical-align: top;
}
.weixin-table-wrap td small {
  display: block;
  margin-top: 5px;
  color: var(--text-secondary, #64748b);
}
.weixin-appid {
  overflow-wrap: anywhere;
  max-width: 260px;
}
.weixin-row-actions {
  white-space: nowrap;
}
.weixin-row-actions button + button {
  margin-left: 6px;
}
.weixin-error {
  color: var(--danger, #b91c1c);
  line-height: 1.65;
  overflow-wrap: anywhere;
}
.weixin-error small {
  display: block;
}
.weixin-app-dialog {
  width: min(760px, calc(100vw - 24px));
  max-height: 92dvh;
  overflow: auto;
  box-sizing: border-box;
}
.weixin-app-dialog header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
}
.weixin-app-dialog header h2 {
  margin: 0 0 16px;
  font-size: 18px;
}
.weixin-app-dialog form > fieldset {
  display: grid;
  gap: 14px;
  border: 0;
  padding: 0;
  min-width: 0;
}
.weixin-app-dialog label:not(.weixin-check) {
  display: grid;
  gap: 7px;
}
.weixin-app-dialog input:not([type="checkbox"]),
.weixin-app-dialog select {
  width: 100%;
  min-width: 0;
  box-sizing: border-box;
}
.weixin-app-dialog .muted {
  line-height: 1.65;
  margin: 0;
}
.weixin-check {
  display: flex;
  gap: 8px;
  align-items: center;
}
.weixin-bindings {
  border-top: 1px solid var(--border, #e2e8f0);
  padding-top: 14px;
}
.weixin-bindings summary {
  cursor: pointer;
  margin-bottom: 14px;
}
.weixin-bindings label:not(.weixin-check) {
  margin: 14px 0;
}
.weixin-merchants {
  max-height: 220px;
  overflow: auto;
  border: 1px solid var(--border, #e2e8f0);
  padding: 12px;
  display: grid;
  gap: 10px;
}
.weixin-modal-actions {
  display: flex;
  justify-content: flex-end;
  gap: 10px;
  flex-wrap: wrap;
  margin-top: 20px;
}
@media (max-width: 480px) {
  .weixin-filters label {
    min-width: 0;
    flex-basis: 100%;
  }
  .weixin-filters > div {
    width: 100%;
    justify-content: flex-end;
  }
  .weixin-app-dialog {
    padding: 16px;
  }
  .weixin-toolbar {
    gap: 8px;
  }
  .weixin-modal-actions button {
    flex: 1;
  }
  .weixin-table-wrap th,
  .weixin-table-wrap td {
    padding: 10px;
  }
}
</style>
