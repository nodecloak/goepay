<script setup lang="ts">
import { ref, onBeforeUnmount } from "vue";
import ShellIcon from "./ShellIcon.vue";
import { copyText } from "../order-number";
// An icon button placed right after a value (e.g. an order number). Clicking
// copies the exact value; the icon turns into a check and 已复制 is announced.
const props = defineProps<{ text: string; label: string }>();
const state = ref<"idle" | "copied" | "failed">("idle");
const button = ref<HTMLButtonElement | null>(null);
let timer: ReturnType<typeof setTimeout> | undefined;
async function copy() {
  const ok = await copyText(props.text, button.value?.parentElement);
  state.value = ok ? "copied" : "failed";
  if (timer) clearTimeout(timer);
  timer = setTimeout(() => (state.value = "idle"), 1500);
}
onBeforeUnmount(() => timer && clearTimeout(timer));
</script>
<template>
  <button
    ref="button"
    type="button"
    class="pa-copy-btn"
    :class="{ 'pa-copy-btn--done': state === 'copied' }"
    :aria-label="'复制' + label"
    :title="
      state === 'copied'
        ? '已复制'
        : state === 'failed'
          ? '复制失败，请手动选择复制'
          : '复制' + label
    "
    @click="copy"
  >
    <ShellIcon :name="state === 'copied' ? 'check' : 'copy'" />
  </button>
  <span class="sr-only" role="status" aria-live="polite">{{
    state === "copied"
      ? label + "已复制"
      : state === "failed"
        ? label + "复制失败"
        : ""
  }}</span>
</template>
<style scoped>
.pa-copy-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 22px;
  height: 22px;
  min-height: 0;
  margin-left: 6px;
  padding: 0;
  border: 0;
  border-radius: var(--radius-sm);
  background: transparent;
  color: var(--ink-muted);
  vertical-align: -5px;
  cursor: pointer;
}
.pa-copy-btn:hover {
  background: var(--bg-hover);
  color: var(--ink);
}
.pa-copy-btn:focus-visible {
  outline: 2px solid var(--primary);
  outline-offset: 1px;
}
.pa-copy-btn--done {
  color: var(--success-text);
}
.pa-copy-btn > .shell-icon {
  width: 14px;
  height: 14px;
  margin: 0;
}
</style>
