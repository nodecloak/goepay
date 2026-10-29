<script setup lang="ts">
import {
  computed,
  nextTick,
  onBeforeUnmount,
  onMounted,
  ref,
  watch,
} from "vue";
import { ApiError, type ApiClient } from "../api";
import { effectiveRiskDescription } from "../core-risk-status";
import RuntimeChannelLimitsPanel from "./RuntimeChannelLimitsPanel.vue";
import BuyerFeeSettings104 from "./BuyerFeeSettings104.vue";
import {
  channelRetainedRate,
  readRuntimeChannelCatalog,
  readRuntimeChannelPolicy,
  readRuntimeChannelPolicyResult,
  runtimeChannelPolicyPath,
  type RuntimeChannelPolicy,
  type RuntimeChannelPolicyChange,
} from "../runtime-channel-policy";
const props = defineProps<{
  client: ApiClient;
  allowed: boolean;
  channelID?: string;
}>();
const emit = defineEmits<{ close: []; contextInvalid: [] }>();
let controlledStarted = false;
const limitsID = ref<string>();
const minimumBusy104 = ref(false);
const items = ref<RuntimeChannelPolicy[]>([]),
  loading = ref(false),
  error = ref(""),
  modalID = ref<string>(),
  target = ref<RuntimeChannelPolicy>(),
  detailLoading = ref(false),
  busy = ref(false),
  detailError = ref(""),
  notice = ref(""),
  rate = ref(""),
  enabled = ref(true),
  confirmation = ref<RuntimeChannelPolicyChange>();
const modal = ref<HTMLElement>(),
  rateInput = ref<HTMLInputElement>(),
  refreshButton = ref<HTMLButtonElement>(),
  closeButton = ref<HTMLButtonElement>(),
  catalogRefreshButton = ref<HTMLButtonElement>(),
  returnButton = ref<HTMLButtonElement>(),
  confirmGroup = ref<HTMLElement>();
let active = true,
  epoch = 0,
  controller: AbortController | undefined,
  opener: HTMLElement | undefined;
let contextClient = props.client,
  contextCSRF = props.client.csrfToken;
function resetContext() {
  minimumBusy104.value = false;
  limitsID.value = undefined;
  ++epoch;
  controller?.abort();
  controller = undefined;
  modalID.value = undefined;
  target.value = undefined;
  confirmation.value = undefined;
  busy.value = false;
  loading.value = false;
  detailLoading.value = false;
  items.value = [];
  opener = undefined;
  detailError.value = "";
  notice.value = "";
  error.value = "";
  rate.value = "";
  contextClient = props.client;
  contextCSRF = props.client.csrfToken;
}
function ensureSession() {
  if (props.client === contextClient && props.client.csrfToken === contextCSRF)
    return true;
  resetContext();
  if (props.channelID) emit("contextInvalid");
  error.value = "会话已变更，请刷新支付通道后重新核对。";
  const current = epoch;
  void nextTick(() => {
    if (active && current === epoch && !modalID.value)
      catalogRefreshButton.value?.focus();
  });
  return false;
}
const currentRate = computed(() => {
  try {
    return channelRetainedRate(rate.value);
  } catch {
    return undefined;
  }
});
const fee = (bps: number) =>
  Math.floor(bps / 100) + "." + String(bps % 100).padStart(2, "0");
const paymentType = (v: string) => (v === "alipay" ? "支付宝" : "微信支付");
const product = (v: string) =>
  (
    ({
      page: "电脑网站",
      wap: "手机网站",
      h5: "H5支付",
      native: "扫码支付",
      jsapi: "公众号支付",
      app: "APP支付",
      mini: "小程序",
      scan: "付款码支付",
      preauth: "预授权",
    }) as Record<string, string>
  )[v] || v;
function begin() {
  controller?.abort();
  const c = new AbortController();
  controller = c;
  return { c, current: ++epoch };
}
const valid = (current: number, c: AbortController) =>
  active &&
  current === epoch &&
  !c.signal.aborted &&
  props.allowed &&
  props.client.area === "admin" &&
  ensureSession();
async function load() {
  if (!ensureSession()) return;
  if (
    busy.value ||
    modalID.value ||
    limitsID.value ||
    !props.allowed ||
    props.client.area !== "admin"
  )
    return;
  const { c, current } = begin();
  loading.value = true;
  error.value = "";
  items.value = [];
  try {
    const raw = await props.client.request<unknown>("/channel-policies", {
      signal: c.signal,
    });
    if (valid(current, c)) items.value = readRuntimeChannelCatalog(raw);
  } catch {
    if (valid(current, c)) error.value = "支付通道费率读取失败，请刷新重试。";
  } finally {
    if (valid(current, c)) loading.value = false;
  }
}
async function loadDetail() {
  if (!ensureSession()) return;
  const id = modalID.value;
  if (!id || busy.value) return;
  const { c, current } = begin();
  detailLoading.value = true;
  target.value = undefined;
  confirmation.value = undefined;
  detailError.value = "";
  notice.value = "";
  try {
    const raw = await props.client.request<unknown>(
      runtimeChannelPolicyPath(id),
      { signal: c.signal },
    );
    if (!valid(current, c) || modalID.value !== id) return;
    const state = readRuntimeChannelPolicy(raw);
    if (state.channel_id !== id) throw Error("wrong target");
    target.value = state;
    rate.value = state.rate ?? "";
    enabled.value = state.enabled;
  } catch {
    if (valid(current, c) && modalID.value === id)
      detailError.value = "当前通道配置无法核实，请刷新后重新确认。";
  } finally {
    if (valid(current, c) && modalID.value === id) {
      detailLoading.value = false;
      await nextTick();
      if (valid(current, c))
        (detailError.value ? refreshButton.value : rateInput.value)?.focus();
    }
  }
}
async function open(item: RuntimeChannelPolicy, event: MouseEvent) {
  if (!ensureSession()) return;
  if (busy.value || modalID.value || limitsID.value) return;
  opener =
    event.currentTarget instanceof HTMLElement
      ? event.currentTarget
      : undefined;
  modalID.value = item.channel_id;
  await loadDetail();
}
function openLimits(item: RuntimeChannelPolicy, event: MouseEvent) {
  if (!ensureSession() || busy.value || modalID.value || limitsID.value) return;
  opener =
    event.currentTarget instanceof HTMLElement
      ? event.currentTarget
      : undefined;
  limitsID.value = item.channel_id;
}
async function closeLimits() {
  if (!ensureSession()) return;
  const original = opener;
  opener = undefined;
  limitsID.value = undefined;
  await nextTick();
  if (active && original?.isConnected) original.focus();
}
async function close() {
  if (!ensureSession()) return;
  if (busy.value || minimumBusy104.value) return;
  const original = opener;
  opener = undefined;
  ++epoch;
  controller?.abort();
  modalID.value = undefined;
  target.value = undefined;
  confirmation.value = undefined;
  detailError.value = "";
  notice.value = "";
  rate.value = "";
  await nextTick();
  if (active && original?.isConnected) original.focus();
  if (props.channelID) emit("close");
}
async function confirm() {
  if (!ensureSession()) return;
  if (
    busy.value ||
    minimumBusy104.value ||
    detailLoading.value ||
    !target.value?.can_update
  )
    return;
  if (!currentRate.value) {
    detailError.value = "费率须为0至100，最多两位小数。";
    await nextTick();
    rateInput.value?.focus();
    return;
  }
  detailError.value = "";
  notice.value = "";
  confirmation.value = {
    rate: currentRate.value.rate,
    mode: 0,
    enabled: enabled.value,
  };
  await nextTick();
  returnButton.value?.focus();
}
async function back() {
  if (!ensureSession()) return;
  if (busy.value) return;
  confirmation.value = undefined;
  await nextTick();
  rateInput.value?.focus();
}
async function submit() {
  if (!ensureSession()) return;
  const before = target.value,
    desired = confirmation.value,
    id = modalID.value;
  if (
    busy.value ||
    !before?.can_update ||
    !desired ||
    !id ||
    before.channel_id !== id
  )
    return;
  const { c, current } = begin();
  busy.value = true;
  detailError.value = "";
  notice.value = "";
  try {
    await nextTick();
    if (!valid(current, c) || modalID.value !== id) return;
    confirmGroup.value?.focus();
    const raw = await props.client.request<unknown>(
      runtimeChannelPolicyPath(id),
      {
        method: "PATCH",
        body: { ...desired, expected_fingerprint: before.fingerprint },
        signal: c.signal,
      },
    );
    if (!valid(current, c) || modalID.value !== id) return;
    const after = readRuntimeChannelPolicyResult(raw, before, desired);
    target.value = after;
    items.value = items.value.map((v) => (v.channel_id === id ? after : v));
    rate.value = after.rate!;
    enabled.value = after.enabled;
    confirmation.value = undefined;
    notice.value = "费率设置已保存。";
  } catch (e) {
    if (valid(current, c) && modalID.value === id) {
      target.value = undefined;
      confirmation.value = undefined;
      detailError.value =
        e instanceof ApiError && e.status === 409
          ? "通道配置已变更，请刷新后重新确认；不会自动重试。"
          : "本次通道修改未能确认，请刷新当前状态；不要直接重复提交。";
    }
  } finally {
    if (valid(current, c) && modalID.value === id) {
      busy.value = false;
      await nextTick();
      if (valid(current, c))
        (detailError.value ? refreshButton.value : closeButton.value)?.focus();
    }
  }
}
function keydown(event: KeyboardEvent) {
  if (!modalID.value) return;
  if (
    (busy.value || minimumBusy104.value) &&
    ["Tab", "Enter", "Escape"].includes(event.key)
  ) {
    event.preventDefault();
    event.stopImmediatePropagation();
    (confirmGroup.value ?? modal.value)?.focus();
    return;
  }
  if (event.key === "Escape") {
    event.preventDefault();
    event.stopImmediatePropagation();
    void close();
    return;
  }
  if (event.key !== "Tab" || !modal.value) return;
  const nodes = Array.from(
      modal.value.querySelectorAll<HTMLElement>(
        "button:not([disabled]),input:not([disabled]),select:not([disabled]),a[href]",
      ),
    ).filter((el) => el.getClientRects().length > 0),
    first = nodes[0],
    last = nodes.at(-1);
  if (event.shiftKey && document.activeElement === first) {
    event.preventDefault();
    last?.focus();
  } else if (!event.shiftKey && document.activeElement === last) {
    event.preventDefault();
    first?.focus();
  }
}
watch(
  () => [
    props.client,
    props.client.area,
    props.client.csrfToken,
    props.allowed,
    props.channelID,
  ],
  () => {
    resetContext();
    if (props.channelID) {
      if (controlledStarted) {
        emit("contextInvalid");
        return;
      }
      controlledStarted = true;
      modalID.value = props.channelID;
      void loadDetail();
    } else void load();
  },
  { immediate: true, flush: "sync" },
);
onMounted(() => document.addEventListener("keydown", keydown, true));
onBeforeUnmount(() => {
  active = false;
  ++epoch;
  controller?.abort();
  document.removeEventListener("keydown", keydown, true);
});
</script>
<template>
  <section
    v-if="allowed && client.area === 'admin'"
    class="runtime-channel-policy"
    role="region"
    aria-label="支付通道费率"
  >
    <template v-if="!channelID"
      ><div class="toolbar">
        <h2>已接入通道费率</h2>
        <button
          ref="catalogRefreshButton"
          type="button"
          :disabled="loading || !!modalID || !!limitsID"
          @click="load"
        >
          刷新支付通道
        </button>
      </div>
      <p class="muted">
        费率为给商户的分成比例；用户组另设比例时优先使用用户组，未启用手续费时不扣费。
      </p>
      <div v-if="error" class="alert error" role="alert">{{ error }}</div>
      <p v-if="loading" role="status">正在读取支付通道…</p>
      <div v-else class="table-scroll">
        <table>
          <thead>
            <tr>
              <th>通道ID</th>
              <th>支付方式</th>
              <th>支付产品</th>
              <th>费率</th>
              <th>收款配置 / 有效风控</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="item in items" :key="item.channel_id">
              <td>{{ item.channel_id }}</td>
              <td>{{ paymentType(item.pay_type) }}</td>
              <td>{{ item.methods.map(product).join("、") }}</td>
              <td>
                <template v-if="item.configured"
                  >{{ item.rate }}%<small class="cell muted"
                    >手续费{{ fee(item.fee_rate_bps!) }}%</small
                  ></template
                ><span v-else>尚未配置，沿用原费用设置</span>
              </td>
              <td>配置：{{ item.enabled ? "开启" : "关闭" }}<small class="cell muted">{{ effectiveRiskDescription(item.core_risk, item.enabled) }}</small></td>
              <td>
                <button
                  type="button"
                  :disabled="!item.can_update || !!modalID || !!limitsID"
                  @click="open(item, $event)"
                >
                  设置费率
                </button>
                <button
                  type="button"
                  :disabled="!item.can_update || !!modalID || !!limitsID"
                  @click="openLimits(item, $event)"
                >
                  限额设置
                </button>
              </td>
            </tr>
            <tr v-if="!items.length && !error">
              <td colspan="6" class="empty">暂无已接入支付通道</td>
            </tr>
          </tbody>
        </table>
      </div>
      <p class="muted">
        下方为通道资料；资料启用不会代替此处的收款开关。通道暂停及商户权限限制仍会生效。
      </p> </template
    ><RuntimeChannelLimitsPanel
      v-if="limitsID"
      :key="limitsID"
      :channel-i-d="limitsID"
      :client="client"
      :allowed="allowed"
      @close="closeLimits"
      @context-invalid="ensureSession"
    />
    <div v-if="modalID" class="modal-backdrop">
      <section
        ref="modal"
        class="modal panel channel-policy-modal"
        role="dialog"
        aria-modal="true"
        aria-label="设置支付通道费率"
        tabindex="-1"
      >
        <h2>设置支付通道费率</h2>
        <p class="channel-target">通道ID：{{ modalID }}</p>
        <p v-if="detailLoading" role="status">正在核对当前通道…</p>
        <p v-if="detailError" class="alert error" role="alert">
          {{ detailError }}
        </p>
        <p v-if="notice" class="alert success" role="status">{{ notice }}</p>
        <form v-if="target && !confirmation" @submit.prevent="confirm">
          <fieldset :disabled="busy || minimumBusy104 || !target.can_update">
            <label for="runtime-channel-rate">支付费率（商户分成%）</label
            ><input
              id="runtime-channel-rate"
              ref="rateInput"
              v-model="rate"
              inputmode="decimal"
              maxlength="8"
              autocomplete="off"
            />
            <p class="muted">
              留空按100.00%保存；分成99.50%对应手续费0.50%。<span
                v-if="currentRate"
                >当前手续费{{ fee(currentRate.feeBPS) }}%。</span
              >
            </p>
            <label for="runtime-channel-mode">收款模式</label
            ><select id="runtime-channel-mode" :value="0">
              <option :value="0">平台代收</option>
              <option :value="1" disabled>商户直清（暂不可用）</option>
            </select>
            <label class="channel-enabled"
              ><input v-model="enabled" type="checkbox" />启用收款</label
            >
            <button type="submit" class="primary">保存费率设置</button>
          </fieldset>
        </form>
        <BuyerFeeSettings104
          v-if="target && !confirmation"
          kind="parent"
          :key="modalID"
          :channel-id="modalID"
          :client="client"
          :allowed="allowed && !busy"
          @busy="minimumBusy104 = $event"
        />
        <div
          v-if="confirmation"
          ref="confirmGroup"
          class="alert channel-confirm"
          role="group"
          aria-label="确认修改支付通道"
          tabindex="-1"
        >
          <p>通道ID：{{ modalID }}</p>
          <p>
            商户分成{{ confirmation.rate }}%，手续费{{
              fee(channelRetainedRate(confirmation.rate).feeBPS)
            }}%；平台代收，收款{{ confirmation.enabled ? "开启" : "关闭" }}。
          </p>
          <p class="muted">
            历史订单不会重新计费；缺少原费用记录且尚未发起支付的旧单，需要重新下单。
          </p>
          <div class="toolbar-actions">
            <button
              ref="returnButton"
              type="button"
              :disabled="busy"
              @click="back"
            >
              返回修改</button
            ><button
              type="button"
              class="primary"
              :disabled="busy"
              @click="submit"
            >
              {{ busy ? "正在保存…" : "确定保存" }}
            </button>
          </div>
        </div>
        <div class="toolbar-actions">
          <button
            v-if="!detailLoading && !confirmation"
            ref="refreshButton"
            type="button"
            :disabled="busy || minimumBusy104"
            @click="loadDetail"
          >
            刷新通道配置</button
          ><button
            ref="closeButton"
            type="button"
            :disabled="busy || minimumBusy104"
            @click="close"
          >
            关闭
          </button>
        </div>
      </section>
    </div>
  </section>
</template>
<style scoped>
.runtime-channel-policy {
  margin-bottom: 24px;
  min-width: 0;
  max-width: 100%;
  overflow-wrap: anywhere;
}
.runtime-channel-policy .toolbar {
  display: flex;
  justify-content: space-between;
  gap: 12px;
  flex-wrap: wrap;
}
.runtime-channel-policy table {
  min-width: 650px;
}
.runtime-channel-policy h2 {
  margin: 0 0 12px;
}
.runtime-channel-policy .cell {
  display: block;
}
.runtime-channel-policy td button + button {
  margin-left: 8px;
}
.channel-policy-modal {
  width: min(560px, calc(100vw - 32px));
  max-height: calc(100dvh - 32px);
  overflow: auto;
}
.channel-policy-modal input:not([type="checkbox"]),
.channel-policy-modal select {
  width: 100%;
  margin: 8px 0;
}
.channel-policy-modal fieldset {
  min-width: 0;
  border: 0;
  padding: 0;
  margin: 0;
}
.channel-enabled {
  display: flex;
  align-items: center;
  gap: 8px;
  margin: 16px 0;
}
.channel-target,
.channel-confirm {
  overflow-wrap: anywhere;
}
.channel-confirm:focus {
  outline: 2px solid var(--primary, var(--focus));
  outline-offset: 2px;
}
.channel-policy-modal > .toolbar-actions {
  margin-top: 16px;
}
</style>
