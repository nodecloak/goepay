<script setup lang="ts">
import { computed, onBeforeUnmount, reactive, ref, useId, watch } from "vue";
import { ApiClient, ApiError } from "../api";
import { useSettingsDraft212 } from "../settings-drafts212";
import { isPersonalMerchantID } from "../merchant-personal-settings";
import {
  buyerFeeBody104,
  minimumBody104,
  parseBuyerFee104,
  parseMinimum104,
  type BuyerFee104,
  type Minimum104,
} from "../buyer-charge104-settings";
const props = defineProps<{
  client: ApiClient;
  kind: "merchant" | "global" | "parent";
  merchantId?: string;
  channelId?: string;
  allowed: boolean;
}>();
const emit = defineEmits<{ busy: [value: boolean] }>();
const id = useId(),
  state = ref<BuyerFee104 | Minimum104 | null>(null),
  loading = ref(false),
  busy = ref(false),
  unresolvedSave = ref(false),
  error = ref(""),
  notice = ref("");
const draft = reactive({
  mode: 0,
  mode_feerate: "",
  payfee_lessthan: "",
  payfee_mincost: "",
});
const owner = computed(() =>
  props.kind === "merchant"
    ? (props.merchantId ?? "")
    : props.kind === "global"
      ? "0"
      : (props.channelId ?? ""),
);
const allowed = computed(
  () =>
    props.allowed &&
    (props.kind === "merchant"
      ? props.client.area === "merchant" && isPersonalMerchantID(owner.value)
      : props.client.area === "admin" &&
        (props.kind === "global" || isPersonalMerchantID(owner.value))),
);
const path = computed(() =>
  props.kind === "merchant"
    ? "/account/buyer-fees"
    : props.kind === "global"
      ? "/buyer-fees/global"
      : "/buyer-fees/parents/" + owner.value,
);
const title = computed(() =>
  props.kind === "merchant"
    ? "手续费承担设置"
    : props.kind === "global"
      ? "最低手续费"
      : "通道最低手续费",
);
const { confirmDiscard: confirmDraftDiscard } = useSettingsDraft212(
  title.value,
  () =>
    unresolvedSave.value ||
    (!!state.value &&
      ("mode" in state.value
        ? draft.mode !== state.value.mode ||
          draft.mode_feerate !== state.value.mode_feerate
        : draft.payfee_lessthan !== state.value.payfee_lessthan ||
          draft.payfee_mincost !== state.value.payfee_mincost)),
);
let epoch = 0,
  controller: AbortController | undefined,
  confirmedContext: Context | undefined;
function context() {
  return {
    client: props.client,
    csrf: props.client.csrfToken,
    kind: props.kind,
    owner: owner.value,
    allowed: props.allowed,
    path: path.value,
  };
}
type Context = ReturnType<typeof context>;
function same(a: Context) {
  const b = context();
  return (
    a.client === b.client &&
    a.csrf === b.csrf &&
    a.kind === b.kind &&
    a.owner === b.owner &&
    a.allowed === b.allowed &&
    a.path === b.path
  );
}
function draftFrom(value: BuyerFee104 | Minimum104 | null) {
  draft.mode = value && "mode" in value ? value.mode : 0;
  draft.mode_feerate =
    value && "mode_feerate" in value ? value.mode_feerate : "";
  draft.payfee_lessthan =
    value && "payfee_lessthan" in value ? value.payfee_lessthan : "";
  draft.payfee_mincost =
    value && "payfee_mincost" in value ? value.payfee_mincost : "";
}
function clear(message = "", preserveUnresolved = false) {
  confirmedContext = undefined;
  epoch++;
  controller?.abort();
  controller = undefined;
  state.value = null;
  draftFrom(null);
  loading.value = false;
  busy.value = false;
  error.value = message;
  notice.value = "";
  if (!preserveUnresolved) unresolvedSave.value = false;
}
function valid(run: number, c: AbortController, bound: Context) {
  if (run !== epoch || c.signal.aborted) return false;
  if (!same(bound)) {
    clear("会话已变化，请刷新页面后核对");
    return false;
  }
  return allowed.value;
}
function parse(value: unknown, bound: Context) {
  return bound.kind === "merchant"
    ? parseBuyerFee104(value, bound.owner)
    : parseMinimum104(value, bound.kind, bound.owner);
}
async function load() {
  if (loading.value || busy.value) return;
  const discardEpoch = epoch;
  if (
    !(await confirmDraftDiscard()) ||
    discardEpoch !== epoch ||
    loading.value ||
    busy.value ||
    !allowed.value
  )
    return;
  clear("", true);
  if (!allowed.value) return;
  const bound = context(),
    run = epoch,
    c = new AbortController();
  controller = c;
  loading.value = true;
  try {
    const value = await bound.client.request(bound.path, { signal: c.signal });
    if (!valid(run, c, bound)) return;
    state.value = parse(value, bound);
    confirmedContext = bound;
    draftFrom(state.value);
    unresolvedSave.value = false;
  } catch {
    if (valid(run, c, bound))
      error.value = "手续费设置无法确认，请刷新页面后核对";
  } finally {
    if (valid(run, c, bound)) loading.value = false;
  }
}
async function save() {
  if (!allowed.value || busy.value || loading.value || !state.value) return;
  if (!confirmedContext || !same(confirmedContext)) {
    clear("会话已变化，请刷新页面后核对");
    return;
  }
  const before = state.value,
    bound = confirmedContext;
  let body: Record<string, unknown>;
  try {
    body =
      "mode" in before
        ? buyerFeeBody104(before, draft)
        : minimumBody104(before, draft);
  } catch (e) {
    error.value = e instanceof Error ? e.message : "请核对手续费设置";
    return;
  }
  const run = ++epoch,
    c = new AbortController();
  controller?.abort();
  controller = c;
  busy.value = true;
  unresolvedSave.value = true;
  error.value = "";
  notice.value = "";
  try {
    const value = await bound.client.request(bound.path, {
      method: "PATCH",
      body,
      signal: c.signal,
    });
    if (!valid(run, c, bound)) return;
    const after = parse(value, bound);
    if (
      after.fingerprint === before.fingerprint ||
      BigInt(after.revision) !== BigInt(before.revision) + 1n
    )
      throw Error("unconfirmed state");
    for (const key of bound.kind === "merchant"
      ? ["mode", "mode_feerate"]
      : ["payfee_lessthan", "payfee_mincost"])
      if ((after as unknown as Record<string, unknown>)[key] !== body[key])
        throw Error("unconfirmed values");
    state.value = after;
    confirmedContext = bound;
    draftFrom(after);
    unresolvedSave.value = false;
    notice.value = "保存成功，仅对后续新订单生效";
  } catch (e) {
    if (valid(run, c, bound))
      clear(
        e instanceof ApiError && e.status === 409
          ? "手续费设置已变化，请刷新页面后核对"
          : "保存结果无法确认，请刷新页面后核对",
        true,
      );
  } finally {
    if (valid(run, c, bound)) busy.value = false;
  }
}
function cancel() {
  if (busy.value || loading.value || !state.value) return;
  if (!confirmedContext || !same(confirmedContext)) {
    clear("会话已变化，请刷新页面后核对");
    return;
  }
  draftFrom(state.value);
  unresolvedSave.value = false;
  error.value = "";
  notice.value = "";
}
function modeChanged() {
  if (draft.mode !== 1) draft.mode_feerate = "";
}
watch(
  () => [
    props.client,
    props.client.csrfToken,
    props.kind,
    props.merchantId,
    props.channelId,
    props.allowed,
  ],
  () => {
    clear();
    void load();
  },
  { immediate: true, flush: "sync" },
);
watch(busy, (value) => emit("busy", value), { flush: "sync" });
onBeforeUnmount(() => clear());
</script>
<template>
  <section class="panel buyer-fee104" :aria-label="title">
    <div class="toolbar">
      <h2>{{ title }}</h2>
    </div>
    <p v-if="!allowed" class="muted">
      需要本人{{ kind === "merchant" ? "商户" : "超级管理员" }}会话查看和修改。
    </p>
    <template v-else>
      <p v-if="loading" role="status">正在读取手续费设置…</p>
      <p v-if="error" class="error" role="alert">{{ error }}</p>
      <p v-if="notice" class="notice" role="status">{{ notice }}</p>
      <form class="console-config-form" v-if="state" @submit.prevent="save">
        <template v-if="kind === 'merchant'">
          <div class="fee-fields104">
            <label :for="id + '-mode'"
              >手续费承担方<select
                :id="id + '-mode'"
                v-model.number="draft.mode"
                name="mode"
                :disabled="busy"
                @change="modeChanged"
              >
                <option :value="0">商家承担（余额扣费）</option>
                <option :value="1">买家承担（订单加费）</option>
              </select></label
            >
            <label v-if="draft.mode === 1" :for="id + '-rate'"
              >买家承担费率
              <div class="money-field104">
                <input
                  :id="id + '-rate'"
                  v-model="draft.mode_feerate"
                  name="mode_feerate"
                  inputmode="decimal"
                  maxlength="16"
                  placeholder="留空则全部由买家承担"
                  :disabled="busy"
                /><span>%</span>
              </div></label
            >
          </div>
          <p class="muted">
            买家承担时在订单金额上加收手续费。留空则全部由买家承担；填写费率可与商家分担，超过订单总手续费的部分不再加收。
          </p>
        </template>
        <template v-else>
          <p v-if="kind === 'parent'">通道ID：{{ owner }}</p>
          <div class="fee-fields104">
            <label :for="id + '-threshold'"
              >手续费低于
              <div class="money-field104">
                <input
                  :id="id + '-threshold'"
                  v-model="draft.payfee_lessthan"
                  name="payfee_lessthan"
                  inputmode="decimal"
                  maxlength="21"
                  placeholder="手续费低于多少元时"
                  :disabled="busy"
                /><span>元</span>
              </div></label
            ><label :for="id + '-minimum'"
              >按以下金额收取
              <div class="money-field104">
                <input
                  :id="id + '-minimum'"
                  v-model="draft.payfee_mincost"
                  name="payfee_mincost"
                  inputmode="decimal"
                  maxlength="21"
                  placeholder="收取多少元手续费"
                  :disabled="busy"
                /><span>元</span>
              </div></label
            >
          </div>
          <p class="muted">
            手续费低于门槛时按指定金额收取。{{
              kind === "parent"
                ? "通道两项均大于0时整组覆盖全局设置；留空、0或只填一项均使用全局设置。"
                : "两项均大于0时启用最低手续费。"
            }}
          </p>
          <p v-if="'effective_payfee_lessthan' in state" class="muted">
            当前生效：低于 {{ state.effective_payfee_lessthan }} 元时，收取
            {{ state.effective_payfee_mincost }} 元。
          </p>
        </template>
        <div class="toolbar">
          <button type="submit" class="primary" :disabled="busy || loading">
            {{ busy ? "正在保存…" : "保存设置" }}</button
          ><button
            type="button"
            data-action="cancel"
            :disabled="busy || loading"
            @click="cancel"
          >
            取消修改
          </button>
        </div>
      </form>
    </template>
  </section>
</template>
<style scoped>
.buyer-fee104 h2 {
  margin: 0;
}
.fee-fields104 {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 18px;
  margin: 20px 0;
}
.fee-fields104 label {
  display: flex;
  flex-direction: column;
  gap: 8px;
  min-width: 0;
}
.fee-fields104 input,
.fee-fields104 select {
  min-width: 0;
  width: 100%;
  box-sizing: border-box;
}
.money-field104 {
  display: flex;
  align-items: center;
  gap: 8px;
}
.buyer-fee104 .toolbar {
  flex-wrap: wrap;
  gap: 10px;
}
.buyer-fee104 .muted {
  line-height: 1.6;
}
@media (max-width: 600px) {
  .fee-fields104 {
    grid-template-columns: 1fr;
  }
}
</style>
