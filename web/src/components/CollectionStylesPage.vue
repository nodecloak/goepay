<script setup lang="ts">
import { computed, ref, watch, onBeforeUnmount } from "vue";
import { ApiClient, ApiError } from "../api";
import {
  newCollectionStyle,
  styleDraft,
  readCollectionLayout,
  collectionStylePayload,
  readCollectionStyleID,
  collectionBackground,
  renderCollectionPoster,
  uploadCollectionBackground,
  type CollectionStyleView,
  type CollectionStyleDraft,
} from "../collection-style";
import KitInput from "./KitInput.vue";
import KitSwitch from "./KitSwitch.vue";
import KitEmpty from "./KitEmpty.vue";
import KitModal from "./KitModal.vue";
import KitPagination from "./KitPagination.vue";
const props = defineProps<{
  client: ApiClient;
  ownSession: boolean;
  editing: boolean;
  styleId?: unknown;
}>();
const items = ref<CollectionStyleView[]>([]),
  draft = ref(newCollectionStyle()),
  active = ref<CollectionStyleView | null>(null),
  loading = ref(false),
  busy = ref(false),
  previewing = ref(false),
  blocked = ref(false),
  error = ref(""),
  notice = ref(""),
  preview = ref(""),
  previewSnapshot = ref(""),
  query = ref(""),
  page = ref(1),
  pageSize = ref(12),
  deleteTarget = ref<CollectionStyleView | null>(null),
  cardImages = ref<Record<string, string>>({});
const canvas = ref<HTMLElement | null>(null);
const canWrite = computed(
  () => props.client.area === "admin" && props.ownSession,
);
const immutable = computed(
  () => !canWrite.value || busy.value || loading.value || blocked.value,
);
const filtered = computed(() =>
  items.value.filter(
    (s) =>
      String(s.style?.label || s.fields.title || s.fields.name || "")
        .toLowerCase()
        .includes(query.value.toLowerCase()) ||
      String(s.fields.name || "")
        .toLowerCase()
        .includes(query.value.toLowerCase()),
  ),
);
const visible = computed(() =>
  filtered.value.slice(
    (page.value - 1) * pageSize.value,
    page.value * pageSize.value,
  ),
);
const pendingPreview = computed(
  () => preview.value && JSON.stringify(draft.value) !== previewSnapshot.value,
);
const fontSize = computed({
  get: () => draft.value.font.match(/^(\d+)px/)?.[1] || "60",
  set: (v: string) => (draft.value.font = `${v}px '${fontFamily.value}'`),
});
const fontFamily = computed({
  get: () => draft.value.font.match(/px ['"]?([^'"]+)['"]?$/)?.[1] || "黑体",
  set: (v: string) => (draft.value.font = `${fontSize.value}px '${v}'`),
});
const fontOptions = computed(() => [
  ...new Set([
    "黑体",
    "宋体",
    "微软雅黑",
    "Arial",
    "monospace",
    fontFamily.value,
  ]),
]);
const width = computed(() =>
    Math.max(64, Math.min(4096, Number(draft.value.img_width) || 900)),
  ),
  height = computed(() =>
    Math.max(64, Math.min(4096, Number(draft.value.img_height) || 1200)),
  );
const qrBox = computed(() => ({
  left:
    Math.max(
      0,
      Math.min(100, (Number(draft.value.qr_left) / width.value) * 100),
    ) + "%",
  top:
    Math.max(
      0,
      Math.min(100, (Number(draft.value.qr_top) / height.value) * 100),
    ) + "%",
  width:
    Math.max(
      0,
      Math.min(100, (Number(draft.value.qr_width) / width.value) * 100),
    ) + "%",
  height:
    Math.max(
      0,
      Math.min(100, (Number(draft.value.qr_height) / height.value) * 100),
    ) + "%",
}));
const textBox = computed(() => ({
  left:
    Math.max(
      0,
      Math.min(
        100,
        ((draft.value.rec_name_left === ""
          ? width.value / 2
          : Number(draft.value.rec_name_left)) /
          width.value) *
          100,
      ),
    ) + "%",
  top:
    Math.max(
      0,
      Math.min(100, (Number(draft.value.rec_name_top) / height.value) * 100),
    ) + "%",
}));
let epoch = 0,
  alive = true,
  readController: AbortController | undefined,
  renderController: AbortController | undefined;
let pointer: {
  id: number;
  kind: "qr" | "text" | "resize";
  x: number;
  y: number;
  left: number;
  top: number;
  w: number;
  h: number;
  scale: number;
} | null = null;
const message = (e: unknown) =>
  e instanceof Error ? e.message : "样式操作失败";
function privateImage(address: string) {
  return /^\/collection-style-assets\/[a-f0-9]{64}$/.test(address)
    ? "/console/v1/admin/collection-style-images/" + address.split("/").at(-1)
    : address;
}
async function initialize() {
  const current = ++epoch;
  readController?.abort();
  renderController?.abort();
  const c = new AbortController();
  readController = c;
  loading.value = true;
  busy.value = false;
  previewing.value = false;
  pointer = null;
  deleteTarget.value = null;
  error.value = "";
  notice.value = "";
  blocked.value = false;
  preview.value = "";
  previewSnapshot.value = "";
  cardImages.value = {};
  active.value = null;
  draft.value = newCollectionStyle();
  try {
    if (props.editing) {
      const id = readCollectionStyleID(props.styleId);
      if (id === "") throw Error("样式ID无效，请从样式列表重新打开");
      if (id) {
        const value = await props.client.request<CollectionStyleView>(
          "/collection-styles/" + id,
          { signal: c.signal },
        );
        if (current !== epoch || !alive || c.signal.aborted) return;
        active.value = value;
        draft.value = styleDraft(value);
      }
    } else {
      const value = await props.client.request<{
        items: CollectionStyleView[];
      }>("/collection-styles", { signal: c.signal });
      if (current !== epoch || !alive || c.signal.aborted) return;
      items.value = value.items;
    }
  } catch (e) {
    if (alive && current === epoch && !c.signal.aborted) {
      error.value = message(e);
      blocked.value = true;
    }
  } finally {
    if (alive && current === epoch) loading.value = false;
  }
}
async function showPreview() {
  if (previewing.value || loading.value) return;
  renderController?.abort();
  const c = new AbortController();
  renderController = c;
  const current = epoch;
  previewing.value = true;
  error.value = "";
  try {
    const value = readCollectionLayout({
      ...draft.value,
      name: draft.value.name || "preview_style",
      label: draft.value.label || "收款码样式",
    });
    const url = await renderCollectionPoster(
      value,
      "https://pay.example.test/style-preview",
      "商户",
      { imageSource: privateImage(value.url), signal: c.signal },
    );
    if (alive && current === epoch && !c.signal.aborted) {
      preview.value = url;
      previewSnapshot.value = JSON.stringify(draft.value);
    }
  } catch (e) {
    if (alive && current === epoch && !c.signal.aborted)
      error.value = message(e);
  } finally {
    if (alive && current === epoch) previewing.value = false;
  }
}
async function previewCard(item: CollectionStyleView) {
  if (!item.style || previewing.value) return;
  previewing.value = true;
  error.value = "";
  const current = epoch;
  const c = new AbortController();
  renderController?.abort();
  renderController = c;
  try {
    const png = await renderCollectionPoster(
      item.style,
      "https://pay.example.test/style-preview",
      "商户",
      { imageSource: privateImage(item.style.url), signal: c.signal },
    );
    if (alive && epoch === current && !c.signal.aborted)
      cardImages.value[item.id] = png;
  } catch (e) {
    if (alive && epoch === current && !c.signal.aborted)
      error.value = message(e);
  } finally {
    if (alive && epoch === current) previewing.value = false;
  }
}
async function save() {
  if (immutable.value) return;
  error.value = "";
  let body;
  try {
    body = collectionStylePayload(draft.value);
  } catch (e) {
    error.value = message(e);
    return;
  }
  const current = epoch;
  busy.value = true;
  try {
    const value = await props.client.request<CollectionStyleView>(
      "/collection-styles" + (active.value ? "/" + active.value.id : ""),
      {
        method: active.value ? "PUT" : "POST",
        body: active.value
          ? { expected_revision: active.value.revision, style: body }
          : body,
      },
    );
    if (alive && current === epoch) {
      active.value = value;
      draft.value = styleDraft(value);
      notice.value = "样式已保存，商户生成收款码时会使用当前布局。";
    }
  } catch (e) {
    if (alive && current === epoch) {
      blocked.value = !(
        e instanceof ApiError &&
        e.status >= 400 &&
        e.status < 500 &&
        e.status !== 409 &&
        e.status !== 408
      );
      error.value = blocked.value
        ? "保存结果需核对。请重新读取原样式；新增结果未知时先返回列表按风格标识核对。 " +
          message(e)
        : message(e);
    }
  } finally {
    if (alive && current === epoch) busy.value = false;
  }
}
async function upload(event: Event) {
  const input = event.target as HTMLInputElement,
    file = input.files?.[0];
  if (!file || immutable.value) return;
  busy.value = true;
  error.value = "";
  const current = epoch;
  try {
    const asset = await uploadCollectionBackground(
      props.client,
      file,
      readController?.signal,
    );
    if (alive && current === epoch) {
      draft.value.url = "/collection-style-assets/" + asset.id;
      notice.value = `背景已上传（${asset.width}×${asset.height}），保存启用样式后公开。`;
    }
  } catch (e) {
    if (alive && current === epoch) error.value = message(e);
  } finally {
    input.value = "";
    if (alive && current === epoch) busy.value = false;
  }
}
async function syncDimensions() {
  if (immutable.value || previewing.value) return;
  previewing.value = true;
  error.value = "";
  const current = epoch;
  try {
    const image = await collectionBackground(
      privateImage(draft.value.url),
      readController?.signal,
    );
    if (current !== epoch || !alive) return;
    if (image.naturalWidth < 64 || image.naturalHeight < 64)
      throw Error("画布背景的宽和高至少64像素");
    draft.value.img_width = String(image.naturalWidth);
    draft.value.img_height = String(image.naturalHeight);
    draft.value.qr_width = String(
      Math.min(Number(draft.value.qr_width), image.naturalWidth),
    );
    draft.value.qr_height = String(
      Math.min(Number(draft.value.qr_height), image.naturalHeight),
    );
    draft.value.qr_left = String(
      Math.min(
        Number(draft.value.qr_left),
        image.naturalWidth - Number(draft.value.qr_width),
      ),
    );
    draft.value.qr_top = String(
      Math.min(
        Number(draft.value.qr_top),
        image.naturalHeight - Number(draft.value.qr_height),
      ),
    );
    draft.value.rec_name_top = String(
      Math.min(Number(draft.value.rec_name_top), image.naturalHeight),
    );
    if (draft.value.rec_name_left !== "")
      draft.value.rec_name_left = String(
        Math.min(Number(draft.value.rec_name_left), image.naturalWidth),
      );
  } catch (e) {
    if (alive && current === epoch) error.value = message(e);
  } finally {
    if (alive && current === epoch) previewing.value = false;
  }
}
async function toggle(item: CollectionStyleView) {
  if (!canWrite.value || busy.value) return;
  busy.value = true;
  error.value = "";
  const current = epoch;
  try {
    await props.client.request("/collection-styles/" + item.id, {
      method: "PUT",
      body: {
        expected_revision: item.revision,
        style: {
          enabled: !(item.style?.enabled ?? item.fields.enabled === true),
        },
      },
    });
    if (alive && current === epoch) await initialize();
  } catch (e) {
    if (alive && current === epoch) error.value = message(e);
  } finally {
    if (alive) busy.value = false;
  }
}
async function remove() {
  if (!canWrite.value || busy.value || !deleteTarget.value) return;
  busy.value = true;
  error.value = "";
  const current = epoch;
  const id = deleteTarget.value.id;
  try {
    await props.client.request("/collection-styles/" + id, {
      method: "DELETE",
    });
    if (alive && epoch === current) {
      deleteTarget.value = null;
      await initialize();
    }
  } catch (e) {
    if (alive && epoch === current) error.value = message(e);
  } finally {
    if (alive) busy.value = false;
  }
}
function begin(e: PointerEvent, kind: "qr" | "text" | "resize") {
  if (immutable.value || previewing.value || e.button !== 0 || !canvas.value)
    return;
  e.preventDefault();
  e.stopPropagation();
  (e.currentTarget as HTMLElement).setPointerCapture(e.pointerId);
  pointer = {
    id: e.pointerId,
    kind,
    x: e.clientX,
    y: e.clientY,
    left:
      kind === "text"
        ? draft.value.rec_name_left === ""
          ? width.value / 2
          : Number(draft.value.rec_name_left)
        : Number(draft.value.qr_left),
    top: Number(
      kind === "text" ? draft.value.rec_name_top : draft.value.qr_top,
    ),
    w: Number(draft.value.qr_width),
    h: Number(draft.value.qr_height),
    scale: canvas.value.getBoundingClientRect().width / width.value,
  };
}
function move(e: PointerEvent) {
  if (!pointer || pointer.id !== e.pointerId || immutable.value) return;
  const dx = (e.clientX - pointer.x) / pointer.scale,
    dy = (e.clientY - pointer.y) / pointer.scale;
  if (pointer.kind === "resize") {
    const side = Math.round(
      Math.max(
        32,
        Math.min(
          pointer.w + dx,
          pointer.h + dy,
          width.value - Number(draft.value.qr_left),
          height.value - Number(draft.value.qr_top),
        ),
      ),
    );
    draft.value.qr_width = String(side);
    draft.value.qr_height = String(side);
  } else if (pointer.kind === "qr") {
    draft.value.qr_left = String(
      Math.round(
        Math.max(0, Math.min(width.value - pointer.w, pointer.left + dx)),
      ),
    );
    draft.value.qr_top = String(
      Math.round(
        Math.max(0, Math.min(height.value - pointer.h, pointer.top + dy)),
      ),
    );
  } else {
    draft.value.rec_name_left = String(
      Math.round(Math.max(0, Math.min(width.value, pointer.left + dx))),
    );
    draft.value.rec_name_top = String(
      Math.round(Math.max(0, Math.min(height.value, pointer.top + dy))),
    );
  }
}
function end(e: PointerEvent) {
  if (pointer?.id !== e.pointerId) return;
  pointer = null;
  void showPreview();
}
function moveKey(e: KeyboardEvent, kind: "qr" | "text") {
  if (
    immutable.value ||
    !["ArrowLeft", "ArrowRight", "ArrowUp", "ArrowDown"].includes(e.key)
  )
    return;
  e.preventDefault();
  const step = e.shiftKey ? 10 : 1,
    dx = e.key === "ArrowLeft" ? -step : e.key === "ArrowRight" ? step : 0,
    dy = e.key === "ArrowUp" ? -step : e.key === "ArrowDown" ? step : 0;
  if (kind === "qr") {
    draft.value.qr_left = String(
      Math.max(
        0,
        Math.min(
          width.value - Number(draft.value.qr_width),
          Number(draft.value.qr_left) + dx,
        ),
      ),
    );
    draft.value.qr_top = String(
      Math.max(
        0,
        Math.min(
          height.value - Number(draft.value.qr_height),
          Number(draft.value.qr_top) + dy,
        ),
      ),
    );
  } else {
    draft.value.rec_name_left = String(
      Math.max(
        0,
        Math.min(
          width.value,
          (draft.value.rec_name_left === ""
            ? width.value / 2
            : Number(draft.value.rec_name_left)) + dx,
        ),
      ),
    );
    draft.value.rec_name_top = String(
      Math.max(
        0,
        Math.min(height.value, Number(draft.value.rec_name_top) + dy),
      ),
    );
  }
  void showPreview();
}
watch(() => [props.client, props.editing, props.styleId], initialize, {
  immediate: true,
});
watch(query, () => (page.value = 1));
onBeforeUnmount(() => {
  alive = false;
  epoch++;
  readController?.abort();
  renderController?.abort();
  pointer = null;
});
</script>
<template>
  <section class="collection-styles">
    <div v-if="error" class="alert error" role="alert">{{ error }}</div>
    <div v-if="notice" class="alert success" role="status">{{ notice }}</div>
    <template v-if="!editing">
      <div class="toolbar">
        <label class="style-search"
          >搜索样式<KitInput
            v-model="query"
            placeholder="显示名称或风格标识"
            aria-label="搜索样式" /></label
        ><button :disabled="loading || busy" @click="initialize">刷新</button
        ><RouterLink class="button-link" to="/admin/other/onecode/edit"
          >新增样式</RouterLink
        >
      </div>
      <div class="style-cards">
        <article
          v-for="item in visible"
          :key="item.id"
          class="panel style-card"
        >
          <div class="style-card-image">
            <img
              v-if="cardImages[item.id]"
              :src="cardImages[item.id]"
              :alt="
                String(
                  item.style?.label || item.fields.title || item.fields.name,
                )
              "
            />
            <div v-else class="empty-state">
              {{ item.style ? "点击预览查看收款海报" : "旧版简单颜色样式" }}
            </div>
          </div>
          <h3>
            {{ item.style?.label || item.fields.title || item.fields.name }}
          </h3>
          <p class="muted">
            标识：{{ item.fields.name }} · 排序：{{ item.style?.sort || 0 }}
          </p>
          <div class="toolbar">
            <KitSwitch
              :model-value="item.style?.enabled ?? item.fields.enabled === true"
              :disabled="!canWrite || busy"
              :label="
                (item.style?.enabled ?? item.fields.enabled === true)
                  ? '已启用'
                  : '已禁用'
              "
              @change="toggle(item)"
            /><button
              v-if="item.style"
              :disabled="previewing"
              @click="previewCard(item)"
            >
              预览</button
            ><RouterLink
              :to="{
                path: '/admin/other/onecode/edit',
                query: { id: item.id },
              }"
              >编辑</RouterLink
            ><button
              class="danger"
              :disabled="!canWrite || busy"
              @click="deleteTarget = item"
            >
              删除
            </button>
          </div>
        </article>
      </div>
      <div v-if="loading && !filtered.length" class="loading-row" role="status">
        正在读取收款码样式…
      </div>
      <KitEmpty
        v-else-if="!filtered.length"
        text="暂无收款码样式，点击右上角新增"
      />
      <KitPagination
        v-else
        v-model:page="page"
        v-model:page-size="pageSize"
        :total="filtered.length"
        :page-sizes="[12, 24, 48]"
      />
    </template>
    <template v-else>
      <div class="toolbar">
        <RouterLink to="/admin/other/onecode">返回样式列表</RouterLink
        ><button
          :disabled="busy || loading || (!active && blocked)"
          @click="initialize"
        >
          重新读取</button
        ><span v-if="!ownSession" class="muted">当前会话只读</span>
      </div>
      <p v-if="active && !active.style" class="alert">
        原简单颜色样式尚无海报布局；填写背景及位置后保存才会转换，不会自动改变原样式。
      </p>
      <div class="style-editor-layout">
        <form class="panel style-editor-form" @submit.prevent="save">
          <fieldset :disabled="immutable">
            <h3>基本信息</h3>
            <div class="style-fields">
              <label
                >风格标识<input
                  v-model="draft.name"
                  required
                  maxlength="100"
                  pattern="[A-Za-z][A-Za-z0-9_]*" /></label
              ><label
                >显示名称<input
                  v-model="draft.label"
                  required
                  maxlength="100" /></label
              ><label
                >排序<input
                  v-model="draft.sort"
                  type="number"
                  min="0"
                  max="1000000" /></label
              ><label class="check-field"
                ><input
                  v-model="draft.enabled"
                  type="checkbox"
                />启用样式</label
              >
            </div>
            <h3>背景图</h3>
            <label
              >图片地址<input
                v-model="draft.url"
                placeholder="上传背景图或填写HTTPS图片地址"
                required
                maxlength="2048" /></label
            ><label
              >上传背景图<input
                type="file"
                accept="image/png,image/jpeg"
                @change="upload"
            /></label>
            <p class="muted">
              PNG/JPEG最大1MiB、4MP、4096px边长。HTTPS地址需允许跨域读取；不会由服务器抓取远程图片。
            </p>
            <div class="style-fields">
              <label
                >画布宽度<input
                  v-model="draft.img_width"
                  type="number"
                  min="64"
                  max="4096" /></label
              ><label
                >画布高度<input
                  v-model="draft.img_height"
                  type="number"
                  min="64"
                  max="4096"
              /></label>
            </div>
            <button
              type="button"
              :disabled="previewing"
              @click="syncDimensions"
            >
              同步图片尺寸
            </button>
            <h3>二维码</h3>
            <div class="style-fields">
              <label
                >二维码宽度<input
                  v-model="draft.qr_width"
                  type="number"
                  min="32"
                  max="4096" /></label
              ><label
                >二维码高度<input
                  v-model="draft.qr_height"
                  type="number"
                  min="32"
                  max="4096" /></label
              ><label
                >二维码左边距<input
                  v-model="draft.qr_left"
                  type="number"
                  min="0"
                  max="4096" /></label
              ><label
                >二维码上边距<input
                  v-model="draft.qr_top"
                  type="number"
                  min="0"
                  max="4096" /></label
              ><label
                >前景色<input v-model="draft.foreground" type="color" /></label
              ><label
                >背景色<input
                  v-model="draft.background"
                  type="color"
                  :disabled="draft.background === 'transparent'"
              /></label>
            </div>
            <label class="check-field"
              ><input
                type="checkbox"
                :checked="draft.background === 'transparent'"
                @change="
                  draft.background = ($event.target as HTMLInputElement).checked
                    ? 'transparent'
                    : '#FFFFFF'
                "
              />二维码背景透明</label
            >
            <h3>收款方文字</h3>
            <label
              >文案<select v-model="draft.word">
                <option value="0">不显示</option>
                <option value="1">商户名称</option>
                <option value="2">扫码向 商户 付款</option>
              </select></label
            >
            <div class="style-fields">
              <label
                >字号<input
                  v-model="fontSize"
                  type="number"
                  min="1"
                  max="256" /></label
              ><label
                >字体<select v-model="fontFamily">
                  <option v-for="font in fontOptions" :key="font" :value="font">
                    {{ font }}
                  </option>
                </select></label
              ><label
                >文字颜色<input
                  v-model="draft.font_color"
                  type="color"
                  :disabled="draft.font_color === 'transparent'" /></label
              ><label class="check-field"
                ><input
                  type="checkbox"
                  :checked="draft.font_color === 'transparent'"
                  @change="
                    draft.font_color = ($event.target as HTMLInputElement)
                      .checked
                      ? 'transparent'
                      : '#FFFFFF'
                  "
                />文字透明</label
              ><label
                >文字横坐标<input
                  v-model="draft.rec_name_left"
                  type="number"
                  min="0"
                  max="4096"
                  placeholder="留空居中" /></label
              ><label
                >文字纵坐标<input
                  v-model="draft.rec_name_top"
                  type="number"
                  min="0"
                  max="4096"
              /></label>
            </div>
          </fieldset>
          <div class="toolbar">
            <button
              type="button"
              :disabled="loading || previewing"
              @click="showPreview"
            >
              {{ previewing ? "预览中…" : "预览" }}</button
            ><button class="primary" :disabled="immutable">
              {{ busy ? "正在保存…" : "保存样式" }}
            </button>
          </div>
        </form>
        <aside class="panel style-preview-panel">
          <h3>实时预览</h3>
          <p class="muted">
            拖动二维码或文字；右下角拖动可等比缩放二维码。方向键微调，Shift加速。预览二维码仅为样式示意，保存后商户生成实际收款链接。
          </p>
          <p v-if="pendingPreview" class="muted" role="status">
            布局已修改，请预览确认最终效果。
          </p>
          <div
            v-if="preview"
            ref="canvas"
            class="style-live-canvas"
            :style="{ aspectRatio: width + '/' + height }"
          >
            <img :src="preview" alt="收款码样式预览" draggable="false" /><button
              class="style-qr-overlay"
              type="button"
              :style="qrBox"
              aria-label="拖动二维码"
              :disabled="immutable"
              @pointerdown="begin($event, 'qr')"
              @pointermove="move"
              @pointerup="end"
              @pointercancel="end"
              @keydown="moveKey($event, 'qr')"
            ></button
            ><button
              class="style-resize-overlay"
              type="button"
              :style="{
                left: 'calc(' + qrBox.left + ' + ' + qrBox.width + ' - 14px)',
                top: 'calc(' + qrBox.top + ' + ' + qrBox.height + ' - 14px)',
              }"
              aria-label="缩放二维码"
              :disabled="immutable"
              @pointerdown="begin($event, 'resize')"
              @pointermove="move"
              @pointerup="end"
              @pointercancel="end"
            >
              ↘</button
            ><button
              v-if="draft.word !== '0'"
              class="style-text-overlay"
              type="button"
              :style="textBox"
              aria-label="拖动收款方文字"
              :disabled="immutable"
              @pointerdown="begin($event, 'text')"
              @pointermove="move"
              @pointerup="end"
              @pointercancel="end"
              @keydown="moveKey($event, 'text')"
            ></button>
          </div>
          <div v-else class="empty-state">上传背景或填写图片地址后点击预览</div>
        </aside>
      </div>
    </template>
  </section>
  <KitModal
    v-if="deleteTarget"
    title="删除收款码样式"
    submit-text="确认删除"
    :loading="busy"
    danger
    @close="!busy && (deleteTarget = null)"
    @submit="remove"
  >
    <p>
      确定删除
      {{
        deleteTarget.style?.label ||
        deleteTarget.fields.title ||
        deleteTarget.fields.name
      }}？引用该样式的码将回退默认二维码；不会删除收款码或订单。
    </p>
  </KitModal>
</template>
<style scoped>
.collection-styles {
  min-width: 0;
}
.style-cards {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(min(100%, 260px), 1fr));
  gap: 20px;
}
.style-card {
  min-width: 0;
}
.style-card-image {
  aspect-ratio: 16 / 9;
  display: grid;
  place-items: center;
  background: var(--bg-raised);
  overflow: hidden;
  border: 1px solid var(--line);
  border-radius: var(--radius-sm);
}
.style-card-image img {
  max-width: 100%;
  max-height: 100%;
}
/* The card has no padding of its own; the title and meta line take the
   same 20px side inset as the action row (.toolbar) under them. */
.style-card h3 {
  margin: 16px var(--space-5) 8px;
}
.style-card > p {
  margin-inline: var(--space-5);
}
.style-card .toolbar {
  flex-wrap: wrap;
  gap: 10px;
}
.style-editor-layout {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(260px, 380px);
  gap: 24px;
  align-items: start;
}
.style-editor-form {
  min-width: 0;
  padding: 24px;
}
.style-editor-form fieldset {
  border: 0;
  padding: 0;
  margin: 0;
  min-width: 0;
}
.style-editor-form h3 {
  padding-bottom: 10px;
  border-bottom: 1px solid var(--line);
  margin: 24px 0 18px;
}
.style-editor-form h3:first-child {
  margin-top: 0;
}
.style-fields {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 16px;
}
.style-editor-form label {
  display: block;
  margin: 0 0 14px;
}
.style-editor-form input:not([type="checkbox"]),
.style-editor-form select {
  display: block;
  width: 100%;
  min-width: 0;
  margin-top: 7px;
}
.style-editor-form input[type="color"] {
  height: 38px;
}
.style-editor-form .check-field {
  display: flex;
  gap: 9px;
  align-items: center;
  min-height: 42px;
}
.style-editor-form .toolbar {
  margin-top: 24px;
}
.style-preview-panel {
  position: sticky;
  top: 20px;
  min-width: 0;
  padding: 20px;
}
.style-preview-panel p {
  line-height: 1.7;
}
.style-live-canvas {
  position: relative;
  width: 100%;
  overflow: hidden;
  background: #f0f1f4;
}
.style-live-canvas > img {
  display: block;
  width: 100%;
  height: 100%;
  object-fit: fill;
  user-select: none;
}
.style-live-canvas > button {
  position: absolute;
  padding: 0;
  touch-action: none;
  min-height: 0;
  border-radius: 0;
}
.style-qr-overlay {
  border: 2px dashed #3f6ad8;
  background: transparent;
  cursor: move;
}
.style-text-overlay {
  transform: translateX(-50%);
  border: 1px dashed transparent;
  background: transparent;
  width: 80%;
  height: 24px;
  white-space: nowrap;
  font-size: 11px;
  cursor: move;
}
.style-text-overlay:hover,
.style-text-overlay:focus-visible {
  border-color: #3f6ad8;
  background: #ffffff22;
}
.style-resize-overlay {
  width: 20px;
  height: 20px;
  line-height: 18px;
  background: #3f6ad8;
  color: white;
  border: 0;
  cursor: nwse-resize;
  z-index: 2;
}
.style-qr-overlay:focus-visible,
.style-text-overlay:focus-visible {
  outline: 3px solid #e48926;
  outline-offset: -3px;
}
@media (max-width: 1000px) {
  .style-editor-layout {
    grid-template-columns: minmax(0, 1fr);
  }
  .style-preview-panel {
    position: static;
  }
  .style-live-canvas {
    max-width: 450px;
    margin: auto;
  }
}
@media (max-width: 450px) {
  .style-fields {
    grid-template-columns: minmax(0, 1fr);
  }
  .style-editor-form,
  .style-preview-panel {
    padding: 18px;
  }
  .style-editor-form .toolbar {
    flex-wrap: wrap;
  }
}
</style>
