<script setup lang="ts">
// KitHelpPopover — a compact 说明 trigger that opens a kit popover with a
// list-level explanation, so long paragraphs no longer push the table down.
// Keyboard reachable: the trigger is a real button, Enter/Space toggles, Esc
// closes and returns focus, outside-click closes. Pass `text`, or the default
// slot for rich content.
import { nextTick, onBeforeUnmount, ref, watch } from "vue";
withDefaults(defineProps<{ label?: string; text?: string }>(), {
  label: "说明",
});
const open = ref(false);
const trigger = ref<HTMLButtonElement>();
const panel = ref<HTMLElement>();
function close(focus = false) {
  open.value = false;
  if (focus) trigger.value?.focus();
}
function toggle() {
  open.value = !open.value;
  if (open.value) void nextTick(() => panel.value?.focus());
}
function onOutside(event: Event) {
  const target = event.target as Node;
  if (!panel.value?.contains(target) && !trigger.value?.contains(target))
    close();
}
function onKeydown(event: KeyboardEvent) {
  if (event.key === "Escape") {
    event.preventDefault();
    close(true);
  }
}
watch(open, (value) => {
  if (value) {
    document.addEventListener("pointerdown", onOutside);
    document.addEventListener("keydown", onKeydown);
  } else {
    document.removeEventListener("pointerdown", onOutside);
    document.removeEventListener("keydown", onKeydown);
  }
});
onBeforeUnmount(() => {
  document.removeEventListener("pointerdown", onOutside);
  document.removeEventListener("keydown", onKeydown);
});
</script>
<template>
  <span class="pa-help">
    <button
      ref="trigger"
      type="button"
      class="pa-help-trigger"
      :aria-label="label"
      aria-haspopup="dialog"
      :aria-expanded="open"
      @click="toggle"
    >
      <i class="pa-help-mark" aria-hidden="true">?</i>{{ label }}
    </button>
    <div
      v-if="open"
      ref="panel"
      class="pa-help-panel"
      role="dialog"
      :aria-label="label"
      tabindex="-1"
    >
      <slot>{{ text }}</slot>
    </div>
  </span>
</template>
