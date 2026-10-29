<script setup lang="ts">
import { computed, inject, nextTick, onBeforeUnmount, ref, watch } from "vue";
import { routerKey } from "vue-router";
const router = inject(routerKey, undefined);
const props = defineProps<{
  label: string;
  items: {
    id: string;
    label: string;
    href?: string;
    separatorBefore?: boolean;
  }[];
  triggerText?: string;
  disabled?: boolean;
}>();
const emit = defineEmits<{ action: [id: string] }>();
const trigger = ref<HTMLButtonElement>(),
  menu = ref<HTMLElement>(),
  opened = ref(false),
  position = ref({ top: "0px", left: "0px" });
const actions = computed(() =>
  props.items.filter(
    (item) =>
      !item.href ||
      (/^\/(?![\/\\])/.test(item.href) && !/[\\\r\n]/.test(item.href)),
  ),
);
const name = computed(() => props.label);
function close(focus = false) {
  opened.value = false;
  if (focus) trigger.value?.focus();
}
function outside(event: Event) {
  const target = event.target as Node;
  if (!menu.value?.contains(target) && !trigger.value?.contains(target))
    close();
}
function geometry() {
  if (!opened.value || !trigger.value) return;
  const r = trigger.value.getBoundingClientRect(),
    height = menu.value?.offsetHeight || 250;
  position.value = {
    left: `${Math.max(8, Math.min(r.right - 148, innerWidth - 156))}px`,
    top: `${Math.max(8, Math.min(r.bottom + 4, innerHeight - height - 8))}px`,
  };
}
async function open(last = false) {
  if (props.disabled) return;
  if (opened.value) {
    close(true);
    return;
  }
  opened.value = true;
  await nextTick();
  geometry();
  const buttons =
    menu.value?.querySelectorAll<HTMLElement>('[role="menuitem"]');
  (last ? buttons?.[buttons.length - 1] : buttons?.[0])?.focus();
}
function keydown(event: KeyboardEvent) {
  if (event.key === "Escape") {
    event.preventDefault();
    close(true);
    return;
  }
  if (event.key === "Tab") {
    close(true);
    return;
  }
  const buttons = Array.from(
    menu.value?.querySelectorAll<HTMLElement>('[role="menuitem"]') || [],
  );
  const index = buttons.indexOf(document.activeElement as HTMLElement);
  let target = index;
  if (event.key === "ArrowDown") target = (index + 1) % buttons.length;
  else if (event.key === "ArrowUp")
    target = (index - 1 + buttons.length) % buttons.length;
  else if (event.key === "Home") target = 0;
  else if (event.key === "End") target = buttons.length - 1;
  else return;
  event.preventDefault();
  buttons[target]?.focus();
}
function choose(id: string) {
  close(true);
  if (props.disabled) return;
  emit("action", id);
}
function navigate(event: MouseEvent, href: string) {
  close(true);
  if (props.disabled) {
    event.preventDefault();
    return;
  }
  if (
    router &&
    event.button === 0 &&
    !event.ctrlKey &&
    !event.metaKey &&
    !event.shiftKey &&
    !event.altKey &&
    !event.defaultPrevented
  ) {
    event.preventDefault();
    void router.push(href);
  }
}
watch(opened, (value) => {
  if (value) {
    document.addEventListener("pointerdown", outside);
    window.addEventListener("resize", geometry);
    window.addEventListener("scroll", geometry, true);
  } else cleanup();
});
watch(
  () => [props.label, props.items, props.disabled],
  () => close(),
  { flush: "sync" },
);
function cleanup() {
  document.removeEventListener("pointerdown", outside);
  window.removeEventListener("resize", geometry);
  window.removeEventListener("scroll", geometry, true);
}
onBeforeUnmount(cleanup);
</script>
<template>
  <button
    ref="trigger"
    type="button"
    class="order-menu-trigger"
    :class="{ 'related-menu-trigger': !!triggerText }"
    :aria-label="name"
    aria-haspopup="menu"
    :aria-expanded="opened"
    :disabled="disabled"
    @click="open()"
    @keydown.down.prevent="!opened && open()"
    @keydown.up.prevent="!opened && open(true)"
  >
    <span>{{ triggerText || "···" }}</span>
  </button>
  <Teleport to="body"
    ><div
      v-if="opened"
      ref="menu"
      class="order-action-menu"
      role="menu"
      :aria-label="name"
      :style="position"
      @keydown="keydown"
    >
      <template v-for="action in actions" :key="action.id"
        ><div
          v-if="action.separatorBefore"
          class="order-action-divider"
          role="separator"
        />
        <a
          v-if="action.href"
          role="menuitem"
          :href="action.href"
          @click="navigate($event, action.href)"
          >{{ action.label }}</a
        >
        <button
          v-else
          type="button"
          role="menuitem"
          :class="{ 'order-delete': action.id === 'delete' }"
          @click="choose(action.id)"
        >
          {{ action.label }}
        </button></template
      >
    </div></Teleport
  >
</template>
<style scoped>
.order-menu-trigger {
  padding: 0 6px;
  white-space: nowrap;
  font-size: 12px;
  line-height: 1;
  margin-left: 4px;
  border: 1px solid var(--line-control);
  border-radius: var(--radius-sm);
  background: var(--bg-raised);
  color: var(--ink);
  min-width: 28px;
  height: 24px;
  min-height: 24px;
  font-weight: 600;
  letter-spacing: 1px;
  cursor: pointer;
  vertical-align: middle;
}
.order-menu-trigger:hover {
  background: var(--bg-hover);
  color: var(--ink);
  border-color: var(--line-control);
}
.order-menu-trigger:focus-visible {
  outline: 2px solid var(--focus);
  outline-offset: 2px;
}
.order-action-menu {
  position: fixed;
  z-index: 1400;
  width: 148px;
  max-height: calc(100dvh - 16px);
  overflow-y: auto;
  box-sizing: border-box;
  padding: var(--space-1);
  background: var(--bg-raised);
  border: 1px solid var(--line);
  border-radius: var(--radius-md);
  box-shadow: var(--shadow-popover);
  color: var(--ink);
}
.order-action-menu button,
.order-action-menu a {
  display: flex;
  align-items: center;
  width: 100%;
  border: 0;
  border-radius: var(--radius-sm);
  background: transparent;
  padding: 0 var(--space-3);
  min-height: 32px;
  text-align: left;
  white-space: nowrap;
  font-size: 13px;
  color: inherit;
  cursor: pointer;
  box-sizing: border-box;
  text-decoration: none;
}
.order-action-menu button:hover,
.order-action-menu a:hover {
  background: var(--bg-hover);
  color: var(--ink);
}
.order-action-menu button:focus-visible,
.order-action-menu a:focus-visible {
  background: var(--bg-hover);
  color: var(--ink);
  outline: 2px solid var(--focus);
  outline-offset: -2px;
}
.order-action-menu .order-delete {
  color: var(--danger-text);
}
.order-action-menu .order-delete:hover {
  color: var(--danger-text);
}
.related-menu-trigger {
  background: var(--bg-card);
  color: var(--ink);
  font-weight: 400;
  font-size: 13px;
  letter-spacing: 0;
  height: var(--control-h);
  min-height: var(--control-h);
  padding: 0 12px;
}
.related-menu-trigger:hover {
  background: var(--bg-card);
  border-color: var(--primary-text);
  color: var(--primary-text);
}
.order-action-divider {
  height: 1px;
  margin: var(--space-1) 0;
  background: var(--line);
}
@media (prefers-reduced-motion: reduce) {
  .order-action-menu {
    transition: none;
  }
}
</style>
