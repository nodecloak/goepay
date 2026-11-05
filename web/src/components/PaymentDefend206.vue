<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, toRaw, watch } from "vue";
import { ApiClient, ApiError } from "../api";
import { useSettingsDraft212 } from "../settings-drafts212";
import {
  paymentDefend206Body,
  paymentDefend206Keys,
  paymentDefend206Modes,
  paymentDefend206Types,
  readPaymentDefend206Policy,
  samePaymentDefend206Settings,
  validPaymentDefend206Settings,
  type PaymentDefend206Policy,
  type PaymentDefend206Settings,
  type PaymentDefend206Update,
} from "../payment-defend206";

const props = withDefaults(
  defineProps<{
    client: ApiClient;
    adminClass?: string;
    ownSession?: boolean;
    disabled?: boolean;
  }>(),
  { adminClass: "", ownSession: false, disabled: false },
);
const emit = defineEmits<{ busy: [boolean] }>();
const state = ref<PaymentDefend206Policy>(),
  draft = ref<PaymentDefend206Settings>(),
  loading = ref(false),
  busy = ref(false),
  uncertain = ref(false),
  unresolvedSave = ref(false),
  confirming = ref(false),
  error = ref(""),
  notice = ref("");
const errorTarget = ref<HTMLElement>(),
  prepareButton = ref<HTMLButtonElement>(),
  dialog = ref<HTMLElement>();
let alive = true,
  epoch = 0,
  controller: AbortController | undefined,
  prepared: PaymentDefend206Update | undefined;
function context() {
  return {
    client: toRaw(props.client),
    area: props.client.area,
    csrf: props.client.csrfToken,
    own: props.ownSession,
    adminClass: props.adminClass,
  };
}
let tracked = context();
function isAllowed() {
  return (
    props.client.area === "admin" &&
    !!props.client.csrfToken &&
    props.adminClass === "super" &&
    props.ownSession === true
  );
}
const allowed = computed(isAllowed);
function same(source = tracked) {
  const now = context();
  return (
    alive &&
    source.client === now.client &&
    source.area === now.area &&
    source.csrf === now.csrf &&
    source.own === now.own &&
    source.adminClass === now.adminClass
  );
}
function clear(message = "") {
  epoch++;
  controller?.abort();
  controller = undefined;
  state.value = undefined;
  draft.value = undefined;
  prepared = undefined;
  loading.value = false;
  busy.value = false;
  uncertain.value = false;
  unresolvedSave.value = false;
  confirming.value = false;
  error.value = message;
  notice.value = "";
  tracked = context();
  emit("busy", false);
}
function ensure() {
  if (!same()) {
    clear("会话已变化，请刷新页面后核对。");
    return false;
  }
  return isAllowed();
}
function current(
  run: number,
  c: AbortController,
  source: ReturnType<typeof context>,
) {
  if (!alive || run !== epoch || c.signal.aborted) return false;
  if (!same(source)) {
    clear("会话已变化，请刷新页面后核对。");
    return false;
  }
  return isAllowed();
}
const dirty = computed(
  () =>
    !!state.value &&
    !!draft.value &&
    !samePaymentDefend206Settings(state.value.settings, draft.value),
);
const { confirmDiscard: confirmDraftDiscard } = useSettingsDraft212(
  "创建订单人机验证",
  () => dirty.value || confirming.value || unresolvedSave.value,
);
const locked = computed(
  () =>
    !allowed.value ||
    loading.value ||
    busy.value ||
    uncertain.value ||
    confirming.value ||
    props.disabled,
);
const canSave = computed(
  () =>
    !locked.value && dirty.value && validPaymentDefend206Settings(draft.value),
);
async function load() {
  if (!ensure() || busy.value || confirming.value || props.disabled) return;
  const discardEpoch = epoch;
  if (
    !(await confirmDraftDiscard()) ||
    discardEpoch !== epoch ||
    !ensure() ||
    busy.value ||
    confirming.value ||
    props.disabled
  )
    return;
  controller?.abort();
  const source = tracked,
    run = ++epoch,
    c = new AbortController();
  controller = c;
  state.value = undefined;
  draft.value = undefined;
  prepared = undefined;
  loading.value = true;
  uncertain.value = true;
  error.value = "";
  notice.value = "";
  try {
    const raw = await source.client.request<unknown>(
      "/settings/payment-defend",
      { signal: c.signal },
    );
    if (!current(run, c, source)) return;
    const next = readPaymentDefend206Policy(raw);
    state.value = next;
    draft.value = { ...next.settings };
    uncertain.value = false;
    unresolvedSave.value = false;
  } catch {
    if (current(run, c, source))
      error.value = "当前支付验证设置无法确认，请刷新页面后核对。";
  } finally {
    if (current(run, c, source)) loading.value = false;
  }
}
async function prepare() {
  if (!ensure() || !canSave.value || !state.value || !draft.value) return;
  prepared = paymentDefend206Body(state.value, draft.value);
  confirming.value = true;
  error.value = "";
  notice.value = "";
  emit("busy", true);
  await nextTick();
  if (confirming.value && same()) dialog.value?.focus();
}
async function cancel() {
  if (busy.value) return;
  prepared = undefined;
  confirming.value = false;
  emit("busy", false);
  await nextTick();
  if (same() && isAllowed()) prepareButton.value?.focus();
}
async function submit() {
  if (
    !ensure() ||
    !confirming.value ||
    !prepared ||
    !state.value ||
    busy.value ||
    props.disabled
  )
    return;
  const before = state.value,
    body = prepared,
    source = tracked,
    run = ++epoch,
    c = new AbortController();
  controller?.abort();
  controller = c;
  busy.value = true;
  unresolvedSave.value = true;
  confirming.value = false;
  prepared = undefined;
  error.value = "";
  notice.value = "";
  emit("busy", true);
  try {
    const raw = await source.client.request<unknown>(
      "/settings/payment-defend",
      { method: "PATCH", body, signal: c.signal },
    );
    if (!current(run, c, source)) return;
    const next = readPaymentDefend206Policy(raw);
    if (
      !next.configured ||
      next.revision !== String(BigInt(before.revision) + 1n) ||
      next.fingerprint === before.fingerprint ||
      !samePaymentDefend206Settings(next.settings, body.settings)
    )
      throw Error("保存结果无法核对");
    state.value = next;
    draft.value = { ...next.settings };
    uncertain.value = false;
    unresolvedSave.value = false;
    notice.value = "支付验证设置已保存。";
  } catch (reason) {
    if (!current(run, c, source)) return;
    if (
      reason instanceof ApiError &&
      reason.responseDecoded &&
      [400, 422].includes(reason.status)
    )
      error.value = "保存未被接受，请核对原字段后重新确认。";
    else {
      state.value = undefined;
      draft.value = undefined;
      uncertain.value = true;
      error.value =
        reason instanceof ApiError && reason.status === 409
          ? "支付验证设置已变化，请刷新页面后核对；不会自动重试。"
          : "本次保存结果无法确认，请刷新页面后核对当前设置；不会自动重试。";
    }
  } finally {
    if (current(run, c, source)) {
      busy.value = false;
      emit("busy", false);
      await nextTick();
      if (current(run, c, source))
        (state.value ? prepareButton.value : errorTarget.value)?.focus();
    }
  }
}
function keydown(event: KeyboardEvent) {
  if (event.key === "Escape") {
    event.preventDefault();
    void cancel();
    return;
  }
  if (event.key !== "Tab" || !dialog.value) return;
  const fields = [
      ...dialog.value.querySelectorAll<HTMLElement>(
        'button:not(:disabled), [tabindex="0"]',
      ),
    ],
    first = fields[0],
    last = fields.at(-1);
  if (
    event.shiftKey &&
    (document.activeElement === first ||
      document.activeElement === dialog.value)
  ) {
    event.preventDefault();
    last?.focus();
  } else if (!event.shiftKey && document.activeElement === last) {
    event.preventDefault();
    first?.focus();
  }
}
function display(
  key: (typeof paymentDefend206Keys)[number],
  settings: PaymentDefend206Settings,
) {
  const options =
    key === "pay_verify"
      ? paymentDefend206Modes
      : key === "pay_verify_type"
        ? paymentDefend206Types
        : undefined;
  return (
    options?.find((option) => option.value === settings[key])?.label ??
    (settings[key] || "留空")
  );
}
const labels: Record<(typeof paymentDefend206Keys)[number], string> = {
  pay_verify: "创建订单人机验证",
  pay_verify_type: "人机验证类型",
  pay_verify_check_second: "检测时间（秒）",
  pay_verify_check_count: "订单数量",
  pay_verify_check_rate: "成功率（%）",
  pay_verify_check_ip: "单个IP连续未支付订单数量",
  pay_verify_check_uid: "开启人机验证的商户",
};
watch(
  () => [
    props.client,
    props.client.area,
    props.client.csrfToken,
    props.adminClass,
    props.ownSession,
  ],
  () => {
    clear();
    void load();
  },
  { immediate: true, flush: "sync" },
);
onBeforeUnmount(() => {
  alive = false;
  epoch++;
  controller?.abort();
  prepared = undefined;
  emit("busy", false);
});
</script>

<template>
  <section class="panel payment-defend206" aria-label="创建订单人机验证">
    <div class="toolbar">
      <h3>创建订单人机验证</h3>
    </div>
    <p v-if="!allowed" class="muted">此设置需要超级管理员本人会话。</p>
    <template v-else>
      <p v-if="loading" role="status">正在读取支付验证设置…</p>
      <p
        ref="errorTarget"
        tabindex="-1"
        v-if="error"
        role="alert"
        class="alert error"
      >
        {{ error }}
      </p>
      <p v-if="notice" role="status" class="alert success">{{ notice }}</p>
      <template v-if="state && draft">
        <p class="muted">
          {{ state.configured ? "已配置" : "尚未配置，默认关闭" }} · 修订
          {{ state.revision }}
        </p>
        <form novalidate @submit.prevent="prepare">
          <div class="defend-field">
            <label for="defend206-mode">创建订单人机验证</label>
            <select
              id="defend206-mode"
              v-model="draft.pay_verify"
              name="pay_verify"
              :disabled="locked"
            >
              <option
                v-for="option in paymentDefend206Modes"
                :key="option.value"
                :value="option.value"
              >
                {{ option.label }}
              </option>
            </select>
            <small class="muted"
              >在商户网站通过submit发起支付的时候，可先进行人机验证再创建订单，防止被刷订单，开启后无法通过mapi接口发起支付</small
            >
          </div>
          <div v-if="draft.pay_verify === '1'" class="defend-rules">
            <fieldset>
              <legend>人机验证开启规则1</legend>
              <div class="defend-grid">
                <div class="defend-field">
                  <label for="defend206-second">检测时间（秒）</label
                  ><input
                    id="defend206-second"
                    v-model="draft.pay_verify_check_second"
                    name="pay_verify_check_second"
                    inputmode="numeric"
                    maxlength="128"
                    :disabled="locked"
                    placeholder="检测商户多少秒内（不填写则不启用该条规则）"
                  />
                </div>
                <div class="defend-field">
                  <label for="defend206-count">订单数量</label
                  ><input
                    id="defend206-count"
                    v-model="draft.pay_verify_check_count"
                    name="pay_verify_check_count"
                    inputmode="numeric"
                    maxlength="128"
                    :disabled="locked"
                    placeholder="超过多少个订单时"
                  />
                </div>
                <div class="defend-field">
                  <label for="defend206-rate">成功率（%）</label
                  ><input
                    id="defend206-rate"
                    v-model="draft.pay_verify_check_rate"
                    name="pay_verify_check_rate"
                    inputmode="decimal"
                    maxlength="128"
                    :disabled="locked"
                    placeholder="成功率低于多少时（不填写则不判断成功率）"
                  />
                </div>
              </div>
            </fieldset>
            <fieldset>
              <legend>人机验证开启规则2</legend>
              <div class="defend-field">
                <label for="defend206-ip">单个IP连续未支付订单数量</label
                ><input
                  id="defend206-ip"
                  v-model="draft.pay_verify_check_ip"
                  name="pay_verify_check_ip"
                  inputmode="numeric"
                  maxlength="128"
                  :disabled="locked"
                  placeholder="1小时内连续未支付订单数量（不填写则不启用该条规则）"
                />
              </div>
            </fieldset>
          </div>
          <div v-if="draft.pay_verify !== '0'" class="defend-field">
            <label for="defend206-uid">开启人机验证的商户</label
            ><input
              id="defend206-uid"
              v-model="draft.pay_verify_check_uid"
              name="pay_verify_check_uid"
              maxlength="8192"
              :disabled="locked"
              placeholder="多个商户号用|隔开，留空为全部商户"
            />
          </div>
          <div class="defend-field">
            <label for="defend206-type">人机验证类型</label
            ><select
              id="defend206-type"
              v-model="draft.pay_verify_type"
              name="pay_verify_type"
              :disabled="locked"
            >
              <option
                v-for="option in paymentDefend206Types"
                :key="option.value"
                :value="option.value"
              >
                {{ option.label }}
              </option>
            </select>
          </div>
          <div class="toolbar">
            <button
              ref="prepareButton"
              type="submit"
              class="primary"
              :disabled="!canSave"
            >
              核对并保存支付验证设置</button
            ><button
              type="button"
              :disabled="locked || !dirty"
              @click="draft = { ...state.settings }"
            >
              撤销编辑
            </button>
          </div>
        </form>
      </template>
    </template>
    <div v-if="confirming && prepared" class="modal-backdrop">
      <section
        ref="dialog"
        class="modal"
        role="dialog"
        aria-modal="true"
        aria-labelledby="defend206-title"
        tabindex="-1"
        @keydown="keydown"
      >
        <h3 id="defend206-title">确认支付验证设置</h3>
        <p>请核对完整配置。保存后影响新的付款请求。</p>
        <dl>
          <template v-for="key in paymentDefend206Keys" :key="key"
            ><dt>{{ labels[key] }}</dt>
            <dd>{{ display(key, prepared.settings) }}</dd></template
          >
        </dl>
        <div class="toolbar">
          <button type="button" :disabled="busy" @click="cancel">取消</button
          ><button
            type="button"
            class="primary"
            :disabled="busy || disabled || !allowed"
            @click="submit"
          >
            确认保存
          </button>
        </div>
      </section>
    </div>
  </section>
</template>

<style scoped>
.payment-defend206 {
  min-width: 0;
}
.payment-defend206 h3 {
  margin: 0;
}
.defend-field {
  display: grid;
  gap: 0.4rem;
  min-width: 0;
  margin-block: 1rem;
}
.defend-field input,
.defend-field select {
  width: 100%;
  min-width: 0;
  box-sizing: border-box;
}
.defend-field small,
.payment-defend206 dd {
  overflow-wrap: anywhere;
}
.defend-rules fieldset {
  min-width: 0;
  margin: 1rem 0;
  border: 1px solid var(--line, var(--line-control));
  border-radius: 0.5rem;
  padding: 0.5rem 0.75rem;
}
.defend-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(min(100%, 15rem), 1fr));
  gap: 0 1rem;
}
.payment-defend206 .toolbar {
  flex-wrap: wrap;
}
.payment-defend206 .modal {
  width: min(38rem, calc(100vw - 2rem));
  max-height: calc(100dvh - 2rem);
  overflow: auto;
}
.payment-defend206 dl {
  display: grid;
  grid-template-columns: minmax(7rem, 1fr) minmax(0, 2fr);
  gap: 0.6rem 1rem;
}
.payment-defend206 dd {
  margin: 0;
}
@media (max-width: 480px) {
  .payment-defend206 dl {
    grid-template-columns: minmax(0, 1fr);
    gap: 0.25rem;
  }
  .payment-defend206 dd {
    margin-bottom: 0.6rem;
  }
}
</style>
