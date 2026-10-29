<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, watch } from "vue";
type Option = { value: string; label: string } | string;
const props = defineProps<{
  modelValue: string;
  options: Option[];
  placeholder?: string;
  disabled?: boolean;
  ariaLabel?: string;
}>();
const emit = defineEmits<{
  "update:modelValue": [value: string];
  change: [value: string];
}>();
const trigger = ref<HTMLButtonElement>();
const menu = ref<HTMLElement>();
const opened = ref(false);
const normalized = computed(() =>
  props.options.map((o) =>
    typeof o === "string" ? { value: o, label: o } : o,
  ),
);
const current = computed(
  () =>
    normalized.value.find((o) => o.value === props.modelValue)?.label ??
    props.placeholder ??
    "请选择",
);
function close(focus = false) {
  opened.value = false;
  if (focus) trigger.value?.focus();
}
// The visible band the list can use: the window, narrowed by every ancestor
// that clips its content (a list card with overflow hidden, a scroll box).
function visibleBand(el: HTMLElement) {
  let top = 0;
  let bottom = window.innerHeight;
  for (let p = el.parentElement; p; p = p.parentElement) {
    const s = getComputedStyle(p);
    if (/(auto|hidden|scroll|clip)/.test(`${s.overflowY} ${s.overflow}`)) {
      const r = p.getBoundingClientRect();
      top = Math.max(top, r.top);
      bottom = Math.min(bottom, r.bottom);
    }
  }
  return { top, bottom };
}
const dropUp = ref(false);
function open() {
  if (props.disabled) return;
  opened.value = !opened.value;
  if (!opened.value) return;
  dropUp.value = false;
  void nextTick(() => {
    // Open upward when the list would run past the bottom of the visible band
    // (the pager's 条/页 select sits at the bottom of a clipping list card) and
    // fits above, so the list is never cut off and never stretches the page.
    const el = trigger.value;
    const h = menu.value?.offsetHeight ?? 0;
    if (el) {
      const t = el.getBoundingClientRect();
      const band = visibleBand(el);
      if (t.bottom + 4 + h > band.bottom && t.top - 4 - h >= band.top)
        dropUp.value = true;
    }
    menu.value?.focus({ preventScroll: true });
  });
}
function choose(value: string) {
  emit("update:modelValue", value);
  emit("change", value);
  close(true);
}
function outside(event: Event) {
  const target = event.target as Node;
  if (!menu.value?.contains(target) && !trigger.value?.contains(target))
    close();
}
function keydown(event: KeyboardEvent) {
  if (event.key === "Escape") {
    event.preventDefault();
    close(true);
    return;
  }
  const items = Array.from(
    menu.value?.querySelectorAll<HTMLElement>('[role="option"]') || [],
  );
  const index = items.indexOf(document.activeElement as HTMLElement);
  let target = index;
  if (event.key === "ArrowDown") target = (index + 1) % items.length;
  else if (event.key === "ArrowUp")
    target = (index - 1 + items.length) % items.length;
  else return;
  event.preventDefault();
  items[Math.max(0, target)]?.focus();
}
watch(opened, (value) => {
  if (value) document.addEventListener("pointerdown", outside);
  else document.removeEventListener("pointerdown", outside);
});
onBeforeUnmount(() => document.removeEventListener("pointerdown", outside));
</script>
<template>
  <div class="pa-select">
    <button
      ref="trigger"
      type="button"
      class="pa-select-trigger"
      :aria-label="ariaLabel"
      aria-haspopup="listbox"
      :aria-expanded="opened"
      :disabled="disabled"
      @click="open"
      @keydown.down.prevent="!opened && open()"
    >
      <span :class="{ 'pa-muted': modelValue === '' }">{{ current }}</span>
      <i class="pa-icon pa-select-chev" aria-hidden="true"></i>
    </button>
    <div
      v-if="opened"
      ref="menu"
      class="pa-menu"
      :class="{ 'pa-menu--up': dropUp }"
      role="listbox"
      tabindex="-1"
      @keydown="keydown"
    >
      <!-- .prevent: inside a KitField <label>, the option is gone by the time
           the click reaches the label, which would pass it on to the trigger
           and open the menu again. -->
      <button
        v-for="o in normalized"
        :key="o.value"
        type="button"
        class="pa-menu-item"
        role="option"
        :aria-selected="o.value === modelValue"
        @click.prevent="choose(o.value)"
      >
        {{ o.label
        }}<i
          v-if="o.value === modelValue"
          class="pa-icon pa-option-check"
          aria-hidden="true"
        ></i>
      </button>
    </div>
  </div>
</template>
