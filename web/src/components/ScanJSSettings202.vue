<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, watch } from "vue";
import { ApiClient } from "../api";

interface Profile202 {
  id: string;
  revision: string;
  channel_id: string;
  app_id: string;
}
interface Setting202 {
  enabled: boolean;
  configured: boolean;
  revision: string;
  fingerprint: string;
  profile_id: string;
  profile_revision: string;
  available: boolean;
  can_update: boolean;
  profiles: Profile202[];
}
const props = defineProps<{
  client: ApiClient;
  adminClass?: string;
  ownSession: boolean;
}>();
const emit = defineEmits<{ busy: [value: boolean] }>();
const state = ref<Setting202 | null>(null),
  enabled = ref(false),
  profileID = ref("");
const loading = ref(false),
  busy = ref(false),
  confirming = ref(false),
  error = ref(""),
  notice = ref("");
const confirmButton = ref<HTMLButtonElement | null>(null),
  saveButton = ref<HTMLButtonElement | null>(null);
const allowed = computed(
  () =>
    props.client.area === "admin" &&
    props.adminClass === "super" &&
    props.ownSession &&
    !!props.client.csrfToken,
);
const selected = computed(() =>
  state.value?.profiles.find((p) => p.id === profileID.value),
);
const changed = computed(
  () =>
    !!state.value &&
    (enabled.value !== state.value.enabled ||
      (enabled.value &&
        !!selected.value &&
        (!state.value.available ||
          selected.value.id !== state.value.profile_id ||
          selected.value.revision !== state.value.profile_revision))),
);
const canSave = computed(
  () =>
    allowed.value &&
    !!state.value?.can_update &&
    !busy.value &&
    !loading.value &&
    changed.value &&
    (!enabled.value || !!selected.value),
);
let alive = true,
  epoch = 0,
  controller: AbortController | undefined;

function parse(value: Setting202): Setting202 {
  if (
    !value ||
    typeof value.enabled !== "boolean" ||
    typeof value.configured !== "boolean" ||
    typeof value.available !== "boolean" ||
    typeof value.can_update !== "boolean" ||
    !/^(?:0|[1-9]\d*)$/.test(value.revision) ||
    !/^[a-f0-9]{64}$/.test(value.fingerprint) ||
    typeof value.profile_id !== "string" ||
    typeof value.profile_revision !== "string" ||
    !Array.isArray(value.profiles) ||
    value.profiles.some(
      (p) =>
        !p ||
        !/^[1-9]\d*$/.test(p.id) ||
        !/^[1-9]\d*$/.test(p.revision) ||
        !/^[1-9]\d*$/.test(p.channel_id) ||
        typeof p.app_id !== "string" ||
        !p.app_id,
    ) ||
    new Set(value.profiles.map((p) => p.id)).size !== value.profiles.length ||
    (value.enabled &&
      (!/^[1-9]\d*$/.test(value.profile_id) ||
        !/^[1-9]\d*$/.test(value.profile_revision))) ||
    (!value.enabled &&
      (value.profile_id !== "" || value.profile_revision !== ""))
  )
    throw new Error("unconfirmed");
  return value;
}
function accept(value: Setting202) {
  state.value = parse(value);
  enabled.value = value.enabled;
  // A rotation leaves the old binding visible but requires a fresh selection.
  profileID.value = value.enabled && value.available ? value.profile_id : "";
}
async function load() {
  if (!allowed.value || loading.value || busy.value) return;
  const run = ++epoch,
    ac = new AbortController();
  controller?.abort();
  controller = ac;
  loading.value = true;
  state.value = null;
  confirming.value = false;
  error.value = "";
  notice.value = "";
  try {
    const value = await props.client.request<Setting202>(
      "/settings/scanpay-js",
      { signal: ac.signal },
    );
    if (alive && run === epoch && !ac.signal.aborted) accept(value);
  } catch {
    if (alive && run === epoch)
      error.value = "原生扫码设置无法确认，请重新读取";
  } finally {
    if (alive && run === epoch) loading.value = false;
  }
}
function prepare() {
  if (!canSave.value) return;
  confirming.value = true;
  void nextTick(() => confirmButton.value?.focus());
}
function cancel() {
  if (busy.value) return;
  confirming.value = false;
  void nextTick(() => saveButton.value?.focus());
}
async function save() {
  if (!canSave.value || !confirming.value || !state.value) return;
  const previous = state.value;
  const body = {
    enabled: enabled.value,
    profile_id: enabled.value ? selected.value!.id : "",
    profile_revision: enabled.value ? selected.value!.revision : "",
    expected_fingerprint: previous.fingerprint,
  };
  const run = ++epoch,
    ac = new AbortController();
  controller?.abort();
  controller = ac;
  busy.value = true;
  confirming.value = false;
  error.value = "";
  notice.value = "";
  try {
    const value = parse(
      await props.client.request<Setting202>("/settings/scanpay-js", {
        method: "PATCH",
        body,
        signal: ac.signal,
      }),
    );
    if (!alive || run !== epoch || ac.signal.aborted) return;
    if (
      value.enabled !== body.enabled ||
      value.profile_id !== body.profile_id ||
      value.profile_revision !== body.profile_revision ||
      value.fingerprint === previous.fingerprint ||
      (body.enabled && !value.available)
    )
      throw new Error("unconfirmed");
    accept(value);
    notice.value = "原生扫码授权已保存";
  } catch {
    if (alive && run === epoch) {
      state.value = null;
      error.value = "保存结果无法确认，请重新读取核对后再操作";
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
    profileID.value = "";
    loading.value = false;
    busy.value = false;
    confirming.value = false;
    void load();
  },
  { immediate: true, flush: "sync" },
);
watch(busy, (value) => emit("busy", value), { flush: "sync" });
onBeforeUnmount(() => {
  alive = false;
  epoch++;
  controller?.abort();
  emit("busy", false);
});
</script>

<template>
  <section class="scan-js-settings202" aria-label="原生扫码授权设置">
    <h3>微信原生扫码授权</h3>
    <p v-if="!allowed" class="muted">此设置需要超级管理员本人会话。</p>
    <template v-else>
      <p v-if="error" role="alert" class="alert error">{{ error }}</p>
      <p v-if="notice" role="status">{{ notice }}</p>
      <button :disabled="loading || busy" @click="load">
        重新读取原生扫码设置
      </button>
      <p v-if="loading" role="status">正在读取原生扫码授权…</p>
      <template v-if="state">
        <label class="scan-js-switch202"
          ><input
            v-model="enabled"
            type="checkbox"
            :disabled="busy || confirming || !state.can_update"
          />开启微信内原生扫码</label
        >
        <p class="muted">
          选择用于微信扫一扫的公众号应用。付款码收款仍需核对金额并确认；支付宝内使用支付宝扫一扫。
        </p>
        <p
          v-if="state.enabled && !state.available"
          role="alert"
          class="alert warning"
        >
          原授权资料已变化或不可用，请重新选择当前应用并确认授权。
        </p>
        <label v-if="enabled"
          >公众号应用
          <select
            v-model="profileID"
            :disabled="busy || confirming || !state.can_update"
          >
            <option value="">请选择当前公众号应用</option>
            <option
              v-for="profile in state.profiles"
              :key="profile.id"
              :value="profile.id"
            >
              {{ profile.app_id }} · 资料 {{ profile.id }} · 版本
              {{ profile.revision }}
            </option>
          </select>
        </label>
        <p v-if="enabled && !state.profiles.length" class="muted">
          当前没有可选的微信公众号资料。
        </p>
        <div
          v-if="confirming"
          role="group"
          aria-label="确认原生扫码授权"
          @keydown.esc.prevent="cancel"
        >
          <p>
            {{
              enabled
                ? `确认授权 ${selected?.app_id} 使用微信原生扫码？`
                : "确认关闭微信原生扫码授权？"
            }}
          </p>
          <button :disabled="busy" @click="cancel">取消修改</button>
          <button
            ref="confirmButton"
            class="primary"
            :disabled="!canSave"
            @click="save"
          >
            确认{{ enabled ? "授权原生扫码" : "关闭原生扫码" }}
          </button>
        </div>
        <button
          v-else
          ref="saveButton"
          class="primary"
          :disabled="!canSave"
          @click="prepare"
        >
          保存原生扫码设置
        </button>
      </template>
    </template>
  </section>
</template>
<style scoped>
.scan-js-settings202 {
  border: 1px solid var(--line);
  border-radius: 6px;
  padding: 16px;
  margin: 16px 0;
}
.scan-js-settings202 h3 {
  font-size: 16px;
  margin: 0 0 16px;
}
.scan-js-settings202 p {
  font-size: 13px;
  line-height: 1.7;
}
.scan-js-settings202 label {
  display: block;
  margin: 16px 0;
}
.scan-js-switch202 {
  display: flex !important;
  align-items: center;
  gap: 10px;
}
.scan-js-switch202 input {
  width: auto;
}
.scan-js-settings202 select {
  display: block;
  margin-top: 8px;
  width: 100%;
  max-width: 560px;
}
.scan-js-settings202 button {
  margin: 8px 10px 0 0;
}
</style>
