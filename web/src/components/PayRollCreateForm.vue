<script setup lang="ts">
import {
  computed,
  nextTick,
  onBeforeUnmount,
  onMounted,
  ref,
  watch,
} from "vue";
import type { ApiClient } from "../api";
import { readGroupRoutingCatalog } from "../group-routing";
import {
  knownPayRollRejection,
  payRollStrategies,
  payRollTypes,
  readPayRollRecord,
  type PayRollRecord,
} from "../pay-roll";
const props = defineProps<{ client: ApiClient; disabled?: boolean }>();
const nameInput = ref<HTMLInputElement>();
const emit = defineEmits<{
  busy: [boolean];
  saved: [PayRollRecord];
  "recovery-required": [];
}>();
const name = ref(""),
  type = ref(""),
  strategy = ref("round_robin"),
  types = ref<string[]>([]),
  loading = ref(false),
  saving = ref(false),
  unknown = ref(false),
  done = ref(false),
  error = ref(""),
  readback = ref("");
let epoch = 0,
  alive = true,
  controller: AbortController | undefined;
const valid = computed(
  () =>
    !!name.value.trim() &&
    new TextEncoder().encode(name.value.trim()).length <= 100 &&
    types.value.includes(type.value) &&
    payRollStrategies.some((s) => s.value === strategy.value),
);
watch(saving, (value) => emit("busy", value), { immediate: true });
const current = (token: number, c: ApiClient, csrf: string) =>
  alive &&
  token === epoch &&
  c === props.client &&
  csrf === c.csrfToken &&
  c.area === "admin";
async function loadTypes() {
  const token = ++epoch,
    c = props.client,
    csrf = c.csrfToken;
  controller?.abort();
  controller = new AbortController();
  loading.value = true;
  types.value = [];
  error.value = "";
  saving.value = false;
  unknown.value = false;
  done.value = false;
  readback.value = "";
  name.value = "";
  strategy.value = "round_robin";
  try {
    if (c.area !== "admin") throw Error("仅管理员可配置轮询组");
    const raw = await c.request("/resources/pay-rolls/routing-options", {
      signal: controller.signal,
    });
    if (!current(token, c, csrf)) return;
    types.value = [...new Set(readGroupRoutingCatalog(raw).map((c) => c.type))];
    type.value = types.value[0] || "";
    if (!types.value.length) throw Error("没有可配置的运行支付通道");
  } catch (e) {
    if (current(token, c, csrf))
      error.value = e instanceof Error ? e.message : "支付方式读取失败";
  } finally {
    if (current(token, c, csrf)) loading.value = false;
  }
}
watch(
  () => [props.client, props.client.area, props.client.csrfToken],
  loadTypes,
  { immediate: true },
);
async function submit() {
  if (
    props.disabled ||
    loading.value ||
    saving.value ||
    unknown.value ||
    done.value ||
    !valid.value
  )
    return;
  const token = epoch,
    c = props.client,
    csrf = c.csrfToken;
  saving.value = true;
  error.value = "";
  const requested = {
    creation_mode: "draft",
    name: name.value.trim(),
    type: type.value,
    strategy: strategy.value,
  };
  try {
    const raw = await c.request("/resources/pay-rolls/drafts", {
      method: "POST",
      body: requested,
    });
    if (!current(token, c, csrf)) return;
    const record = readPayRollRecord(raw);
    if (
      record.enabled ||
      record.default_route !== false ||
      record.merchant_id !== "0" ||
      record.channel_ids !== "" ||
      record.name !== requested.name ||
      record.type !== requested.type ||
      record.strategy !== requested.strategy ||
      Object.keys(JSON.parse(record.limits)).length !== 0 ||
      Object.keys(JSON.parse(record.rules)).length !== 0
    )
      throw Error("创建结果无法核对");
    done.value = true;
    emit("saved", record);
  } catch (e) {
    if (!current(token, c, csrf)) return;
    if (knownPayRollRejection(e)) {
      error.value = e.message;
    } else {
      unknown.value = true;
      name.value = "";
      error.value = "创建结果尚未确认，请读取列表核对。当前表单不会再次创建。";
      emit("recovery-required");
    }
  } finally {
    if (current(token, c, csrf)) saving.value = false;
  }
}
async function verify() {
  if (saving.value || loading.value) return;
  const token = epoch,
    c = props.client,
    csrf = c.csrfToken;
  loading.value = true;
  readback.value = "";
  try {
    const raw = await c.request<{ items: unknown[]; total: unknown }>(
      "/resources/pay-rolls?page=1&limit=20",
    );
    if (!current(token, c, csrf)) return;
    if (!Array.isArray(raw.items)) throw Error("列表格式无法核对");
    readback.value = `已读取最新 ${raw.items.length} 条记录，请返回列表核对创建结果。当前表单保持锁定。`;
  } catch {
    if (current(token, c, csrf))
      readback.value = "列表读取失败，可再次只读核对。";
  } finally {
    if (current(token, c, csrf)) loading.value = false;
  }
}
onBeforeUnmount(() => {
  alive = false;
  epoch++;
  controller?.abort();
  emit("busy", false);
});
onMounted(async () => {
  await nextTick();
  if (alive && !props.disabled) nameInput.value?.focus({ preventScroll: true });
});
</script>
<template>
  <form class="pay-roll-create" @submit.prevent="submit">
    <p class="roll-note">
      先创建轮询组，再配置通道。新组保持停用，配置完成后可在编辑中明确启用。
    </p>
    <label
      >显示名称<input
        ref="nameInput"
        v-model="name"
        aria-label="显示名称"
        maxlength="100"
        :disabled="disabled || saving || unknown || done"
        required
        autofocus
    /></label>
    <label
      >支付方式<select
        v-model="type"
        aria-label="支付方式"
        :disabled="disabled || loading || saving || unknown || done"
      >
        <option v-for="kind in types" :key="kind" :value="kind">
          {{ payRollTypes[kind] }}
        </option>
      </select></label
    >
    <label
      >轮询方式<select
        v-model="strategy"
        aria-label="轮询方式"
        :disabled="disabled || saving || unknown || done"
      >
        <option
          v-for="option in payRollStrategies"
          :key="option.value"
          :value="option.value"
        >
          {{ option.label }}
        </option>
      </select></label
    >
    <p v-if="error" role="alert">{{ error }}</p>
    <p v-if="readback" role="status">{{ readback }}</p>
    <div class="roll-actions">
      <button
        type="submit"
        class="primary"
        :disabled="disabled || loading || saving || unknown || done || !valid"
      >
        {{ saving ? "正在创建…" : "创建轮询组" }}</button
      ><button
        v-if="unknown"
        type="button"
        :disabled="loading || saving"
        @click="verify"
      >
        读取列表核对</button
      ><button
        v-else-if="error && !types.length"
        type="button"
        :disabled="loading || saving"
        @click="loadTypes"
      >
        重新读取支付方式
      </button>
    </div>
  </form>
</template>
<style scoped>
.pay-roll-create {
  display: grid;
  gap: 16px;
  min-width: 0;
}
.pay-roll-create label {
  display: grid;
  gap: 7px;
}
.pay-roll-create input,
.pay-roll-create select {
  width: 100%;
  min-width: 0;
  box-sizing: border-box;
}
.roll-note {
  margin: 0;
  color: var(--text-secondary, #64748b);
  line-height: 1.7;
}
.roll-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
}
.pay-roll-create [role="alert"] {
  color: var(--danger, #b91c1c);
  margin: 0;
}
</style>
