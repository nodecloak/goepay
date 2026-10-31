<script setup lang="ts">
import { ref, watch, nextTick, onMounted, onBeforeUnmount } from "vue";
import { richContent, richFragment, safeRichHTML } from "../rich-content";
const props = withDefaults(
  defineProps<{ modelValue: string; label: string; disabled?: boolean }>(),
  { disabled: false },
);
const emit = defineEmits<{ "update:modelValue": [string] }>();
const editor = ref<HTMLElement>(),
  dialog = ref<HTMLElement>(),
  source = ref(""),
  error = ref(""),
  mode = ref<"html" | "link" | "image" | "table" | null>(null),
  url = ref(""),
  linkText = ref(""),
  ink = ref("#333333"),
  paper = ref("#ffffff");
let selection: Range | undefined, returnFocus: HTMLElement | undefined;
function capture() {
  const s = window.getSelection();
  if (
    s?.rangeCount &&
    editor.value?.contains(s.getRangeAt(0).commonAncestorContainer)
  )
    selection = s.getRangeAt(0).cloneRange();
}
function paint(value: string) {
  if (!editor.value) return;
  try {
    if (editor.value.innerHTML !== value)
      editor.value.replaceChildren(richFragment(richContent(value, "html")));
  } catch (e) {
    error.value = e instanceof Error ? e.message : "内容格式错误";
  }
}
function publish() {
  if (!editor.value || props.disabled) return;
  try {
    const html = safeRichHTML(editor.value.innerHTML);
    emit("update:modelValue", html);
    error.value = "";
  } catch (e) {
    error.value = e instanceof Error ? e.message : "内容格式错误";
  }
}
function plain(value: string) {
  const div = document.createElement("div");
  div.textContent = value;
  return div.innerHTML;
}
function insert(html: string) {
  if (props.disabled || !editor.value) return;
  try {
    const fragment = richFragment(richContent(html, "html"));
    if (selection && editor.value.contains(selection.commonAncestorContainer)) {
      selection.deleteContents();
      const last = fragment.lastChild;
      selection.insertNode(fragment);
      if (last) {
        selection.setStartAfter(last);
        selection.collapse(true);
        const s = window.getSelection();
        s?.removeAllRanges();
        s?.addRange(selection);
      }
    } else editor.value.append(fragment);
    editor.value.focus();
    capture();
    publish();
  } catch (e) {
    error.value = e instanceof Error ? e.message : "内容格式错误";
  }
}
function wrap(tag: string, style = "") {
  if (props.disabled || !editor.value) return;
  capture();
  if (
    !selection ||
    !editor.value.contains(selection.commonAncestorContainer) ||
    selection.collapsed
  ) {
    error.value = "请先选中需要排版的文字";
    return;
  }
  const node = document.createElement(tag);
  if (style) node.setAttribute("style", style);
  const contents = selection.extractContents();
  if (tag === "ul" || tag === "ol") {
    const li = document.createElement("li");
    li.append(contents);
    node.append(li);
  } else node.append(contents);
  selection.insertNode(node);
  selection.selectNodeContents(node);
  const s = window.getSelection();
  s?.removeAllRanges();
  s?.addRange(selection);
  publish();
  editor.value.focus();
}
function clearStyle() {
  if (props.disabled || !editor.value) return;
  capture();
  if (
    !selection ||
    selection.collapsed ||
    !editor.value.contains(selection.commonAncestorContainer)
  ) {
    error.value = "请先选中需要清除样式的文字";
    return;
  }
  editor.value.focus();
  const s = window.getSelection();
  s?.removeAllRanges();
  s?.addRange(selection);
  if (typeof document.execCommand !== "function") {
    error.value = "当前浏览器不支持此操作，请使用编辑 HTML 调整";
    return;
  }
  document.execCommand("removeFormat", false);
  capture();
  publish();
}
async function open(value: NonNullable<typeof mode.value>) {
  if (props.disabled) return;
  capture();
  returnFocus = document.activeElement as HTMLElement;
  source.value = safeRichHTML(editor.value?.innerHTML || "");
  url.value = "";
  linkText.value = selection?.toString() || "";
  error.value = "";
  mode.value = value;
  await nextTick();
  dialog.value?.querySelector<HTMLElement>("textarea,input,button")?.focus();
}
async function close() {
  mode.value = null;
  await nextTick();
  returnFocus?.focus();
}
async function apply() {
  if (props.disabled) return;
  try {
    if (mode.value === "html") {
      const html = safeRichHTML(source.value);
      paint(html);
      emit("update:modelValue", html);
    } else if (mode.value === "table")
      insert(
        "<table><tbody><tr><th>标题</th><th>标题</th></tr><tr><td>内容</td><td>内容</td></tr></tbody></table><p><br></p>",
      );
    else {
      const el = document.createElement(mode.value === "link" ? "a" : "img");
      el.setAttribute(mode.value === "link" ? "href" : "src", url.value.trim());
      if (mode.value === "link") {
        el.textContent = linkText.value || url.value;
        el.setAttribute("target", "_blank");
      } else el.setAttribute("alt", linkText.value);
      const safe = safeRichHTML(el.outerHTML),
        test = document.createElement("template");
      test.innerHTML = safe;
      if (
        !test.content.querySelector(
          mode.value === "link" ? "a[href]" : "img[src]",
        )
      )
        throw new Error(
          mode.value === "link"
            ? "请输入有效链接"
            : "图片需使用本站位图地址或有效位图数据",
        );
      insert(safe);
    }
    await close();
    error.value = "";
  } catch (e) {
    error.value = e instanceof Error ? e.message : "内容格式错误";
  }
}
function pasted(e: ClipboardEvent) {
  e.preventDefault();
  if (props.disabled) return;
  capture();
  insert(
    e.clipboardData?.getData("text/html") ||
      plain(e.clipboardData?.getData("text/plain") || ""),
  );
}
function dropped(e: DragEvent) {
  e.preventDefault();
  if (props.disabled) return;
  capture();
  insert(
    e.dataTransfer?.getData("text/html") ||
      plain(e.dataTransfer?.getData("text/plain") || ""),
  );
}
function keys(e: KeyboardEvent) {
  if (e.key === "Escape") {
    e.preventDefault();
    void close();
  }
  if (e.key !== "Tab" || !dialog.value) return;
  const nodes = Array.from(
    dialog.value.querySelectorAll<HTMLElement>(
      'button:not(:disabled),input,textarea,[tabindex="0"]',
    ),
  );
  const first = nodes[0],
    last = nodes.at(-1);
  if (e.shiftKey && document.activeElement === first) {
    e.preventDefault();
    last?.focus();
  } else if (!e.shiftKey && document.activeElement === last) {
    e.preventDefault();
    first?.focus();
  }
}
watch(
  () => props.modelValue,
  (v) => paint(v),
);
watch(
  () => props.disabled,
  (v) => {
    if (v) {
      mode.value = null;
      selection = undefined;
    }
  },
);
onMounted(() => paint(props.modelValue));
onBeforeUnmount(() => {
  selection = undefined;
});
</script>
<template>
  <div class="rich-editor">
    <div
      class="rich-toolbar"
      role="toolbar"
      :aria-label="label + '排版'"
      @pointerdown="capture"
    >
      <button
        v-for="item in [
          { tag: 'strong', label: '粗体', text: 'B' },
          { tag: 'em', label: '斜体', text: 'I' },
          { tag: 'u', label: '下划线', text: 'U' },
          { tag: 's', label: '删除线', text: 'S' },
          { tag: 'h3', label: '标题', text: '标题' },
          { tag: 'p', label: '段落', text: '段落' },
          { tag: 'ul', label: '无序列表', text: '• 列表' },
          { tag: 'ol', label: '有序列表', text: '1. 列表' },
        ]"
        :key="item.tag"
        type="button"
        :title="item.label"
        :aria-label="item.label"
        :disabled="disabled"
        @pointerdown.prevent
        @click="wrap(item.tag)"
      >
        {{ item.text }}
      </button>
      <button
        type="button"
        aria-label="清除样式"
        :disabled="disabled"
        @pointerdown.prevent
        @click="clearStyle"
      >
        清除样式
      </button>
      <label title="文字颜色"
        >文字<input
          v-model="ink"
          type="color"
          aria-label="选中文字颜色"
          :disabled="disabled"
          @input="wrap('span', 'color:' + ink)"
      /></label>
      <label title="背景颜色"
        >背景<input
          v-model="paper"
          type="color"
          aria-label="选中背景颜色"
          :disabled="disabled"
          @input="wrap('span', 'background-color:' + paper)"
      /></label>
      <button
        v-for="item in [
          { mode: 'link', label: '链接' },
          { mode: 'image', label: '图片' },
          { mode: 'table', label: '表格' },
          { mode: 'html', label: '编辑 HTML' },
        ] as const"
        :key="item.mode"
        type="button"
        :data-rich-command="item.mode"
        :disabled="disabled"
        @click="open(item.mode)"
      >
        {{ item.label }}
      </button>
    </div>
    <div
      ref="editor"
      class="rich-content rich-input"
      role="textbox"
      aria-multiline="true"
      :aria-label="label"
      :aria-disabled="disabled"
      :contenteditable="disabled ? 'false' : 'true'"
      @input="publish"
      @keyup="capture"
      @mouseup="capture"
      @paste="pasted"
      @drop="dropped"
      @dragover.prevent
    ></div>
    <p v-if="error" class="error" role="alert">{{ error }}</p>
    <div
      v-if="mode"
      class="modal-backdrop rich-source-backdrop"
      @keydown="keys"
    >
      <section
        ref="dialog"
        class="modal rich-source"
        role="dialog"
        aria-modal="true"
        :aria-label="
          mode === 'html'
            ? '编辑 HTML'
            : mode === 'link'
              ? '插入链接'
              : mode === 'image'
                ? '插入图片'
                : '插入表格'
        "
      >
        <h2>
          {{
            mode === "html"
              ? "编辑 HTML"
              : mode === "link"
                ? "插入链接"
                : mode === "image"
                  ? "插入图片"
                  : "插入表格"
          }}
        </h2>
        <div v-if="mode === 'html'" class="form-rows">
          <label
            ><span class="field-label">HTML 内容</span
            ><textarea
              v-model="source"
              aria-label="HTML 内容"
              maxlength="20000"
              rows="12"
              spellcheck="false"
            ></textarea>
          </label>
        </div>
        <div v-else-if="mode === 'link' || mode === 'image'" class="form-rows">
          <label
            ><span class="field-label">{{
              mode === "link" ? "链接地址" : "图片地址"
            }}</span
            ><input
              v-model="url"
              :aria-label="mode === 'link' ? '链接地址' : '图片地址'"
              maxlength="16000" /></label
          ><label
            ><span class="field-label">{{
              mode === "link" ? "链接文字" : "图片说明"
            }}</span
            ><input v-model="linkText" maxlength="200"
          /></label>
        </div>
        <p v-else>插入两列两行表格，可继续编辑标题和内容。</p>
        <p v-if="error" class="error" role="alert">{{ error }}</p>
        <div class="toolbar-actions">
          <button type="button" data-rich-source="cancel" @click="close">
            取消</button
          ><button
            type="button"
            class="primary"
            data-rich-source="apply"
            @click="apply"
          >
            应用
          </button>
        </div>
      </section>
    </div>
  </div>
</template>
<style scoped>
.rich-editor {
  min-width: 0;
  border: 1px solid var(--border, var(--line));
  border-radius: 5px;
  overflow: hidden;
}
.rich-toolbar {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 4px;
  padding: 8px;
  background: var(--surface-soft, var(--bg-raised));
  border-bottom: 1px solid var(--border, var(--line));
}
.rich-toolbar button {
  min-height: 30px;
  padding: 3px 8px;
}
.rich-toolbar label {
  display: flex;
  align-items: center;
  gap: 3px;
  margin: 0;
  font-size: 12px;
}
.rich-toolbar input[type="color"] {
  width: 28px;
  min-height: 26px;
  height: 26px;
  padding: 1px;
  margin: 0;
}
.rich-input {
  min-height: 190px;
  max-height: 500px;
  overflow: auto;
  padding: 12px;
  outline-offset: -2px;
}
.rich-input[contenteditable="false"] {
  background: var(--bg-raised);
}
.rich-source {
  width: min(760px, calc(100vw - 32px));
  max-height: calc(100dvh - 32px);
  overflow: auto;
  min-width: 0;
}
.rich-source textarea {
  font-family: monospace;
  width: 100%;
  box-sizing: border-box;
}
.rich-source-backdrop {
  z-index: 2200;
}
.error {
  margin: 8px 12px;
  color: var(--danger, var(--danger-text));
}
</style>
