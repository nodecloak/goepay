<script setup lang="ts">
import { computed, onBeforeUnmount, ref, toRaw, watch } from "vue";
import { ApiClient, ApiError } from "../api";
import { useSettingsDraft212 } from "../settings-drafts212";
import KitSelect from "./KitSelect.vue";
import KitModal from "./KitModal.vue";

const props = defineProps<{
  client: ApiClient;
  adminClass?: string;
  ownSession: boolean;
  disabled?: boolean;
}>();
const emit = defineEmits<{ busy: [value: boolean] }>();
const kinds = ["alipay", "wxpay", "qqpay", "bank"] as const;
type Kind = (typeof kinds)[number];
type State = {
  fingerprint: string;
  revision: string;
  routes: Record<Kind, string>;
};
type Choice = { id: string; name: string; types: Kind[] };
const labels: Record<Kind, string> = {
  alipay: "支付宝转账接口通道",
  wxpay: "微信转账接口通道",
  qqpay: "QQ钱包转账接口通道",
  bank: "银行卡转账接口通道",
};
const state = ref<State | null>(null),
  choices = ref<Choice[]>([]),
  draft = ref<Record<Kind, string>>({
    alipay: "inherit",
    wxpay: "inherit",
    qqpay: "inherit",
    bank: "inherit",
  });
const loading = ref(false),
  saving = ref(false),
  confirming = ref(false),
  uncertain = ref(false),
  error = ref(""),
  notice = ref("");
const allowed = computed(
  () =>
    props.client.area === "admin" &&
    props.adminClass === "super" &&
    props.ownSession,
);
const dirty = computed(
  () =>
    !!state.value &&
    kinds.some((k) => draft.value[k] !== state.value!.routes[k]),
);
const { confirmDiscard } = useSettingsDraft212(
  "转账接口通道",
  () => dirty.value || uncertain.value || confirming.value,
);
let epoch = 0,
  alive = true,
  controller: AbortController | undefined;
let captured = capture();
function capture() {
  return {
    client: toRaw(props.client),
    csrf: toRaw(props.client).csrfToken,
    own: props.ownSession,
    role: props.adminClass,
  };
}
function current() {
  const now = capture();
  return (
    alive &&
    allowed.value &&
    Object.keys(now).every(
      (k) =>
        now[k as keyof typeof now] === captured[k as keyof typeof captured],
    )
  );
}
function valid(run: number, c: AbortController) {
  return run === epoch && !c.signal.aborted && current();
}
function validID(value: unknown): value is string {
  return (
    typeof value === "string" &&
    /^[1-9]\d{0,18}$/.test(value) &&
    BigInt(value) <= 9223372036854775807n
  );
}
function parse(value: unknown) {
  const v = value as { state?: State; channels?: Choice[] };
  if (
    !v?.state ||
    !/^[0-9a-f]{64}$/.test(v.state.fingerprint) ||
    !/^(0|[1-9]\d*)$/.test(v.state.revision) ||
    !v.state.routes ||
    Object.keys(v.state.routes).length !== 4 ||
    !Array.isArray(v.channels)
  )
    throw Error("invalid transfer settings");
  for (const k of kinds) {
    const raw = v.state.routes[k];
    if (
      !["inherit", "disabled", "manual"].includes(raw) &&
      !(
        typeof raw === "string" &&
        raw.startsWith("channel:") &&
        validID(raw.slice(8))
      )
    )
      throw Error("invalid route");
  }
  for (const c of v.channels) {
    if (
      !validID(c.id) ||
      typeof c.name !== "string" ||
      !Array.isArray(c.types) ||
      !c.types.every((t) => kinds.includes(t))
    )
      throw Error("invalid channel");
  }
  return v as { state: State; channels: Choice[] };
}
function accept(raw: unknown) {
  const v = parse(raw);
  state.value = v.state;
  choices.value = v.channels;
  draft.value = { ...v.state.routes };
  uncertain.value = false;
}
function options(kind: Kind) {
  const out = [
    { value: "inherit", label: "沿用兼容开关及商户授权通道" },
    { value: "disabled", label: "关闭" },
    { value: "manual", label: "手动转账" },
    ...choices.value
      .filter((c) => c.types.includes(kind))
      .map((c) => ({
        value: `channel:${c.id}`,
        label: `${c.name}（${c.id}）`,
      })),
  ];
  if (!out.some((o) => o.value === draft.value[kind]))
    out.push({ value: draft.value[kind], label: "原通道已不可用，请重新选择" });
  return out;
}
async function load(initial = false) {
  if (loading.value || saving.value || !allowed.value) return;
  if (!initial && !(await confirmDiscard())) return;
  controller?.abort();
  const c = new AbortController(),
    run = ++epoch;
  controller = c;
  captured = capture();
  loading.value = true;
  error.value = "";
  notice.value = "";
  try {
    const v = await captured.client.request("/settings/transfer-routes", {
      signal: c.signal,
    });
    if (valid(run, c)) accept(v);
  } catch {
    if (valid(run, c)) error.value = "转账通道配置无法读取，请刷新页面后核对";
  } finally {
    if (valid(run, c)) loading.value = false;
  }
}
async function save() {
  if (
    !current() ||
    !state.value ||
    !confirming.value ||
    saving.value ||
    loading.value ||
    props.disabled ||
    uncertain.value
  )
    return;
  const body = {
    expected_fingerprint: state.value.fingerprint,
    routes: { ...draft.value },
  };
  const c = new AbortController(),
    run = ++epoch;
  controller = c;
  saving.value = true;
  error.value = "";
  notice.value = "";
  try {
    const v = await captured.client.request("/settings/transfer-routes", {
      method: "PATCH",
      body,
      signal: c.signal,
    });
    if (valid(run, c)) {
      accept(v);
      notice.value = "转账通道配置已保存；未发起任何转账";
      confirming.value = false;
    }
  } catch (e) {
    if (valid(run, c)) {
      confirming.value = false;
      uncertain.value = !(
        e instanceof ApiError &&
        e.responseDecoded &&
        e.status >= 400 &&
        e.status < 500
      );
      error.value = uncertain.value
        ? "保存结果尚未核实，请只刷新页面后核对配置，不要重复提交"
        : "配置已变化或通道不可用，请刷新页面后核对";
    }
  } finally {
    if (valid(run, c)) saving.value = false;
  }
}
watch(
  () => [
    toRaw(props.client),
    toRaw(props.client).csrfToken,
    props.adminClass,
    props.ownSession,
  ],
  () => {
    epoch++;
    controller?.abort();
    state.value = null;
    choices.value = [];
    confirming.value = false;
    uncertain.value = false;
    loading.value = false;
    saving.value = false;
    error.value = "";
    notice.value = "";
    captured = capture();
    if (allowed.value) void load(true);
  },
  { immediate: true },
);
watch(
  () => loading.value || saving.value,
  (v) => emit("busy", v),
  { immediate: true },
);
onBeforeUnmount(() => {
  alive = false;
  epoch++;
  controller?.abort();
  emit("busy", false);
});
</script>
<template>
  <section class="settings-embed" aria-label="转账接口通道配置">
    <h3 class="settings-group">转账接口通道配置</h3>
    <p class="muted">
      默认通道用于未指定通道的新转账，仍校验商户的通道授权。关闭或手动转账会阻止新自动转账；手动处理请使用结算管理。已有转账和已准备批次保留原通道，不会重新发送。
    </p>
    <p v-if="!allowed" role="status">仅超级管理员本人会话可管理转账通道。</p>
    <p v-if="error" class="error" role="alert">{{ error }}</p>
    <p v-if="notice" role="status">{{ notice }}</p>
    <template v-if="state">
      <div v-for="kind in kinds" :key="kind" class="setting-field">
        <label :id="`route228-${kind}`">{{ labels[kind] }}</label>
        <KitSelect
          v-model="draft[kind]"
          :aria-label="labels[kind]"
          :options="options(kind)"
          :disabled="!allowed || loading || saving || disabled || uncertain"
        />
      </div>
      <p class="muted">
        版本
        {{
          state.revision
        }}。沿用兼容配置时，下面的旧开关仍生效；明确选择通道后使用新配置。支付宝余额阈值自动转账的触发条件仍由结算设置控制，同样遵守关闭或手动转账限制。
      </p>
      <button
        type="button"
        class="pa-btn pa-btn--primary"
        :disabled="
          !allowed || !dirty || loading || saving || disabled || uncertain
        "
        @click="confirming = true"
      >
        保存转账通道
      </button>
    </template>

    <KitModal
      v-if="confirming"
      title="确认转账通道配置"
      :loading="saving"
      :submit-disabled="disabled"
      submit-text="确认保存转账通道"
      @submit="save"
      @close="confirming = false"
    >
      <p>
        此设置只影响新的自动转账。选择通道不会授予商户权限，保存不会向上游发起转账。
      </p>
    </KitModal>
  </section>
</template>
