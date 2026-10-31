<script setup lang="ts">
import { computed, nextTick, onMounted, onBeforeUnmount, ref } from "vue";
import type { ApiClient } from "../api";
import ShellIcon from "./ShellIcon.vue";
import {
  readTemplateConfig,
  readTemplateList,
  templateConfigBody,
  templatePreviewImage,
  uploadTemplateImage,
  type TemplateKind,
  type TemplateName,
  type TemplateConfigView,
  type TemplateList,
} from "../page-template";
import KitStatusTag from "./KitStatusTag.vue";
import CustomHtmlEditor from "./CustomHtmlEditor.vue";
const props = defineProps<{
  client: ApiClient;
  adminClass?: string;
  ownSession: boolean;
}>();
const allowed = computed(
  () =>
    props.adminClass === "super" &&
    props.ownSession &&
    props.client.area === "admin",
);
const kind = ref<TemplateKind>("home"),
  catalog = ref<TemplateList | null>(null),
  view = ref<TemplateConfigView | null>(null),
  draft = ref<Record<string, string | boolean>>({});
const loading = ref(false),
  busy = ref(false),
  uncertain = ref(false),
  error = ref(""),
  notice = ref(""),
  confirmation = ref<"save" | "select" | null>(null),
  preview = ref(false);
// The card 预览 button opens a template over the whole main content area.
// A direct `src` lets the custom-HTML editor open a specific version preview.
const fullPreview = ref<{
    key: string;
    title: string;
    src: string;
  } | null>(null),
  fullPreviewClose = ref<HTMLButtonElement | null>(null);
let fullPreviewReturn: HTMLElement | null = null;
function openFullPreview(item: { name: string; title: string }, ev: Event) {
  fullPreviewReturn = ev.currentTarget as HTMLElement;
  fullPreview.value = {
    key: `${kind.value}-${item.name}`,
    title: item.title,
    src: `/console/v1/admin/templates/${kind.value}/${encodeURIComponent(item.name)}/preview`,
  };
  void nextTick(() => fullPreviewClose.value?.focus());
}
// A custom HTML save or restore moved the kind-wide revision. Keep the typed
// config form and the catalog on it without reloading, which would remount
// the editor and drop its result message.
function customSaved(revision: string) {
  if (view.value) view.value = { ...view.value, revision };
  if (catalog.value) catalog.value = { ...catalog.value, revision };
}
// The custom-HTML editor opens a version preview with a direct URL.
function openCustomPreview(src: string, title: string) {
  fullPreviewReturn = document.activeElement as HTMLElement;
  fullPreview.value = { key: src, title, src };
  void nextTick(() => fullPreviewClose.value?.focus());
}
function closeFullPreview() {
  fullPreview.value = null;
  void nextTick(() => fullPreviewReturn?.focus());
}
let epoch = 0,
  active = true,
  controller: AbortController | undefined;
const locked = computed(
  () =>
    !allowed.value ||
    loading.value ||
    busy.value ||
    uncertain.value ||
    !view.value ||
    !catalog.value ||
    view.value.revision !== catalog.value.revision,
);
const label: Record<TemplateKind, string> = {
  home: "首页",
  pay: "支付收银台",
  qrpay: "聚合收款页",
};
// Tab names as the original console shows them.
const tabLabel: Record<TemplateKind, string> = {
  home: "首页模板",
  pay: "支付页模板",
  qrpay: "聚合收款页模板",
};
const selectedTitle = computed(
  () =>
    catalog.value?.items.find((item) => item.name === catalog.value?.selected)
      ?.title ?? catalog.value?.selected,
);
const endpoint = computed(() =>
  view.value ? `/templates/${view.value.type}/${view.value.name}` : "",
);
async function load(nextKind = kind.value, nextName?: TemplateName) {
  if (!allowed.value) return;
  controller?.abort();
  controller = new AbortController();
  const token = ++epoch;
  if (nextKind !== kind.value) fullPreview.value = null;
  kind.value = nextKind;
  loading.value = true;
  confirmation.value = null;
  preview.value = false;
  error.value = "";
  notice.value = "";
  view.value = null;
  catalog.value = null;
  try {
    const list = readTemplateList(
      await props.client.request(`/templates?type=${nextKind}`, {
        signal: controller.signal,
      }),
    );
    if (!active || token !== epoch) return;
    if (list.type !== nextKind)
      throw new Error("模板目录类型不一致，请重新读取");
    const name =
      nextName ||
      (list.items.some((item) => item.name === list.selected)
        ? list.selected
        : "default");
    const config = readTemplateConfig(
      await props.client.request(`/templates/${nextKind}/${name}/config`, {
        signal: controller.signal,
      }),
    );
    if (!active || token !== epoch) return;
    if (
      config.type !== nextKind ||
      config.name !== name ||
      config.revision !== list.revision
    )
      throw new Error("模板版本已变化，请重新读取后确认");
    catalog.value = list;
    view.value = config;
    draft.value = { ...config.config };
    uncertain.value = false;
  } catch (e) {
    if (active && token === epoch)
      error.value = e instanceof Error ? e.message : "模板读取失败";
  } finally {
    if (active && token === epoch) loading.value = false;
  }
}
function check(action: "save" | "select") {
  if (locked.value) return;
  try {
    if (action === "save") templateConfigBody(view.value!, draft.value);
    confirmation.value = action;
    error.value = "";
  } catch (e) {
    error.value = String(e instanceof Error ? e.message : e);
  }
}
async function commit() {
  if (locked.value || !confirmation.value || !view.value) return;
  const action = confirmation.value,
    original = { ...view.value },
    token = epoch;
  const body =
    action === "save"
      ? templateConfigBody(original, draft.value)
      : { expected_revision: original.revision, name: original.name };
  confirmation.value = null;
  busy.value = true;
  error.value = "";
  try {
    await props.client.request(
      action === "save"
        ? `${endpoint.value}/config`
        : `/templates/${original.type}/selection`,
      { method: "PUT", body, signal: controller?.signal },
    );
    if (!active || token !== epoch) return;
    await load(original.type, original.name);
    notice.value =
      action === "save"
        ? "模板设置已保存；仅当前选中的模板影响公开页面。"
        : "模板已切换；后续页面使用新外观。";
  } catch (e) {
    if (active && token === epoch) {
      const status = (e as { status?: number })?.status;
      // Selecting custom while no usable version exists is refused with 422
      // and nothing changed: show the reason instead of asking for a reload.
      if (
        action === "select" &&
        original.type === "home" &&
        original.name === "custom" &&
        status === 422
      ) {
        error.value =
          e instanceof Error && e.message ? e.message : "请先保存自定义 HTML";
      } else {
        uncertain.value = true;
        error.value = `${e instanceof Error ? e.message : "保存响应未知"}。请重新读取原版本，禁止自动重发。`;
      }
    }
  } finally {
    if (active) busy.value = false;
  }
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
      draft.value.logo_asset_id = asset.id;
      notice.value =
        "图片已私有入库；请检查并保存模板，当前选中模板引用后才公开。";
    }
  } catch (e) {
    if (active && token === epoch)
      error.value = e instanceof Error ? e.message : "图片上传失败";
  } finally {
    if (active && token === epoch) busy.value = false;
  }
}
onMounted(() => load());
onBeforeUnmount(() => {
  active = false;
  epoch++;
  controller?.abort();
});
</script>
<template>
  <section class="panel template-page">
    <p class="muted template-intro">
      内置首页、支付收银台和聚合收款页模板。修改仅影响外观，订单身份、金额和支付确认规则由原支付流程校验。
    </p>
    <p v-if="!allowed" role="alert" class="alert">
      仅本人超级管理员会话可配置模板。
    </p>
    <template v-else>
      <nav class="template-tabs" aria-label="模板类型">
        <button
          v-for="t in ['home', 'pay', 'qrpay'] as const"
          :key="t"
          type="button"
          :aria-pressed="kind === t"
          :disabled="busy || loading"
          @click="load(t)"
        >
          {{ tabLabel[t] }}
        </button>
      </nav>
      <div class="template-current">
        <span v-if="catalog"
          >当前使用：<KitStatusTag
            tone="info"
            :text="`${selectedTitle}（${catalog.selected}）`"
        /></span>
        <span v-if="catalog" class="muted"
          >类型修订：{{ catalog.revision }}</span
        >
        <button
          type="button"
          class="text-button"
          :disabled="busy || loading"
          @click="load()"
        >
          <ShellIcon name="refresh" />重新读取模板
        </button>
      </div>
      <p v-if="loading" role="status" class="muted">正在读取模板…</p>
      <p v-if="error" role="alert" class="alert error">{{ error }}</p>
      <p v-if="notice" role="status" class="alert success">{{ notice }}</p>
      <div v-if="catalog" class="template-cards">
        <div
          v-for="item in catalog.items"
          :key="item.name"
          class="template-card"
          :class="{ 'is-current': view?.name === item.name }"
        >
          <button
            type="button"
            class="template-card-pick"
            :aria-pressed="view?.name === item.name"
            :disabled="busy || loading"
            @click="load(kind, item.name)"
          >
            <img
              class="template-thumb"
              :src="templatePreviewImage(kind, item.name)"
              alt=""
              loading="lazy"
            />
            <span class="template-card-body">
              <strong
                >{{ item.title
                }}<KitStatusTag
                  v-if="catalog.selected === item.name"
                  tone="success"
                  text="当前使用"
              /></strong>
              <small>标识：{{ item.name }} · {{ item.description }}</small>
            </span>
          </button>
          <button
            type="button"
            class="template-card-preview"
            :aria-label="`预览 ${item.title}`"
            @click="openFullPreview(item, $event)"
          >
            <ShellIcon name="fullscreen" />预览
          </button>
        </div>
      </div>
      <div
        v-if="fullPreview"
        class="template-fullpreview"
        role="dialog"
        aria-modal="true"
        :aria-label="`预览 ${fullPreview.title}`"
        @keydown.esc="closeFullPreview"
      >
        <header class="template-fullpreview-head">
          <strong>{{ fullPreview.title }}</strong>
          <span class="muted"
            >{{ label[kind] }} · 已保存配置，演示数据，不发起支付</span
          >
          <button
            ref="fullPreviewClose"
            type="button"
            class="template-fullpreview-close"
            aria-label="关闭预览"
            @click="closeFullPreview"
          >
            <ShellIcon name="close" />关闭预览
          </button>
        </header>
        <iframe
          :key="fullPreview.key"
          :title="`${fullPreview.title} 预览`"
          :src="fullPreview.src"
          sandbox="allow-same-origin"
          referrerpolicy="no-referrer"
        />
      </div>
      <template v-if="view">
        <h3 class="template-config-title">
          {{ label[kind] }} / {{ view.name }} 配置
        </h3>
        <p v-if="view.fallback" role="alert" class="alert">
          已存外观设置无效，当前显示安全默认值；可检查后保存修复。
        </p>
        <form class="template-form" @submit.prevent="check('save')">
          <fieldset :disabled="locked">
            <div
              v-for="field in view.fields"
              :key="field.name"
              class="template-field"
              :class="`template-field-${field.type}`"
            >
              <template v-if="field.type === 'image'"
                ><label :for="`field-${field.name}`"
                  >{{ field.label }}（PNG/JPEG，最多512KiB）</label
                >
                <div class="template-image">
                  <img
                    v-if="draft.logo_asset_id"
                    class="template-logo"
                    :src="`/console/v1/admin/templates/images/${draft.logo_asset_id}`"
                    alt="当前草稿标志"
                  /><input
                    :id="`field-${field.name}`"
                    type="file"
                    accept="image/png,image/jpeg"
                    @change="upload"
                  /><button
                    v-if="draft.logo_asset_id"
                    type="button"
                    @click="draft.logo_asset_id = ''"
                  >
                    清除标志
                  </button>
                </div></template
              >
              <template v-else-if="field.type === 'switch'"
                ><label
                  ><input v-model="draft[field.name]" type="checkbox" />{{
                    field.label
                  }}</label
                ></template
              >
              <template v-else
                ><label :for="`field-${field.name}`">{{ field.label }}</label
                ><textarea
                  v-if="field.type === 'textarea'"
                  :id="`field-${field.name}`"
                  :value="String(draft[field.name])"
                  @input="
                    draft[field.name] = (
                      $event.target as HTMLTextAreaElement
                    ).value
                  "
                  :maxlength="field.max_length"
                  rows="3" /><select
                  v-else-if="field.type === 'select'"
                  :id="`field-${field.name}`"
                  v-model="draft[field.name]"
                >
                  <option
                    v-for="option in field.options"
                    :key="option.value"
                    :value="option.value"
                  >
                    {{ option.label }}
                  </option></select
                ><span v-else-if="field.type === 'color'" class="template-color"
                  ><input
                    :id="`field-${field.name}`"
                    v-model="draft[field.name]"
                    type="color"
                    :required="field.required"
                  /><code>{{ draft[field.name] }}</code></span
                ><input
                  v-else
                  :id="`field-${field.name}`"
                  v-model="draft[field.name]"
                  type="text"
                  :maxlength="field.max_length"
                  :required="field.required"
              /></template>
            </div>
            <div class="template-actions">
              <button type="submit" class="primary">检查并保存模板设置</button
              ><button
                type="button"
                :disabled="catalog?.selected === view.name"
                @click="check('select')"
              >
                检查并设为当前模板
              </button>
            </div>
          </fieldset>
        </form>
        <CustomHtmlEditor
          v-if="kind === 'home' && view.name === 'custom'"
          :client="client"
          @preview="openCustomPreview"
          @saved="customSaved"
        />
        <div
          v-if="confirmation"
          class="template-confirm"
          role="dialog"
          aria-label="确认模板变更"
        >
          <p>
            确认{{
              confirmation === "save" ? "保存完整外观设置" : "切换当前模板"
            }}？版本 {{ view.revision }}。{{
              confirmation === "save" && catalog?.selected === view.name
                ? "保存后立即影响该类型公开页面。"
                : "未选中的模板配置作为草稿保留。"
            }}
          </p>
          <div class="template-confirm-actions">
            <button class="primary" :disabled="locked" @click="commit">
              确认{{ confirmation === "save" ? "保存" : "切换" }}</button
            ><button @click="confirmation = null">取消</button>
          </div>
        </div>
        <div class="template-preview">
          <button :disabled="busy || loading" @click="preview = !preview">
            {{ preview ? "关闭预览" : "预览已保存配置" }}
          </button>
          <p class="muted">
            预览仅使用固定演示订单；草稿须保存后才能预览，不会发起支付。
          </p>
        </div>
        <iframe
          v-if="preview"
          :key="`${endpoint}-${view.revision}`"
          title="内置模板预览"
          :src="`/console/v1/admin${endpoint}/preview`"
          sandbox="allow-same-origin"
          referrerpolicy="no-referrer"
        />
      </template>
    </template>
  </section>
</template>
<style scoped>
.template-page {
  min-width: 0;
}
.template-intro {
  margin: 0 0 12px;
  font-size: 12px;
}
/* Underlined tabs, as in the original console. */
.template-tabs {
  display: flex;
  gap: 28px;
  margin: 0 0 16px;
  border-bottom: 1px solid var(--line);
}
.template-tabs button {
  position: relative;
  min-height: 40px;
  padding: 0 2px;
  border: 0;
  border-radius: 0;
  background: transparent;
  color: var(--ink);
  font-size: 14px;
}
.template-tabs button:hover {
  background: transparent;
  color: var(--primary-text);
}
.template-tabs button[aria-pressed="true"] {
  color: var(--primary-text);
  font-weight: 500;
}
.template-tabs button[aria-pressed="true"]::after {
  content: "";
  position: absolute;
  left: 0;
  right: 0;
  bottom: -1px;
  height: 2px;
  background: var(--primary);
}
.template-current {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px 16px;
  margin: 0 0 16px;
  font-size: 13px;
}
.template-current .muted {
  font-size: 12px;
}
.template-current .text-button {
  margin-left: auto;
  min-height: var(--control-h-sm);
  font-size: 12px;
}
/* Template cards: preview area, then name, current tag and details. */
.template-cards {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(220px, 264px));
  gap: 16px;
  margin: 0 0 24px;
}
.template-card {
  position: relative;
  display: flex;
  flex-direction: column;
  min-height: 0;
  overflow: hidden;
  border: 1px solid var(--line);
  border-radius: var(--radius-lg);
  background: var(--bg-card);
  color: var(--ink);
}
.template-card:hover {
  border-color: var(--primary-text);
}
.template-card.is-current {
  border-color: var(--primary);
  box-shadow: 0 0 0 1px var(--primary);
}
.template-card-pick {
  display: flex;
  flex-direction: column;
  align-items: stretch;
  width: 100%;
  height: auto;
  min-height: 0;
  padding: 0;
  border: 0;
  border-radius: 0;
  background: transparent;
  color: inherit;
  font: inherit;
  text-align: left;
}
.template-card-pick:hover:not(:disabled) {
  background: transparent;
  color: inherit;
}
.template-card-preview {
  position: absolute;
  top: 8px;
  right: 8px;
  display: inline-flex;
  align-items: center;
  gap: 4px;
  height: 26px;
  min-height: 0;
  padding: 0 10px;
  border: 1px solid var(--line);
  border-radius: 13px;
  background: color-mix(in srgb, var(--bg-card) 90%, transparent);
  color: var(--ink);
  font-size: 12px;
  box-shadow: 0 2px 8px -4px rgb(0 0 0 / 0.5);
}
.template-card-preview:hover {
  border-color: var(--primary-text);
  color: var(--primary-text);
}
.template-card-preview .shell-icon {
  width: 14px;
  height: 14px;
}
/* Card 预览: the template over the whole main content area, right of the sidebar. */
.template-fullpreview {
  position: fixed;
  z-index: 45;
  inset: 0 0 0 var(--sidebar-width, 200px);
  display: flex;
  flex-direction: column;
  background: var(--bg-card);
  border-left: 1px solid var(--line);
}
.template-fullpreview-head {
  display: flex;
  flex: none;
  align-items: center;
  gap: 12px;
  height: 48px;
  padding: 0 12px 0 20px;
  border-bottom: 1px solid var(--line);
}
.template-fullpreview-head .muted {
  font-size: 12px;
}
.template-fullpreview-close {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  margin-left: auto;
}
.template-fullpreview-close .shell-icon {
  width: 14px;
  height: 14px;
}
.template-fullpreview iframe {
  flex: 1;
  width: 100%;
  min-height: 0;
  margin: 0;
  border: 0;
  border-radius: 0;
  background: #fff;
}
@media (max-width: 768px) {
  .template-fullpreview {
    left: 0;
  }
}
.template-thumb {
  display: block;
  width: 100%;
  aspect-ratio: 16 / 9;
  object-fit: cover;
  object-position: top;
  background: var(--bg-raised);
  border-bottom: 1px solid var(--line);
}
.template-card-body {
  display: flex;
  flex-direction: column;
  gap: 6px;
  padding: 12px 14px;
}
.template-card-body strong {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  font-size: 14px;
}
.template-card-body small {
  color: var(--ink-muted);
  font-size: 12px;
  line-height: 1.5;
}
.template-config-title {
  margin: 8px 0 16px;
  padding-bottom: 8px;
  border-bottom: 1px solid var(--line);
}
.template-form fieldset {
  min-width: 0;
  margin: 0;
  padding: 0;
  border: 0;
}
/* One field per row: label on the left, control on the right. */
.template-field {
  display: grid;
  grid-template-columns: 160px minmax(0, 1fr);
  column-gap: 16px;
  align-items: center;
  max-width: 820px;
  margin: 0 0 16px;
}
.template-field > label {
  margin: 0;
  text-align: right;
  font-size: 13px;
  line-height: 1.4;
}
.template-field > :not(label) {
  grid-column: 2;
}
.template-field-switch > label {
  grid-column: 2;
  justify-self: start;
  text-align: left;
}
.template-field-image,
.template-field-textarea {
  align-items: start;
}
.template-field-image > label,
.template-field-textarea > label {
  padding-top: 6px;
}
.template-field input:not([type="checkbox"]):not([type="color"]),
.template-field select,
.template-field textarea {
  width: 100%;
  min-width: 0;
  margin: 0;
}
.template-field textarea {
  min-height: 72px;
}
.template-color {
  display: inline-flex;
  align-items: center;
  gap: 10px;
}
.template-color input[type="color"] {
  width: 56px;
  height: var(--control-h);
  margin: 0;
  padding: 2px;
  cursor: pointer;
}
.template-color code {
  font-family: var(--font-mono);
  font-size: 12px;
  color: var(--ink-muted);
}
.template-image {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 10px;
}
.template-image input[type="file"] {
  width: auto;
  max-width: 360px;
}
.template-logo {
  max-width: 128px;
  max-height: 64px;
  padding: 6px;
  border: 1px dashed var(--line-control);
  border-radius: var(--radius-md);
  background: var(--bg-raised);
  object-fit: contain;
}
.template-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  padding-left: 176px;
  margin-top: 8px;
}
.template-confirm {
  max-width: 820px;
  margin: 16px 0;
  padding: 12px 16px;
  border: 1px solid var(--warning-text);
  border-radius: var(--radius-md);
  background: var(--warning-soft);
}
.template-confirm p {
  margin: 0 0 10px;
}
.template-confirm-actions {
  display: flex;
  gap: 8px;
}
.template-preview {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 12px;
  margin-top: 24px;
  padding-top: 16px;
  border-top: 1px solid var(--line);
}
.template-preview p {
  margin: 0;
  font-size: 12px;
}
iframe {
  display: block;
  width: 100%;
  min-height: 460px;
  margin-top: 12px;
  border: 1px solid var(--line);
  border-radius: var(--radius-md);
  background: #ffffff;
}
@media (max-width: 768px) {
  .template-field {
    grid-template-columns: minmax(0, 1fr);
  }
  .template-field > label {
    text-align: left;
    margin-bottom: 6px;
  }
  .template-field > :not(label),
  .template-field-switch > label {
    grid-column: 1;
  }
  .template-actions {
    padding-left: 0;
  }
}
</style>
