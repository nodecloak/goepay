<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, watch } from "vue";
import type { ApiClient } from "../api";
import { readGroupRoutingCatalog, type RoutingChannel } from "../group-routing";
import {
  knownPayRollRejection,
  payRollID,
  readPayRollChannels,
  readPayRollSnapshot,
  type PayRollChannel,
  type PayRollSnapshot,
} from "../pay-roll";
const props = defineProps<{
  client: ApiClient;
  rollID: string;
  disabled?: boolean;
}>();
const emit = defineEmits<{ busy: [boolean]; saved: [PayRollSnapshot] }>();
const root = ref<HTMLElement>();
const snapshot = ref<PayRollSnapshot | null>(null),
  catalog = ref<RoutingChannel[]>([]),
  selected = ref<PayRollChannel[]>([]),
  confirmed = ref(false),
  loading = ref(false),
  saving = ref(false),
  ready = ref(false),
  unknown = ref(false),
  error = ref("");
let epoch = 0,
  alive = true,
  controller: AbortController | undefined;
watch(saving, (value) => emit("busy", value), { immediate: true });
const current = (token: number, c: ApiClient, csrf: string, id: string) =>
  alive &&
  epoch === token &&
  props.client === c &&
  csrf === c.csrfToken &&
  c.area === "admin" &&
  id === props.rollID;
const path = (id: string) =>
  `/resources/pay-rolls/${encodeURIComponent(payRollID(id))}/channels`;
const ids = computed(() =>
  selected.value.map((c) => `${c.id}:${c.weight}`).join(","),
);
const oldIds = computed(() =>
  snapshot.value
    ? readPayRollChannels(snapshot.value.fields.channel_ids)
        .map((c) => `${c.id}:${c.weight}`)
        .join(",")
    : "",
);
const changed = computed(() => ids.value !== oldIds.value);
const rows = computed(() => {
  const kind = snapshot.value?.fields.type,
    available = catalog.value.filter((c) => c.type === kind),
    order = selected.value.map((c) => c.id);
  for (const channel of snapshot.value
    ? readPayRollChannels(snapshot.value.fields.channel_ids)
    : [])
    if (!available.some((c) => c.id === channel.id))
      available.push({ id: channel.id, type: kind!, methods: [] });
  return available.sort((a, b) => {
    const x = order.indexOf(a.id),
      y = order.indexOf(b.id);
    return x >= 0 && y >= 0
      ? x - y
      : x >= 0
        ? -1
        : y >= 0
          ? 1
          : BigInt(a.id) < BigInt(b.id)
            ? -1
            : 1;
  });
});
const available = (id: string) =>
  catalog.value.some(
    (c) => c.id === id && c.type === snapshot.value?.fields.type,
  );
const chosen = (id: string) => selected.value.some((c) => c.id === id);
const weight = (id: string) =>
  selected.value.find((c) => c.id === id)?.weight || "0";
const disabledInputs = computed(
  () =>
    !!props.disabled ||
    loading.value ||
    saving.value ||
    !ready.value ||
    unknown.value,
);
const valid = computed(() => {
  if (
    !snapshot.value ||
    !selected.value.length ||
    selected.value.length > 100 ||
    ids.value.length > 1000
  )
    return false;
  if (
    selected.value.some(
      (c) =>
        !available(c.id) ||
        !/^(0|[1-9][0-9]*)$/.test(c.weight) ||
        BigInt(c.weight) > 1000000n,
    )
  )
    return false;
  return (
    snapshot.value.fields.strategy !== "weighted" ||
    selected.value.some((c) => BigInt(c.weight) > 0n)
  );
});
const canSave = computed(
  () =>
    !disabledInputs.value &&
    changed.value &&
    valid.value &&
    (!snapshot.value?.fields.enabled || confirmed.value),
);
function toggle(id: string) {
  if (disabledInputs.value) return;
  confirmed.value = false;
  if (chosen(id)) selected.value = selected.value.filter((c) => c.id !== id);
  else if (available(id)) selected.value.push({ id, weight: "1" });
}
function setWeight(id: string, event: Event) {
  if (disabledInputs.value) return;
  const item = selected.value.find((c) => c.id === id);
  if (item) {
    item.weight = (event.target as HTMLInputElement).value;
    confirmed.value = false;
  }
}
function move(id: string, delta: number) {
  if (disabledInputs.value) return;
  const index = selected.value.findIndex((c) => c.id === id),
    next = index + delta;
  if (index < 0 || next < 0 || next >= selected.value.length) return;
  const item = selected.value.splice(index, 1)[0]!;
  selected.value.splice(next, 0, item);
  confirmed.value = false;
}
async function load(reset = false) {
  if (saving.value && !reset) return;
  const token = ++epoch,
    c = props.client,
    csrf = c.csrfToken,
    id = props.rollID;
  controller?.abort();
  controller = new AbortController();
  loading.value = true;
  ready.value = false;
  confirmed.value = false;
  if (reset) {
    snapshot.value = null;
    catalog.value = [];
    selected.value = [];
    unknown.value = false;
    saving.value = false;
  }
  error.value = "";
  try {
    if (c.area !== "admin") throw Error("仅管理员可配置轮询组");
    const [record, directory] = await Promise.allSettled([
      c.request(path(id), { signal: controller.signal }),
      c.request("/resources/pay-rolls/routing-options", {
        signal: controller.signal,
      }),
    ]);
    if (!current(token, c, csrf, id)) return;
    if (record.status !== "fulfilled") throw record.reason;
    const fresh = readPayRollSnapshot(record.value, id);
    snapshot.value = fresh;
    if (directory.status !== "fulfilled") throw directory.reason;
    catalog.value = readGroupRoutingCatalog(directory.value);
    selected.value = readPayRollChannels(fresh.fields.channel_ids);
    unknown.value = false;
    ready.value = true;
  } catch (e) {
    if (current(token, c, csrf, id))
      error.value = e instanceof Error ? e.message : "配置读取失败";
  } finally {
    if (current(token, c, csrf, id)) loading.value = false;
    if (reset && current(token, c, csrf, id)) {
      await nextTick();
      if (current(token, c, csrf, id))
        root.value
          ?.querySelector<HTMLElement>(
            "input:not(:disabled),button:not(:disabled)",
          )
          ?.focus({ preventScroll: true });
    }
  }
}
watch(
  () => [props.client, props.client.area, props.client.csrfToken, props.rollID],
  () => load(true),
  { immediate: true },
);
async function save() {
  if (!canSave.value || !snapshot.value) return;
  const token = epoch,
    c = props.client,
    csrf = c.csrfToken,
    id = props.rollID,
    expected = snapshot.value.revision,
    channels = ids.value,
    confirm = confirmed.value;
  saving.value = true;
  error.value = "";
  try {
    const raw = await c.request(path(id), {
      method: "POST",
      body: {
        expected_revision: expected,
        channel_ids: channels,
        confirm_enabled: confirm,
      },
    });
    if (!current(token, c, csrf, id)) return;
    const fresh = readPayRollSnapshot(raw, id);
    if (fresh.fields.channel_ids !== channels) throw Error("保存结果无法核对");
    snapshot.value = fresh;
    selected.value = readPayRollChannels(fresh.fields.channel_ids);
    confirmed.value = false;
    emit("saved", fresh);
  } catch (e) {
    if (!current(token, c, csrf, id)) return;
    if (knownPayRollRejection(e) && e.status === 422) {
      error.value = e.message;
    } else if (knownPayRollRejection(e) && e.status === 409) {
      selected.value = [];
      saving.value = false;
      await load();
      if (current(epoch, c, csrf, id))
        error.value = "配置版本已变化，已尝试读取最新配置，请核对后重新编辑。";
    } else {
      selected.value = [];
      confirmed.value = false;
      ready.value = false;
      unknown.value = true;
      error.value = "";
    }
  } finally {
    if (current(token, c, csrf, id)) saving.value = false;
  }
}
onBeforeUnmount(() => {
  alive = false;
  epoch++;
  controller?.abort();
  emit("busy", false);
});
</script>
<template>
  <section ref="root" class="roll-channel-editor">
    <p class="roll-note">
      配置通道只更新通道顺序与权重。商户范围、默认用途、执行状态和组限制保持原值；移除通道也会移除该通道的限制规则。
    </p>
    <div v-if="snapshot" class="roll-current">
      <strong>{{ snapshot.fields.name }}</strong
      ><span
        >{{ snapshot.fields.enabled ? "已启用" : "已停用" }} ·
        {{
          snapshot.fields.default_route === false ? "非默认用途" : "默认用途"
        }}
        · 商户
        {{
          snapshot.fields.merchant_id === "0"
            ? "全局"
            : snapshot.fields.merchant_id
        }}</span
      ><small>当前通道：{{ snapshot.fields.channel_ids || "尚未配置" }}</small>
    </div>
    <p v-if="unknown" role="alert">
      保存结果尚未确认。编辑草稿已清除，请重新读取配置核对后再操作。
    </p>
    <p v-if="error" role="alert">{{ error }}</p>
    <p v-if="loading" role="status">正在读取配置…</p>
    <div v-if="snapshot && !unknown" class="roll-table-wrap">
      <table>
        <thead>
          <tr>
            <th>选择</th>
            <th>通道</th>
            <th>权重</th>
            <th>顺序</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="row in rows" :key="row.id" :data-channel-id="row.id">
            <td>
              <input
                type="checkbox"
                :aria-label="`选择通道 ${row.id}`"
                :checked="chosen(row.id)"
                :disabled="
                  disabledInputs || (!available(row.id) && !chosen(row.id))
                "
                @change="toggle(row.id)"
              />
            </td>
            <td>
              通道 #{{ row.id
              }}<small v-if="!available(row.id)"
                >目录中不可用，请明确移除后保存</small
              >
            </td>
            <td>
              <input
                :aria-label="`通道 ${row.id} 权重`"
                inputmode="numeric"
                :value="weight(row.id)"
                :disabled="disabledInputs || !chosen(row.id)"
                @input="setWeight(row.id, $event)"
              />
            </td>
            <td class="roll-order">
              <button
                type="button"
                :aria-label="`上移通道 ${row.id}`"
                :disabled="
                  disabledInputs ||
                  !chosen(row.id) ||
                  selected[0]?.id === row.id
                "
                @click="move(row.id, -1)"
              >
                上移</button
              ><button
                type="button"
                :aria-label="`下移通道 ${row.id}`"
                :disabled="
                  disabledInputs ||
                  !chosen(row.id) ||
                  selected.at(-1)?.id === row.id
                "
                @click="move(row.id, 1)"
              >
                下移
              </button>
            </td>
          </tr>
        </tbody>
      </table>
      <p v-if="!rows.length">当前支付方式没有可配置的运行通道。</p>
    </div>
    <label v-if="snapshot?.fields.enabled && !unknown" class="roll-confirm"
      ><input
        v-model="confirmed"
        type="checkbox"
        aria-label="确认修改已启用轮询组"
        :disabled="disabledInputs || !changed"
      />我确认更改已启用轮询组的通道，之后的新订单会使用新配置；原订单保持原通道。</label
    >
    <p
      v-if="snapshot && !snapshot.fields.enabled && !unknown"
      class="roll-note"
    >
      保存后仍保持停用。需要执行路由时，请在编辑中明确启用。
    </p>
    <div class="roll-actions">
      <button type="button" class="primary" :disabled="!canSave" @click="save">
        {{ saving ? "正在保存…" : "保存通道配置" }}</button
      ><button type="button" :disabled="saving || loading" @click="load()">
        重新读取配置
      </button>
    </div>
  </section>
</template>
<style scoped>
.roll-channel-editor {
  display: grid;
  gap: 16px;
  min-width: 0;
}
.roll-note {
  margin: 0;
  color: var(--text-secondary, #64748b);
  line-height: 1.7;
}
.roll-current {
  display: grid;
  gap: 6px;
  overflow-wrap: anywhere;
}
.roll-table-wrap {
  max-width: 100%;
  overflow-x: auto;
}
.roll-table-wrap table {
  width: 100%;
  border-collapse: collapse;
}
.roll-table-wrap th,
.roll-table-wrap td {
  padding: 9px;
  text-align: left;
  border-bottom: 1px solid var(--border, #e2e8f0);
}
.roll-table-wrap td small {
  display: block;
  color: #b45309;
}
.roll-table-wrap td input:not([type="checkbox"]) {
  width: 76px;
  max-width: 100%;
  box-sizing: border-box;
}
.roll-order {
  white-space: nowrap;
}
.roll-order button {
  padding: 5px 8px;
}
.roll-confirm {
  display: flex;
  gap: 8px;
  align-items: flex-start;
  line-height: 1.7;
}
.roll-confirm input {
  margin-top: 5px;
  flex-shrink: 0;
}
.roll-actions {
  display: flex;
  gap: 10px;
  flex-wrap: wrap;
}
.roll-channel-editor [role="alert"] {
  color: var(--danger, #b91c1c);
  margin: 0;
}
@media (max-width: 480px) {
  .roll-table-wrap th,
  .roll-table-wrap td {
    padding: 7px 4px;
  }
  .roll-table-wrap td input:not([type="checkbox"]) {
    width: 58px;
  }
  .roll-order button {
    font-size: 12px;
    padding: 5px;
  }
  .roll-confirm {
    font-size: 13px;
  }
}
</style>
