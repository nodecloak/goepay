<script setup lang="ts">
import { nextTick, onMounted, ref, useId } from "vue";
import type { SettlementListRow } from "../settlement-list";
defineProps<{ row: SettlementListRow }>();
const emit = defineEmits<{ close: [] }>(),
  closeButton = ref<HTMLButtonElement>(),
  dialog = ref<HTMLElement>(),
  id = useId();
function keydown(event: KeyboardEvent) {
  if (event.key === "Escape") {
    event.preventDefault();
    event.stopPropagation();
    emit("close");
    return;
  }
  if (event.key !== "Tab") return;
  const buttons = Array.from(
      dialog.value?.querySelectorAll<HTMLButtonElement>(
        "button:not(:disabled)",
      ) || [],
    ),
    first = buttons[0],
    last = buttons.at(-1);
  if (event.shiftKey && document.activeElement === first) {
    event.preventDefault();
    last?.focus();
  } else if (!event.shiftKey && document.activeElement === last) {
    event.preventDefault();
    first?.focus();
  }
}
onMounted(() => void nextTick(() => closeButton.value?.focus()));
</script>
<template>
  <Teleport to="body">
    <div class="modal-backdrop" @click.self="emit('close')">
      <section
        ref="dialog"
        role="dialog"
        aria-modal="true"
        :aria-labelledby="id + '-title'"
        class="modal panel settlement-record-detail"
        @keydown="keydown"
      >
        <div class="toolbar">
          <h2 :id="id + '-title'">结算详情 {{ row.id }}</h2>
          <button
            ref="closeButton"
            type="button"
            aria-label="关闭结算详情"
            @click="emit('close')"
          >
            ×
          </button>
        </div>
        <dl class="record-detail">
          <dt>ID</dt>
          <dd>{{ row.id }}</dd>
          <dt>商户号</dt>
          <dd>{{ row.merchantID }}</dd>
          <dt>业务请求号</dt>
          <dd>{{ row.externalNo }}</dd>
          <dt>结算方式</dt>
          <dd>{{ row.method }}</dd>
          <dt>原收款账号</dt>
          <dd>{{ row.account }}</dd>
          <dt>原收款姓名</dt>
          <dd>{{ row.name }}</dd>
          <dt>结算金额</dt>
          <dd>¥{{ row.gross }}</dd>
          <dt>申请手续费</dt>
          <dd>{{ row.fee === "未记录" ? row.fee : "¥" + row.fee }}</dd>
          <dt>预计到账</dt>
          <dd>
            {{ row.expected === "未记录" ? row.expected : "¥" + row.expected }}
          </dd>
          <dt>已记录实际到账</dt>
          <dd>{{ row.actual === "—" ? row.actual : "¥" + row.actual }}</dd>
          <dt>创建时间</dt>
          <dd>{{ row.createdAt }}</dd>
          <dt>完成时间</dt>
          <dd>{{ row.completedAt }}</dd>
          <dt>申请状态</dt>
          <dd>{{ row.requestStatus }}</dd>
          <dt>资金归属</dt>
          <dd>{{ row.funding }}</dd>
          <dt>出款状态</dt>
          <dd>{{ row.payout }}</dd>
        </dl>
        <p class="muted">
          {{
            row.hint
          }}。收款信息取自原申请快照；未记录的完成时间保留为空，上游未给出时区的时间保持原值。
        </p>
        <div class="modal-actions">
          <button type="button" @click="emit('close')">关闭</button>
        </div>
      </section>
    </div>
  </Teleport>
</template>
<style scoped>
.settlement-record-detail {
  width: min(100%, 660px);
}
.settlement-record-detail .toolbar {
  gap: 12px;
  align-items: flex-start;
}
.settlement-record-detail h2,
.settlement-record-detail dd {
  overflow-wrap: anywhere;
  min-width: 0;
}
.settlement-record-detail dd {
  white-space: pre-wrap;
}
</style>
