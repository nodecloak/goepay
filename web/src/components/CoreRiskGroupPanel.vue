<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, toRaw, watch } from "vue";
import type { ApiClient } from "../api";
import { readCoreRiskGroup, type CoreRiskGroup } from "../core-risk-group";
const props = defineProps<{
  client: ApiClient;
  groupID: string;
  allowed: boolean;
  disabled: boolean;
}>();
const emit = defineEmits<{ busy: [boolean] }>();
const value = ref<CoreRiskGroup>(),
  draft = ref({ pay_block_oversea: "", check_pay_regoin: "" }),
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
const dirty = computed(
  () =>
    !!value.value &&
    (draft.value.pay_block_oversea !== value.value.config.pay_block_oversea ||
      draft.value.check_pay_regoin !== value.value.config.check_pay_regoin),
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
    const v = readCoreRiskGroup(
      await c.request("/payment-risk/groups/" + props.groupID, {
        signal: a.signal,
      }),
      props.groupID,
    );
    if (!context() || stamp !== epoch) return;
    value.value = v;
    draft.value = { ...v.config };
    uncertain.value = false;
  } catch {
    if (context() && stamp === epoch)
      error.value = "用户组风控读取失败，请刷新重试。";
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
    config = { ...draft.value },
    stamp = ++epoch,
    a = new AbortController();
  abort = a;
  busy.value = true;
  confirm.value = false;
  error.value = "";
  emit("busy", true);
  try {
    const v = readCoreRiskGroup(
      await c.request("/payment-risk/groups/" + props.groupID, {
        method: "PATCH",
        signal: a.signal,
        body: {
          expected_fingerprint: before.fingerprint,
          config,
        },
      }),
      props.groupID,
    );
    if (!context() || stamp !== epoch) return;
    if (
      BigInt(v.revision) !== BigInt(before.revision) + 1n ||
      v.fingerprint === before.fingerprint ||
      v.config.pay_block_oversea !== config.pay_block_oversea ||
      v.config.check_pay_regoin !== config.check_pay_regoin
    )
      throw Error("commit mismatch");
    value.value = v;
    draft.value = { ...v.config };
    notice.value = "用户组风控配置已保存。";
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
  <section class="panel" aria-label="用户组支付风控">
    <h3>其他配置 · 支付风控</h3>
    <p>
      继承系统选项按商户当前有效会员组生效；新订单保留创建时的扫码校验设置。
    </p>
    <p v-if="error" role="alert">{{ error }}</p>
    <p v-if="notice" role="status">{{ notice }}</p>
    <button
      type="button"
      :disabled="busy || loading || disabled || !allowed"
      @click="load"
    >
      刷新用户组风控
    </button>
    <p v-if="loading" role="status">正在读取用户组风控…</p>
    <template v-if="value"
      ><label
        >禁止海外IP支付<select
          v-model="draft.pay_block_oversea"
          :disabled="busy || loading || disabled || uncertain"
        >
          <option value="">继承系统</option>
          <option value="0">否</option>
          <option value="1">是</option>
        </select></label
      ><label
        >扫码IP与下单IP地区一致性校验<select
          v-model="draft.check_pay_regoin"
          :disabled="busy || loading || disabled || uncertain"
        >
          <option value="">继承系统</option>
          <option value="0">否</option>
          <option value="1">不一致时禁止支付</option>
          <option value="2">不一致时提示</option>
        </select></label
      >
      <p>
        当前生效：海外支付
        {{ value.effective_oversea === "1" ? "禁止" : "允许" }}；扫码地区
        {{
          ["不校验", "不一致时禁止", "不一致时提示"][
            Number(value.effective_scan_region)
          ]
        }}。
      </p>
      <p v-if="!value.geo_configured">尚未配置本地地址库，无法开启地区保护。</p>
      <p v-if="confirm" role="alert">
        确认将此用户组的支付风控改为以上选项？<button
          type="button"
          @click="confirm = false"
        >
          取消</button
        ><button type="button" @click="save">确认保存用户组风控</button>
      </p>
      <button
        type="button"
        :disabled="
          busy || loading || disabled || uncertain || !dirty || !allowed
        "
        @click="confirm = true"
      >
        核对并保存用户组风控
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
  min-height: var(--control-h);
}
p {
  overflow-wrap: anywhere;
}
section {
  margin-top: 16px;
}
</style>
