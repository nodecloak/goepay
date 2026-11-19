<script setup lang="ts">
import { computed } from "vue";
import { beijingTimestamp } from "../console-datetime";
import KitStatusTag from "./KitStatusTag.vue";
import MerchantRecords from "./MerchantRecords.vue";
const props = defineProps<{ items: Record<string, unknown>[] }>();
const states: Record<string, string> = {
  pending: "待审核",
  approved: "成功",
  rejected: "已拒绝",
};
const stateTones: Record<
  string,
  "success" | "warning" | "danger" | "info" | "neutral"
> = { pending: "warning", approved: "success", rejected: "danger" };
const data = computed(() => {
  try {
    const ids = new Set<string>();
    const rows = props.items.map((raw) => {
      const id = (key: string) => {
        const value = raw[key];
        if (
          typeof value !== "string" ||
          !/^[1-9]\d{0,18}$/.test(value) ||
          BigInt(value) > 9223372036854775807n
        )
          throw Error();
        return value;
      };
      const time = (value: unknown) => {
        if (
          typeof value !== "string" ||
          !/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})$/.test(
            value,
          ) ||
          !Number.isFinite(Date.parse(value))
        )
          throw Error();
        return beijingTimestamp(value);
      };
      const identifier = id("id"),
        peer = id("peer_merchant_id");
      if (ids.has(identifier)) throw Error();
      ids.add(identifier);
      if (raw.direction !== "in" && raw.direction !== "out") throw Error();
      if (typeof raw.status !== "string" || !Object.hasOwn(states, raw.status))
        throw Error();
      if (
        typeof raw.amount !== "string" ||
        !/^(0|[1-9]\d*)\.\d{2}$/.test(raw.amount) ||
        BigInt(raw.amount.replace(".", "")) <= 0n ||
        BigInt(raw.amount.replace(".", "")) > 9223372036854775807n
      )
        throw Error();
      if (raw.status === "pending" && raw.completed_at !== null) throw Error();
      if (
        raw.remark !== null &&
        (typeof raw.remark !== "string" || raw.remark.length > 2000)
      )
        throw Error();
      return {
        id: identifier,
        peer,
        direction: raw.direction,
        amount: raw.amount,
        status: states[raw.status],
        state: raw.status,
        remark: raw.remark || "未记录",
        created: time(raw.created_at),
        completed:
          raw.completed_at === null ? "未记录" : time(raw.completed_at),
      };
    });
    return { rows, error: "" };
  } catch {
    return { rows: [], error: "站内转账记录无法核对，请刷新后重试" };
  }
});
</script>
<template>
  <p v-if="data.error" class="alert error" role="alert">{{ data.error }}</p>
  <MerchantRecords
    v-else
    class="table-scroll internal-transfer-table"
    aria-label="站内转账记录"
  >
    <table role="table">
      <thead role="rowgroup">
        <tr role="row">
          <th
            v-for="label in [
              'ID',
              '方向',
              '对方商户',
              '金额',
              '状态',
              '备注',
              '提交时间',
              '完成时间',
            ]"
            :key="label"
            scope="col"
          >
            {{ label }}
          </th>
        </tr>
      </thead>
      <tbody role="rowgroup">
        <tr
          v-for="row in data.rows"
          :key="row.id"
          :data-transfer-id="row.id"
          role="row"
        >
          <td data-cell-label="ID" role="cell">{{ row.id }}</td>
          <td data-cell-label="方向" role="cell">
            {{ row.direction === "in" ? "转入" : "转出" }}
          </td>
          <td data-cell-label="对方商户" role="cell">{{ row.peer }}</td>
          <td
            data-cell-label="金额"
            :class="row.direction === 'in' ? 'incoming' : 'outgoing'"
            role="cell"
          >
            {{ row.direction === "in" ? "+" : "-" }}{{ row.amount }}
          </td>
          <td data-cell-label="状态" role="cell">
            <KitStatusTag
              :text="row.status"
              :tone="stateTones[row.state] || 'neutral'"
            />
          </td>
          <td class="transfer-remark" data-cell-label="备注" role="cell">
            {{ row.remark }}
          </td>
          <td data-cell-label="提交时间" role="cell">{{ row.created }}</td>
          <td data-cell-label="完成时间" role="cell">{{ row.completed }}</td>
        </tr>
        <tr v-if="!data.rows.length" role="row">
          <td class="empty" colspan="8" role="cell">暂无站内转账记录</td>
        </tr>
      </tbody>
    </table>
  </MerchantRecords>
</template>
<style scoped>
.internal-transfer-table {
  max-width: 100%;
  overflow-x: auto;
}
.internal-transfer-table table {
  min-width: 1000px;
}
.internal-transfer-table th,
.internal-transfer-table td {
  white-space: nowrap;
}
.internal-transfer-table .transfer-remark {
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  min-width: 100px;
  max-width: 240px;
}
.incoming {
  color: var(--success-text);
}
.outgoing {
  color: var(--danger-text);
}
</style>
