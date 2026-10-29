<script setup lang="ts">
// KitHelpPopover — a compact 说明 trigger that opens a kit popover with a
// list-level explanation, so long paragraphs no longer push the table down.
// Keyboard reachable: the trigger is a real button, Enter/Space toggles, Esc
// closes and returns focus, Tab closes and moves on, outside-click closes.
// Pass `text`, or the default slot for rich content.
//
// The panel is rendered in <body> and fixed to the window, so a card's
// overflow:hidden never clips it. It opens under the trigger, starting at the
// trigger's left edge, or ending at its right edge when there is no room to
// the right, and always stays inside the window.
import { nextTick, onBeforeUnmount, ref, watch } from "vue";
withDefaults(defineProps<{ label?: string; text?: string }>(), {
  label: "说明",
});
const open = ref(false);
const trigger = ref<HTMLButtonElement>();
const panel = ref<HTMLElement>();
const hidden = { visibility: "hidden" } as Record<string, string>;
const place = ref<Record<string, string>>(hidden);
function position() {
  const t = trigger.value?.getBoundingClientRect();
  const p = panel.value;
  if (!open.value || !t || !p) return;
  const gap = 6,
    edge = 8,
    width = p.offsetWidth,
    height = p.offsetHeight;
  let left = t.left;
  if (left + width > innerWidth - edge) left = t.right - width;
  left = Math.max(edge, Math.min(left, innerWidth - edge - width));
  let top = t.bottom + gap;
  if (top + height > innerHeight - edge && t.top - gap - height >= edge)
    top = t.top - gap - height;
  place.value = {
    left: `${Math.round(left)}px`,
    top: `${Math.round(Math.max(edge, top))}px`,
  };
}
function close(focus = false) {
  open.value = false;
  place.value = hidden;
  if (focus) trigger.value?.focus();
}
async function toggle() {
  if (open.value) {
    close();
    return;
  }
  open.value = true;
  await nextTick();
  position();
  panel.value?.focus({ preventScroll: true });
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
  } else if (event.key === "Tab") {
    // Back on the trigger first, so Tab carries on from there.
    close(true);
  }
}
function cleanup() {
  document.removeEventListener("pointerdown", onOutside);
  document.removeEventListener("keydown", onKeydown);
  window.removeEventListener("resize", position);
  window.removeEventListener("scroll", position, true);
}
watch(open, (value) => {
  if (value) {
    document.addEventListener("pointerdown", onOutside);
    document.addEventListener("keydown", onKeydown);
    window.addEventListener("resize", position);
    window.addEventListener("scroll", position, true);
  } else cleanup();
});
onBeforeUnmount(cleanup);
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
    <Teleport to="body">
      <div
        v-if="open"
        ref="panel"
        class="pa-help-panel"
        role="dialog"
        :aria-label="label"
        tabindex="-1"
        :style="place"
      >
        <slot>{{ text }}</slot>
      </div>
    </Teleport>
  </span>
</template>
