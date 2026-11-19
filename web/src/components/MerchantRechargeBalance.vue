<script setup lang="ts">
import { onBeforeUnmount, ref, watch } from "vue";
import type { ApiClient } from "../api";

const props = defineProps<{ client: ApiClient }>();
const balance = ref<string | null>(null),
  loading = ref(false),
  error = ref("");
let active = true,
  epoch = 0,
  controller: AbortController | undefined;
async function load() {
  controller?.abort();
  const current = ++epoch,
    client = props.client,
    csrf = client.csrfToken;
  const request = new AbortController();
  controller = request;
  balance.value = null;
  error.value = "";
  loading.value = false;
  if (!active || client.area !== "merchant") return;
  loading.value = true;
  const sameContext = () =>
    active &&
    current === epoch &&
    !request.signal.aborted &&
    client === props.client &&
    csrf === client.csrfToken;
  try {
    // Reuse the dashboard's current available-account balance, not a payout
    // allowance after D+1 or reserved-money deductions.
    const data = await client.request<Record<string, unknown>>("/dashboard", {
      signal: request.signal,
    });
    if (!sameContext()) return;
    const value = data?.available_money;
    if (
      typeof value !== "string" ||
      !/^-?(0|[1-9]\d{0,16})\.\d{2}$/.test(value)
    )
      throw Error("Invalid account balance");
    const cents = BigInt(value.replace(".", ""));
    if (cents < -9223372036854775808n || cents > 9223372036854775807n)
      throw Error("Account balance out of range");
    balance.value = value;
  } catch {
    if (sameContext())
      error.value = "余额读取失败，请刷新页面重试。充值记录仍可正常查看。";
  } finally {
    if (active && current === epoch) loading.value = false;
  }
}
watch(() => [props.client, props.client.area, props.client.csrfToken], load, {
  immediate: true,
  flush: "sync",
});
onBeforeUnmount(() => {
  active = false;
  epoch++;
  controller?.abort();
  balance.value = null;
});
</script>
<template>
  <section
    class="panel account-section recharge-account-balance"
    aria-label="充值账户余额"
  >
    <dl>
      <dt>当前余额（元）</dt>
      <dd :class="{ 'balance-ready': balance !== null }" aria-live="polite">
        {{ balance ?? (loading ? "读取中…" : "暂不可用") }}
      </dd>
    </dl>
    <p v-if="error" class="alert error" role="alert">{{ error }}</p>
  </section>
</template>
<style scoped>
.recharge-account-balance dd {
  margin: var(--space-2) 0 0;
  font-variant-numeric: tabular-nums;
  overflow-wrap: anywhere;
}
.recharge-account-balance dt {
  color: var(--muted);
}
.recharge-account-balance .balance-ready {
  font-family: var(--font-mono);
  font-size: clamp(1.375rem, 3.2vw, 2.25rem);
  font-weight: 650;
  line-height: 1.15;
}
.recharge-account-balance dl {
  margin: 0;
}
.recharge-account-balance > .alert {
  margin: var(--space-4) 0 0;
}
</style>
