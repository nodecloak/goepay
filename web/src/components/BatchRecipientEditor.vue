<script setup lang="ts">
import type { BatchDraftRow, BatchPolicy } from "../transfer-batches";
import { batchChannelTypes, emptyBatchRow } from "../transfer-batches";
import {
  bankChannelProduct,
  bankProductLabel,
  transferTypeLabel,
} from "../transfers";
import bankCodes from "../data/wechat-bank-codes.json";
import bankNames from "../data/wechat-bank-names.json";
const props = defineProps<{
  rows: BatchDraftRow[];
  policy: BatchPolicy;
  disabled?: boolean;
}>();
const emit = defineEmits<{ change: [rows: BatchDraftRow[]] }>();
function update(index: number, key: keyof BatchDraftRow, value: string) {
  const rows = props.rows.map((row) => ({ ...row }));
  rows[index]![key] = value;
  if (key === "channel_id") {
    rows[index]!.type =
      batchChannelTypes(props.policy.channels.find((c) => c.id === value))[0] ||
      "";
    rows[index]!.bank_code = "";
  }
  if (key === "type") rows[index]!.bank_code = "";
  emit("change", rows);
}
function bankProduct(row: BatchDraftRow) {
  return bankChannelProduct(
    props.policy.channels.find((c) => c.id === row.channel_id),
  );
}
function bankName(code: string, key: string) {
  return (bankNames as Record<string, string>)[code] || key;
}
</script>
<template>
  <section aria-label="批量付款收款项目">
    <fieldset
      v-for="(row, index) in rows"
      :key="index"
      :disabled="disabled"
      class="batch-recipient"
    >
      <legend>付款项目 {{ index + 1 }}</legend>
      <div class="batch-recipient-fields">
        <label
          >通道<select
            :value="row.channel_id"
            @change="
              update(
                index,
                'channel_id',
                ($event.target as HTMLSelectElement).value,
              )
            "
          >
            <option value="">请选择</option>
            <option
              v-for="channel in policy.channels"
              :key="channel.id"
              :value="channel.id"
            >
              {{ channel.id }}
            </option>
          </select></label
        >
        <label
          >付款方式<select
            :value="row.type"
            @change="
              update(index, 'type', ($event.target as HTMLSelectElement).value)
            "
          >
            <option value="">请选择</option>
            <option
              v-for="type in batchChannelTypes(
                policy.channels.find((c) => c.id === row.channel_id),
              )"
              :key="type"
              :value="type"
            >
              {{ transferTypeLabel[type] }}
            </option>
          </select></label
        >
        <label
          >收款账号<input
            :value="row.account"
            autocomplete="off"
            maxlength="256"
            @input="
              update(
                index,
                'account',
                ($event.target as HTMLInputElement).value,
              )
            "
        /></label>
        <label
          >收款人姓名<input
            :value="row.name"
            autocomplete="off"
            maxlength="128"
            :required="row.type === 'bank'"
            @input="
              update(index, 'name', ($event.target as HTMLInputElement).value)
            "
        /></label>
        <label
          >付款金额（元）<input
            :value="row.amount"
            inputmode="decimal"
            @input="
              update(index, 'amount', ($event.target as HTMLInputElement).value)
            "
        /></label>
        <label
          >付款说明<input
            :value="row.remark"
            maxlength="256"
            @input="
              update(index, 'remark', ($event.target as HTMLInputElement).value)
            "
        /></label>
        <label v-if="row.type === 'bank' && bankProduct(row) === 'wxpay_v2'"
          >开户银行<select
            :value="row.bank_code"
            @change="
              update(
                index,
                'bank_code',
                ($event.target as HTMLSelectElement).value,
              )
            "
          >
            <option value="">请选择开户银行</option>
            <option v-for="(code, key) in bankCodes" :key="key" :value="code">
              {{ bankName(code, key) }}（{{ code }}）
            </option>
          </select></label
        >
      </div>
      <p v-if="row.type === 'bank' && bankProduct(row)" class="muted">
        {{
          bankProductLabel[bankProduct(row)!]
        }}；只有微信银行产品需要银行编号。
      </p>
      <button
        type="button"
        :disabled="disabled || rows.length <= 1"
        @click="
          emit(
            'change',
            rows.filter((_, i) => i !== index),
          )
        "
      >
        删除项目 {{ index + 1 }}
      </button>
    </fieldset>
    <button
      type="button"
      :disabled="disabled || rows.length >= 100"
      @click="emit('change', [...rows, emptyBatchRow()])"
    >
      添加付款项目
    </button>
  </section>
</template>
<style scoped>
.batch-recipient {
  min-width: 0;
  margin: 1rem 0;
  padding: 1rem;
  border: 1px solid var(--border, #dce3ec);
  border-radius: 8px;
}
.batch-recipient-fields {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(min(100%, 220px), 1fr));
  gap: 1rem;
  margin-bottom: 1rem;
}
.batch-recipient-fields label {
  display: flex;
  flex-direction: column;
  gap: 0.35rem;
  min-width: 0;
}
.batch-recipient-fields input,
.batch-recipient-fields select {
  width: 100%;
  min-width: 0;
}
</style>
