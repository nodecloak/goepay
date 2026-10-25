<script setup lang="ts">
import { computed, onMounted, onBeforeUnmount, ref } from "vue";
import type { ApiClient } from "../api";
import {
  readTemplateConfig,
  readTemplateList,
  templateConfigBody,
  uploadTemplateImage,
  type TemplateKind,
  type TemplateName,
  type TemplateConfigView,
  type TemplateList,
} from "../page-template";
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
const kind = ref<TemplateKind>("pay"),
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
const endpoint = computed(() =>
  view.value ? `/templates/${view.value.type}/${view.value.name}` : "",
);
async function load(nextKind = kind.value, nextName?: TemplateName) {
  if (!allowed.value) return;
  controller?.abort();
  controller = new AbortController();
  const token = ++epoch;
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
      nextName || (list.selected === "compact" ? "compact" : "default");
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
      uncertain.value = true;
      error.value = `${e instanceof Error ? e.message : "保存响应未知"}。请重新读取原版本，禁止自动重发。`;
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
    <h2>模板配置</h2>
    <p>
      内置首页、支付收银台和聚合收款页模板。修改仅影响外观，订单身份、金额和支付确认规则由原支付流程校验。
    </p>
    <p v-if="!allowed" role="alert">仅本人超级管理员会话可配置模板。</p>
    <template v-else>
      <nav aria-label="模板类型">
        <button
          v-for="t in ['home', 'pay', 'qrpay'] as const"
          :key="t"
          :aria-pressed="kind === t"
          :disabled="busy || loading"
          @click="load(t)"
        >
          {{ label[t] }}
        </button>
      </nav>
      <button :disabled="busy || loading" @click="load()">重新读取模板</button>
      <p v-if="loading" role="status">正在读取模板…</p>
      <p v-if="error" role="alert">{{ error }}</p>
      <p v-if="notice" role="status">{{ notice }}</p>
      <p v-if="catalog">
        当前选中：{{ catalog.selected }} · 类型修订：{{ catalog.revision }}
      </p>
      <div v-if="catalog" class="template-options">
        <button
          v-for="item in catalog.items"
          :key="item.name"
          :aria-pressed="view?.name === item.name"
          :disabled="busy || loading"
          @click="load(kind, item.name)"
        >
          <strong>{{ item.title }}</strong
          ><span>{{ item.description }}</span>
        </button>
      </div>
      <template v-if="view">
        <h3>{{ label[kind] }} / {{ view.name }}</h3>
        <p v-if="view.fallback" role="alert">
          已存外观设置无效，当前显示安全默认值；可检查后保存修复。
        </p>
        <form @submit.prevent="check('save')">
          <fieldset :disabled="locked">
            <div
              v-for="field in view.fields"
              :key="field.name"
              class="template-field"
            >
              <template v-if="field.type === 'image'"
                ><label :for="`field-${field.name}`"
                  >{{ field.label }}（PNG/JPEG，最多512KiB）</label
                ><input
                  :id="`field-${field.name}`"
                  type="file"
                  accept="image/png,image/jpeg"
                  @change="upload"
                /><img
                  v-if="draft.logo_asset_id"
                  class="template-logo"
                  :src="`/console/v1/admin/templates/images/${draft.logo_asset_id}`"
                  alt="当前草稿标志"
                /><button
                  v-if="draft.logo_asset_id"
                  type="button"
                  @click="draft.logo_asset_id = ''"
                >
                  清除标志
                </button></template
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
                ><input
                  v-else
                  :id="`field-${field.name}`"
                  v-model="draft[field.name]"
                  :type="field.type === 'color' ? 'color' : 'text'"
                  :maxlength="field.max_length"
                  :required="field.required"
              /></template>
            </div>
            <button type="submit">检查并保存模板设置</button
            ><button
              type="button"
              :disabled="catalog?.selected === view.name"
              @click="check('select')"
            >
              检查并设为当前模板
            </button>
          </fieldset>
        </form>
        <div v-if="confirmation" role="dialog" aria-label="确认模板变更">
          <p>
            确认{{
              confirmation === "save" ? "保存完整外观设置" : "切换当前模板"
            }}？版本 {{ view.revision }}。{{
              confirmation === "save" && catalog?.selected === view.name
                ? "保存后立即影响该类型公开页面。"
                : "未选中的模板配置作为草稿保留。"
            }}
          </p>
          <button :disabled="locked" @click="commit">
            确认{{ confirmation === "save" ? "保存" : "切换" }}</button
          ><button @click="confirmation = null">取消</button>
        </div>
        <button :disabled="busy || loading" @click="preview = !preview">
          {{ preview ? "关闭预览" : "预览已保存配置" }}
        </button>
        <p>预览仅使用固定演示订单；草稿须保存后才能预览，不会发起支付。</p>
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
nav,
.template-options {
  display: flex;
  gap: 0.75rem;
  flex-wrap: wrap;
  margin: 1rem 0;
}
.template-options button {
  display: grid;
  text-align: left;
  gap: 0.5rem;
  flex: 1 1 230px;
  max-width: 100%;
}
button[aria-pressed="true"] {
  outline: 2px solid var(--accent, #007c91);
}
fieldset {
  border: 0;
  padding: 0;
  min-width: 0;
}
.template-field {
  display: grid;
  gap: 0.5rem;
  margin: 1rem 0;
  max-width: 42rem;
}
.template-field input:not([type="checkbox"]):not([type="color"]),
select,
textarea {
  width: 100%;
  max-width: 100%;
  box-sizing: border-box;
}
label {
  font-weight: 600;
}
.template-logo {
  max-width: 128px;
  max-height: 96px;
  object-fit: contain;
}
iframe {
  display: block;
  width: 100%;
  min-height: 460px;
  border: 1px solid #ccd6dd;
  margin-top: 1rem;
}
[role="dialog"] {
  padding: 1rem;
  border: 1px solid #007c91;
  border-radius: 8px;
  margin: 1rem 0;
}
button {
  margin: 0.25rem 0;
}
</style>
