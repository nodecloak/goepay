<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from "vue";
import ShellIcon from "./ShellIcon.vue";
import KitStatusTag from "./KitStatusTag.vue";
import {
  builtinPlugin,
  pluginTypes,
  priceLabel,
  templateKinds,
  type CatalogEntry,
} from "../plugin-catalog";
import { templatePreviewImage } from "../page-template";
const props = defineProps<{ entry: CatalogEntry; title: string }>();
const emit = defineEmits<{ close: [] }>();
const drawer = ref<HTMLElement | null>(null);
let lastFocused: HTMLElement | null = null;
function topDialogElement() {
  const dialogs = [
    ...document.querySelectorAll<HTMLElement>(
      '[role="dialog"][aria-modal="true"]',
    ),
  ].filter((element) => {
    for (
      let current: HTMLElement | null = element;
      current;
      current = current.parentElement
    ) {
      if (
        current.hidden ||
        current.hasAttribute("inert") ||
        getComputedStyle(current).display === "none"
      )
        return false;
    }
    return true;
  });
  return dialogs.at(-1);
}
function topDialog() {
  return topDialogElement() === drawer.value;
}
function close() {
  if (topDialog()) emit("close");
}
function onKeydown(event: KeyboardEvent) {
  if (event.defaultPrevented || !topDialog()) return;
  if (event.key === "Escape") {
    event.preventDefault();
    close();
    return;
  }
  if (event.key !== "Tab") return;
  const focusable = [
    ...(drawer.value?.querySelectorAll<HTMLElement>(
      'a[href],button,input:not([type="hidden"]),select,textarea,summary,[contenteditable="true"],[tabindex]',
    ) || []),
  ].filter((element) => {
    if (element.matches(":disabled") || element.tabIndex < 0) return false;
    for (
      let current: HTMLElement | null = element;
      current;
      current = current.parentElement
    ) {
      const style = getComputedStyle(current);
      if (
        current.hidden ||
        current.hasAttribute("inert") ||
        current.getAttribute("aria-hidden") === "true" ||
        style.display === "none" ||
        style.visibility === "hidden"
      )
        return false;
    }
    return true;
  });
  const first = focusable[0],
    last = focusable.at(-1);
  if (!first || !last) {
    event.preventDefault();
    drawer.value?.focus({ preventScroll: true });
    return;
  }
  if (
    document.activeElement === drawer.value ||
    !drawer.value?.contains(document.activeElement) ||
    (event.shiftKey && document.activeElement === first) ||
    (!event.shiftKey && document.activeElement === last)
  ) {
    event.preventDefault();
    (event.shiftKey ? last : first).focus({ preventScroll: true });
  }
}
const builtin = computed(() => builtinPlugin(props.entry));
const typeLabel = computed(
  () =>
    (props.entry.type === "template" && props.entry.sub
      ? templateKinds[props.entry.sub]
      : undefined) ||
    pluginTypes.find((t) => t[0] === props.entry.type)?.[1] ||
    props.entry.type,
);
onMounted(() => {
  lastFocused =
    document.activeElement instanceof HTMLElement
      ? document.activeElement
      : null;
  document.addEventListener("keydown", onKeydown);
  if (topDialog()) drawer.value?.focus({ preventScroll: true });
});
onBeforeUnmount(() => {
  document.removeEventListener("keydown", onKeydown);
  const top = topDialogElement();
  if (
    lastFocused?.isConnected &&
    (top === drawer.value ||
      (!drawer.value?.isConnected && (!top || top.contains(lastFocused))))
  )
    lastFocused.focus({ preventScroll: true });
});
</script>
<template>
  <div class="modal-backdrop drawer-backdrop" @click.self="close">
    <section
      ref="drawer"
      class="drawer plugin-drawer"
      role="dialog"
      aria-modal="true"
      :aria-label="title"
      tabindex="-1"
    >
      <header class="drawer-head">
        <h2>{{ title }}</h2>
        <button
          type="button"
          class="icon-button"
          :aria-label="'关闭' + title"
          title="关闭"
          @click="close"
        >
          <ShellIcon name="close" />
        </button>
      </header>
      <div class="drawer-body">
        <div class="plugin-drawer-title">
          <img v-if="entry.icon" :src="entry.icon" alt="" />
          <span v-else class="plugin-mark" aria-hidden="true">{{
            Array.from(entry.title)[0]
          }}</span>
          <div>
            <strong>{{ entry.title }}</strong>
            <KitStatusTag
              :tone="builtin ? 'success' : 'neutral'"
              :text="builtin ? '已内置' : '暂不支持'"
            />
          </div>
        </div>
        <img
          v-if="entry.type === 'template' && entry.sub && builtin"
          class="plugin-preview"
          :src="templatePreviewImage(entry.sub, entry.name)"
          :alt="`${entry.title} 预览截图`"
        />
        <dl class="record-detail">
          <dt>标识</dt>
          <dd class="numeric">{{ entry.name }}</dd>
          <dt>类型</dt>
          <dd>{{ typeLabel }}</dd>
          <dt>开发商</dt>
          <dd>{{ entry.author || "-" }}</dd>
          <dt>
            {{
              entry.type === "template" && entry.sub === "home" && builtin
                ? "模板版本"
                : "插件版本"
            }}
          </dt>
          <dd>{{ entry.version || "-" }}</dd>
          <dt>参考售价</dt>
          <dd>{{ priceLabel(entry.price) }}</dd>
          <template v-if="entry.type === 'payment'">
            <dt>包含的支付方式</dt>
            <dd>{{ entry.pay.join(",") || "-" }}</dd>
            <dt>包含的转账方式</dt>
            <dd>{{ entry.trans.join(",") || "-" }}</dd>
          </template>
          <dt>简介</dt>
          <dd>{{ entry.desc || "-" }}</dd>
          <template v-if="entry.link">
            <dt>链接</dt>
            <dd>
              <a :href="entry.link" target="_blank" rel="noreferrer noopener">{{
                entry.link
              }}</a>
            </dd>
          </template>
          <dt>Go 版状态</dt>
          <dd>
            {{ builtin ? builtin.note : "该插件尚未实现，暂不可使用。" }}
          </dd>
        </dl>
      </div>
      <div class="modal-actions drawer-foot">
        <button type="button" @click="close">关闭</button>
      </div>
    </section>
  </div>
</template>
<style scoped>
.plugin-preview {
  display: block;
  width: 100%;
  aspect-ratio: 16 / 9;
  margin: 0 0 16px;
  object-fit: cover;
  object-position: top;
  border: 1px solid var(--line);
  border-radius: var(--radius-md);
  background: var(--bg-raised);
}
.plugin-drawer-title {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 16px;
}
.plugin-drawer-title img,
.plugin-drawer-title .plugin-mark {
  width: 40px;
  height: 40px;
  flex: none;
  border-radius: var(--radius-md);
  object-fit: contain;
}
.plugin-drawer-title > div {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
  font-size: 15px;
}
</style>
