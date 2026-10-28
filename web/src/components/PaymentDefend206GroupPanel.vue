<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, toRaw, watch } from "vue";
import type { ApiClient } from "../api";
import {
  groupPayVerifyOptions,
  payVerifyLabels,
  readPaymentDefendGroup,
  type PaymentDefendGroup,
} from "../payment-defend206-group";
const props = defineProps<{
  client: ApiClient;
  groupID: string;
  allowed: boolean;
  disabled: boolean;
}>();
const emit = defineEmits<{ busy: [boolean] }>();
const value = ref<PaymentDefendGroup>(),
  draft = ref(""),
  loading = ref(false),
  busy = ref(false),
  uncertain = ref(false),
  error = ref(""),
  notice = ref(""),
  confirm = ref(false);
let alive = true,
  epoch = 0,
  c = toRaw(props.client),
  csrf = c.csrfToken,
  abort: AbortController | undefined;
const path = () => "/settings/payment-defend/groups/" + props.groupID;
const dirty = computed(
  () => !!value.value && draft.value !== value.value.pay_verify,
);
function context() {
  return (
    alive && c === toRaw(props.client) && csrf === c.csrfToken && props.allowed
  );
}
function reset() {
  ++epoch;
  abort?.abort();
  value.value = undefined;
  confirm.value = false;
  busy.value = false;
  loading.value = false;
  uncertain.value = false;
  emit("busy", false);
  c = toRaw(props.client);
  csrf = c.csrfToken;
}
async function load() {
  if (busy.value || props.disabled || !props.allowed) return;
  if (!context()) reset();
  const stamp = ++epoch,
    a = new AbortController();
  abort?.abort();
  abort = a;
  loading.value = true;
  error.value = "";
  confirm.value = false;
  try {
    const v = readPaymentDefendGroup(
      await c.request(path(), { signal: a.signal }),
      props.groupID,
    );
    if (!context() || stamp !== epoch) return;
    value.value = v;
    draft.value = v.pay_verify;
    uncertain.value = false;
  } catch {
    if (context() && stamp === epoch)
      error.value = "用户组人机验证读取失败，请刷新重试。";
  } finally {
    if (alive && stamp === epoch) loading.value = false;
  }
}
async function save() {
  if (
    !context() ||
    busy.value ||
    loading.value ||
    props.disabled ||
    uncertain.value ||
    !dirty.value ||
    !value.value ||
    !confirm.value
  )
    return;
  const before = value.value,
    mode = draft.value,
    stamp = ++epoch,
    a = new AbortController();
  abort = a;
  busy.value = true;
  confirm.value = false;
  error.value = "";
  emit("busy", true);
  try {
    const v = readPaymentDefendGroup(
      await c.request(path(), {
        method: "PATCH",
        signal: a.signal,
        body: { expected_fingerprint: before.fingerprint, pay_verify: mode },
      }),
      props.groupID,
    );
    if (!context() || stamp !== epoch) return;
    if (
      BigInt(v.revision) !== BigInt(before.revision) + 1n ||
      v.fingerprint === before.fingerprint ||
      v.pay_verify !== mode
    )
      throw Error("commit mismatch");
    value.value = v;
    draft.value = v.pay_verify;
    notice.value = "用户组人机验证配置已保存。";
  } catch {
    if (context() && stamp === epoch) {
      uncertain.value = true;
      error.value = "保存结果尚未核实，请刷新读取后重新确认；不会自动重试。";
    }
  } finally {
    if (alive && stamp === epoch) {
      busy.value = false;
      emit("busy", false);
    }
  }
}
watch(
  () => [props.client, props.client.csrfToken, props.groupID, props.allowed],
  () => {
    reset();
    void load();
  },
  { flush: "sync" },
);
onMounted(load);
onBeforeUnmount(() => {
  alive = false;
  ++epoch;
  abort?.abort();
  emit("busy", false);
});
</script>
<template>
  <section class="panel" aria-label="用户组创建订单人机验证">
    <h3>其他配置 · 创建订单人机验证</h3>
    <p v-if="error" role="alert">{{ error }}</p>
    <p v-if="notice" role="status">{{ notice }}</p>
    <button
      type="button"
      :disabled="busy || loading || disabled || !allowed"
      @click="load"
    >
      刷新人机验证配置
    </button>
    <p v-if="loading" role="status">正在读取人机验证配置…</p>
    <template v-if="value"
      ><label
        >创建订单人机验证<select
          v-model="draft"
          :disabled="busy || loading || disabled || uncertain"
        >
          <option
            v-for="[v, label] in groupPayVerifyOptions"
            :key="v"
            :value="v"
          >
            {{ label }}
          </option>
        </select></label
      >
      <p>当前生效：{{ payVerifyLabels[value.effective_pay_verify] }}</p>
      <p v-if="confirm" role="alert">
        确认将此用户组的创建订单人机验证改为以上选项？<button
          type="button"
          @click="confirm = false"
        >
          取消</button
        ><button type="button" @click="save">确认保存人机验证配置</button>
      </p>
      <button
        type="button"
        :disabled="
          busy || loading || disabled || uncertain || !dirty || !allowed
        "
        @click="confirm = true"
      >
        核对并保存人机验证配置
      </button></template
    >
  </section>
</template>
<style scoped>
label {
  display: grid;
  gap: 6px;
  margin: 12px 0;
  max-width: 480px;
}
select {
  width: 100%;
  min-height: 36px;
}
p {
  overflow-wrap: anywhere;
}
section {
  margin-top: 16px;
}
</style>
