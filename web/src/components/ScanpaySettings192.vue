<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, watch } from "vue";
import { ApiClient } from "../api";
import { useSettingsDraft212 } from "../settings-drafts212";
interface Setting {
  scanpay_open: "0" | "1";
  configured: boolean;
  revision: string;
  fingerprint: string;
  can_update: boolean;
}
const props = defineProps<{
  client: ApiClient;
  adminClass?: string;
  ownSession: boolean;
}>();
const emit = defineEmits<{ busy: [value: boolean] }>();
const state = ref<Setting | null>(null),
  enabled = ref(false),
  loading = ref(false),
  busy = ref(false),
  confirming = ref(false),
  unresolvedSave = ref(false),
  error = ref(""),
  notice = ref("");
const confirm = ref<HTMLButtonElement | null>(null),
  saveButton = ref<HTMLButtonElement | null>(null);
const allowed = computed(
  () =>
    props.client.area === "admin" &&
    props.adminClass === "super" &&
    props.ownSession,
);
const { confirmDiscard: confirmDraftDiscard } = useSettingsDraft212(
  "付款码支付设置",
  () =>
    confirming.value ||
    unresolvedSave.value ||
    (!!state.value && enabled.value !== (state.value.scanpay_open === "1")),
);
let epoch = 0,
  alive = true,
  controller: AbortController | undefined;
function parse(v: Setting): Setting {
  if (
    !v ||
    !["0", "1"].includes(v.scanpay_open) ||
    typeof v.configured !== "boolean" ||
    typeof v.can_update !== "boolean" ||
    !/^\d+$/.test(v.revision) ||
    !/^[a-f0-9]{64}$/.test(v.fingerprint)
  )
    throw new Error("unconfirmed");
  return {
    scanpay_open: v.scanpay_open,
    configured: v.configured,
    revision: v.revision,
    fingerprint: v.fingerprint,
    can_update: v.can_update,
  };
}
function accept(v: Setting) {
  state.value = parse(v);
  enabled.value = v.scanpay_open === "1";
  unresolvedSave.value = false;
}
async function load() {
  if (busy.value || loading.value || !allowed.value) return;
  const discardEpoch = epoch;
  if (
    !(await confirmDraftDiscard()) ||
    discardEpoch !== epoch ||
    busy.value ||
    loading.value ||
    !allowed.value
  )
    return;
  const run = ++epoch,
    ac = new AbortController();
  controller?.abort();
  controller = ac;
  loading.value = true;
  state.value = null;
  error.value = "";
  notice.value = "";
  confirming.value = false;
  try {
    const v = await props.client.request<Setting>("/settings/scanpay", {
      signal: ac.signal,
    });
    if (alive && run === epoch && !ac.signal.aborted) accept(v);
  } catch {
    if (alive && run === epoch)
      error.value = "付款码支付设置无法确认，请刷新页面后核对";
  } finally {
    if (alive && run === epoch) loading.value = false;
  }
}
function prepare() {
  if (!allowed.value || !state.value?.can_update || busy.value) return;
  confirming.value = true;
  void nextTick(() => confirm.value?.focus());
}
function cancel() {
  if (busy.value) return;
  confirming.value = false;
  void nextTick(() => saveButton.value?.focus());
}
async function save() {
  if (
    !allowed.value ||
    !state.value?.can_update ||
    !confirming.value ||
    busy.value
  )
    return;
  const previous = state.value,
    body = {
      scanpay_open: enabled.value ? "1" : "0",
      expected_fingerprint: previous.fingerprint,
    },
    run = ++epoch,
    ac = new AbortController();
  controller?.abort();
  controller = ac;
  confirming.value = false;
  busy.value = true;
  unresolvedSave.value = true;
  error.value = "";
  notice.value = "";
  try {
    const v = parse(
      await props.client.request<Setting>("/settings/scanpay", {
        method: "PATCH",
        body,
        signal: ac.signal,
      }),
    );
    if (!alive || run !== epoch || ac.signal.aborted) return;
    if (
      v.scanpay_open !== body.scanpay_open ||
      v.fingerprint === previous.fingerprint
    )
      throw new Error("unconfirmed");
    accept(v);
    notice.value = "付款码支付设置已保存";
  } catch {
    if (alive && run === epoch) {
      state.value = null;
      error.value = "保存结果无法确认，请刷新页面后核对";
    }
  } finally {
    if (alive && run === epoch) busy.value = false;
  }
}
watch(
  () => [
    props.client,
    props.client.csrfToken,
    props.adminClass,
    props.ownSession,
  ],
  () => {
    epoch++;
    controller?.abort();
    state.value = null;
    enabled.value = false;
    busy.value = false;
    loading.value = false;
    confirming.value = false;
    unresolvedSave.value = false;
    void load();
  },
  { immediate: true, flush: "sync" },
);
watch(busy, (v) => emit("busy", v), { flush: "sync" });
onBeforeUnmount(() => {
  alive = false;
  epoch++;
  controller?.abort();
  emit("busy", false);
});
</script>
<template>
  <section class="scanpay-settings192" aria-label="付款码支付设置">
    <h3>开启付款码支付</h3>
    <p v-if="!allowed" class="muted">此设置需要超级管理员本人会话。</p>
    <template v-else>
      <p v-if="error" role="alert" class="alert error">{{ error }}</p>
      <p v-if="notice" role="status">{{ notice }}</p>

      <p v-if="loading" role="status">正在读取付款码支付设置…</p>
      <template v-if="state">
        <label class="scanpay-switch192"
          ><input
            v-model="enabled"
            type="checkbox"
            :disabled="busy || confirming || !state.can_update"
          /><span>开启付款码支付</span></label
        >
        <p class="muted">
          关闭后禁止新的付款码收款和旧准备请求的首次扣款，已提交订单仍可查询与恢复。重新开启后需重新准备收款。
        </p>
        <div
          v-if="confirming"
          role="group"
          aria-label="确认付款码支付设置"
          @keydown.esc.prevent="cancel"
        >
          <p>确认{{ enabled ? "开启" : "关闭" }}付款码支付？</p>
          <button :disabled="busy" @click="cancel">取消修改</button
          ><button ref="confirm" class="primary" :disabled="busy" @click="save">
            确认{{ enabled ? "开启" : "关闭" }}付款码支付
          </button>
        </div>
        <button
          v-else
          ref="saveButton"
          class="primary"
          :disabled="
            busy ||
            loading ||
            !state.can_update ||
            enabled === (state.scanpay_open === '1')
          "
          @click="prepare"
        >
          保存付款码设置
        </button>
      </template>
    </template>
  </section>
</template>
<style scoped>
.scanpay-settings192 {
  border: 1px solid var(--line);
  border-radius: 6px;
  padding: 16px;
  margin: 16px 0;
}
.scanpay-settings192 h3 {
  font-size: 16px;
  margin: 0 0 16px;
}
.scanpay-settings192 p {
  font-size: 13px;
  line-height: 1.7;
}
.scanpay-switch192 {
  display: flex;
  align-items: center;
  gap: 10px;
  margin: 16px 0;
}
.scanpay-switch192 input {
  width: auto;
}
.scanpay-settings192 button {
  margin-right: 10px;
  margin-bottom: 6px;
}
</style>
