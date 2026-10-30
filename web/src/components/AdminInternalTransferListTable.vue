<script setup lang="ts">
import { computed } from "vue";
import { beijingTimestamp } from "../console-datetime";
import KitStatusTag from "./KitStatusTag.vue";
const props = defineProps<{
  items: Record<string, unknown>[];
  busy: boolean;
}>();
const emit = defineEmits<{
  decision: [row: Record<string, unknown>, action: "approve" | "reject"];
}>();
const states: Record<string, string> = {
  pending: "待审核",
  approved: "成功",
  rejected: "已拒绝",
};
const stateTones: Record<
  string,
  "success" | "warning" | "danger" | "info" | "neutral"
> = { pending: "warning", approved: "success", rejected: "danger" };
const origins: Record<string, string> = {
  admin: "管理员",
  merchant: "商户",
};
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
        from = id("merchant_id"),
        to = id("recipient_id");
      if (ids.has(identifier)) throw Error();
      ids.add(identifier);
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
        raw.reason !== null &&
        raw.reason !== undefined &&
        (typeof raw.reason !== "string" || raw.reason.length > 500)
      )
        throw Error();
      if (
        typeof raw.origin !== "string" ||
        (raw.origin !== "" && !Object.hasOwn(origins, raw.origin))
      )
        throw Error();
      return {
        raw,
        id: identifier,
        from,
        to,
        amount: raw.amount,
        status: states[raw.status],
        state: raw.status,
        origin: raw.origin ? origins[raw.origin] : "未知",
        reason: (raw.reason as string | null) || "—",
        created: time(raw.created_at),
        completed: raw.completed_at === null ? "—" : time(raw.completed_at),
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
  <div
    v-else
    class="table-scroll admin-internal-transfer-table"
    aria-label="站内转账记录"
  >
    <table>
      <thead>
        <tr>
          <th
            v-for="label in [
              'ID',
              '发起方',
              '收款方',
              '金额',
              '来源',
              '状态',
              '备注',
              '提交时间',
              '完成时间',
              '操作',
            ]"
            :key="label"
            scope="col"
          >
            {{ label }}
          </th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="row in data.rows" :key="row.id" :data-transfer-id="row.id">
          <td>{{ row.id }}</td>
          <td>{{ row.from }}</td>
          <td>{{ row.to }}</td>
          <td>{{ row.amount }}</td>
          <td>{{ row.origin }}</td>
          <td>
            <KitStatusTag
              :text="row.status"
              :tone="stateTones[row.state] || 'neutral'"
            />
          </td>
          <td class="transfer-remark">{{ row.reason }}</td>
          <td>{{ row.created }}</td>
          <td>{{ row.completed }}</td>
          <td>
            <div v-if="row.state === 'pending'" class="row-actions">
              <button
                class="link-button"
                :disabled="busy"
                @click="emit('decision', row.raw, 'approve')"
              >
                通过</button
              ><button
                class="link-button danger-text"
                :disabled="busy"
                @click="emit('decision', row.raw, 'reject')"
              >
                拒绝
              </button>
            </div>
            <span v-else class="muted">已处理</span>
          </td>
        </tr>
        <tr v-if="!data.rows.length">
          <td class="empty" colspan="10">暂无站内转账记录</td>
        </tr>
      </tbody>
    </table>
  </div>
</template>
<style scoped>
.admin-internal-transfer-table {
  max-width: 100%;
  overflow-x: auto;
}
.admin-internal-transfer-table table {
  min-width: 1080px;
}
.admin-internal-transfer-table th,
.admin-internal-transfer-table td {
  white-space: nowrap;
}
.admin-internal-transfer-table .transfer-remark {
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  min-width: 100px;
  max-width: 240px;
}
</style>
