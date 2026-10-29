<script setup lang="ts">
import { nextTick, onBeforeUnmount, onMounted, ref } from "vue";
const props = defineProps<{
  title: string;
  loading?: boolean;
  submitText?: string;
  cancelText?: string;
  width?: string;
  danger?: boolean;
}>();
const emit = defineEmits<{ close: []; submit: [] }>();
const panel = ref<HTMLElement>();
let lastFocused: HTMLElement | null = null;
function onKeydown(event: KeyboardEvent) {
  if (event.key === "Escape") {
    event.preventDefault();
    emit("close");
    return;
  }
  if (event.key !== "Tab") return;
  const focusable = panel.value?.querySelectorAll<HTMLElement>(
    'a[href],button:not([disabled]),input:not([disabled]),select:not([disabled]),textarea:not([disabled]),[tabindex]:not([tabindex="-1"])',
  );
  if (!focusable || !focusable.length) return;
  const first = focusable[0]!;
  const last = focusable[focusable.length - 1]!;
  if (event.shiftKey && document.activeElement === first) {
    event.preventDefault();
    last.focus();
  } else if (!event.shiftKey && document.activeElement === last) {
    event.preventDefault();
    first.focus();
  }
}
onMounted(() => {
  lastFocused = document.activeElement as HTMLElement;
  document.addEventListener("keydown", onKeydown);
  void nextTick(() => {
    panel.value
      ?.querySelector<HTMLElement>(
        "input:not([disabled]),select,textarea,button",
      )
      ?.focus();
  });
});
onBeforeUnmount(() => {
  document.removeEventListener("keydown", onKeydown);
  lastFocused?.focus();
});
</script>
<template>
  <Teleport to="body">
    <div class="pa-scrim pa-modal-scrim" @pointerdown.self="emit('close')">
      <div
        ref="panel"
        class="pa-modal"
        role="dialog"
        aria-modal="true"
        :aria-label="title"
        :style="width ? { width } : undefined"
      >
        <header class="pa-modal-head">
          <h2 class="pa-modal-title">{{ title }}</h2>
          <button
            type="button"
            class="pa-icon-btn pa-icon-btn--ghost pa-modal-x"
            aria-label="关闭"
            @click="emit('close')"
          >
            ×
          </button>
        </header>
        <form class="pa-modal-body" @submit.prevent="emit('submit')">
          <div class="pa-modal-content"><slot /></div>
          <footer class="pa-modal-foot">
            <span class="pa-modal-foot-extra"
              ><slot name="footer-extra"
            /></span>
            <button type="button" class="pa-btn" @click="emit('close')">
              {{ cancelText || "取消" }}
            </button>
            <button
              type="submit"
              class="pa-btn"
              :class="[
                danger ? 'pa-btn--danger' : 'pa-btn--primary',
                { 'pa-btn--loading': loading },
              ]"
              :disabled="loading"
            >
              {{ submitText || "保存" }}
            </button>
          </footer>
        </form>
      </div>
    </div>
  </Teleport>
</template>
