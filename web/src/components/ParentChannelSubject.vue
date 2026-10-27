<script setup lang="ts">
import { ref, watch, onMounted, onBeforeUnmount } from "vue";
import { ApiError, type ApiClient } from "../api";
const props = defineProps<{
  client: ApiClient;
  allowed: boolean;
  channelID: string;
}>();
const emit = defineEmits<{ locked: [boolean]; contextInvalid: [] }>();
type State = {
  channel_id: string;
  ordername: string;
  ordername_rules: string;
  fingerprint: string;
};
const target = ref<State>(),
  name = ref(""),
  rules = ref(""),
  busy = ref(false),
  unknown = ref(false),
  confirm = ref(false),
  error = ref(""),
  notice = ref(""),
  invalid = ref(false);
let active = true,
  epoch = 0,
  c = props.client,
  csrf = c.csrfToken,
  id = props.channelID;
function read(v: unknown): State {
  const a = v as State;
  if (
    !a ||
    Object.keys(a).sort().join(",") !==
      "channel_id,fingerprint,ordername,ordername_rules" ||
    a.channel_id !== id ||
    typeof a.ordername !== "string" ||
    typeof a.ordername_rules !== "string" ||
    !/^[a-f0-9]{64}$/.test(a.fingerprint)
  )
    throw new Error("原父通道标题状态无法核实");
  return a;
}
function reset() {
  epoch++;
  target.value = undefined;
  name.value = "";
  rules.value = "";
  busy.value = false;
  unknown.value = false;
  confirm.value = false;
  error.value = "";
  notice.value = "";
  emit("locked", false);
}
function current(n: number) {
  if (!active || invalid.value) return false;
  if (
    props.client !== c ||
    props.client.csrfToken !== csrf ||
    props.channelID !== id ||
    !props.allowed ||
    props.client.area !== "admin"
  ) {
    reset();
    invalid.value = true;
    emit("contextInvalid");
    return false;
  }
  return n === epoch;
}
async function load() {
  if (!current(epoch) || busy.value) return;
  const n = epoch;
  busy.value = true;
  emit("locked", true);
  error.value = "";
  try {
    const v = read(await c.request("/payment-channels/" + id + "/subject"));
    if (!current(n)) return;
    target.value = v;
    name.value = v.ordername;
    rules.value = v.ordername_rules;
    unknown.value = false;
    confirm.value = false;
    notice.value = "已读取原通道当前标题设置";
  } catch (e) {
    if (current(n)) {
      error.value = e instanceof Error ? e.message : "读取失败";
      if (e instanceof ApiError && (e.status === 401 || e.status === 403)) {
        reset();
        invalid.value = true;
        emit("contextInvalid");
      }
    }
  } finally {
    if (current(n)) {
      busy.value = false;
      emit("locked", unknown.value);
    }
  }
}
function prepare() {
  if (!current(epoch) || !target.value || busy.value || unknown.value) return;
  confirm.value = true;
}
async function save() {
  if (
    !current(epoch) ||
    !target.value ||
    !confirm.value ||
    busy.value ||
    unknown.value
  )
    return;
  const n = epoch,
    before = target.value;
  busy.value = true;
  emit("locked", true);
  error.value = "";
  notice.value = "";
  const submittedName = name.value,
    submittedRules = rules.value;
  try {
    const v = read(
      await c.request("/payment-channels/" + id + "/subject", {
        method: "PATCH",
        body: {
          ordername: submittedName,
          ordername_rules: submittedRules,
          expected_fingerprint: before.fingerprint,
        },
      }),
    );
    if (!current(n)) return;
    if (
      v.ordername !== submittedName ||
      v.ordername_rules !== submittedRules ||
      v.fingerprint === before.fingerprint
    )
      throw new Error("保存结果与原字段不符");
    target.value = v;
    confirm.value = false;
    notice.value = "订单名称设置已保存";
  } catch (e) {
    if (current(n)) {
      unknown.value = !(
        e instanceof ApiError &&
        e.responseDecoded &&
        e.status >= 400 &&
        e.status < 500
      );
      error.value = unknown.value
        ? "结果未知：请读取原通道状态核对，勿重复提交。"
        : e instanceof Error
          ? e.message
          : "保存失败";
      if (e instanceof ApiError && (e.status === 401 || e.status === 403)) {
        reset();
        invalid.value = true;
        emit("contextInvalid");
      }
    }
  } finally {
    if (current(n)) {
      busy.value = false;
      emit("locked", unknown.value);
    }
  }
}
function focus() {
  current(epoch);
}
watch(
  () => [props.client, props.client.area, props.allowed, props.channelID],
  () => {
    reset();
    c = props.client;
    csrf = c.csrfToken;
    id = props.channelID;
    invalid.value = false;
    void load();
  },
  { immediate: true, flush: "sync" },
);
onMounted(() => window.addEventListener("focus", focus));
onBeforeUnmount(() => {
  active = false;
  reset();
  window.removeEventListener("focus", focus);
});
</script>
<template>
  <section
    v-if="!invalid"
    class="parent-subject"
    aria-label="父通道订单名称设置"
  >
    <h3>自定义订单名称</h3>
    <p class="muted">
      留空继承默认设置。支持
      [name]、[order]、[outorder]、[time]、[qq]、[phone]；多个名称用 |
      分隔。首次发送固定，原订单名称保持原值。
    </p>
    <p v-if="error" role="alert" class="alert error">{{ error }}</p>
    <p v-if="notice" role="status" class="muted">{{ notice }}</p>
    <form v-if="target" @submit.prevent="prepare">
      <fieldset :disabled="busy || unknown || confirm">
        <label
          >自定义订单名称<input
            v-model="name"
            name="ordername"
            maxlength="1024"
            autocomplete="off" /></label
        ><label
          >按金额设置订单名称<textarea
            v-model="rules"
            name="ordername_rules"
            rows="4"
            maxlength="20000"
            placeholder='[{"name":"商品[name]","min":"1.00","max":"100.00"}]'
          />
        </label>
        <p class="muted">
          金额区间含首尾且不可重叠；[] 表示清空金额规则，留空继承。
        </p>
        <button type="submit">保存订单名称</button>
      </fieldset>
    </form>
    <div
      v-if="confirm && !unknown"
      class="alert"
      role="group"
      aria-label="确认订单名称设置"
    >
      <p>确认更新通道 #{{ id }} 的订单名称？新设置仅用于之后首次发送的订单。</p>
      <button :disabled="busy" @click="confirm = false">返回修改</button
      ><button data-subject-confirm :disabled="busy" @click="save">
        确定保存
      </button>
    </div>
    <button data-subject-read :disabled="busy" @click="load">
      {{ busy ? "正在核对…" : "读取原通道标题状态" }}
    </button>
  </section>
</template>
<style scoped>
.parent-subject {
  border-top: 1px solid var(--border);
  margin-top: 20px;
  padding-top: 12px;
}
.parent-subject label {
  display: block;
  margin: 10px 0;
}
.parent-subject input,
.parent-subject textarea {
  width: 100%;
  box-sizing: border-box;
  min-width: 0;
}
.parent-subject fieldset {
  border: 0;
  padding: 0;
  min-width: 0;
}
</style>
