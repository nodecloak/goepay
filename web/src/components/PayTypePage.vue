<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import type { ApiClient } from "../api";
import type { ResourceSchema } from "../resources";
import { validateFields, buildPayload, formValues } from "../resources";
import { payTypeLabel } from "../pay-types";
import KitSwitch from "./KitSwitch.vue";
import KitStatusTag from "./KitStatusTag.vue";
import KitPayMethod from "./KitPayMethod.vue";
import KitModal from "./KitModal.vue";
import KitEmpty from "./KitEmpty.vue";
import KitHelpPopover from "./KitHelpPopover.vue";
import ResourceFields from "./ResourceFields.vue";
import ShellIcon from "./ShellIcon.vue";

// 支付方式 (pre_type) console on Pro's model: the original 7 types always show
// with their effective 状态 even before a record exists (the server merges the
// catalog defaults into /resources/pay-types), and the 状态 switch creates the
// record (POST) when the type has none yet, or updates it (PATCH) when it does.
// Routing already honours these records via payTypeCatalogOpen.
const props = defineProps<{ client: ApiClient }>();
type Row = {
  id: string;
  type: string;
  name: string;
  enabled: boolean;
  device: string;
};
const schema = ref<ResourceSchema | null>(null);
const items = ref<Row[]>([]);
const loading = ref(false);
const busy = ref(false);
const togglingType = ref("");
const error = ref("");
const notice = ref("");
const mode = ref<"create" | "edit" | "delete" | null>(null);
const selected = ref<Row | null>(null);
const values = ref<Record<string, unknown>>({});
const fieldErrors = ref<Record<string, string>>({});
const endpoint = "/resources/pay-types";
let controller: AbortController | undefined;
let epoch = 0;
const canWrite = computed(() => schema.value?.permissions.update === true);
const canCreate = computed(() => schema.value?.permissions.create === true);
const canDelete = computed(() => schema.value?.permissions.delete === true);
const editableFields = computed(
  () => schema.value?.fields.filter((f) => f.type !== "password") ?? [],
);
function message(e: unknown) {
  return e instanceof Error ? e.message : "支付方式操作失败，请重试";
}
async function load() {
  const current = ++epoch;
  controller?.abort();
  controller = new AbortController();
  const c = controller;
  loading.value = true;
  error.value = "";
  // A newly read schema is shown together with the rows (see finally), so
  // the 新增 and 说明 buttons do not appear while the list is still loading.
  let pendingSchema: ResourceSchema | null = null;
  try {
    if (!schema.value) {
      const loaded = await props.client.request<ResourceSchema>(
        endpoint + "/schema",
        { signal: c.signal },
      );
      if (c.signal.aborted || current !== epoch) return;
      pendingSchema = loaded;
    }
    const data = await props.client.request<{ items: Row[] }>(endpoint, {
      signal: c.signal,
    });
    if (c.signal.aborted || current !== epoch) return;
    items.value = (data.items || []).map((r) => ({
      id: String(r.id),
      type: String(r.type ?? ""),
      name: String(r.name ?? payTypeLabel(r.type)),
      enabled: r.enabled === true,
      device: String(r.device ?? "0"),
    }));
  } catch (e) {
    if (!c.signal.aborted && current === epoch) error.value = message(e);
  } finally {
    if (!c.signal.aborted && current === epoch) {
      if (pendingSchema) schema.value = pendingSchema;
      loading.value = false;
    }
  }
}
// The 状态 switch saves at once: a type with no record yet (id "0") POSTs a new
// record, an existing record PATCHes only its enabled flag.
async function toggle(row: Row, value: boolean) {
  if (!canWrite.value || busy.value || togglingType.value) return;
  const current = epoch;
  busy.value = true;
  togglingType.value = row.type;
  error.value = "";
  notice.value = "";
  try {
    if (row.id === "0") {
      if (!canCreate.value) throw new Error("当前身份不能新增支付方式");
      await props.client.request(endpoint, {
        method: "POST",
        body: {
          name: row.name,
          type: row.type,
          enabled: value,
          device: row.device,
        },
      });
    } else {
      await props.client.request(endpoint + "/" + encodeURIComponent(row.id), {
        method: "PATCH",
        body: { enabled: value },
      });
    }
    if (current !== epoch) return;
    notice.value = value ? "已开启" : "已关闭";
    await load();
  } catch (e) {
    if (current === epoch) {
      error.value = message(e);
      await load();
    }
  } finally {
    if (current === epoch) {
      busy.value = false;
      togglingType.value = "";
    }
  }
}
function openCreate() {
  if (!canCreate.value || busy.value || mode.value) return;
  selected.value = null;
  values.value = formValues(schema.value!);
  fieldErrors.value = {};
  error.value = "";
  mode.value = "create";
}
async function openEdit(row: Row) {
  if (!canWrite.value || busy.value || mode.value) return;
  error.value = "";
  fieldErrors.value = {};
  if (row.id === "0") {
    // No record yet: seed the create form with the catalog defaults.
    selected.value = null;
    values.value = {
      ...formValues(schema.value!),
      name: row.name,
      type: row.type,
      enabled: row.enabled,
      device: row.device,
    };
    mode.value = "create";
    return;
  }
  busy.value = true;
  const current = epoch;
  try {
    const record = await props.client.request<Record<string, unknown>>(
      endpoint + "/" + encodeURIComponent(row.id),
    );
    if (current !== epoch) return;
    selected.value = { ...row };
    values.value = formValues(schema.value!, record);
    mode.value = "edit";
  } catch (e) {
    if (current === epoch) error.value = message(e);
  } finally {
    if (current === epoch) busy.value = false;
  }
}
function openDelete(row: Row) {
  if (!canDelete.value || busy.value || mode.value || row.id === "0") return;
  selected.value = { ...row };
  error.value = "";
  mode.value = "delete";
}
async function save() {
  if (busy.value || !schema.value) return;
  fieldErrors.value = validateFields(
    schema.value,
    values.value,
    mode.value === "edit",
  );
  if (Object.keys(fieldErrors.value).length) return;
  const current = epoch;
  busy.value = true;
  error.value = "";
  try {
    const payload = buildPayload(
      schema.value,
      values.value,
      mode.value === "edit",
    );
    await props.client.request(
      endpoint +
        (mode.value === "edit"
          ? "/" + encodeURIComponent(String(selected.value!.id))
          : ""),
      { method: mode.value === "edit" ? "PATCH" : "POST", body: payload },
    );
    if (current !== epoch) return;
    notice.value = mode.value === "edit" ? "修改已保存" : "新增已保存";
    mode.value = null;
    await load();
  } catch (e) {
    if (current === epoch) error.value = message(e);
  } finally {
    if (current === epoch) busy.value = false;
  }
}
async function remove() {
  if (busy.value || !selected.value) return;
  const current = epoch;
  busy.value = true;
  error.value = "";
  try {
    await props.client.request(
      endpoint + "/" + encodeURIComponent(selected.value.id),
      { method: "DELETE" },
    );
    if (current !== epoch) return;
    mode.value = null;
    notice.value = "记录已删除";
    await load();
  } catch (e) {
    if (current === epoch) error.value = message(e);
  } finally {
    if (current === epoch) busy.value = false;
  }
}
function orderHref(type: string) {
  return `/${props.client.area}/trade/order?type=${encodeURIComponent(type)}`;
}
// 支持设备 label, matching epay_pro's display_device: 0 PC+Mobile, 1 PC,
// 2 Mobile (an unknown/absent value reads as the 0 default).
function deviceLabel(device: string) {
  return device === "1" ? "PC" : device === "2" ? "Mobile" : "PC+Mobile";
}
watch(
  () => [props.client, props.client.csrfToken, props.client.area],
  () => {
    schema.value = null;
    mode.value = null;
    void load();
  },
  { flush: "sync" },
);
onMounted(load);
onBeforeUnmount(() => {
  epoch++;
  controller?.abort();
});
</script>
<template>
  <div v-if="error" class="alert error" role="alert">{{ error }}</div>
  <div v-if="notice" class="alert success" role="status">{{ notice }}</div>
  <section class="panel records pay-type-page" aria-label="支付方式">
    <div v-if="schema || !loading" class="table-tools">
      <button
        v-if="canCreate"
        type="button"
        class="pa-btn pa-btn--primary"
        :disabled="busy || loading || !!mode"
        @click="openCreate"
      >
        <ShellIcon name="plus" />新增
      </button>
      <span class="table-summary"
        >系统共有<strong>{{ items.length }}</strong
        >个支付方式</span
      >
      <div class="table-tools-end">
        <button
          type="button"
          class="pa-btn"
          :disabled="loading || busy"
          @click="load"
        >
          <ShellIcon name="refresh" />刷新
        </button>
        <KitHelpPopover
          v-if="schema?.description"
          label="说明"
          :text="schema.description"
        />
      </div>
    </div>
    <div v-if="loading" class="loading-row" role="status">正在加载记录…</div>
    <div v-else class="table-scroll">
      <table class="pa-table">
        <thead>
          <tr>
            <th scope="col">调用值</th>
            <th scope="col">名称</th>
            <th scope="col">支持设备</th>
            <th scope="col">状态</th>
            <th scope="col">操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="row in items" :key="row.type + ':' + row.id">
            <td class="pa-c-num">{{ row.type }}</td>
            <td><KitPayMethod :type="row.type" /></td>
            <td>{{ deviceLabel(row.device) }}</td>
            <td>
              <KitSwitch
                v-if="canWrite"
                :model-value="row.enabled"
                :disabled="busy || loading || togglingType === row.type"
                :label="row.enabled ? '已开启' : '已关闭'"
                @change="(value) => toggle(row, value)"
              />
              <KitStatusTag
                v-else
                :text="row.enabled ? '已开启' : '已关闭'"
                :tone="row.enabled ? 'success' : 'neutral'"
              />
            </td>
            <td>
              <div class="row-actions">
                <button
                  v-if="canWrite"
                  type="button"
                  class="link-button"
                  :disabled="busy || loading || !!mode"
                  @click="openEdit(row)"
                >
                  编辑
                </button>
                <button
                  v-if="canDelete && row.id !== '0'"
                  type="button"
                  class="link-button danger-text"
                  :disabled="busy || loading || !!mode"
                  @click="openDelete(row)"
                >
                  删除
                </button>
                <a :href="orderHref(row.type)">订单</a>
              </div>
            </td>
          </tr>
          <tr v-if="!items.length">
            <td colspan="5"><KitEmpty /></td>
          </tr>
        </tbody>
      </table>
    </div>
  </section>
  <KitModal
    v-if="mode === 'create' || mode === 'edit'"
    :title="
      (mode === 'create' ? '新增' : '编辑') + (schema?.title || '支付方式')
    "
    :loading="busy"
    @close="mode = null"
    @submit="save"
  >
    <div v-if="error" class="alert error" role="alert">{{ error }}</div>
    <ResourceFields
      v-if="schema"
      class="form-rows"
      :fields="editableFields"
      :values="values"
      :errors="fieldErrors"
      :editing="mode === 'edit'"
      @change="(name, value) => (values[name] = value)"
    />
  </KitModal>
  <KitModal
    v-else-if="mode === 'delete' && selected"
    title="删除确认"
    submit-text="确认删除"
    :loading="busy"
    danger
    @close="mode = null"
    @submit="remove"
  >
    <p>确认删除支付方式「{{ selected.name }}」的本地记录？此操作会立即生效。</p>
  </KitModal>
</template>
<style scoped>
.pay-type-page {
  min-width: 0;
}
.table-scroll {
  overflow: auto;
}
.pay-type-page table {
  width: 100%;
}
/* No pager under this table: the card edge closes the last row. */
.pay-type-page tbody tr:last-child td {
  border-bottom: 0;
}
.row-actions {
  display: flex;
  gap: 10px;
  align-items: center;
}
</style>
