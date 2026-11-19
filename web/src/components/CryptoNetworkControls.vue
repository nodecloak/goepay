<script setup lang="ts">
import { nextTick, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { ApiError, type ApiClient } from "../api";
import { cryptoNetworkLabel } from "../crypto-channels";
import KitModal from "./KitModal.vue";
import KitStatusTag from "./KitStatusTag.vue";

const props = defineProps<{ client: ApiClient }>();
const emit = defineEmits<{ close: []; changed: [] }>();

type NetworkControl = {
  network: "tron" | "ethereum";
  enabled: boolean;
  effective_enabled: boolean;
  revision: string;
  wallets_configured: boolean;
  deposit_address_available: boolean;
  listener_ready: boolean;
  ready_to_enable: boolean;
};
const endpoint = "/payment-channels/crypto/networks";
const flags = [
  "enabled",
  "effective_enabled",
  "wallets_configured",
  "deposit_address_available",
  "listener_ready",
  "ready_to_enable",
] as const;

function readNetwork(raw: unknown): NetworkControl {
  if (!raw || typeof raw !== "object" || Array.isArray(raw))
    throw new Error("网络状态无法核实");
  const row = raw as Record<string, unknown>;
  if (
    (row.network !== "tron" && row.network !== "ethereum") ||
    typeof row.revision !== "string" ||
    !/^[1-9]\d{0,18}$/.test(row.revision) ||
    BigInt(row.revision) > 9223372036854775807n ||
    flags.some((key) => typeof row[key] !== "boolean") ||
    (row.effective_enabled &&
      (!row.enabled || !row.wallets_configured || !row.listener_ready)) ||
    (row.ready_to_enable &&
      (!row.wallets_configured ||
        !row.deposit_address_available ||
        !row.listener_ready))
  )
    throw new Error("网络状态无法核实");
  return row as NetworkControl;
}

function readNetworks(raw: unknown): NetworkControl[] {
  if (!raw || typeof raw !== "object" || !("items" in raw))
    throw new Error("网络状态无法核实");
  if (!Array.isArray(raw.items) || raw.items.length !== 2)
    throw new Error("网络状态无法核实");
  const rows = raw.items.map(readNetwork);
  if (rows[0]!.network === rows[1]!.network)
    throw new Error("网络状态无法核实");
  return rows;
}

const rows = ref<NetworkControl[]>([]);
const selected = ref<NetworkControl>();
const uncertain = ref<NetworkControl>();
const password = ref("");
const passwordInput = ref<HTMLInputElement>();
const controls = ref<HTMLElement>();
const loading = ref(false);
const saving = ref(false);
const error = ref("");
const notice = ref("");
const client = props.client;
const csrf = client.csrfToken;
let active = true;
let request: AbortController | undefined;

function current() {
  return (
    active &&
    client === props.client &&
    csrf === client.csrfToken &&
    client.area === "admin"
  );
}

async function refresh() {
  if (!current() || loading.value || saving.value || selected.value) return;
  error.value = "";
  notice.value = "";
  rows.value = [];
  loading.value = true;
  const controller = new AbortController();
  request = controller;
  try {
    const fresh = readNetworks(
      await client.request(endpoint, { signal: controller.signal }),
    );
    if (!current() || request !== controller) return;
    rows.value = fresh;
    const pending = uncertain.value;
    if (pending) {
      const row = fresh.find((entry) => entry.network === pending.network)!;
      if (BigInt(row.revision) > BigInt(pending.revision)) {
        uncertain.value = undefined;
        notice.value = "已核对当前网络状态";
        emit("changed");
      }
    }
  } catch {
    if (current() && request === controller)
      error.value = "暂时无法核对 USDT 网络状态，请刷新后重试。";
  } finally {
    if (current() && request === controller) loading.value = false;
  }
}

function choose(row: NetworkControl) {
  if (
    !current() ||
    loading.value ||
    saving.value ||
    uncertain.value ||
    (!row.enabled && !row.ready_to_enable)
  )
    return;
  error.value = "";
  notice.value = "";
  password.value = "";
  selected.value = row;
  void nextTick(() => passwordInput.value?.focus({ preventScroll: true }));
}

function close() {
  if (saving.value) return;
  if (!selected.value) {
    emit("close");
    return;
  }
  const network = selected.value.network;
  selected.value = undefined;
  password.value = "";
  error.value = "";
  void nextTick(() => {
    if (!current()) return;
    controls.value
      ?.querySelector<HTMLElement>(`[data-network="${network}"] button`)
      ?.focus({ preventScroll: true });
  });
}

async function submit() {
  const original = selected.value;
  if (!original) {
    close();
    return;
  }
  if (
    !current() ||
    loading.value ||
    saving.value ||
    uncertain.value ||
    (!original.enabled && !original.ready_to_enable) ||
    !password.value
  )
    return;
  const body = {
    enabled: !original.enabled,
    expected_revision: original.revision,
    current_password: password.value,
  };
  password.value = "";
  error.value = "";
  saving.value = true;
  const controller = new AbortController();
  request = controller;
  try {
    const row = readNetwork(
      await client.request(endpoint + "/" + original.network, {
        method: "PATCH",
        body,
        signal: controller.signal,
      }),
    );
    if (!current() || request !== controller) return;
    if (
      row.network !== original.network ||
      row.enabled !== body.enabled ||
      BigInt(row.revision) <= BigInt(original.revision)
    )
      throw new Error("网络修改结果无法核实");
    rows.value = rows.value.map((entry) =>
      entry.network === row.network ? row : entry,
    );
    selected.value = undefined;
    notice.value = `${cryptoNetworkLabel(row.network)}${row.enabled ? "已开启" : "已关闭"}`;
    emit("changed");
    void nextTick(() =>
      controls.value
        ?.querySelector<HTMLButtonElement>(
          `[data-network="${original.network}"] button`,
        )
        ?.focus({ preventScroll: true }),
    );
  } catch (reason) {
    if (!current() || request !== controller) return;
    const rejected =
      reason instanceof ApiError &&
      reason.responseDecoded &&
      [400, 401, 403, 409, 415, 422, 429].includes(reason.status);
    if (!rejected) {
      uncertain.value = original;
      selected.value = undefined;
      rows.value = [];
      emit("changed");
    } else {
      error.value = reason.message;
      if (reason.status === 409) {
        selected.value = undefined;
        rows.value = [];
      }
    }
  } finally {
    if (current() && request === controller) saving.value = false;
  }
}

watch(
  () => [props.client, props.client.csrfToken, props.client.area],
  () => emit("close"),
);
onMounted(refresh);
onBeforeUnmount(() => {
  active = false;
  password.value = "";
  request?.abort();
});
</script>

<template>
  <KitModal
    title="USDT 网络开关"
    drawer
    width="580px"
    :loading="saving"
    :danger="selected?.enabled"
    :submit-text="
      selected ? (selected.enabled ? '确认关闭' : '确认开启') : '关闭'
    "
    :cancel-text="selected ? '返回网络列表' : undefined"
    :hide-cancel="!selected"
    :submit-disabled="!!selected && !password"
    @close="close"
    @submit="submit"
  >
    <div ref="controls" class="crypto-network-controls">
      <p v-if="error" class="alert error" role="alert">{{ error }}</p>
      <p v-if="notice" class="alert success" role="status">{{ notice }}</p>
      <template v-if="selected">
        <h3>
          {{ selected.enabled ? "关闭" : "开启" }}
          {{ cryptoNetworkLabel(selected.network) }}
        </h3>
        <p v-if="selected.enabled">
          关闭后暂停该网络的新收款、结算和退币申请。已有订单继续按原方式核对。
        </p>
        <p v-else>开启后，符合支付通道及商户配置的 USDT 交易可以使用该网络。</p>
        <label class="pa-field">
          <span class="pa-label">管理员登录密码</span>
          <input
            ref="passwordInput"
            v-model="password"
            type="password"
            autocomplete="current-password"
            required
            :disabled="saving"
          />
        </label>
      </template>
      <template v-else>
        <p v-if="uncertain" class="alert error" role="alert">
          操作结果尚未确认。请核对原操作结果，勿重复提交。
        </p>
        <div class="network-controls-header">
          <p class="muted">按网络检查钱包、收款地址池和监听服务。</p>
          <button
            v-if="uncertain || (!loading && !rows.length && error)"
            type="button"
            class="pa-btn"
            data-crypto-network-refresh
            :disabled="loading || saving"
            @click="refresh"
          >
            {{ uncertain ? "核对原操作结果" : "重试" }}
          </button>
        </div>
        <p v-if="loading" role="status">正在核对网络状态…</p>
        <article
          v-for="row in rows"
          :key="row.network"
          :data-network="row.network"
          class="network-control-card"
        >
          <div class="network-controls-header">
            <h3>{{ cryptoNetworkLabel(row.network) }}</h3>
            <KitStatusTag
              :text="
                row.enabled
                  ? row.effective_enabled
                    ? '已开启'
                    : '已开启 · 收款暂停'
                  : '已关闭'
              "
              :tone="
                row.effective_enabled
                  ? 'success'
                  : row.enabled
                    ? 'warning'
                    : 'neutral'
              "
            />
          </div>
          <dl class="network-readiness">
            <div>
              <dt>钱包地址</dt>
              <dd>{{ row.wallets_configured ? "已配置" : "待配置" }}</dd>
            </div>
            <div>
              <dt>收款地址池</dt>
              <dd>
                {{
                  row.deposit_address_available ? "有可用地址" : "无可用地址"
                }}
              </dd>
            </div>
            <div>
              <dt>监听服务</dt>
              <dd>{{ row.listener_ready ? "正常" : "未就绪" }}</dd>
            </div>
          </dl>
          <p
            v-if="row.enabled && !row.effective_enabled && row.ready_to_enable"
            class="muted"
          >
            当前启用记录需要重新确认。核对钱包后，请关闭并重新开启此网络。
          </p>
          <div class="network-control-actions">
            <a
              v-if="row.wallets_configured && !row.deposit_address_available"
              href="/admin/system/crypto/operations"
              >管理收款地址池</a
            >
            <button
              type="button"
              class="pa-btn"
              :class="row.enabled ? 'pa-btn--danger' : 'pa-btn--primary'"
              :aria-label="
                (row.enabled ? '关闭 ' : '开启 ') +
                cryptoNetworkLabel(row.network)
              "
              :disabled="!!uncertain || (!row.enabled && !row.ready_to_enable)"
              @click="choose(row)"
            >
              {{ row.enabled ? "关闭网络" : "开启网络" }}
            </button>
          </div>
        </article>
      </template>
    </div>
  </KitModal>
</template>

<style scoped>
.crypto-network-controls {
  display: grid;
  gap: 12px;
}
.crypto-network-controls p,
.crypto-network-controls h3 {
  margin: 0;
}
.network-control-card {
  min-width: 0;
  padding: 14px;
  border: 1px solid var(--line);
  border-radius: 8px;
}
.network-controls-header,
.network-control-actions {
  display: flex;
  gap: 8px 12px;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
}
.network-control-actions {
  justify-content: flex-end;
}
.network-readiness {
  display: grid;
  gap: 4px;
  margin: 12px 0;
}
.network-readiness > div {
  display: flex;
  gap: 8px;
  justify-content: space-between;
}
.network-readiness dd {
  margin: 0;
}
</style>
