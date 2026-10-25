<script setup lang="ts">
import type { FundsAvailability } from "../funds-availability";
import FundsAvailabilitySummary from "./FundsAvailabilitySummary.vue";
defineProps<{
  policy: FundsAvailability & {
    minimum_money?: string;
    maximum_money?: string;
    daily_recipient_limit?: number;
    default_remark?: string;
    default_title?: string;
  };
}>();
</script>
<template>
  <FundsAvailabilitySummary :funds="policy" />
  <dl class="record-detail transfer-policy">
    <dt>单笔最低金额（元）</dt>
    <dd>{{ policy.minimum_money ?? "服务端未提供" }}</dd>
    <dt>单笔最高金额（元）</dt>
    <dd>
      {{
        policy.maximum_money === "0.00"
          ? "未设置上限，仍受通道限制"
          : (policy.maximum_money ?? "服务端未提供")
      }}
    </dd>
    <dt>每日同收款方次数上限</dt>
    <dd>
      {{
        policy.daily_recipient_limit === 0
          ? "未设置次数上限"
          : (policy.daily_recipient_limit ?? "服务端未提供")
      }}
    </dd>
    <dt>默认付款说明</dt>
    <dd>{{ policy.default_remark || "未设置" }}</dd>
    <dt>默认支付宝付款标题</dt>
    <dd>{{ policy.default_title || "未设置" }}</dd>
  </dl>
  <p class="muted">
    金额、限额和同收款方次数最终由服务端核验；此处不显示未经核实的剩余次数。
  </p>
</template>
