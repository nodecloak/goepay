<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from "vue";
import type { ApiClient } from "../api";
import ShellIcon from "./ShellIcon.vue";
import KitStatusTag from "./KitStatusTag.vue";
import { beijingTimestamp } from "./../console-datetime";
import {
  customHtmlBody,
  customHtmlByteLength,
  customCssLimit,
  customHtmlLimit,
  readCustomHtml,
  readCustomSample,
  readCustomSaveResult,
  readCustomVersions,
  uploadTemplateImage,
  type CustomNoticeState,
  type CustomVersionRow,
} from "../page-template";
const props = defineProps<{ client: ApiClient }>();
// The parent opens its full preview pane (right of the sidebar) with a direct
// URL; a save reloads the 首页模板 catalog revision used for the card CAS.
const emit = defineEmits<{
  (event: "preview", src: string, title: string): void;
  (event: "saved", revision: string): void;
}>();
const base = "/templates/home/custom";
// Editor state.
const html = ref(""),
  css = ref(""),
  frame = ref(true),
  revision = ref(""),
  current = ref(0);
const versions = ref<CustomVersionRow[]>([]);
const loading = ref(false),
  busy = ref(false),
  uncertain = ref(false),
  error = ref(""),
  notice = ref(""),
  removedReport = ref(""),
  imageCode = ref("");
// Two-step confirmations: save, restore <n>, and 载入示例 over a non-empty editor.
const confirmation = ref<
  | { kind: "save" }
  | { kind: "restore"; version: number }
  | { kind: "sample" }
  | null
>(null);
const copied = ref(false);
let active = true,
  epoch = 0,
  controller: AbortController | undefined,
  copyTimer: ReturnType<typeof setTimeout> | undefined;
const htmlBytes = computed(() => customHtmlByteLength(html.value)),
  cssBytes = computed(() => customHtmlByteLength(css.value));
const htmlOver = computed(() => htmlBytes.value > customHtmlLimit),
  cssOver = computed(() => cssBytes.value > customCssLimit);
const locked = computed(() => loading.value || busy.value || uncertain.value);
// The N of the version a save would create (current newest + 1).
const nextVersion = computed(() => current.value + 1);
function kib(bytes: number): string {
  return `${(bytes / 1024).toFixed(1)} KiB`;
}
const noticeLabel: Record<
  CustomNoticeState,
  { tone: "info" | "success" | "warning"; text: string }
> = {
  pending: { tone: "warning", text: "等待发送" },
  accepted: { tone: "success", text: "已提交" },
  unknown: { tone: "info", text: "结果未知" },
};
function sourceLabel(row: CustomVersionRow): string {
  return row.restored_from > 0 ? `恢复自第 ${row.restored_from} 版` : "保存";
}
async function loadVersions(token: number) {
  const list = readCustomVersions(
    await props.client.request(`${base}/versions`, {
      signal: controller?.signal,
    }),
  );
  if (!active || token !== epoch) return;
  versions.value = list;
}
async function fetchState(token: number) {
  const head = readCustomHtml(
    await props.client.request(`${base}/html`, { signal: controller?.signal }),
  );
  if (!active || token !== epoch) return;
  current.value = head.version;
  revision.value = head.revision;
  html.value = head.html;
  css.value = head.css;
  frame.value = head.frame;
  await loadVersions(token);
  if (!active || token !== epoch) return;
  uncertain.value = false;
}
async function load() {
  controller?.abort();
  controller = new AbortController();
  const token = ++epoch;
  loading.value = true;
  error.value = "";
  notice.value = "";
  removedReport.value = "";
  confirmation.value = null;
  try {
    await fetchState(token);
  } catch (e) {
    if (active && token === epoch)
      error.value = e instanceof Error ? e.message : "自定义模板读取失败";
  } finally {
    if (active && token === epoch) loading.value = false;
  }
}
function formatRemoved(removed: Record<string, number>): string {
  const parts = Object.entries(removed).map(([k, n]) => `${k} ×${n}`);
  return parts.length ? `已移除：${parts.join("，")}` : "未移除任何内容";
}
function checkSave() {
  if (locked.value) return;
  try {
    customHtmlBody(revision.value, html.value, css.value, frame.value);
    confirmation.value = { kind: "save" };
    error.value = "";
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e);
  }
}

async function commit() {
  if (locked.value || !confirmation.value) return;
  const step = confirmation.value;
  confirmation.value = null;
  if (step.kind === "sample") {
    await loadSample();
    return;
  }
  const token = epoch,
    rev = revision.value;
  busy.value = true;
  error.value = "";
  removedReport.value = "";
  try {
    const path =
      step.kind === "save"
        ? `${base}/html`
        : `${base}/versions/${step.version}/restore`;
    const body =
      step.kind === "save"
        ? customHtmlBody(rev, html.value, css.value, frame.value)
        : { expected_revision: rev };
    const result = readCustomSaveResult(
      await props.client.request(path, {
        method: step.kind === "save" ? "PUT" : "POST",
        body,
        signal: controller?.signal,
      }),
    );
    if (!active || token !== epoch) return;
    emit("saved", result.revision);
    // Refresh head + versions under the same token (fetchState does not bump
    // the epoch, so the success notice set below survives).
    await fetchState(token);
    if (!active || token !== epoch) return;
    notice.value = `已${step.kind === "save" ? "保存" : "恢复为"}第 ${result.version} 版`;
    removedReport.value = formatRemoved(result.removed);
  } catch (e) {
    if (active && token === epoch) {
      const err = e as { status?: number; message?: string };
      if (err?.status === 409) {
        uncertain.value = true;
        error.value =
          "首页自定义模板版本已变化，请点击“重新读取”后再保存，禁止自动重发。";
      } else {
        error.value = err?.message || "保存响应未知，请重新读取。";
      }
    }
  } finally {
    if (active && epoch === token) busy.value = false;
  }
}
function loadSampleRequested() {
  if (locked.value) return;
  // Confirm before overwriting a non-empty editor.
  if (html.value.trim() || css.value.trim()) {
    confirmation.value = { kind: "sample" };
    return;
  }
  void loadSample();
}
async function loadSample() {
  const token = epoch;
  busy.value = true;
  error.value = "";
  try {
    const sample = readCustomSample(
      await props.client.request(`${base}/sample`, {
        signal: controller?.signal,
      }),
    );
    if (!active || token !== epoch) return;
    html.value = sample.html;
    css.value = sample.css;
    frame.value = sample.frame;
    notice.value = "已载入示例，可修改后保存为新版本。";
  } catch (e) {
    if (active && token === epoch)
      error.value = e instanceof Error ? e.message : "示例读取失败";
  } finally {
    if (active && epoch === token) busy.value = false;
  }
}
function previewVersion(row: CustomVersionRow) {
  emit(
    "preview",
    `/console/v1/admin${base}/versions/${row.version}/preview`,
    `首页自定义 HTML 第 ${row.version} 版`,
  );
}
async function upload(event: Event) {
  const input = event.target as HTMLInputElement,
    file = input.files?.[0];
  input.value = "";
  if (!file || locked.value) return;
  const token = epoch;
  busy.value = true;
  error.value = "";
  try {
    const asset = await uploadTemplateImage(
      props.client,
      file,
      controller?.signal,
    );
    if (active && token === epoch) {
      imageCode.value = `<img src="/template-assets/${asset.id}" alt="">`;
      notice.value = "图片已入库，复制下方代码粘贴到 HTML 中即可。";
    }
  } catch (e) {
    if (active && token === epoch)
      error.value = e instanceof Error ? e.message : "图片上传失败";
  } finally {
    if (active && epoch === token) busy.value = false;
  }
}
async function copyImageCode() {
  if (!imageCode.value) return;
  let ok = false;
  try {
    if (navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(imageCode.value);
      ok = true;
    }
  } catch {
    ok = false;
  }
  if (!ok) {
    // Fallback for browsers/contexts without the async clipboard API.
    const area = document.createElement("textarea");
    area.value = imageCode.value;
    area.style.position = "fixed";
    area.style.opacity = "0";
    document.body.append(area);
    area.select();
    try {
      ok = document.execCommand("copy");
    } catch {
      ok = false;
    }
    area.remove();
  }
  notice.value = ok ? "已复制图片代码。" : "复制失败，请手动复制。";
  copied.value = ok;
  if (copyTimer) clearTimeout(copyTimer);
  if (ok) copyTimer = setTimeout(() => (copied.value = false), 2000);
}
onMounted(() => load());
onBeforeUnmount(() => {
  active = false;
  epoch++;
  controller?.abort();
  if (copyTimer) clearTimeout(copyTimer);
});
defineExpose({ reload: load });
</script>
<template>
  <section class="custom-html">
    <h3 class="custom-html-title">自定义 HTML</h3>
    <p v-if="loading" role="status" class="muted">正在读取自定义模板…</p>
    <p v-if="error" role="alert" class="alert error">{{ error }}</p>
    <p v-if="notice" role="status" class="alert success">
      {{ notice }}
      <template v-if="removedReport">（{{ removedReport }}）</template>
    </p>
    <form class="custom-html-form" @submit.prevent="checkSave">
      <fieldset :disabled="locked">
        <div class="custom-field">
          <label for="custom-html-input">HTML</label>
          <textarea
            id="custom-html-input"
            v-model="html"
            class="custom-code"
            rows="12"
            spellcheck="false"
            autocapitalize="off"
            autocomplete="off"
          ></textarea>
          <small class="custom-counter" :class="{ 'is-over': htmlOver }"
            >{{ kib(htmlBytes) }} / 64 KiB</small
          >
        </div>
        <div class="custom-field">
          <label for="custom-css-input">CSS</label>
          <textarea
            id="custom-css-input"
            v-model="css"
            class="custom-code"
            rows="8"
            spellcheck="false"
            autocapitalize="off"
            autocomplete="off"
          ></textarea>
          <small class="custom-counter" :class="{ 'is-over': cssOver }"
            >{{ kib(cssBytes) }} / 32 KiB</small
          >
        </div>
        <label class="custom-frame"
          ><input v-model="frame" type="checkbox" />保留系统导航和页脚</label
        >
        <div class="custom-actions">
          <button type="button" @click="loadSampleRequested">载入示例</button
          ><button type="submit" class="primary">检查并保存为新版本</button>
        </div>
      </fieldset>
    </form>
    <div
      v-if="confirmation"
      class="template-confirm"
      role="dialog"
      aria-label="确认自定义模板操作"
    >
      <p v-if="confirmation.kind === 'save'">
        保存后会生成第
        {{ nextVersion }}
        版；如果首页正在使用自定义模板，网站立即显示新版本；同时通知管理员。
      </p>
      <p v-else-if="confirmation.kind === 'restore'">
        确认恢复为第
        {{ confirmation.version }}
        版？这会生成一个新版本，如果首页正在使用自定义模板，网站立即显示；同时通知管理员。
      </p>
      <p v-else>载入示例会覆盖当前 HTML 和 CSS，确认继续？</p>
      <div class="template-confirm-actions">
        <button class="primary" :disabled="locked" @click="commit">
          确认{{
            confirmation.kind === "save"
              ? "保存"
              : confirmation.kind === "restore"
                ? "恢复"
                : "载入"
          }}</button
        ><button @click="confirmation = null">取消</button>
      </div>
    </div>
    <div class="custom-image">
      <label for="custom-image-file">上传图片</label>
      <input
        id="custom-image-file"
        type="file"
        accept="image/png,image/jpeg"
        :disabled="locked"
        @change="upload"
      />
      <template v-if="imageCode">
        <code class="custom-image-code">{{ imageCode }}</code>
        <button type="button" @click="copyImageCode">
          {{ copied ? "已复制" : "复制图片代码" }}
        </button>
      </template>
    </div>
    <h4 class="custom-versions-title">版本</h4>
    <div class="table-scroll">
      <table class="pa-table custom-versions">
        <thead>
          <tr>
            <th>版本</th>
            <th>时间</th>
            <th>操作人</th>
            <th>大小</th>
            <th>来源</th>
            <th>通知</th>
            <th>操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-if="!versions.length">
            <td colspan="7" class="muted custom-empty">暂无版本</td>
          </tr>
          <tr v-for="row in versions" :key="row.version">
            <td>第 {{ row.version }} 版</td>
            <td>{{ beijingTimestamp(row.created_at) }}</td>
            <td>{{ row.actor }}</td>
            <td>
              HTML {{ kib(row.html_bytes) }} · CSS {{ kib(row.css_bytes) }}
            </td>
            <td>{{ sourceLabel(row) }}</td>
            <td>
              <KitStatusTag
                :tone="noticeLabel[row.notice].tone"
                :text="noticeLabel[row.notice].text"
              />
            </td>
            <td class="custom-row-actions">
              <button type="button" @click="previewVersion(row)">预览</button
              ><button
                type="button"
                :disabled="locked"
                @click="
                  confirmation = { kind: 'restore', version: row.version }
                "
              >
                恢复
              </button>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
    <ul class="custom-notes muted">
      <li>不运行脚本；外部图片、字体和样式表不会加载；图片请先在这里上传。</li>
      <li>只影响首页，支付页不变。</li>
      <li>每次保存都会通知管理员（与支付通道自动关闭提醒同一渠道）。</li>
    </ul>
  </section>
</template>
<style scoped>
.custom-html {
  margin-top: 24px;
  padding-top: 16px;
  border-top: 1px solid var(--line);
  min-width: 0;
}
.custom-html-title {
  margin: 0 0 12px;
  font-size: 15px;
}
.custom-html-form fieldset {
  min-width: 0;
  margin: 0;
  padding: 0;
  border: 0;
}
.custom-field {
  display: flex;
  flex-direction: column;
  gap: 6px;
  margin: 0 0 16px;
  min-width: 0;
}
.custom-field > label {
  font-size: 13px;
  font-weight: 500;
}
.custom-code {
  width: 100%;
  min-width: 0;
  margin: 0;
  font-family: var(--font-mono);
  font-size: 12px;
  line-height: 1.5;
  resize: vertical;
}
.custom-counter {
  align-self: flex-end;
  font-size: 12px;
  color: var(--ink-muted);
}
.custom-counter.is-over {
  color: var(--danger-text, #d23);
  font-weight: 600;
}
.custom-frame {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  margin: 0 0 16px;
  font-size: 13px;
}
.custom-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}
.custom-image {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 10px;
  margin: 16px 0;
  font-size: 13px;
}
.custom-image input[type="file"] {
  width: auto;
  max-width: 320px;
}
.custom-image-code {
  font-family: var(--font-mono);
  font-size: 12px;
  padding: 4px 8px;
  border: 1px solid var(--line);
  border-radius: var(--radius-md);
  background: var(--bg-raised);
  word-break: break-all;
}
.custom-versions-title {
  margin: 20px 0 10px;
  font-size: 14px;
}
.custom-versions {
  width: 100%;
}
.custom-empty {
  text-align: center;
  padding: 16px 0;
}
.custom-row-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}
.custom-notes {
  margin: 16px 0 0;
  padding-left: 18px;
  font-size: 12px;
  line-height: 1.6;
}
@media (max-width: 480px) {
  .custom-actions button,
  .custom-image input[type="file"] {
    width: 100%;
    max-width: none;
  }
}
</style>
