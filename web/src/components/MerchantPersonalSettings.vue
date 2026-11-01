<script setup lang="ts">
import { computed, onBeforeUnmount, reactive, ref, watch } from "vue";
import { ApiClient, ApiError } from "../api";
import {
  isPersonalMerchantID,
  parsePersonalSettings,
  personalSettingsBody,
  type PersonalSettings,
} from "../merchant-personal-settings";
const props = withDefaults(
  defineProps<{
    client: ApiClient;
    merchantId: string;
    ownSession: boolean;
    section?: "all" | "contact" | "settlement";
  }>(),
  { section: "all" },
);
const title = computed(() =>
  props.section === "settlement" ? "预留余额设置" : "个人功能设置",
);
const state = ref<PersonalSettings | null>(null),
  draft = reactive({
    qq: "",
    pay_minmoney: "",
    pay_maxmoney: "",
    remain_money: "",
  }),
  loading = ref(false),
  busy = ref(false),
  error = ref(""),
  notice = ref("");
const allowed = computed(
  () =>
    props.ownSession &&
    props.client.area === "merchant" &&
    isPersonalMerchantID(props.merchantId),
);
let epoch = 0,
  controller: AbortController | undefined,
  trackedClient = props.client,
  trackedCSRF = props.client.csrfToken,
  trackedID = props.merchantId,
  trackedOwn = props.ownSession,
  trackedSection = props.section;
function setDraft(value?: {
  qq: string;
  pay_minmoney: string;
  pay_maxmoney: string;
  remain_money: string;
}) {
  draft.qq = value?.qq ?? "";
  draft.pay_minmoney = value?.pay_minmoney ?? "";
  draft.pay_maxmoney = value?.pay_maxmoney ?? "";
  draft.remain_money = value?.remain_money ?? "";
}
function sameContext() {
  return (
    trackedClient === props.client &&
    trackedCSRF === props.client.csrfToken &&
    trackedID === props.merchantId &&
    trackedOwn === props.ownSession &&
    trackedSection === props.section
  );
}
function clear(message = "") {
  epoch++;
  controller?.abort();
  controller = undefined;
  state.value = null;
  setDraft();
  loading.value = false;
  busy.value = false;
  error.value = message;
  notice.value = "";
  trackedClient = props.client;
  trackedCSRF = props.client.csrfToken;
  trackedID = props.merchantId;
  trackedOwn = props.ownSession;
  trackedSection = props.section;
}
function ensureContext() {
  if (!sameContext()) {
    clear("会话已变化，请重新读取个人设置");
    return false;
  }
  return allowed.value;
}
function valid(run: number, c: AbortController) {
  if (run !== epoch || c.signal.aborted) return false;
  if (!sameContext()) {
    clear("会话已变化，请重新读取个人设置");
    return false;
  }
  return allowed.value;
}
async function load() {
  if (busy.value || loading.value) return;
  clear();
  if (!allowed.value) return;
  const client = props.client,
    mid = props.merchantId,
    run = epoch,
    c = new AbortController();
  controller = c;
  loading.value = true;
  try {
    const value = await client.request("/account/personal-settings", {
      signal: c.signal,
    });
    if (!valid(run, c)) return;
    state.value = parsePersonalSettings(value, mid);
    setDraft(state.value);
  } catch {
    if (valid(run, c)) error.value = "个人设置无法确认，请重新读取";
  } finally {
    if (valid(run, c)) loading.value = false;
  }
}
function cancel() {
  if (!ensureContext() || busy.value || !state.value) return;
  setDraft(state.value);
  error.value = "";
  notice.value = "";
}
async function save() {
  if (!ensureContext() || busy.value || loading.value || !state.value) return;
  const previous = state.value;
  let body: ReturnType<typeof personalSettingsBody>;
  try {
    body = personalSettingsBody(
      previous,
      props.section === "contact"
        ? { ...draft, remain_money: previous.remain_money }
        : props.section === "settlement"
          ? {
              qq: previous.qq,
              pay_minmoney: previous.pay_minmoney,
              pay_maxmoney: previous.pay_maxmoney,
              remain_money: draft.remain_money,
            }
          : draft,
    );
  } catch (e) {
    error.value = e instanceof Error ? e.message : "请核对个人设置";
    return;
  }
  const client = props.client,
    mid = props.merchantId,
    run = ++epoch,
    c = new AbortController();
  controller?.abort();
  controller = c;
  busy.value = true;
  error.value = "";
  notice.value = "";
  try {
    const value = await client.request("/account/personal-settings", {
      method: "PATCH",
      body,
      signal: c.signal,
    });
    if (!valid(run, c)) return;
    const next = parsePersonalSettings(value, mid);
    if (
      next.fingerprint === previous.fingerprint ||
      !next.bounds_valid ||
      next.qq !== body.qq ||
      next.pay_minmoney !== body.pay_minmoney ||
      next.pay_maxmoney !== body.pay_maxmoney ||
      next.remain_money !== body.remain_money
    )
      throw new Error("unconfirmed result");
    state.value = next;
    setDraft(next);
    notice.value = "保存成功";
  } catch (e) {
    if (valid(run, c)) {
      state.value = null;
      setDraft();
      error.value =
        e instanceof ApiError && e.status === 409
          ? "设置已变更，请重新读取并核对后保存"
          : "保存结果无法确认，请重新读取后核对";
    }
  } finally {
    if (valid(run, c)) busy.value = false;
  }
}
watch(
  () => [
    props.client,
    props.client.csrfToken,
    props.merchantId,
    props.ownSession,
    props.section,
  ],
  () => {
    clear();
    void load();
  },
  { immediate: true, flush: "sync" },
);
onBeforeUnmount(() => clear());
</script>
<template>
  <section class="panel personal-settings" :aria-label="title">
    <div class="personal-heading">
      <h2>{{ title }}</h2>
      <button
        v-if="allowed"
        type="button"
        data-action="reload"
        :disabled="loading || busy"
        @click="load"
      >
        重新读取
      </button>
    </div>
    <p v-if="!allowed" class="muted">个人设置需要商户本人登录后查看和修改。</p>
    <template v-else>
      <p v-if="loading" role="status">正在读取个人设置…</p>
      <p v-if="error" role="alert" class="error">{{ error }}</p>
      <p v-if="notice" role="status" class="notice">{{ notice }}</p>
      <form v-if="state" @submit.prevent="save">
        <div class="personal-fields">
          <label v-if="section !== 'settlement'"
            >联系QQ<input
              v-model="draft.qq"
              name="qq"
              inputmode="numeric"
              maxlength="20"
              autocomplete="off"
              :disabled="busy"
          /></label>
          <label v-if="section !== 'settlement'"
            >最大支付金额限制
            <div class="money-field">
              <input
                v-model="draft.pay_maxmoney"
                name="pay_maxmoney"
                inputmode="decimal"
                maxlength="21"
                placeholder="留空则不限制"
                :disabled="busy"
              /><span>元</span>
            </div></label
          >
          <label v-if="section !== 'settlement'"
            >最小支付金额限制
            <div class="money-field">
              <input
                v-model="draft.pay_minmoney"
                name="pay_minmoney"
                inputmode="decimal"
                maxlength="21"
                placeholder="留空则不限制"
                :disabled="busy"
              /><span>元</span>
            </div></label
          >
          <label v-if="section !== 'contact'"
            >{{ section === "settlement" ? "预留余额" : "结算留存" }}
            <input
              v-model="draft.remain_money"
              name="remain_money"
              inputmode="text"
              maxlength="20"
              placeholder="如 10% 或 50.00，留空不保留"
              autocomplete="off"
              :disabled="busy"
            />
          </label>
        </div>
        <p v-if="section !== 'settlement'" class="muted">
          留空或0不额外限制；平台、用户组和通道限制仍然生效。限制用于新订单，历史订单金额保持原记录。
        </p>
        <p v-if="section !== 'contact'" class="muted">
          {{
            section === "settlement" ? "预留余额" : "结算留存"
          }}：每次自动结算前从可结算余额中保留的部分，可填百分比(如
          10%)或固定金额(如 50.00)。
        </p>
        <p v-if="!state.bounds_valid" role="alert" class="error">
          当前个人上下限不一致，请修正后保存。
          <a v-if="section === 'settlement'" href="/merchant/account/settings"
            >前往账户设置修正支付金额限制</a
          >
        </p>
        <div class="personal-actions">
          <button type="submit" class="primary" :disabled="busy || loading">
            {{ busy ? "正在保存…" : "保存设置" }}</button
          ><button
            type="button"
            data-action="cancel"
            :disabled="busy"
            @click="cancel"
          >
            取消修改
          </button>
        </div>
      </form>
      <button v-else type="submit" disabled class="personal-unavailable">
        保存设置
      </button>
    </template>
  </section>
</template>
<style scoped>
.personal-heading {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}
.personal-heading h2 {
  margin: 0;
}
.personal-fields {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 18px;
  margin: 20px 0;
}
.personal-fields label {
  display: flex;
  flex-direction: column;
  gap: 8px;
  min-width: 0;
}
.money-field {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
}
.money-field input,
.personal-fields input {
  min-width: 0;
  width: 100%;
  box-sizing: border-box;
}
.personal-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  margin-top: 18px;
}
.personal-unavailable {
  margin-top: 12px;
}
@media (max-width: 600px) {
  .personal-fields {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
