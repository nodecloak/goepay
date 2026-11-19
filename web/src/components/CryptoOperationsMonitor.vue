<script setup lang="ts">
import { computed, ref, watch } from "vue";
import type { ApiClient } from "../api";
import { cryptoNetworkLabel } from "../crypto-channels";
import {
  cryptoUsageStates,
  readCryptoMonitoring,
  type CryptoMonitoring,
} from "../crypto-monitoring";

const props = defineProps<{ client: ApiClient; refresh: number }>();
const snapshot = ref<CryptoMonitoring | null>(null);
const failed = ref(false);
const activeNetworks = computed(
  () =>
    new Set(
      snapshot.value?.networks
        .filter((row) => row.enabled)
        .map((row) => row.network),
    ),
);
const utcTime = (value: string) =>
  `${value.slice(0, 10)} ${value.slice(11, 19)} UTC`;

watch(
  () => [props.client, props.client.csrfToken, props.refresh],
  async (_current, _previous, onCleanup) => {
    const controller = new AbortController();
    onCleanup(() => controller.abort());
    snapshot.value = null;
    failed.value = false;
    try {
      const result = await props.client.request("/crypto/monitoring", {
        signal: controller.signal,
      });
      if (!controller.signal.aborted)
        snapshot.value = readCryptoMonitoring(result);
    } catch {
      if (!controller.signal.aborted) failed.value = true;
    }
  },
  { immediate: true },
);
</script>

<template>
  <section
    class="monitor"
    data-testid="crypto-monitoring"
    aria-label="USDT 容量监控"
  >
    <h3>USDT 容量监控</h3>
    <p v-if="failed" role="alert" class="monitor-alert">
      监控数据暂时无法读取，请刷新页面。
    </p>
    <p v-else-if="!snapshot" role="status" class="muted">正在读取容量监控…</p>
    <template v-else>
      <p class="muted">
        平台汇总 · 截至
        <time :datetime="snapshot.as_of">{{ utcTime(snapshot.as_of) }}</time>
      </p>
      <h4>收款地址池</h4>
      <div class="monitor-grid">
        <article
          v-for="pool in snapshot.networks"
          :key="pool.network"
          :data-network="pool.network"
          class="monitor-item"
        >
          <div class="monitor-item-header">
            <h5>{{ cryptoNetworkLabel(pool.network) }}</h5>
            <span class="muted">{{
              pool.enabled ? "网络已启用" : "网络已关闭"
            }}</span>
          </div>
          <dl>
            <div>
              <dt>已登记</dt>
              <dd>{{ pool.deposit_total }}</dd>
            </div>
            <div>
              <dt>已分配</dt>
              <dd>{{ pool.deposit_used }}</dd>
            </div>
            <div>
              <dt>未分配</dt>
              <dd>{{ pool.deposit_available }}</dd>
            </div>
          </dl>
          <p v-if="pool.deposit_total === 0" class="muted">尚未登记收款地址</p>
          <p v-else-if="pool.deposit_available === 0" class="monitor-alert">
            地址池余量耗尽
          </p>
        </article>
      </div>
      <h4>通道日额度</h4>
      <p class="muted">
        统计当日建单金额，包含待付款和已过期订单。下次重置时间为
        <time :datetime="snapshot.window_end">{{
          utcTime(snapshot.window_end)
        }}</time
        >。
      </p>
      <p v-if="!snapshot.channels.length" class="muted">暂无 USDT 通道</p>
      <div v-else class="monitor-grid">
        <article
          v-for="channel in snapshot.channels"
          :key="channel.id"
          :data-channel="channel.id"
          class="monitor-item"
        >
          <div class="monitor-item-header">
            <h5>{{ channel.name }}</h5>
            <span class="muted">
              {{
                channel.archived
                  ? "已归档"
                  : channel.enabled
                    ? "通道已开启"
                    : "通道已关闭"
              }}
            </span>
          </div>
          <p class="muted">
            {{ cryptoNetworkLabel(channel.network) }}
            <template v-if="!activeNetworks.has(channel.network)">
              · 网络已关闭</template
            >
          </p>
          <dl>
            <div>
              <dt>上限</dt>
              <dd>
                {{
                  channel.day_remaining === null
                    ? "不限"
                    : `${channel.day_limit} USDT`
                }}
              </dd>
            </div>
            <div>
              <dt>已占用</dt>
              <dd>{{ channel.day_used }} USDT</dd>
            </div>
            <div>
              <dt>剩余</dt>
              <dd>
                {{
                  channel.day_remaining === null
                    ? "不限"
                    : `${channel.day_remaining} USDT`
                }}
              </dd>
            </div>
          </dl>
          <p v-if="channel.day_remaining === '0'" class="monitor-alert">
            当日额度用完
          </p>
          <details>
            <summary>按订单状态查看占用金额</summary>
            <dl>
              <div v-for="(label, state) in cryptoUsageStates" :key="state">
                <dt>{{ label }}</dt>
                <dd>{{ channel.day_usage_by_state[state] }} USDT</dd>
              </div>
            </dl>
          </details>
        </article>
      </div>
    </template>
  </section>
</template>

<style scoped>
.monitor {
  min-width: 0;
  padding: 22px;
  border: 1px solid var(--line);
  border-radius: 12px;
  background: var(--bg-card);
}
.monitor h3,
.monitor h5,
.monitor p {
  margin: 0;
}
.monitor h3,
.monitor h4 {
  margin-bottom: 10px;
}
.monitor h4 {
  margin-top: 18px;
}
.monitor h5 {
  font-size: 1rem;
}
.monitor h4 + p {
  margin-bottom: 10px;
}
.monitor-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(min(100%, 285px), 1fr));
  gap: 12px;
}
.monitor-item {
  min-width: 0;
  padding: 14px;
  border: 1px solid var(--line);
  border-radius: 8px;
  overflow-wrap: anywhere;
}
.monitor-item-header {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: 4px 12px;
  margin-bottom: 4px;
}
.monitor dl {
  display: grid;
  gap: 5px;
  margin: 12px 0 0;
}
.monitor dl > div {
  display: grid;
  grid-template-columns: minmax(max-content, 1fr) minmax(0, auto);
  align-items: baseline;
  gap: 12px;
}
.monitor dt,
.muted {
  color: var(--ink-muted);
}
.monitor dd {
  margin: 0;
  text-align: right;
  font-variant-numeric: tabular-nums;
  overflow-wrap: anywhere;
}
.monitor details,
.monitor-item dl + p {
  margin-top: 12px;
}
.monitor summary {
  cursor: pointer;
}
.monitor-alert {
  color: var(--warning-text);
}
@media (max-width: 600px) {
  .monitor {
    padding: 16px;
  }
}
</style>
