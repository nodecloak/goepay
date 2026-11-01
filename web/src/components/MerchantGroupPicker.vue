<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from "vue";
import type { ApiClient } from "../api";
const props = defineProps<{
  client: ApiClient;
  value: unknown;
  disabled: boolean;
  error?: string;
  creating?: boolean;
}>();
const emit = defineEmits<{ change: [value: string] }>();
type Group = { id: string; name: string; enabled?: boolean };
const items = ref<Group[]>([]),
  total = ref(0),
  page = ref(1),
  query = ref(""),
  applied = ref(""),
  loading = ref(false),
  loadError = ref(""),
  selectedName = ref(""),
  defaultName = ref("默认用户组"),
  namedID = ref(""),
  searchAvailable = ref(false);
const limit = 100,
  current = computed(() =>
    props.value === "0" || props.value === 0
      ? ""
      : typeof props.value === "string"
        ? props.value
        : props.value == null
          ? ""
          : String(props.value),
  ),
  dirty = computed(() => query.value !== applied.value);
const validID = (value: unknown): value is string =>
  typeof value === "string" &&
  /^(0|[1-9]\d{0,18})$/.test(value) &&
  BigInt(value) <= 9223372036854775807n;
function group(raw: unknown): Group {
  const v = raw as Group | null;
  if (
    !v ||
    typeof v !== "object" ||
    !validID(v.id) ||
    typeof v.name !== "string" ||
    !v.name.trim() ||
    v.name.length > 256 ||
    /[\x00-\x1f\x7f]/.test(v.name)
  )
    throw Error("用户组返回格式无效，请重试读取");
  return { id: v.id, name: v.name, enabled: v.enabled };
}
let context = 0,
  listEpoch = 0,
  detailEpoch = 0,
  listController: AbortController | undefined,
  detailController: AbortController | undefined;
async function ensureCurrent() {
  const id = current.value,
    found = items.value.find((g) => g.id === id);
  if (!id) {
    selectedName.value = "";
    namedID.value = "";
    return;
  }
  if (found) {
    selectedName.value = found.name;
    namedID.value = id;
    return;
  }
  if (namedID.value === id) return;
  if (!validID(id)) {
    loadError.value = "当前用户组值需核对，请明确选择用户组";
    return;
  }
  detailController?.abort();
  const c = new AbortController(),
    serial = ++detailEpoch,
    ctx = context,
    client = props.client;
  detailController = c;
  try {
    const raw = await client.request(
      "/resources/groups/" + encodeURIComponent(id),
      { signal: c.signal },
    );
    if (
      ctx !== context ||
      serial !== detailEpoch ||
      c.signal.aborted ||
      current.value !== id
    )
      return;
    const v = group(raw);
    if (v.id !== id) throw Error("返回用户组与当前选择不一致");
    selectedName.value = v.name;
    namedID.value = id;
  } catch (e) {
    if (
      ctx === context &&
      serial === detailEpoch &&
      !c.signal.aborted &&
      current.value === id
    )
      loadError.value =
        e instanceof Error ? e.message : "当前用户组读取失败，原选择已保留";
  }
}
async function load() {
  listController?.abort();
  const c = new AbortController(),
    serial = ++listEpoch,
    ctx = context,
    client = props.client;
  listController = c;
  loading.value = true;
  loadError.value = "";
  items.value = [];
  total.value = 0;
  const params = new URLSearchParams({
    page: String(page.value),
    limit: String(limit),
  });
  if (applied.value) params.set("q", applied.value);
  try {
    const data = await client.request<{ items: unknown[]; total: number }>(
      "/resources/groups?" + params,
      { signal: c.signal },
    );
    if (ctx !== context || serial !== listEpoch || c.signal.aborted) return;
    if (
      !data ||
      !Array.isArray(data.items) ||
      data.items.length > limit ||
      !Number.isSafeInteger(data.total) ||
      data.total < 0
    )
      throw Error("用户组列表返回格式无效");
    const parsed = data.items.map(group);
    if (new Set(parsed.map((g) => g.id)).size !== parsed.length)
      throw Error("用户组列表含重复编号，请重试");
    const defaultGroup = parsed.find((g) => g.id === "0");
    if (defaultGroup) defaultName.value = defaultGroup.name;
    items.value = parsed.filter((g) => g.id !== "0");
    total.value = data.total;
    if (data.total > limit) searchAvailable.value = true;
    const selected = parsed.find((g) => g.id === current.value);
    if (selected) {
      selectedName.value = selected.name;
      namedID.value = selected.id;
    }
  } catch (e) {
    if (ctx === context && serial === listEpoch && !c.signal.aborted)
      loadError.value =
        e instanceof Error ? e.message : "用户组读取失败，原选择已保留";
  } finally {
    if (ctx === context && serial === listEpoch) loading.value = false;
  }
}
function search() {
  if (props.disabled) return;
  if (
    new TextEncoder().encode(query.value).length > 256 ||
    /[\x00\r\n]/.test(query.value)
  ) {
    loadError.value = "用户组搜索最多256字节且不能包含控制字符";
    return;
  }
  applied.value = query.value;
  page.value = 1;
  void load();
}
function choose(event: Event) {
  if (props.disabled || loading.value || dirty.value) return;
  const id = (event.target as HTMLSelectElement).value,
    found = items.value.find((g) => g.id === id);
  if (id !== "" && id !== current.value && !found) return;
  selectedName.value = found?.name || "";
  namedID.value = id;
  emit("change", id);
}
watch(
  () => [props.client, props.client.csrfToken],
  async () => {
    const ctx = ++context;
    listController?.abort();
    detailController?.abort();
    page.value = 1;
    query.value = "";
    applied.value = "";
    selectedName.value = "";
    defaultName.value = "默认用户组";
    namedID.value = "";
    searchAvailable.value = false;
    await load();
    if (ctx === context) await ensureCurrent();
  },
  { immediate: true, flush: "sync" },
);
watch(
  () => props.value,
  () => {
    void ensureCurrent();
  },
);
onBeforeUnmount(() => {
  context++;
  listController?.abort();
  detailController?.abort();
  items.value = [];
  selectedName.value = "";
});
</script>
<template>
  <div class="merchant-group-picker">
    <label for="field-group_id">用户组</label>
    <select
      id="field-group_id"
      :value="current"
      :disabled="disabled || loading || dirty"
      :aria-invalid="!!error"
      :aria-describedby="error ? 'merchant-group-error' : 'merchant-group-help'"
      @change="choose"
    >
      <option value="">{{ defaultName }}</option>
      <option
        v-if="current && !items.some((g) => g.id === current)"
        :value="current"
      >
        {{
          namedID === current && selectedName ? selectedName : "当前用户组"
        }}（{{ current }}）
      </option>
      <option v-for="g in items" :key="g.id" :value="g.id">
        {{ g.name }}（{{ g.id }}）{{ g.enabled === false ? " · 已停用" : "" }}
      </option>
    </select>
    <small id="merchant-group-help" class="muted">{{
      creating
        ? "未指定时使用默认用户组。"
        : "指定基础用户组；有效会员组另按会员期限计算。未选择新值时保留原设置。"
    }}</small>
    <small
      v-if="error"
      id="merchant-group-error"
      class="field-error"
      role="alert"
      >{{ error }}</small
    >
    <p v-if="loading" role="status">正在读取用户组…</p>
    <p v-if="loadError" class="field-error" role="alert">
      {{ loadError }}。原选择不会被清空。
    </p>
    <button
      v-if="loadError"
      type="button"
      :disabled="disabled || loading"
      @click="load().then(ensureCurrent)"
    >
      重试读取用户组
    </button>
    <div
      v-if="searchAvailable || page > 1 || applied || dirty"
      class="merchant-group-search"
    >
      <input
        v-model="query"
        aria-label="搜索用户组名称"
        placeholder="输入用户组名称"
        :disabled="disabled || loading"
        maxlength="256"
        @keydown.enter.prevent="search"
      />
      <button type="button" :disabled="disabled || loading" @click="search">
        查询用户组
      </button>
      <small v-if="dirty" class="muted">搜索条件已变更，请查询后再选择。</small>
      <small class="muted">共 {{ total }} 个组 · 第 {{ page }} 页</small>
      <button
        type="button"
        :disabled="disabled || loading || dirty || page <= 1"
        @click="
          page--;
          load();
        "
      >
        上页用户组
      </button>
      <button
        type="button"
        :disabled="disabled || loading || dirty || page * limit >= total"
        @click="
          page++;
          load();
        "
      >
        下页用户组
      </button>
    </div>
  </div>
</template>
<style scoped>
.merchant-group-picker {
  min-width: 0;
  margin: 10px 0;
}
.merchant-group-picker > label {
  display: block;
  margin: 0 0 8px;
}
.merchant-group-picker > select {
  width: 100%;
  min-width: 0;
}
.merchant-group-picker > small {
  display: block;
  margin-top: 6px;
  line-height: 1.6;
}
.merchant-group-search {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  align-items: center;
  margin-top: 10px;
}
.merchant-group-search input {
  min-width: 0;
  flex: 1 1 140px;
  margin: 0;
}
.merchant-group-search small {
  flex-basis: 100%;
}
</style>
