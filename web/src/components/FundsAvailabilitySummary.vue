<script setup lang="ts">
import type { FundsAvailability } from "../funds-availability";
defineProps<{ funds: FundsAvailability }>();
</script>
<template>
  <dl class="record-detail funds-availability">
    <dt>出账政策</dt>
    <dd>
      {{
        funds.settle_type === 1 ? "T+1（按北京时间核算当日受限资金）" : "T+0"
      }}
    </dd>
    <dt>可出账金额（元）</dt>
    <dd>{{ funds.available_money }}</dd>
    <dt>账面可用余额（元）</dt>
    <dd>{{ funds.ledger_available_money }}</dd>
    <dt>当日实际受限金额（元）</dt>
    <dd>{{ funds.today_blocked_money }}</dd>
  </dl>
  <p class="muted">
    这是读取时的资金快照，提交时由服务端重新核验。T+1
    当日受限收款暂不可出账，充值按服务端规则即时可用。
  </p>
</template>
