<script setup lang="ts">
import { ref, computed, watch, onBeforeUnmount, nextTick, toRaw } from "vue";
import { ApiClient } from "../api";
import type { ResourceSchema } from "../resources";
import RichTextEditor from "./RichTextEditor.vue";
import RichContent from "./RichContent.vue";
import KitPagination from "./KitPagination.vue";
import { beijingTimestamp } from "../console-datetime";
import {
  richText,
  richContent,
  richFragment,
  safeRichHTML,
} from "../rich-content";
type Row = {
  id: string;
  content: string;
  content_format: string;
  sort: number;
  color: string;
  enabled: boolean;
  createdAt: string;
};
const props = defineProps<{ client: ApiClient; ownSession: boolean }>();
const rows = ref<Row[]>([]),
  schema = ref<ResourceSchema>(),
  content = ref(""),
  sort = ref("50"),
  color = ref(""),
  selected = ref(""),
  error = ref(""),
  notice = ref(""),
  loading = ref(false),
  busy = ref(false),
  uncertain = ref(true),
  page = ref(1),
  total = ref(0),
  adding = ref(false),
  removing = ref<Row>(),
  confirm = ref<HTMLElement>(),
  editorSection = ref<HTMLElement>();
const endpoint = "/resources/announcements",
  writable = computed(
    () => props.ownSession && !loading.value && !busy.value && !uncertain.value,
  ),
  canSave = computed(
    () =>
      writable.value &&
      (selected.value
        ? schema.value?.permissions.update
        : schema.value?.permissions.create),
  ),
  formOpen = computed(
    () =>
      adding.value ||
      !!selected.value ||
      (!!props.ownSession && !!schema.value?.permissions.create),
  );
let epoch = 0,
  controller: AbortController | undefined,
  alive = true,
  readyClient: ApiClient | undefined,
  readyCSRF = "",
  returnFocus: HTMLElement | undefined;
function clear() {
  content.value = "";
  sort.value = "50";
  color.value = "";
  selected.value = "";
  adding.value = false;
  removing.value = undefined;
}
function openAdd() {
  if (!writable.value || !schema.value?.permissions.create) return;
  if (selected.value || !formOpen.value) clear();
  adding.value = true;
  notice.value = "";
  returnFocus = document.activeElement as HTMLElement;
  void nextTick(() => {
    editorSection.value
      ?.querySelector<HTMLElement>("[contenteditable]")
      ?.focus();
  });
}
function closeForm() {
  clear();
  if (returnFocus?.isConnected) returnFocus.focus();
}
function formKeys(e: KeyboardEvent) {
  if (e.key === "Escape" && !busy.value) {
    e.preventDefault();
    closeForm();
  }
}
function invalidate() {
  epoch++;
  controller?.abort();
  rows.value = [];
  schema.value = undefined;
  readyClient = undefined;
  readyCSRF = "";
  clear();
  loading.value = false;
  busy.value = false;
  uncertain.value = true;
  error.value = "会话已变化，请刷新核对公告";
}
function context() {
  return {
    epoch,
    client: toRaw(props.client),
    csrf: toRaw(props.client).csrfToken,
  };
}
function current(c: ReturnType<typeof context>) {
  if (!alive || c.epoch !== epoch) return false;
  if (
    c.client !== toRaw(props.client) ||
    c.csrf !== toRaw(props.client).csrfToken
  ) {
    invalidate();
    return false;
  }
  return true;
}
function authorized() {
  if (
    !readyCSRF ||
    readyClient !== toRaw(props.client) ||
    readyCSRF !== toRaw(props.client).csrfToken ||
    !props.ownSession
  ) {
    invalidate();
    return false;
  }
  return true;
}
function row(raw: unknown): Row {
  if (!raw || typeof raw !== "object" || Array.isArray(raw))
    throw Error("公告格式不正确");
  const r = raw as Record<string, unknown>,
    id = r.id;
  if (
    typeof id !== "string" ||
    !/^[1-9]\d{0,18}$/.test(id) ||
    BigInt(id) > 9223372036854775807n ||
    typeof r.content !== "string" ||
    r.content.length > 20000 ||
    typeof r.enabled !== "boolean"
  )
    throw Error("公告格式不正确");
  const s = r.sort === undefined ? 50 : Number(r.sort),
    c = r.color === undefined ? "" : r.color;
  if (
    !Number.isSafeInteger(s) ||
    s < 1 ||
    s > 2147483647 ||
    typeof c !== "string" ||
    (c !== "" && !/^#[\da-f]{6}$/i.test(c))
  )
    throw Error("公告排序或颜色格式不正确");
  return {
    id,
    content: r.content,
    content_format: r.content_format === "html" ? "html" : "text",
    sort: s,
    color: c,
    enabled: r.enabled,
    createdAt: typeof r.created_at === "string" ? r.created_at : "",
  };
}
async function load() {
  if (busy.value) return;
  controller?.abort();
  const ac = new AbortController();
  controller = ac;
  const c = context();
  loading.value = true;
  uncertain.value = true;
  error.value = "";
  try {
    const s = await c.client.request<ResourceSchema>(endpoint + "/schema", {
      signal: ac.signal,
    });
    if (!current(c) || ac.signal.aborted) return;
    if (
      !s.permissions ||
      !["create", "update", "delete"].every(
        (k) =>
          typeof s.permissions[k as keyof typeof s.permissions] === "boolean",
      )
    )
      throw Error("公告权限格式不正确");
    const data = await c.client.request<{ items: unknown[]; total: number }>(
      endpoint + `?page=${page.value}&limit=20`,
      { signal: ac.signal },
    );
    if (!current(c) || ac.signal.aborted) return;
    if (
      !Array.isArray(data.items) ||
      data.items.length > 20 ||
      !Number.isSafeInteger(data.total) ||
      data.total < 0
    )
      throw Error("公告列表格式不正确");
    const found = data.items.map(row);
    if (new Set(found.map((r) => r.id)).size !== found.length)
      throw Error("公告列表编号重复");
    schema.value = s;
    rows.value = found;
    total.value = data.total;
    readyClient = c.client;
    readyCSRF = c.csrf;
    uncertain.value = false;
  } catch (e) {
    if (current(c) && !ac.signal.aborted)
      error.value = e instanceof Error ? e.message : "加载公告列表失败";
  } finally {
    if (current(c) && !ac.signal.aborted) loading.value = false;
  }
}
async function detail(r: Row) {
  const c = context(),
    ac = new AbortController();
  controller?.abort();
  controller = ac;
  busy.value = true;
  error.value = "";
  try {
    const found = row(
      await c.client.request(endpoint + "/" + r.id, { signal: ac.signal }),
    );
    if (!current(c) || ac.signal.aborted) return;
    if (found.id !== r.id) throw Error("公告编号不匹配");
    return found;
  } catch (e) {
    if (current(c) && !ac.signal.aborted) {
      error.value = e instanceof Error ? e.message : "读取公告失败";
      uncertain.value = true;
    }
  } finally {
    if (current(c) && !ac.signal.aborted) busy.value = false;
  }
}
async function edit(r: Row) {
  if (!writable.value || !schema.value?.permissions.update || !authorized())
    return;
  const found = await detail(r);
  if (!found) return;
  const box = document.createElement("div");
  box.append(richFragment(richContent(found.content, found.content_format)));
  content.value = box.innerHTML;
  sort.value = String(found.sort);
  color.value = found.color;
  selected.value = found.id;
  notice.value = "";
  returnFocus = document.activeElement as HTMLElement;
  await nextTick();
  editorSection.value?.querySelector<HTMLElement>("[contenteditable]")?.focus();
}
async function mutate(
  id: string,
  method: "POST" | "PATCH" | "DELETE",
  body?: Record<string, unknown>,
) {
  if (!writable.value || !authorized()) return;
  const c = context(),
    ac = new AbortController();
  controller?.abort();
  controller = ac;
  busy.value = true;
  error.value = "";
  notice.value = "";
  try {
    await c.client.request(endpoint + (id ? "/" + id : ""), {
      method,
      ...(body ? { body } : {}),
      signal: ac.signal,
    });
    if (!current(c) || ac.signal.aborted) return;
    notice.value =
      method === "DELETE"
        ? "删除公告成功"
        : method === "POST"
          ? "添加公告成功"
          : "修改公告成功";
    if (method !== "PATCH" || !body || "content" in body) clear();
    removing.value = undefined;
    busy.value = false;
    await load();
    if (returnFocus?.isConnected) returnFocus.focus();
  } catch (e) {
    if (current(c) && !ac.signal.aborted) {
      uncertain.value = true;
      removing.value = undefined;
      error.value =
        (e instanceof Error ? e.message : "操作结果无法确认") +
        "；请先刷新核对公告，再决定是否操作";
    }
  } finally {
    if (current(c) && !ac.signal.aborted) busy.value = false;
  }
}
async function save() {
  if (!canSave.value || !authorized()) return;
  try {
    const html = safeRichHTML(content.value);
    if (!richText(html).trim() && !/<(?:img|table)\b/.test(html))
      throw Error("公告内容不能为空");
    if (!/^[1-9]\d{0,9}$/.test(sort.value) || Number(sort.value) > 2147483647)
      throw Error("排序必须为1至2147483647的整数");
    if (color.value !== "" && !/^#[\da-f]{6}$/i.test(color.value))
      throw Error("文字颜色请填写#及6位十六进制颜色");
    await mutate(selected.value, selected.value ? "PATCH" : "POST", {
      content: html,
      sort: Number(sort.value),
      color: color.value,
      content_format: "html",
      ...(!selected.value ? { audience: "merchant", enabled: true } : {}),
    });
  } catch (e) {
    error.value = e instanceof Error ? e.message : "公告内容不正确";
  }
}
async function askRemove(r: Row) {
  if (!writable.value || !schema.value?.permissions.delete || !authorized())
    return;
  returnFocus = document.activeElement as HTMLElement;
  const found = await detail(r);
  if (!found) return;
  removing.value = found;
  await nextTick();
  confirm.value?.querySelector<HTMLElement>("button")?.focus();
}
function cancelDelete() {
  removing.value = undefined;
  returnFocus?.focus();
}
function dialogKeys(e: KeyboardEvent) {
  if (e.key === "Escape" && !busy.value) {
    e.preventDefault();
    cancelDelete();
  }
  if (e.key === "Tab") {
    const buttons = confirm.value?.querySelectorAll<HTMLButtonElement>(
      "button:not(:disabled)",
    );
    if (!buttons?.length) return;
    const first = buttons[0],
      last = buttons[buttons.length - 1];
    if (e.shiftKey && document.activeElement === first) {
      e.preventDefault();
      last?.focus();
    } else if (!e.shiftKey && document.activeElement === last) {
      e.preventDefault();
      first?.focus();
    }
  }
}
watch(
  () => [props.client, props.ownSession] as const,
  () => {
    invalidate();
    page.value = 1;
    error.value = "";
    void load();
  },
  { immediate: true },
);
onBeforeUnmount(() => {
  alive = false;
  epoch++;
  controller?.abort();
  clear();
});
</script>
<template>
  <div class="announcements-page">
    <p v-if="error" role="alert" class="alert error">{{ error }}</p>
    <p v-if="notice" role="status" class="alert success">{{ notice }}</p>
    <section
      v-if="formOpen"
      ref="editorSection"
      class="panel announcement-editor"
      role="region"
      @keydown="formKeys"
      :aria-label="selected ? '编辑公告' : '添加公告'"
    >
      <h2>{{ selected ? "编辑公告" : "添加公告" }}</h2>
      <form @submit.prevent="save">
        <label class="field-label">公告内容</label
        ><RichTextEditor
          v-model="content"
          label="公告内容"
          :disabled="!canSave"
        />
        <div class="announcement-fields">
          <label
            >排序<input
              v-model="sort"
              class="sort-input"
              aria-label="排序"
              inputmode="numeric"
              :disabled="!canSave"
              maxlength="10"
            /><small class="muted">数字越小越靠前</small></label
          ><label
            >文字颜色<span class="color-input"
              ><input
                v-model="color"
                aria-label="公告文字颜色"
                placeholder="#333333"
                maxlength="7"
                :disabled="!canSave" /><input
                type="color"
                aria-label="选择公告文字颜色"
                :value="color || '#333333'"
                :disabled="!canSave"
                @input="
                  color = ($event.target as HTMLInputElement).value
                " /></span
            ><small class="muted">留空使用默认颜色</small></label
          >
        </div>
        <div class="modal-actions">
          <button type="button" :disabled="busy" @click="closeForm">
            {{ selected ? "取消编辑" : "清空" }}</button
          ><button
            v-if="uncertain"
            type="button"
            :disabled="busy || loading"
            @click="load"
          >
            刷新核对公告</button
          ><button type="submit" class="primary" :disabled="!canSave">
            {{ busy ? "处理中…" : selected ? "保存" : "添加" }}
          </button>
        </div>
      </form>
    </section>
    <section class="panel announcement-list" aria-label="已发公告">
      <div class="panel-heading">
        <div class="announcement-head-actions">
          <button
            v-if="schema?.permissions.create"
            type="button"
            class="primary"
            :disabled="!writable"
            @click="openAdd"
          >
            添加公告</button
          ><button type="button" :disabled="busy || loading" @click="load">
            刷新核对公告
          </button>
        </div>
      </div>
      <p v-if="loading && !rows.length" class="empty" role="status">
        正在加载公告…
      </p>
      <p v-else-if="!rows.length" class="empty">暂无公告</p>
      <article v-for="r in rows" :key="r.id" class="announcement-row">
        <div class="announcement-copy">
          <RichContent
            :content="r.content"
            :format="r.content_format"
            :color="r.color"
          /><small class="muted"
            ><time v-if="r.createdAt"
              >{{ beijingTimestamp(r.createdAt) }} · </time
            >排序：{{ r.sort }} · {{ r.enabled ? "显示中" : "已隐藏" }}</small
          >
        </div>
        <div class="announcement-actions">
          <button
            v-if="schema?.permissions.update"
            type="button"
            :disabled="!writable"
            @click="mutate(r.id, 'PATCH', { enabled: !r.enabled })"
          >
            {{ r.enabled ? "隐藏" : "显示" }}</button
          ><button
            v-if="schema?.permissions.update"
            type="button"
            :disabled="!writable"
            @click="edit(r)"
          >
            编辑</button
          ><button
            v-if="schema?.permissions.delete"
            type="button"
            class="danger"
            :disabled="!writable"
            @click="askRemove(r)"
          >
            删除
          </button>
        </div>
      </article>
      <div v-if="total > 20" class="pagination">
        <KitPagination
          v-model:page="page"
          :page-size="20"
          :total="total"
          :page-sizes="[20]"
          @change="load"
        />
      </div>
    </section>
    <div v-if="removing" class="modal-backdrop" @keydown="dialogKeys">
      <section
        ref="confirm"
        class="modal announcement-confirm"
        role="dialog"
        aria-modal="true"
        aria-label="删除公告"
      >
        <h2>提示</h2>
        <p>确定要删除此公告吗？</p>
        <RichContent
          :content="removing.content"
          :format="removing.content_format"
        />
        <div class="modal-actions">
          <button type="button" :disabled="busy" @click="cancelDelete">
            取消</button
          ><button
            type="button"
            class="danger"
            :disabled="busy"
            @click="mutate(removing.id, 'DELETE')"
          >
            确定删除
          </button>
        </div>
      </section>
    </div>
  </div>
</template>
<style scoped>
.announcements-page {
  display: grid;
  gap: 20px;
  min-width: 0;
}
.announcement-editor {
  width: 100%;
  min-width: 0;
  padding: var(--space-5, 20px);
  scroll-margin-top: 80px;
}
.announcement-head-actions {
  display: flex;
  gap: var(--space-2);
  flex-wrap: wrap;
}
.announcements-page > section > h2 {
  font-size: 16px;
  margin: 0 0 18px;
}
.announcement-fields {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 20px;
  margin: 18px 0;
}
.announcement-fields label {
  display: flex;
  flex-direction: column;
  gap: 6px;
  min-width: 0;
}
.sort-input {
  max-width: 160px;
}
.field-label {
  display: block;
  margin-bottom: 8px;
}
.color-input {
  display: flex;
  gap: 8px;
}
.color-input > input:first-child {
  min-width: 0;
  flex: 1;
}
.color-input > input[type="color"] {
  width: 44px;
  padding: 3px;
}
.announcement-row {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 18px;
  border-bottom: 1px solid var(--border, var(--line));
  padding: 16px 4px;
}
.announcement-row:last-of-type {
  border-bottom: 0;
}
.announcement-copy {
  min-width: 0;
  flex: 1;
}
.announcement-copy > small {
  display: block;
  margin-top: 8px;
}
.announcement-actions {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
}
.panel-heading {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 12px;
}
.announcement-confirm {
  width: min(520px, calc(100vw - 32px));
  max-height: calc(100dvh - 32px);
  overflow: auto;
}
.announcement-confirm .rich-content {
  max-height: 35dvh;
  overflow: auto;
}
.pagination {
  display: flex;
  gap: 10px;
  align-items: center;
  justify-content: flex-end;
  margin-top: 16px;
  flex-wrap: wrap;
}
@media (max-width: 600px) {
  .announcement-fields {
    grid-template-columns: 1fr;
    gap: 12px;
  }
  .announcement-row {
    flex-direction: column;
  }
  .announcement-actions {
    align-self: flex-end;
  }
  .panel-heading {
    align-items: flex-start;
  }
  .panel-heading button {
    max-width: 160px;
  }
}
</style>
