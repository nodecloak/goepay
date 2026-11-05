<script setup lang="ts">
import { computed, nextTick, onMounted, onBeforeUnmount, ref } from "vue";
import type { ApiClient } from "../api";
import ShellIcon from "./ShellIcon.vue";
import {
  checkTemplatePackage,
  readRemoveResult,
  readRestoreResult,
  readTemplateConfig,
  readTemplateList,
  readTemplateVersions,
  templateConfigBody,
  templatePreviewImage,
  templateRevisionBody,
  templateSampleURL,
  templateVersionPackageURL,
  templateVersionPreviewURL,
  uploadTemplateImage,
  uploadPackageLimit,
  uploadTemplatePackage,
  TemplateUploadError,
  type TemplateKind,
  type TemplateName,
  type TemplateConfigView,
  type TemplateList,
  type TemplateListItem,
  type TemplateUploadCheck,
  type TemplateVersionRow,
  type TemplateNoticeState,
} from "../page-template";
import { beijingTimestamp } from "../console-datetime";
import KitStatusTag from "./KitStatusTag.vue";
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
// `home` indicates an uploaded home template (scripts allowed, no same-origin).
const fullPreview = ref<{
    key: string;
    title: string;
    src: string;
    home: boolean;
  } | null>(null),
  fullPreviewClose = ref<HTMLButtonElement | null>(null);
let fullPreviewReturn: HTMLElement | null = null;
// An uploaded home template runs scripts in the preview iframe; a built-in or
// an uploaded pay/qrpay template is same-origin and runs none (spec §4, §9).
const previewIsHomeUpload = computed(
  () =>
    kind.value === "home" &&
    catalog.value?.items.some(
      (item) => item.name === currentName.value && item.source === "upload",
    ) === true,
);
function openFullPreview(item: TemplateListItem, ev: Event) {
  fullPreviewReturn = ev.currentTarget as HTMLElement;
  const isHomeUpload = kind.value === "home" && item.source === "upload";
  fullPreview.value = {
    key: `${kind.value}-${item.name}`,
    title: item.title,
    src: `/console/v1/admin/templates/${kind.value}/${encodeURIComponent(item.name)}/preview`,
    home: isHomeUpload,
  };
  void nextTick(() => fullPreviewClose.value?.focus());
}
// The versions dialog opens a specific version preview with a direct URL.
function openVersionPreview(row: TemplateVersionRow) {
  if (!versionsFor.value) return;
  fullPreviewReturn = document.activeElement as HTMLElement;
  const isHomeUpload = kind.value === "home";
  fullPreview.value = {
    key: `${kind.value}-${versionsFor.value}-${row.version}`,
    title: `${versionsTitle.value} 第 ${row.version} 版`,
    src: templateVersionPreviewURL(kind.value, versionsFor.value, row.version),
    home: isHomeUpload,
  };
  void nextTick(() => fullPreviewClose.value?.focus());
}
function closeFullPreview() {
  fullPreview.value = null;
  void nextTick(() => fullPreviewReturn?.focus());
}
let epoch = 0,
  active = true,
  controller: AbortController | undefined;
const currentName = computed(() => view.value?.name ?? "");
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
const selectedItem = computed(() =>
  catalog.value?.items.find((item) => item.name === catalog.value?.selected),
);
const selectedTitle = computed(
  () => selectedItem.value?.title ?? catalog.value?.selected,
);
// The selected name may not be in the list (a removed upload): the public
// page then falls back to the default template.
const selectionMissing = computed(() => !!catalog.value && !selectedItem.value);
const uploadItems = computed(
  () => catalog.value?.items.filter((item) => item.source === "upload") ?? [],
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
      throw new Error("模板目录类型不一致，请刷新页面后核对");
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
      throw new Error("模板版本已变化，请刷新页面后核对");
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
      // Selecting an upload with no usable version is refused with 422 and
      // nothing changed: show the reason instead of asking for a reload.
      if (action === "select" && status === 422) {
        error.value =
          e instanceof Error && e.message ? e.message : "无法切换到该模板";
      } else if (status === 409) {
        uncertain.value = true;
        error.value = "模板版本已变化，请刷新页面后核对。";
      } else {
        uncertain.value = true;
        error.value = `${e instanceof Error ? e.message : "保存响应未知"}。请刷新页面后核对；禁止自动重发。`;
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
// --- Template package upload --------------------------------------------
const packageInput = ref<HTMLInputElement | null>(null);
const uploadConfirm = ref<{ check: TemplateUploadCheck; file: File } | null>(
  null,
);
function pickPackage() {
  if (locked.value) return;
  packageInput.value?.click();
}
async function onPackageChosen(event: Event) {
  const input = event.target as HTMLInputElement,
    file = input.files?.[0];
  input.value = "";
  if (!file || locked.value) return;
  if (file.size < 1 || file.size > uploadPackageLimit) {
    error.value = "模板包大小须为1字节至10MiB";
    return;
  }
  const token = epoch;
  busy.value = true;
  error.value = "";
  notice.value = "";
  try {
    const result = await checkTemplatePackage(
      props.client,
      kind.value,
      file,
      controller?.signal,
    );
    if (!active || token !== epoch) return;
    uploadConfirm.value = { check: result, file };
  } catch (e) {
    if (active && token === epoch) error.value = uploadError(e);
  } finally {
    if (active && token === epoch) busy.value = false;
  }
}
function uploadError(e: unknown): string {
  // A server 422 carries a Chinese reason shown verbatim.
  if (e instanceof TemplateUploadError) return e.message;
  return e instanceof Error ? e.message : "模板包处理失败";
}
async function commitUpload() {
  if (!uploadConfirm.value || !catalog.value || locked.value) return;
  const { file } = uploadConfirm.value,
    rev = catalog.value.revision,
    kindValue = kind.value,
    token = epoch;
  uploadConfirm.value = null;
  busy.value = true;
  error.value = "";
  try {
    const result = await uploadTemplatePackage(
      props.client,
      kindValue,
      rev,
      file,
      controller?.signal,
    );
    if (!active || token !== epoch) return;
    // Reload the list and open the uploaded template.
    await load(kindValue, result.name);
    if (!active) return;
    notice.value = result.created
      ? `已上传模板「${result.title}」（${result.name}）第 ${result.version} 版。`
      : `已为「${result.title}」（${result.name}）生成第 ${result.version} 版。`;
  } catch (e) {
    if (active && token === epoch) {
      if (e instanceof TemplateUploadError && e.status === 409) {
        uncertain.value = true;
        error.value = "模板列表版本已变化，请刷新页面后核对模板版本。";
      } else {
        error.value = uploadError(e);
      }
    }
  } finally {
    if (active) busy.value = false;
  }
}
// --- Uploaded template actions ------------------------------------------
async function selectUpload(item: TemplateListItem) {
  // 设为当前 on an uploaded card: load its config, then run the usual select.
  if (locked.value) return;
  await load(kind.value, item.name);
  if (!active || !view.value || locked.value) return;
  check("select");
}
async function removeUpload(item: TemplateListItem) {
  if (!catalog.value) return;
  removeConfirm.value = item;
}
const removeConfirm = ref<TemplateListItem | null>(null);
async function commitRemove() {
  if (!removeConfirm.value || !catalog.value || locked.value) return;
  const item = removeConfirm.value,
    rev = catalog.value.revision,
    kindValue = kind.value,
    token = epoch;
  removeConfirm.value = null;
  busy.value = true;
  error.value = "";
  try {
    const result = readRemoveResult(
      await props.client.request(
        `/templates/${kindValue}/${item.name}/remove`,
        {
          method: "POST",
          body: templateRevisionBody(rev),
          signal: controller?.signal,
        },
      ),
    );
    if (!active) return;
    void result;
    await load(kindValue);
    if (!active) return;
    notice.value = `已删除模板「${item.title}」（${item.name}）。`;
  } catch (e) {
    if (active && token === epoch) {
      const status = (e as { status?: number })?.status;
      if (status === 409) {
        uncertain.value = true;
        error.value = "模板列表版本已变化，请刷新页面后核对模板版本。";
      } else {
        error.value = e instanceof Error ? e.message : "删除失败";
      }
    }
  } finally {
    if (active) busy.value = false;
  }
}
// --- Versions dialog -----------------------------------------------------
const versionsFor = ref<string | null>(null),
  versionsTitle = ref(""),
  versions = ref<TemplateVersionRow[]>([]),
  versionsLoading = ref(false),
  restoreConfirm = ref<TemplateVersionRow | null>(null);
const noticeLabel: Record<
  TemplateNoticeState,
  { tone: "info" | "success" | "warning"; text: string }
> = {
  pending: { tone: "warning", text: "等待发送" },
  accepted: { tone: "success", text: "已提交" },
  unknown: { tone: "info", text: "结果未知" },
};
function operationLabel(row: TemplateVersionRow): string {
  if (row.operation === "restore" && row.restored_from > 0)
    return `恢复自第 ${row.restored_from} 版`;
  return {
    upload: "上传",
    restore: "恢复",
    remove: "删除",
    select: "切换",
  }[row.operation];
}
function kib(bytes: number): string {
  return `${(bytes / 1024).toFixed(1)} KiB`;
}
async function openVersions(item: TemplateListItem) {
  if (locked.value) return;
  versionsFor.value = item.name;
  versionsTitle.value = item.title;
  versions.value = [];
  restoreConfirm.value = null;
  versionsLoading.value = true;
  const token = epoch;
  error.value = "";
  try {
    const list = readTemplateVersions(
      await props.client.request(
        `/templates/${kind.value}/${item.name}/versions`,
        { signal: controller?.signal },
      ),
    );
    if (!active || token !== epoch) return;
    if (versionsFor.value === item.name) versions.value = list;
  } catch (e) {
    if (active && token === epoch) {
      error.value = e instanceof Error ? e.message : "版本读取失败";
      versionsFor.value = null;
    }
  } finally {
    if (active && token === epoch) versionsLoading.value = false;
  }
}
function closeVersions() {
  versionsFor.value = null;
  restoreConfirm.value = null;
}
function downloadVersion(row: TemplateVersionRow) {
  if (!versionsFor.value) return;
  window.open(
    templateVersionPackageURL(kind.value, versionsFor.value, row.version),
    "_blank",
    "noopener",
  );
}
async function commitRestore() {
  if (
    !restoreConfirm.value ||
    !versionsFor.value ||
    !catalog.value ||
    locked.value
  )
    return;
  const row = restoreConfirm.value,
    name = versionsFor.value,
    rev = catalog.value.revision,
    kindValue = kind.value,
    token = epoch;
  restoreConfirm.value = null;
  busy.value = true;
  error.value = "";
  try {
    const result = readRestoreResult(
      await props.client.request(
        `/templates/${kindValue}/${name}/versions/${row.version}/restore`,
        {
          method: "POST",
          body: templateRevisionBody(rev),
          signal: controller?.signal,
        },
      ),
    );
    if (!active) return;
    versionsFor.value = null;
    await load(kindValue, name);
    if (!active) return;
    notice.value = `已把「${versionsTitle.value}」（${name}）恢复为第 ${row.version} 版（新版本第 ${result.version} 版）。`;
  } catch (e) {
    if (active && token === epoch) {
      const status = (e as { status?: number })?.status;
      if (status === 409) {
        uncertain.value = true;
        error.value = "模板列表版本已变化，请刷新页面后核对模板版本。";
      } else {
        error.value = e instanceof Error ? e.message : "恢复失败";
      }
    }
  } finally {
    if (active) busy.value = false;
  }
}
// The sample package download link for the current kind.
const sampleHref = computed(() => templateSampleURL(kind.value));
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
      内置首页、支付收银台和聚合收款页模板，也可上传模板包。修改仅影响外观，订单身份、金额和支付确认规则由原支付流程校验。
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
      </div>
      <div v-if="catalog" class="template-toolbar">
        <button
          type="button"
          class="primary"
          :disabled="locked"
          @click="pickPackage"
        >
          <ShellIcon name="plus" />上传模板
        </button>
        <a
          class="template-sample"
          :href="sampleHref"
          target="_blank"
          rel="noopener"
        >
          <ShellIcon name="export" />下载示例模板包
        </a>
        <span class="muted template-toolbar-hint"
          >上传 .zip 模板包，最大 10 MiB。</span
        >
        <input
          ref="packageInput"
          type="file"
          accept=".zip,application/zip"
          class="template-package-input"
          aria-hidden="true"
          tabindex="-1"
          @change="onPackageChosen"
        />
      </div>
      <p v-if="loading" role="status" class="muted">正在读取模板…</p>
      <p v-if="error" role="alert" class="alert error">{{ error }}</p>
      <p v-if="notice" role="status" class="alert success">{{ notice }}</p>
      <p v-if="selectionMissing" role="alert" class="alert">
        当前选中的模板「{{
          catalog?.selected
        }}」不在列表中（可能已删除），公开页面已回退到默认模板，请切换到其他模板。
      </p>
      <div v-if="catalog" class="template-cards">
        <div
          v-for="item in catalog.items"
          :key="item.name"
          class="template-card"
          :class="{ 'is-current': currentName === item.name }"
        >
          <button
            type="button"
            class="template-card-pick"
            :aria-pressed="currentName === item.name"
            :disabled="busy || loading"
            @click="load(kind, item.name)"
          >
            <img
              v-if="item.source === 'builtin'"
              class="template-thumb"
              :src="templatePreviewImage(kind, item.name)"
              alt=""
              loading="lazy"
            />
            <img
              v-else-if="item.picture_url"
              class="template-thumb"
              :src="item.picture_url"
              alt=""
              loading="lazy"
            />
            <span v-else class="template-thumb template-thumb-placeholder"
              >上传模板</span
            >
            <span class="template-card-body">
              <strong
                >{{ item.title
                }}<KitStatusTag
                  v-if="item.source === 'upload'"
                  tone="info"
                  text="上传" /><KitStatusTag
                  v-if="catalog.selected === item.name"
                  tone="success"
                  text="当前使用"
              /></strong>
              <small
                >标识：{{ item.name }} ·
                <template v-if="item.source === 'upload'"
                  >第 {{ item.upload_version }} 版</template
                ><template v-else>{{ item.description }}</template></small
              >
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
          <div v-if="item.source === 'upload'" class="template-card-actions">
            <button
              type="button"
              :disabled="locked || catalog.selected === item.name"
              @click="selectUpload(item)"
            >
              设为当前
            </button>
            <button
              type="button"
              :disabled="busy || loading"
              @click="load(kind, item.name)"
            >
              配置
            </button>
            <button
              type="button"
              :disabled="locked"
              @click="openVersions(item)"
            >
              版本
            </button>
            <button
              type="button"
              :disabled="catalog.selected === item.name"
              :title="
                catalog.selected === item.name
                  ? '这个模板正在使用，请先切换到其他模板再删除'
                  : undefined
              "
              @click="removeUpload(item)"
            >
              删除
            </button>
          </div>
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
          :sandbox="
            fullPreview.home
              ? 'allow-scripts allow-popups allow-popups-to-escape-sandbox allow-modals'
              : 'allow-same-origin'
          "
          referrerpolicy="no-referrer"
        />
      </div>
      <div
        v-if="uploadConfirm"
        class="template-confirm template-upload-confirm"
        role="dialog"
        aria-label="确认上传模板"
      >
        <h3 class="template-confirm-title">确认上传模板</h3>
        <dl class="template-upload-facts">
          <div>
            <dt>名称</dt>
            <dd>
              {{ uploadConfirm.check.title }}（{{ uploadConfirm.check.name }}）
            </dd>
          </div>
          <div>
            <dt>版本</dt>
            <dd>
              {{
                uploadConfirm.check.exists
                  ? `第 ${uploadConfirm.check.next_version} 版`
                  : "新模板，第 1 版"
              }}
            </dd>
          </div>
          <div>
            <dt>内容</dt>
            <dd>
              {{ uploadConfirm.check.files }} 个文件 ·
              {{ kib(uploadConfirm.check.size) }}
            </dd>
          </div>
        </dl>
        <p v-if="uploadConfirm.check.selected" role="alert" class="alert">
          该模板正在使用，上传后立即生效。
        </p>
        <p v-if="uploadConfirm.check.identical" role="alert" class="alert">
          与当前最新版本完全相同，不会生成新版本，无法上传。
        </p>
        <div
          v-if="uploadConfirm.check.warnings.length"
          class="template-upload-list"
        >
          <strong>警告</strong>
          <ul>
            <li v-for="(w, i) in uploadConfirm.check.warnings" :key="`w${i}`">
              {{ w }}
            </li>
          </ul>
        </div>
        <div
          v-if="Object.keys(uploadConfirm.check.removed).length"
          class="template-upload-list"
        >
          <strong>已移除元素</strong>
          <ul>
            <li
              v-for="(n, key) in uploadConfirm.check.removed"
              :key="`r${key}`"
            >
              {{ key }} ×{{ n }}
            </li>
          </ul>
        </div>
        <div
          v-if="uploadConfirm.check.ignored.length"
          class="template-upload-list"
        >
          <strong>已忽略文件</strong>
          <ul>
            <li v-for="(f, i) in uploadConfirm.check.ignored" :key="`i${i}`">
              {{ f }}
            </li>
          </ul>
        </div>
        <div class="template-confirm-actions">
          <button
            class="primary"
            :disabled="locked || uploadConfirm.check.identical"
            @click="commitUpload"
          >
            确认上传</button
          ><button @click="uploadConfirm = null">取消</button>
        </div>
      </div>
      <div
        v-if="removeConfirm"
        class="template-confirm"
        role="dialog"
        aria-label="确认删除模板"
      >
        <p>
          确认删除上传模板「{{ removeConfirm.title }}」（{{
            removeConfirm.name
          }}）？删除后该模板不再出现在列表中，历史版本仍保留。
        </p>
        <div class="template-confirm-actions">
          <button class="primary" :disabled="locked" @click="commitRemove">
            确认删除</button
          ><button @click="removeConfirm = null">取消</button>
        </div>
      </div>
      <div
        v-if="versionsFor"
        class="template-fullpreview template-versions-pane"
        role="dialog"
        aria-modal="true"
        :aria-label="`${versionsTitle} 版本`"
        @keydown.esc="closeVersions"
      >
        <header class="template-fullpreview-head">
          <strong>{{ versionsTitle }} · 版本</strong>
          <button
            ref="fullPreviewClose"
            type="button"
            class="template-fullpreview-close"
            aria-label="关闭版本"
            @click="closeVersions"
          >
            <ShellIcon name="close" />关闭
          </button>
        </header>
        <div class="template-versions-body">
          <p v-if="versionsLoading" role="status" class="muted">
            正在读取版本…
          </p>
          <div class="table-scroll">
            <table class="pa-table template-versions">
              <thead>
                <tr>
                  <th>版本</th>
                  <th>时间</th>
                  <th>操作人</th>
                  <th>来源</th>
                  <th>内容</th>
                  <th>通知</th>
                  <th>操作</th>
                </tr>
              </thead>
              <tbody>
                <tr v-if="!versionsLoading && !versions.length">
                  <td colspan="7" class="muted template-versions-empty">
                    暂无版本
                  </td>
                </tr>
                <tr v-for="(row, index) in versions" :key="row.version">
                  <td>第 {{ row.version }} 版</td>
                  <td>{{ beijingTimestamp(row.created_at) }}</td>
                  <td>{{ row.actor }}</td>
                  <td>{{ operationLabel(row) }}</td>
                  <td>{{ row.files }} 文件 · {{ kib(row.size) }}</td>
                  <td>
                    <KitStatusTag
                      :tone="noticeLabel[row.notice].tone"
                      :text="noticeLabel[row.notice].text"
                    />
                  </td>
                  <td class="template-versions-actions">
                    <button type="button" @click="openVersionPreview(row)">
                      预览</button
                    ><button type="button" @click="downloadVersion(row)">
                      下载</button
                    ><button
                      v-if="index !== 0"
                      type="button"
                      :disabled="locked"
                      @click="restoreConfirm = row"
                    >
                      恢复
                    </button>
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
          <div
            v-if="restoreConfirm"
            class="template-confirm"
            role="dialog"
            aria-label="确认恢复版本"
          >
            <p>
              确认把「{{ versionsTitle }}」恢复为第
              {{ restoreConfirm.version }}
              版？这会生成一个新版本；如果该模板正在使用，网站立即显示，并通知管理员。
            </p>
            <div class="template-confirm-actions">
              <button class="primary" :disabled="locked" @click="commitRestore">
                确认恢复</button
              ><button @click="restoreConfirm = null">取消</button>
            </div>
          </div>
        </div>
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
          :sandbox="
            previewIsHomeUpload
              ? 'allow-scripts allow-popups allow-popups-to-escape-sandbox allow-modals'
              : 'allow-same-origin'
          "
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
.template-toolbar {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 12px;
  margin: 0 0 16px;
}
.template-toolbar .primary {
  display: inline-flex;
  align-items: center;
  gap: 6px;
}
.template-toolbar .primary .shell-icon,
.template-sample .shell-icon {
  width: 16px;
  height: 16px;
}
.template-sample {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  min-height: var(--control-h);
  padding: 0 14px;
  border: 1px solid var(--line-control);
  border-radius: var(--radius-md);
  background: var(--bg-card);
  color: var(--ink);
  font-size: 13px;
  text-decoration: none;
}
.template-sample:hover {
  border-color: var(--primary-text);
  color: var(--primary-text);
}
.template-toolbar-hint {
  font-size: 12px;
}
.template-package-input {
  position: absolute;
  width: 1px;
  height: 1px;
  padding: 0;
  margin: -1px;
  overflow: hidden;
  clip: rect(0 0 0 0);
  white-space: nowrap;
  border: 0;
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
.template-card-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  padding: 0 12px 12px;
}
.template-card-actions button {
  min-height: var(--control-h-sm);
  padding: 0 10px;
  font-size: 12px;
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
.template-versions-pane {
  background: var(--bg-page, var(--bg-card));
}
.template-versions-body {
  flex: 1;
  min-height: 0;
  overflow: auto;
  padding: 16px 20px;
}
.template-versions {
  width: 100%;
}
.template-versions-empty {
  text-align: center;
  padding: 16px 0;
}
.template-versions-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
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
.template-thumb-placeholder {
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--ink-muted);
  font-size: 13px;
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
.template-confirm-title {
  margin: 0 0 10px;
  font-size: 15px;
}
.template-confirm p {
  margin: 0 0 10px;
}
.template-confirm-actions {
  display: flex;
  gap: 8px;
}
.template-upload-confirm {
  max-width: 560px;
}
.template-upload-facts {
  margin: 0 0 10px;
  display: grid;
  gap: 4px;
}
.template-upload-facts div {
  display: flex;
  gap: 8px;
}
.template-upload-facts dt {
  flex: none;
  width: 56px;
  color: var(--ink-muted);
  font-size: 13px;
}
.template-upload-facts dd {
  margin: 0;
  font-size: 13px;
}
.template-upload-list {
  margin: 0 0 10px;
  font-size: 12px;
}
.template-upload-list strong {
  display: block;
  margin-bottom: 4px;
}
.template-upload-list ul {
  margin: 0;
  padding-left: 18px;
  max-height: 120px;
  overflow: auto;
  line-height: 1.6;
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
.template-fullpreview iframe {
  margin-top: 0;
  border: 0;
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
