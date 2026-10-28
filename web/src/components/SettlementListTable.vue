<script setup lang="ts">
import { computed, nextTick, ref, watch } from "vue";
import type { Area } from "../menu";
import {
  originalSettlementColumns,
  readSettlementListRow,
  type SettlementListRow,
} from "../settlement-list";
import SettlementRecordDetails from "./SettlementRecordDetails.vue";
const props = defineProps<{
  items: Record<string, unknown>[];
  area: Area;
  checked: string[];
  busy: boolean;
}>();
const emit = defineEmits<{
  check: [id: string, value: boolean];
  decision: [row: Record<string, unknown>, choice: "approve" | "reject"];
  payout: [id: string];
}>();
const columns = computed(() => originalSettlementColumns(props.area));
const data = computed(() => {
  try {
    const rows = props.items.map(readSettlementListRow);
    if (new Set(rows.map((r) => r.id)).size !== rows.length)
      throw Error("结算记录编号重复");
    return { rows, error: "" };
  } catch (e) {
    return {
      rows: [],
      error: e instanceof Error ? e.message : "结算记录无法核对，请刷新",
    };
  }
});
const eligible = computed(() => data.value.rows.filter((r) => r.canReview));
const selectedCount = computed(
  () => eligible.value.filter((r) => props.checked.includes(r.id)).length,
);
const detail = ref<SettlementListRow | null>(null);
let opener: HTMLElement | undefined;
function openDetail(row: SettlementListRow) {
  opener =
    document.activeElement instanceof HTMLElement
      ? document.activeElement
      : undefined;
  detail.value = row;
}
function closeDetail() {
  const target = opener;
  detail.value = null;
  opener = undefined;
  void nextTick(() => {
    if (target?.isConnected) target.focus();
  });
}
watch(
  () => props.items,
  () => {
    detail.value = null;
    opener = undefined;
  },
);
function checkAll(event: Event) {
  const value = (event.target as HTMLInputElement).checked;
  for (const row of eligible.value) emit("check", row.id, value);
}
</script>
<template>
  <p v-if="data.error" role="alert" class="alert error">{{ data.error }}</p>
  <div v-else class="table-scroll original-settlement-list">
    <table :class="area">
      <thead>
        <tr>
          <th v-if="area === 'admin'" scope="col">
            <span class="sr-only">选择</span
            ><input
              type="checkbox"
              aria-label="选择本页待审核申请"
              :disabled="busy || !eligible.length"
              :checked="!!eligible.length && selectedCount === eligible.length"
              :indeterminate="
                selectedCount > 0 && selectedCount < eligible.length
              "
              @change="checkAll"
            />
          </th>
          <th v-for="column in columns" :key="column.key" scope="col">
            {{ column.label }}
          </th>
          <th v-if="area === 'admin'" scope="col" class="settlement-actions">
            操作
          </th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="row in data.rows" :key="row.id" :data-settlement-id="row.id">
          <td v-if="area === 'admin'">
            <input
              type="checkbox"
              :aria-label="'选择申请 ' + row.id"
              :disabled="busy || !row.canReview"
              :checked="checked.includes(row.id)"
              @change="
                emit(
                  'check',
                  row.id,
                  ($event.target as HTMLInputElement).checked,
                )
              "
            />
          </td>
          <td v-for="column in columns" :key="column.key">
            <button
              v-if="column.key === 'id'"
              type="button"
              class="link-button"
              :aria-label="'结算记录 ' + row.id"
              @click="openDetail(row)"
            >
              {{ row.id }}
            </button>
            <template v-else-if="column.key === 'account'"
              >{{ row.account
              }}<small v-if="area === 'admin'" class="settlement-secondary">{{
                row.name
              }}</small></template
            >
            <template v-else-if="column.key === 'money'"
              ><strong>¥{{ row.gross }}</strong
              ><small class="settlement-secondary">{{
                row.actual !== "—"
                  ? "到账 ¥" + row.actual
                  : row.expected !== "未记录"
                    ? "预计 ¥" + row.expected
                    : "预计到账未记录"
              }}</small></template
            >
            <template v-else-if="column.key === 'status'"
              ><span
                class="badge"
                :data-state="
                  {
                    waiting: 'warn',
                    processing: 'warn',
                    success: 'on',
                    danger: 'bad',
                    neutral: 'none',
                  }[row.tone] || 'none'
                "
                >{{ row.status }}</span
              ><small class="settlement-secondary muted">{{ row.hint }}</small
              ><template v-if="area === 'merchant'"
                ><button
                  type="button"
                  class="link-button"
                  :aria-label="'查看结算详情 ' + row.id"
                  @click="openDetail(row)"
                >
                  详情</button
                ><button
                  v-if="row.canInspectPayout"
                  type="button"
                  class="link-button"
                  :disabled="busy"
                  @click="emit('payout', row.id)"
                >
                  核对结算出款
                </button></template
              ></template
            >
            <template v-else>{{
              row[column.key as keyof SettlementListRow]
            }}</template>
          </td>
          <td v-if="area === 'admin'" class="settlement-actions">
            <div class="row-actions">
              <button
                type="button"
                class="link-button"
                :aria-label="'查看结算详情 ' + row.id"
                @click="openDetail(row)"
              >
                详情</button
              ><template v-if="row.canReview"
                ><button
                  type="button"
                  class="link-button"
                  :disabled="busy"
                  @click="emit('decision', row.raw, 'approve')"
                >
                  核准</button
                ><button
                  type="button"
                  class="link-button danger-text"
                  :disabled="busy"
                  @click="emit('decision', row.raw, 'reject')"
                >
                  拒绝
                </button></template
              ><button
                v-else-if="row.canInspectPayout"
                type="button"
                class="link-button"
                :disabled="busy"
                @click="emit('payout', row.id)"
              >
                核对结算出款
              </button>
            </div>
          </td>
        </tr>
        <tr v-if="!data.rows.length">
          <td
            class="empty"
            :colspan="columns.length + (area === 'admin' ? 2 : 0)"
          >
            暂无申请
          </td>
        </tr>
      </tbody>
    </table>
  </div>
  <SettlementRecordDetails v-if="detail" :row="detail" @close="closeDetail" />
</template>
<style scoped>
.original-settlement-list {
  isolation: isolate;
}
.original-settlement-list table.admin {
  min-width: 1320px;
}
.original-settlement-list table.merchant {
  min-width: 920px;
}
.original-settlement-list th,
.original-settlement-list td {
  vertical-align: middle;
  white-space: nowrap;
}
.settlement-secondary {
  display: block;
  margin-top: 4px;
}
.settlement-actions {
  position: sticky;
  right: 0;
  background: var(--panel, var(--bg-card));
  min-width: 145px;
  z-index: 1;
  box-shadow: -4px 0 5px -5px var(--line-control);
}
th.settlement-actions {
  background: var(--table-header, var(--bg-raised));
}
.settlement-actions .row-actions {
  max-width: 180px;
}
</style>
