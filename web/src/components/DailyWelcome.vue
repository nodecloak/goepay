<script setup lang="ts">
import { ref, watch, nextTick, onBeforeUnmount } from "vue";
import { welcomeOnce } from "../announcement-layout";
import RichContent from "./RichContent.vue";
const props = defineProps<{
  content: string;
  businessDate: string;
  merchantId: string;
  dataMerchantId: string;
  ownSession: boolean;
}>();
const shown = ref(false),
  dialog = ref<HTMLElement>();
let previous: HTMLElement | undefined,
  epoch = 0;
watch(
  () => [
    props.content,
    props.businessDate,
    props.merchantId,
    props.dataMerchantId,
    props.ownSession,
  ],
  async () => {
    const e = ++epoch;
    shown.value = false;
    if (!welcomeOnce(props)) return;
    previous = document.activeElement as HTMLElement;
    shown.value = true;
    await nextTick();
    if (e === epoch)
      dialog.value?.querySelector<HTMLElement>("button")?.focus();
  },
  { immediate: true },
);
function close() {
  shown.value = false;
  epoch++;
  if (previous?.isConnected) previous.focus();
}
function keys(e: KeyboardEvent) {
  if (e.key === "Escape") {
    e.preventDefault();
    close();
  } else if (e.key === "Tab") {
    const all = dialog.value?.querySelectorAll<HTMLElement>(
      'a[href],button:not(:disabled),[tabindex="0"]',
    );
    if (!all?.length) return;
    const first = all[0],
      last = all[all.length - 1];
    if (e.shiftKey && document.activeElement === first) {
      e.preventDefault();
      last?.focus();
    } else if (!e.shiftKey && document.activeElement === last) {
      e.preventDefault();
      first?.focus();
    }
  }
}
onBeforeUnmount(() => {
  epoch++;
  shown.value = false;
});
</script>
<template>
  <Teleport to="body"
    ><div v-if="shown" class="modal-backdrop" @keydown="keys">
      <section
        ref="dialog"
        class="modal daily-welcome"
        role="dialog"
        aria-modal="true"
        aria-label="欢迎回来"
      >
        <div class="welcome-heading">
          <h2>欢迎回来</h2>
          <button type="button" aria-label="关闭欢迎公告" @click="close">
            ×
          </button>
        </div>
        <RichContent :content="content" format="html" />
        <div class="toolbar-actions">
          <button type="button" class="primary" @click="close">我知道了</button>
        </div>
      </section>
    </div></Teleport
  >
</template>
<style scoped>
.daily-welcome {
  width: min(560px, calc(100vw - 32px));
  max-height: calc(100dvh - 32px);
  overflow: auto;
  min-width: 0;
}
.welcome-heading {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 16px;
  margin-bottom: 18px;
}
.welcome-heading h2 {
  font-size: 18px;
  margin: 0;
}
.welcome-heading button {
  font-size: 22px;
  line-height: 1;
  min-width: 32px;
  padding: 4px;
}
.daily-welcome .toolbar-actions {
  justify-content: flex-end;
  margin-top: 20px;
}
</style>
