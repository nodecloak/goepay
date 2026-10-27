<script setup lang="ts">
import { computed } from "vue";
import type { Area } from "../menu";
import { transferReadActions } from "../transfers";
import { originalTransferColumns, readTransferListRow } from "../transfer-list";
const props = defineProps<{
  items: Record<string, unknown>[];
  area: Area;
  busy: boolean;
}>();
const emit = defineEmits<{
  details: [row: Record<string, unknown>];
  query: [row: Record<string, unknown>];
}>();
const columns = computed(() => originalTransferColumns(props.area));
const data = computed(() => {
  try {
    const rows = props.items.map(readTransferListRow);
    if (new Set(rows.map((r) => r.id)).size !== rows.length)
      throw Error("付款记录编号重复，请刷新后重试");
    return { rows, error: "" };
  } catch (e) {
    return {
      rows: [],
      error: e instanceof Error ? e.message : "付款记录无法核对",
    };
  }
});
</script>
<template>
  <p v-if="data.error" class="alert error" role="alert">{{ data.error }}</p>
  <div v-else class="table-scroll original-transfer-list">
    <table :class="area">
      <thead>
        <tr>
          <th v-for="column in columns" :key="column.key" scope="col">
            {{ column.label }}
          </th>
          <th scope="col" class="transfer-actions">操作</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="row in data.rows" :key="row.id" :data-transfer-id="row.id">
          <td
            v-for="column in columns"
            :key="column.key"
            :class="{
              'transfer-number': column.key === 'number',
              'transfer-method': column.key === 'method',
            }"
          >
            <template v-if="column.key === 'number'"
              ><span>{{ row.number }}</span
              ><small class="transfer-secondary muted"
                >商户交易号：{{ row.externalNo }}</small
              ><small v-if="row.providerNo" class="transfer-secondary muted"
                >接口交易号：{{ row.providerNo }}</small
              ></template
            >
            <template v-else-if="column.key === 'method'"
              >{{ row.method
              }}<small
                v-if="row.remark"
                class="transfer-secondary transfer-remark muted"
                >备注：{{ row.remark }}</small
              ></template
            >
            <template v-else-if="column.key === 'account'"
              >{{ row.account
              }}<small class="transfer-secondary">{{
                row.name
              }}</small></template
            >
            <template v-else-if="column.key === 'amount'"
              ><strong>¥{{ row.amount }}</strong
              ><small class="transfer-secondary muted"
                >花费金额：¥{{ row.cost }}</small
              ></template
            >
            <template v-else-if="column.key === 'createdAt'"
              >{{ row.createdAt
              }}<small class="transfer-secondary muted"
                >付款时间：{{ row.payDate }}</small
              ></template
            >
            <span
              v-else-if="column.key === 'status'"
              :class="'transfer-status ' + row.tone"
              >{{ row.status }}</span
            >
            <template v-else>{{
              row[column.key as keyof typeof row]
            }}</template>
          </td>
          <td class="transfer-actions">
            <button
              type="button"
              class="link-button"
              :disabled="busy"
              @click="emit('details', row.raw)"
            >
              付款详情
            </button>
            <button
              v-if="transferReadActions(row.raw).query"
              type="button"
              class="link-button"
              :disabled="busy"
              @click="emit('query', row.raw)"
            >
              查询状态
            </button>
          </td>
        </tr>
        <tr v-if="!data.rows.length">
          <td class="empty" :colspan="columns.length + 1">暂无付款记录</td>
        </tr>
      </tbody>
    </table>
  </div>
</template>
<style scoped>
.original-transfer-list {
  isolation: isolate;
}
.original-transfer-list table.admin {
  min-width: 1250px;
}
.original-transfer-list table.merchant {
  min-width: 1160px;
}
.original-transfer-list th,
.original-transfer-list td {
  vertical-align: middle;
  white-space: nowrap;
}
.transfer-number {
  min-width: 240px;
  max-width: 350px;
  white-space: normal !important;
  overflow-wrap: anywhere;
}
.transfer-secondary {
  display: block;
  margin-top: 4px;
}
.transfer-method {
  min-width: 190px;
  max-width: 260px;
  white-space: normal !important;
}
.transfer-remark {
  overflow-wrap: anywhere;
  white-space: pre-wrap;
  max-width: 260px;
}
.transfer-actions {
  position: sticky;
  right: 0;
  background: var(--panel, #fff);
  min-width: 100px;
  z-index: 1;
  box-shadow: -2px 0 4px #00000008;
}
th.transfer-actions {
  background: var(--table-header, #f8fafc);
}
.transfer-status.success {
  color: #15803d;
}
.transfer-status.danger {
  color: #b91c1c;
}
.transfer-status.processing {
  color: #b45309;
}
.transfer-status.neutral {
  color: #64748b;
}
</style>
