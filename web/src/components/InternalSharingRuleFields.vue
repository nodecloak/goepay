<script setup lang="ts">
import { computed, ref, watch } from "vue";
import type { ResourceField } from "../resources";
import KitField from "./KitField.vue";
import KitInput from "./KitInput.vue";
import KitSwitch from "./KitSwitch.vue";

// 站内分账规则 (profitsharing-rules) kit editor as Pro: a receiver list of up to
// 10 rows, each a 收款商户 ID + 分账比例(%), with a running total that must stay
// ≤100%. It writes the `receivers` body the backend validates and canonicalizes
// (no raw JSON shown). name / 付款商户ID(admin) / 启用 render as kit fields.
const props = defineProps<{
  fields: ResourceField[];
  values: Record<string, unknown>;
  errors: Record<string, string>;
  editing: boolean;
  disabled?: boolean;
}>();
const emit = defineEmits<{ change: [name: string, value: unknown] }>();

type Row = { recipient_id: string; rate: string };

function rowsFromValue(): Row[] {
  let raw: any = props.values.receivers;
  // In edit mode a json field arrives as a pretty-printed string; parse it.
  if (typeof raw === "string" && raw.trim()) {
    try {
      raw = JSON.parse(raw);
    } catch {
      raw = null;
    }
  }
  if (Array.isArray(raw) && raw.length) {
    return raw
      .map((r: any) => ({
        recipient_id: String(r?.recipient_id ?? ""),
        // body stores rate_bps (万分比); show percent in the row.
        rate:
          r?.rate_bps != null && /^[0-9]+$/.test(String(r.rate_bps))
            ? String(Number(r.rate_bps) / 100)
            : "",
      }))
      .slice(0, 10);
  }
  // Legacy single body (recipient_id + rate_bps) → one row.
  if (props.values.recipient_id) {
    const bps = String(props.values.rate_bps ?? "");
    return [
      {
        recipient_id: String(props.values.recipient_id),
        rate: /^[0-9]+$/.test(bps) ? String(Number(bps) / 100) : "",
      },
    ];
  }
  return [{ recipient_id: "", rate: "" }];
}

const rows = ref<Row[]>(rowsFromValue());
// Re-seed when the edited record changes (create<->edit, different row).
watch(
  () => props.values.id,
  () => {
    rows.value = rowsFromValue();
  },
);

const hasMerchantField = computed(() =>
  props.fields.some((f) => f.name === "merchant_id"),
);
const nameField = computed(() => props.fields.find((f) => f.name === "name"));

// Running total in 万分比 (bps). 100% = 10000.
const totalBps = computed(() =>
  rows.value.reduce((n, r) => {
    const v = ratePctToBps(r.rate);
    return n + (v == null ? 0 : v);
  }, 0),
);
const totalPct = computed(() => (totalBps.value / 100).toFixed(2));
const overLimit = computed(() => totalBps.value > 10000);

function ratePctToBps(rate: string): number | null {
  if (!/^(0|[1-9][0-9]*)(\.[0-9]{1,2})?$/.test(rate.trim())) return null;
  const [a, b = ""] = rate.trim().split(".");
  const bps = Number(a) * 100 + Number(b.padEnd(2, "0"));
  return bps;
}

function addRow() {
  if (rows.value.length >= 10) return;
  rows.value.push({ recipient_id: "", rate: "" });
}
function removeRow(i: number) {
  if (rows.value.length <= 1) return;
  rows.value.splice(i, 1);
}

// Emit the canonical receivers body whenever the rows change. Only complete,
// valid rows are included; the backend re-validates (≤10, distinct, Σ≤100%).
watch(
  rows,
  () => {
    const receivers = rows.value
      .filter((r) => r.recipient_id.trim() && r.rate.trim())
      .map((r) => {
        const bps = ratePctToBps(r.rate);
        return {
          recipient_id: r.recipient_id.trim(),
          rate_bps: bps == null ? r.rate.trim() : String(bps),
        };
      });
    // The resource `receivers` field is type json: buildPayload does
    // JSON.parse(String(value)), so emit a JSON string, not a raw array.
    emit("change", "receivers", JSON.stringify(receivers));
    // Clear the legacy single fields so only the receivers list is authoritative.
    emit("change", "recipient_id", "");
    emit("change", "rate_bps", "");
  },
  { deep: true, immediate: true },
);
</script>

<template>
  <div class="internal-rule-editor">
    <KitField v-if="hasMerchantField" label="付款商户ID">
      <KitInput
        :model-value="String(values.merchant_id ?? '')"
        inputmode="numeric"
        placeholder="输入付款商户ID"
        aria-label="付款商户ID"
        :disabled="disabled"
        @update:model-value="(v) => emit('change', 'merchant_id', v)"
      />
      <small v-if="errors.merchant_id" class="field-error">{{
        errors.merchant_id
      }}</small>
    </KitField>
    <KitField v-if="nameField" label="名称" required>
      <KitInput
        :model-value="String(values.name ?? '')"
        placeholder="规则名称"
        aria-label="名称"
        :disabled="disabled"
        @update:model-value="(v) => emit('change', 'name', v)"
      />
      <small v-if="errors.name" class="field-error">{{ errors.name }}</small>
    </KitField>

    <div class="recv-head">
      <span>分账接收方列表</span>
      <span class="recv-total" :class="{ over: overLimit }"
        >合计 {{ totalPct }}%</span
      >
    </div>
    <table class="pa-table pa-table--compact">
      <thead>
        <tr>
          <th>收款商户ID</th>
          <th>分账比例(%)</th>
          <th>操作</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="(r, i) in rows" :key="i">
          <td>
            <KitInput
              v-model="r.recipient_id"
              inputmode="numeric"
              placeholder="收款商户ID"
              aria-label="收款商户ID"
              :disabled="disabled"
            />
          </td>
          <td>
            <KitInput
              v-model="r.rate"
              inputmode="decimal"
              placeholder="1-100"
              aria-label="分账比例"
              :disabled="disabled"
            />
          </td>
          <td>
            <button
              type="button"
              class="pa-btn pa-btn--danger"
              :disabled="disabled || rows.length <= 1"
              @click="removeRow(i)"
            >
              删除
            </button>
          </td>
        </tr>
      </tbody>
    </table>
    <div class="recv-add">
      <button
        type="button"
        class="pa-btn pa-btn--success"
        :disabled="disabled || rows.length >= 10"
        @click="addRow"
      >
        增加接收方
      </button>
      <span class="muted">最多10个接收方，合计比例不超过100%</span>
    </div>
    <p v-if="overLimit" class="field-error" role="alert">
      分账比例合计不能超过100%（当前 {{ totalPct }}%）
    </p>

    <KitField label="启用">
      <KitSwitch
        :model-value="values.enabled === true || values.enabled === 'true'"
        aria-label="启用"
        :disabled="disabled"
        @update:model-value="(v) => emit('change', 'enabled', v)"
      />
    </KitField>
  </div>
</template>

<style scoped>
.internal-rule-editor {
  display: grid;
  gap: 14px;
}
.recv-head {
  display: flex;
  justify-content: space-between;
  align-items: baseline;
  font-weight: 600;
}
.recv-total.over {
  color: var(--danger, #dc2626);
}
.recv-add {
  display: flex;
  gap: 12px;
  align-items: center;
}
.field-error {
  color: var(--danger, #dc2626);
  display: block;
  font-size: 12px;
}
</style>
