<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, useId, watch } from "vue";
import { ApiClient, ApiError } from "../api";

const props = defineProps<{
  client: ApiClient;
  tradeNo: string;
  merchantID: string;
  verifiedApiTradeNo: string;
  allowed: boolean;
  ownSession: boolean;
  disabled?: boolean;
}>();
const emit = defineEmits<{ active: [boolean] }>();
type AliasView = {
  trade_no: string;
  merchant_id: string;
  api_trade_no_alias: string;
  verified_api_trade_no: string;
  fingerprint: string;
  can_update: boolean;
};
const id = useId();
const entry = ref<HTMLButtonElement>(),
  modal = ref<HTMLElement>(),
  input = ref<HTMLInputElement>(),
  open = ref(false),
  loading = ref(false),
  writing = ref(false),
  needsRead = ref(true),
  state = ref<AliasView>(),
  savedAlias = ref(""),
  draft = ref(""),
  error = ref(""),
  notice = ref("");
let alive = true,
  epoch = 0,
  controller: AbortController | undefined;

const available = computed(
  () =>
    props.allowed === true &&
    props.ownSession === true &&
    props.client.area === "admin" &&
    !!props.client.csrfToken &&
    !!props.tradeNo &&
    /^[1-9]\d*$/.test(props.merchantID),
);
const canSave = computed(
  () =>
    available.value &&
    !props.disabled &&
    !loading.value &&
    !writing.value &&
    !needsRead.value &&
    state.value?.can_update === true &&
    draft.value !== state.value.api_trade_no_alias,
);
function context() {
  return {
    client: props.client,
    area: props.client.area,
    csrf: props.client.csrfToken,
    trade: props.tradeNo,
    merchant: props.merchantID,
    verified: props.verifiedApiTradeNo,
    own: props.ownSession,
    allowed: props.allowed,
  };
}
function current(
  source: ReturnType<typeof context>,
  run: number,
  c: AbortController,
) {
  return (
    alive &&
    open.value &&
    run === epoch &&
    !c.signal.aborted &&
    available.value &&
    source.client === props.client &&
    source.area === props.client.area &&
    source.csrf === props.client.csrfToken &&
    source.trade === props.tradeNo &&
    source.merchant === props.merchantID &&
    source.verified === props.verifiedApiTradeNo &&
    source.own === props.ownSession &&
    source.allowed === props.allowed
  );
}
function decode(raw: unknown, source: ReturnType<typeof context>): AliasView {
  const fields = [
    "trade_no",
    "merchant_id",
    "api_trade_no_alias",
    "verified_api_trade_no",
    "fingerprint",
    "can_update",
  ];
  if (!raw || typeof raw !== "object" || Array.isArray(raw))
    throw new Error("别名记录无法核对，请重新读取。");
  const value = raw as Record<string, unknown>;
  if (
    Object.keys(value).length !== fields.length ||
    !fields.every((field) =>
      Object.prototype.hasOwnProperty.call(value, field),
    ) ||
    value.trade_no !== source.trade ||
    value.merchant_id !== source.merchant ||
    value.verified_api_trade_no !== source.verified ||
    typeof value.api_trade_no_alias !== "string" ||
    typeof value.fingerprint !== "string" ||
    !/^[a-f0-9]{64}$/.test(value.fingerprint) ||
    typeof value.can_update !== "boolean"
  )
    throw new Error("别名记录无法核对，请重新读取。");
  return value as AliasView;
}
function abort() {
  epoch++;
  controller?.abort();
  controller = undefined;
  loading.value = false;
  writing.value = false;
}
function invalidate() {
  abort();
  open.value = false;
  state.value = undefined;
  savedAlias.value = "";
  draft.value = "";
  needsRead.value = true;
  error.value = "";
  notice.value = "";
  emit("active", false);
}
async function dismiss() {
  if (writing.value) return;
  abort();
  open.value = false;
  draft.value = "";
  error.value = "";
  emit("active", false);
  await nextTick();
  if (alive && available.value) entry.value?.focus();
}
async function read() {
  if (!open.value || !available.value || writing.value || props.disabled)
    return;
  abort();
  const source = context(),
    run = epoch,
    c = new AbortController();
  controller = c;
  loading.value = true;
  needsRead.value = true;
  state.value = undefined;
  draft.value = "";
  error.value = "";
  try {
    const raw = await source.client.request<unknown>(
      `/orders/${encodeURIComponent(source.trade)}/api-trade-alias`,
      { signal: c.signal },
    );
    if (!current(source, run, c)) return;
    const next = decode(raw, source);
    state.value = next;
    savedAlias.value = next.api_trade_no_alias;
    draft.value = next.api_trade_no_alias;
    needsRead.value = false;
  } catch (failure) {
    if (current(source, run, c))
      error.value =
        failure instanceof Error
          ? failure.message
          : "别名读取失败，请重新读取。";
  } finally {
    if (current(source, run, c)) {
      controller = undefined;
      loading.value = false;
      await nextTick();
      if (current(source, run, c)) input.value?.focus();
    }
  }
}
async function edit() {
  if (!available.value || props.disabled || open.value) return;
  open.value = true;
  notice.value = "";
  emit("active", true);
  void read();
  await nextTick();
  if (open.value) modal.value?.focus();
}
async function save() {
  if (!canSave.value || !state.value) return;
  const source = context(),
    value = draft.value,
    fingerprint = state.value.fingerprint,
    run = ++epoch,
    c = new AbortController();
  controller = c;
  writing.value = true;
  error.value = "";
  notice.value = "";
  try {
    const result = await source.client.requestResult<unknown>(
      `/orders/${encodeURIComponent(source.trade)}/api-trade-alias`,
      {
        method: "PATCH",
        body: {
          api_trade_no_alias: value,
          expected_fingerprint: fingerprint,
        },
        signal: c.signal,
      },
    );
    if (!current(source, run, c)) return;
    if (result.status !== 200) throw new Error("别名保存结果尚未确认。");
    const next = decode(result.data, source);
    if (next.api_trade_no_alias !== value)
      throw new Error("别名保存结果不一致，请重新读取。");
    state.value = next;
    savedAlias.value = next.api_trade_no_alias;
    needsRead.value = false;
    notice.value = value ? "显示别名已保存。" : "显示别名已清空。";
    writing.value = false;
    await dismiss();
  } catch (failure) {
    if (!current(source, run, c)) return;
    needsRead.value = true;
    if (failure instanceof ApiError && failure.responseDecoded) {
      error.value =
        failure.status === 409
          ? "别名已由其他管理员更新，请重新读取后核对并确认。"
          : `${failure.message}，请重新读取后再编辑。`;
    } else {
      error.value = "别名保存结果尚未确认，请重新读取核对；不会自动重复提交。";
    }
  } finally {
    if (current(source, run, c)) {
      writing.value = false;
      controller = undefined;
    }
  }
}
function keydown(event: KeyboardEvent) {
  if (event.key === "Escape") {
    event.preventDefault();
    event.stopPropagation();
    void dismiss();
    return;
  }
  if (event.key !== "Tab") return;
  const controls = Array.from(
    modal.value?.querySelectorAll<HTMLElement>(
      'button:not(:disabled), input:not(:disabled), [tabindex="0"]',
    ) || [],
  );
  const first = controls[0],
    last = controls[controls.length - 1];
  if (!first || !last) {
    event.preventDefault();
    modal.value?.focus();
  } else if (
    event.shiftKey &&
    (document.activeElement === first || document.activeElement === modal.value)
  ) {
    event.preventDefault();
    last.focus();
  } else if (
    !event.shiftKey &&
    (document.activeElement === last || document.activeElement === modal.value)
  ) {
    event.preventDefault();
    first.focus();
  }
}
watch(
  () => [
    props.client,
    props.client.area,
    props.client.csrfToken,
    props.tradeNo,
    props.merchantID,
    props.verifiedApiTradeNo,
    props.allowed,
    props.ownSession,
  ],
  invalidate,
  { flush: "sync" },
);
onBeforeUnmount(() => {
  alive = false;
  invalidate();
});
</script>

<template>
  <div v-if="available" class="api-trade-alias">
    <button
      ref="entry"
      type="button"
      class="link-button"
      :disabled="disabled || open"
      @click="edit"
    >
      编辑接口订单号
    </button>
    <span v-if="savedAlias" class="alias-value"
      >显示别名：{{ savedAlias }}</span
    >
    <span v-if="notice" class="alias-notice" role="status">{{ notice }}</span>
    <div v-if="open" class="modal-backdrop alias-backdrop">
      <section
        ref="modal"
        class="modal panel alias-modal"
        role="dialog"
        aria-modal="true"
        aria-label="编辑接口订单号"
        tabindex="-1"
        @keydown="keydown"
      >
        <h3>编辑接口订单号</h3>
        <p>仅修改显示别名；清空会移除别名。已验证的上游交易回执保持不变。</p>
        <dl>
          <dt>系统订单号</dt>
          <dd>{{ tradeNo }}</dd>
          <dt>已验证上游接口订单号</dt>
          <dd>{{ verifiedApiTradeNo || "—" }}</dd>
        </dl>
        <p v-if="loading" role="status">正在读取显示别名…</p>
        <p v-if="error" class="alert error" role="alert">{{ error }}</p>
        <p v-if="state && !state.can_update" class="muted">
          当前记录只读，不能修改显示别名。
        </p>
        <form @submit.prevent="save">
          <label :for="`${id}-value`">接口订单号（显示别名）</label>
          <input
            :id="`${id}-value`"
            ref="input"
            v-model="draft"
            name="api_trade_no_alias"
            placeholder="请输入接口订单号"
            autocomplete="off"
            :disabled="
              loading || writing || needsRead || !state?.can_update || disabled
            "
          />
          <div class="modal-actions">
            <button type="button" :disabled="writing" @click="dismiss">
              取消
            </button>
            <button
              v-if="needsRead && !loading"
              type="button"
              :disabled="writing || disabled"
              @click="read"
            >
              重新读取
            </button>
            <button
              type="submit"
              class="primary"
              :disabled="!canSave"
              @click.prevent="save"
            >
              确定
            </button>
          </div>
        </form>
      </section>
    </div>
  </div>
</template>

<style scoped>
.api-trade-alias {
  display: inline;
  max-width: 100%;
}
.api-trade-alias > button {
  margin-inline-start: 0.5rem;
}
.alias-value,
.alias-notice {
  display: block;
  overflow-wrap: anywhere;
}
.alias-backdrop {
  z-index: 110;
}
.alias-modal {
  width: min(32rem, calc(100vw - 2rem));
  max-height: calc(100dvh - 2rem);
  min-width: 0;
  overflow: auto;
  overflow-wrap: anywhere;
}
.alias-modal dl {
  display: grid;
  gap: 0.35rem;
}
.alias-modal dd {
  margin: 0 0 0.5rem;
}
.alias-modal input {
  width: 100%;
  min-width: 0;
  box-sizing: border-box;
  margin-top: 0.4rem;
}
.alias-modal .modal-actions {
  flex-wrap: wrap;
}
</style>
