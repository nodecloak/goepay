<script setup lang="ts">
import { ref, computed, watch, onBeforeUnmount, onMounted } from "vue";
import type { ApiClient } from "../api";
import type { ParentChannel } from "../parent-channels";
import {
  readRuntimeChannelLimits,
  runtimeChannelLimitsPath,
  type RuntimeChannelLimits,
} from "../runtime-channel-limits";
const props = defineProps<{
  client: ApiClient;
  allowed: boolean;
  row: ParentChannel;
  cost: boolean;
  limits: boolean;
}>();
const state = ref<RuntimeChannelLimits>(),
  invalid = ref(false);
let active = true,
  epoch = 0,
  c = props.client,
  csrf = c.csrfToken,
  id = props.row.id;
function current(n: number) {
  if (!active) return false;
  if (
    c !== props.client ||
    csrf !== props.client.csrfToken ||
    id !== props.row.id ||
    !props.allowed ||
    props.client.area !== "admin"
  ) {
    invalid.value = true;
    state.value = undefined;
    epoch++;
    return false;
  }
  return n === epoch;
}
const costText = computed(() => {
  if (invalid.value) return "未知";
  const f = props.row.account?.fields.find(
    (f) => f.namespace === "config" && f.name === "costrate" && !f.secret,
  );
  return f?.value !== undefined &&
    /^(?:100(?:\.0{1,2})?|(?:0|[1-9][0-9]?)(?:\.[0-9]{1,2})?)$/.test(f.value)
    ? f.value + "%（估算）"
    : "未知";
});
async function load() {
  const n = ++epoch;
  state.value = undefined;
  c = props.client;
  csrf = c.csrfToken;
  id = props.row.id;
  invalid.value = false;
  if (!props.limits || !props.row.configured || !current(n)) return;
  try {
    const v = readRuntimeChannelLimits(
      await c.request(runtimeChannelLimitsPath(id)),
    );
    if (!current(n)) return;
    if (v.channel_id !== id) throw new Error("wrong original parent");
    state.value = v;
  } catch {
    if (current(n)) state.value = undefined;
  }
}
function focus() {
  current(epoch);
}
watch(
  () => [
    props.client,
    props.client.area,
    props.allowed,
    props.row,
    props.limits,
  ],
  () => void load(),
  { immediate: true, flush: "sync" },
);
onMounted(() => window.addEventListener("focus", focus));
onBeforeUnmount(() => {
  active = false;
  epoch++;
  window.removeEventListener("focus", focus);
});
const windowText = computed(() =>
  !state.value
    ? "未知"
    : state.value.timestart === state.value.timestop
      ? "全天"
      : state.value.timestart +
        ":00 至 " +
        state.value.timestop +
        ":59（UTC+8）",
);
const amount = (v: string | undefined) =>
  v === undefined ? "未知" : Number(v) === 0 ? "不限" : v;
</script>
<template>
  <td v-if="cost">{{ costText }}</td>
  <td v-if="limits">{{ amount(state?.daytop) }}</td>
  <td v-if="limits">{{ amount(state?.daymaxorder) }}</td>
  <td v-if="limits">{{ windowText }}</td>
</template>
