<script setup lang="ts">
import { ref, onMounted, nextTick } from "vue";
import { ApiClient } from "../api";
import WorkflowPage from "./WorkflowPage.vue";
const props = defineProps<{ client: ApiClient; merchantID: string }>();
const emit = defineEmits<{ close: []; changed: [] }>();
const busy = ref(false),
  dialog = ref<HTMLElement>(),
  closeButton = ref<HTMLButtonElement>();
function close() {
  if (!busy.value) emit("close");
}
function keydown(e: KeyboardEvent) {
  if (e.key === "Escape") {
    e.preventDefault();
    e.stopPropagation();
    close();
    return;
  }
  if (e.key !== "Tab") return;
  const controls = Array.from(
      dialog.value?.querySelectorAll<HTMLElement>(
        "button:not(:disabled),a[href],input:not(:disabled),select:not(:disabled),textarea:not(:disabled)",
      ) || [],
    ),
    first = controls[0],
    last = controls.at(-1);
  if (e.shiftKey && document.activeElement === first) {
    e.preventDefault();
    last?.focus();
  } else if (!e.shiftKey && document.activeElement === last) {
    e.preventDefault();
    first?.focus();
  }
}
onMounted(async () => {
  await nextTick();
  closeButton.value?.focus();
});
</script>
<template>
  <Teleport to="body"
    ><div class="modal-backdrop" @click.self="close">
      <section
        ref="dialog"
        role="dialog"
        aria-modal="true"
        aria-labelledby="merchant-manual-withdrawal-title"
        class="modal panel merchant-manual-withdrawal"
        @keydown="keydown"
      >
        <div class="toolbar manual-withdrawal-header">
          <h2 id="merchant-manual-withdrawal-title">手动提现</h2>
          <button
            ref="closeButton"
            type="button"
            aria-label="关闭手动提现"
            :disabled="busy"
            @click="close"
          >
            ×
          </button>
        </div>
        <p class="muted">商户号：{{ merchantID }}</p>
        <WorkflowPage
          :key="merchantID"
          :client="client"
          workflow="settlements"
          area="admin"
          application
          :bound-merchant-i-d="merchantID"
          @busy="(value) => (busy = value)"
          @submitted="emit('changed')"
        />
      </section></div
  ></Teleport>
</template>
<style scoped>
.merchant-manual-withdrawal {
  width: min(720px, calc(100vw - 32px));
  max-height: calc(100dvh - 40px);
  box-sizing: border-box;
}
.manual-withdrawal-header {
  position: sticky;
  top: 0;
  z-index: 1;
  background: var(--panel, var(--bg-card));
  padding-block: 8px;
  margin-top: -8px;
  border-bottom: 1px solid var(--line, var(--line));
}
.manual-withdrawal-header h2 {
  margin: 0;
}
.merchant-manual-withdrawal :deep(.workflow-application-form) {
  border: 0;
  box-shadow: none;
  padding: 0;
}
.merchant-manual-withdrawal :deep(.workflow-application-form > h2) {
  display: none;
}
.merchant-manual-withdrawal :deep(.records) {
  border: 0;
  padding: 0;
}
.merchant-manual-withdrawal :deep(input) {
  max-width: 100%;
  box-sizing: border-box;
}
.merchant-manual-withdrawal :deep(.toolbar) {
  flex-wrap: wrap;
}
@media (max-width: 520px) {
  .merchant-manual-withdrawal {
    padding: 16px;
  }
}
</style>
