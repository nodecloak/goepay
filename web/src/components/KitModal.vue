<script setup lang="ts">
import { nextTick, onBeforeUnmount, onMounted, ref } from "vue";
const props = defineProps<{
  title: string;
  loading?: boolean;
  submitText?: string;
  cancelText?: string;
  width?: string;
  danger?: boolean;
  /** Disable the submit button without showing the loading spinner. */
  submitDisabled?: boolean;
  /** Leave out the 取消 button; × and Esc still close the dialog. */
  hideCancel?: boolean;
  /** Open as a right drawer (long forms, details, tables). `width` then sets
   *  the drawer's width (560px by default). */
  drawer?: boolean;
}>();
const emit = defineEmits<{ close: []; submit: [] }>();
const panel = ref<HTMLElement>();
let lastFocused: HTMLElement | null = null;
function available(element: HTMLElement) {
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
  return !element.matches(":disabled") && element.tabIndex >= 0;
}
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
  return topDialogElement() === panel.value;
}
function focusableElements() {
  return [
    ...(panel.value?.querySelectorAll<HTMLElement>(
      'a[href],button,input:not([type="hidden"]),select,textarea,summary,[contenteditable="true"],[tabindex]',
    ) || []),
  ].filter(available);
}
function close() {
  if (!props.loading && topDialog()) emit("close");
}
function onKeydown(event: KeyboardEvent) {
  if (event.defaultPrevented || !topDialog()) return;
  if (event.key === "Escape") {
    event.preventDefault();
    close();
    return;
  }
  if (event.key !== "Tab") return;
  const focusable = focusableElements();
  if (!focusable.length) {
    event.preventDefault();
    panel.value?.focus({ preventScroll: true });
    return;
  }
  const first = focusable[0]!;
  const last = focusable[focusable.length - 1]!;
  if (
    event.shiftKey &&
    (document.activeElement === first ||
      document.activeElement === panel.value ||
      !panel.value?.contains(document.activeElement))
  ) {
    event.preventDefault();
    last.focus();
  } else if (
    !event.shiftKey &&
    (document.activeElement === last ||
      document.activeElement === panel.value ||
      !panel.value?.contains(document.activeElement))
  ) {
    event.preventDefault();
    first.focus();
  }
}
onMounted(() => {
  lastFocused = document.activeElement as HTMLElement;
  document.addEventListener("keydown", onKeydown);
  void nextTick(() => {
    if (!topDialog()) return;
    const fields = focusableElements();
    const initial =
      fields.find((element) =>
        element.matches(
          ".pa-modal-body input,.pa-modal-body select,.pa-modal-body textarea",
        ),
      ) ||
      fields[0] ||
      panel.value;
    initial?.focus({ preventScroll: true });
  });
});
onBeforeUnmount(() => {
  document.removeEventListener("keydown", onKeydown);
  const top = topDialogElement();
  if (
    lastFocused?.isConnected &&
    (top === panel.value ||
      (!panel.value?.isConnected && (!top || top.contains(lastFocused))))
  )
    lastFocused.focus({ preventScroll: true });
});
</script>
<template>
  <Teleport to="body">
    <div
      class="pa-scrim pa-modal-scrim"
      :class="{ 'pa-drawer-scrim': drawer }"
      @pointerdown.self="close"
    >
      <div
        ref="panel"
        class="pa-modal"
        :class="{ 'pa-drawer': drawer }"
        role="dialog"
        aria-modal="true"
        tabindex="-1"
        :aria-label="title"
        :style="
          width
            ? drawer
              ? { '--drawer-w': width }
              : { '--modal-w': width }
            : undefined
        "
      >
        <header class="pa-modal-head">
          <h2 class="pa-modal-title">{{ title }}</h2>
          <button
            type="button"
            class="pa-icon-btn pa-icon-btn--ghost pa-modal-x"
            aria-label="关闭"
            :disabled="loading"
            @click="close"
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
            <button
              v-if="!hideCancel"
              type="button"
              class="pa-btn"
              :disabled="loading"
              @click="close"
            >
              {{ cancelText || "取消" }}
            </button>
            <button
              type="submit"
              class="pa-btn"
              :class="[
                danger ? 'pa-btn--danger' : 'pa-btn--primary',
                { 'pa-btn--loading': loading },
              ]"
              :disabled="loading || submitDisabled"
            >
              {{ submitText || "保存" }}
            </button>
          </footer>
        </form>
      </div>
    </div>
  </Teleport>
</template>
