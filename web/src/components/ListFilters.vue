<script setup lang="ts">
import { computed, ref } from "vue";
import type { ListFilters } from "../list-filters";
import { ledgerTypeFilters } from "../list-filters";
import type { Area } from "../menu";
import KitField from "./KitField.vue";
import KitSelect from "./KitSelect.vue";
import KitInput from "./KitInput.vue";
const props = defineProps<{
  kind: string;
  area: Area;
  loading?: boolean;
  payTypeOptions?: { value: string; label: string }[];
}>();
const filters = defineModel<ListFilters>({ required: true });
defineEmits<{ reset: [] }>();
const expanded = ref(false);

const orderStatusOptions = [
  { value: "", label: "全部状态" },
  { value: "created", label: "已创建" },
  { value: "submitting", label: "提交中" },
  { value: "pending", label: "待支付" },
  { value: "paid", label: "已支付" },
  { value: "closed", label: "已关闭" },
  { value: "unknown", label: "结果待核对" },
  { value: "failed", label: "明确失败" },
];
const refundStatusOptions = [
  { value: "", label: "全部状态" },
  { value: "pending", label: "待处理" },
  { value: "succeeded", label: "退款成功" },
  { value: "unknown", label: "结果待核对" },
  { value: "failed", label: "明确失败" },
];
const typeOptions = computed(() => [
  { value: "", label: "全部方式" },
  ...(props.payTypeOptions ?? [
    { value: "alipay", label: "支付宝" },
    { value: "wxpay", label: "微信支付" },
    { value: "sandbox", label: "本地模拟" },
  ]),
]);
const notificationOptions = [
  { value: "", label: "全部通知状态" },
  { value: "none", label: "尚无通知任务" },
  { value: "pending", label: "等待通知" },
  { value: "running", label: "正在通知" },
  { value: "succeeded", label: "通知成功" },
  { value: "dead", label: "通知失败" },
];
const incomeOptions = [
  { value: "", label: "全部收入状态" },
  { value: "none", label: "未冻结" },
  { value: "held", label: "收入冻结中" },
  { value: "released", label: "收入已解冻" },
];
const ledgerOptions = [
  { value: "", label: "全部类型" },
  ...ledgerTypeFilters.map((t) => ({ value: t.value, label: t.label })),
];
</script>
<template>
  <div class="list-filters list-filters-inline">
    <div class="pa-filter-fields list-filters-grid">
      <KitField v-if="kind === 'orders' || kind === 'refunds'" label="状态筛选">
        <KitSelect
          v-model="filters.status"
          :options="
            kind === 'orders' ? orderStatusOptions : refundStatusOptions
          "
          aria-label="状态筛选"
        />
      </KitField>
      <KitField v-if="kind === 'ledger'" label="类型筛选">
        <KitSelect
          v-model="filters.type"
          :options="ledgerOptions"
          aria-label="资金类型筛选"
        />
      </KitField>
      <template v-if="expanded">
        <KitField
          v-if="kind === 'orders' || kind === 'refunds'"
          label="支付方式"
        >
          <KitSelect
            v-model="filters.type"
            :options="typeOptions"
            aria-label="支付方式筛选"
          />
        </KitField>
        <KitField label="创建开始">
          <input v-model="filters.from" type="date" aria-label="创建开始日期" />
        </KitField>
        <KitField label="创建结束">
          <input v-model="filters.to" type="date" aria-label="创建结束日期" />
        </KitField>
        <KitField v-if="kind === 'orders'" label="通知状态">
          <KitSelect
            v-model="filters.notification_state"
            :options="notificationOptions"
            aria-label="通知状态筛选"
          />
        </KitField>
        <KitField v-if="kind === 'orders'" label="收入状态">
          <KitSelect
            v-model="filters.income_status"
            :options="incomeOptions"
            aria-label="订单收入状态"
          />
        </KitField>
        <KitField v-if="area === 'admin' && kind === 'ledger'" label="商户 ID">
          <KitInput
            v-model="filters.merchant_id"
            inputmode="numeric"
            aria-label="商户 ID 筛选"
          />
        </KitField>
        <KitField
          v-if="kind === 'orders' || kind === 'refunds'"
          label="通道 ID"
        >
          <KitInput
            v-model="filters.channel_id"
            inputmode="numeric"
            aria-label="通道 ID 筛选"
          />
        </KitField>
        <KitField v-if="kind === 'orders'" label="子通道 ID">
          <KitInput
            v-model="filters.subchannel_id"
            inputmode="numeric"
            aria-label="子通道 ID 筛选"
          />
        </KitField>
        <KitField v-if="kind === 'orders'" label="买家标识">
          <KitInput
            v-model="filters.buyer"
            :maxlength="256"
            aria-label="买家标识筛选"
          />
        </KitField>
        <KitField v-if="kind === 'orders'" label="客户 IP">
          <KitInput
            v-model="filters.client_ip"
            :maxlength="45"
            aria-label="客户 IP 筛选"
          />
        </KitField>
      </template>
    </div>
    <div class="pa-filter-actions">
      <button type="button" class="pa-btn" @click="$emit('reset')">重置</button>
      <button
        type="submit"
        class="pa-btn pa-btn--primary"
        :class="{ 'pa-btn--loading': loading }"
        :disabled="loading"
      >
        查询
      </button>
      <button
        type="button"
        class="pa-btn pa-btn--text pa-filter-toggle"
        :aria-expanded="expanded"
        @click="expanded = !expanded"
      >
        {{ expanded ? "收起" : "展开"
        }}<i class="pa-icon pa-filter-chev" aria-hidden="true"></i>
      </button>
    </div>
  </div>
</template>
