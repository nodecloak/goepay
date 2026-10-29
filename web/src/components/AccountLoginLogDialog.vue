<script setup lang="ts">
import {
  computed,
  nextTick,
  onBeforeUnmount,
  onMounted,
  ref,
  useId,
  watch,
} from "vue";
import type { ApiClient } from "../api";
import { beijingTimestamp } from "../console-datetime";
import KitPagination from "./KitPagination.vue";
const props = defineProps<{
  client: ApiClient;
  ownSession: boolean;
  merchantID?: string;
}>();
const emit = defineEmits<{ close: [] }>();
const administrative = computed(() => props.merchantID !== undefined);
const allowed = computed(
  () =>
    props.ownSession &&
    (administrative.value
      ? props.client.area === "admin" &&
        typeof props.merchantID === "string" &&
        /^[1-9]\d{0,18}$/.test(props.merchantID) &&
        BigInt(props.merchantID) <= 9223372036854775807n
      : props.client.area === "merchant"),
);
type Row = {
  id: string;
  type: string;
  ip: string;
  city: string;
  created_at: string;
};
const rows = ref<Row[]>([]),
  total = ref(0),
  page = ref(1),
  loading = ref(false),
  error = ref(""),
  closeButton = ref<HTMLButtonElement>(),
  dialogElement = ref<HTMLElement>(),
  id = useId();
const limit = 20;
let epoch = 0,
  controller: AbortController | undefined;
async function load() {
  const focusWithin = !!dialogElement.value?.contains(document.activeElement);
  controller?.abort();
  const current = ++epoch;
  rows.value = [];
  total.value = 0;
  error.value = "";
  if (!allowed.value) {
    error.value = administrative.value
      ? "商户登录日志仅原始管理员会话及规范商户编号可查看"
      : "登录日志仅本人会话可查看";
    loading.value = false;
    return;
  }
  controller = new AbortController();
  const c = controller,
    client = props.client;
  loading.value = true;
  void nextTick(() => {
    if (
      current === epoch &&
      focusWithin &&
      (!dialogElement.value?.contains(document.activeElement) ||
        (document.activeElement instanceof HTMLButtonElement &&
          document.activeElement.disabled))
    )
      closeButton.value?.focus();
  });
  try {
    const data = await client.request<{ items: Row[]; total: number }>(
      `${administrative.value ? "/merchants/" + props.merchantID + "/login-log" : "/account/login-log"}?page=${page.value}&limit=${limit}`,
      { signal: c.signal },
    );
    if (current !== epoch || c.signal.aborted) return;
    if (
      !Array.isArray(data.items) ||
      data.items.length > limit ||
      !Number.isSafeInteger(data.total) ||
      data.total < 0 ||
      data.items.some(
        (r) =>
          !r ||
          typeof r.id !== "string" ||
          !/^\d+$/.test(r.id) ||
          BigInt(r.id) <= 0n ||
          BigInt(r.id) > 9223372036854775807n ||
          [r.type, r.ip, r.city, r.created_at].some(
            (v) =>
              typeof v !== "string" ||
              v.length > 256 ||
              /[\x00-\x1f\x7f]/.test(v),
          ),
      )
    )
      throw Error("登录日志响应格式无效");
    rows.value = data.items;
    total.value = data.total;
  } catch (e) {
    if (current === epoch && !c.signal.aborted)
      error.value = e instanceof Error ? e.message : "读取登录日志失败";
  } finally {
    if (current === epoch) loading.value = false;
  }
}
watch(
  () => [
    props.client,
    props.client.area,
    props.client.csrfToken,
    props.ownSession,
    props.merchantID,
  ],
  () => {
    page.value = 1;
    void load();
  },
  { immediate: true, flush: "sync" },
);
function keydown(event: KeyboardEvent) {
  if (event.key === "Escape") {
    event.preventDefault();
    emit("close");
    return;
  }
  if (event.key !== "Tab") return;
  const dialog = event.currentTarget as HTMLElement,
    buttons = Array.from(
      dialog.querySelectorAll<HTMLButtonElement>("button:not(:disabled)"),
    );
  const first = buttons[0],
    last = buttons.at(-1);
  if (!first || !last) return;
  if (event.shiftKey && document.activeElement === first) {
    event.preventDefault();
    last.focus();
  } else if (!event.shiftKey && document.activeElement === last) {
    event.preventDefault();
    first.focus();
  }
}
onMounted(() => void nextTick(() => closeButton.value?.focus()));
onBeforeUnmount(() => {
  ++epoch;
  controller?.abort();
  rows.value = [];
});
</script>
<template>
  <div class="modal-backdrop" @click.self="emit('close')">
    <section
      ref="dialogElement"
      tabindex="-1"
      role="dialog"
      aria-modal="true"
      :aria-labelledby="id + '-title'"
      class="modal panel account-login-log"
      @keydown="keydown"
    >
      <div class="toolbar">
        <h2 :id="id + '-title'">
          {{ administrative ? "商户 " + merchantID + " 登录日志" : "登录日志" }}
        </h2>
        <button ref="closeButton" type="button" @click="emit('close')">
          关闭登录日志
        </button>
      </div>
      <p class="muted">
        显示已核实为{{
          administrative ? "该商户" : "本账号"
        }}的成功登录。未记录的登录方式、归属地保留为“未记录”。时间为北京时间。
      </p>
      <p v-if="error" role="alert" class="alert error">{{ error }}</p>
      <button type="button" :disabled="loading || !allowed" @click="load">
        刷新登录日志
      </button>
      <p v-if="loading" role="status">正在读取登录日志…</p>
      <div v-else class="table-scroll">
        <table>
          <thead>
            <tr>
              <th>登录方式</th>
              <th>IP地址</th>
              <th>归属地</th>
              <th>时间</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="row in rows" :key="row.id">
              <td>{{ row.type || "未记录" }}</td>
              <td>{{ row.ip }}</td>
              <td>{{ row.city || "未记录" }}</td>
              <td>{{ beijingTimestamp(row.created_at) }}</td>
            </tr>
            <tr v-if="!rows.length">
              <td colspan="4">
                {{ error ? "读取失败，请刷新核对" : "暂无已核实的登录记录" }}
              </td>
            </tr>
          </tbody>
        </table>
      </div>
      <KitPagination
        v-model:page="page"
        :page-size="limit"
        :total="total"
        :page-sizes="[limit]"
        @change="load"
      />
    </section>
  </div>
</template>
<style scoped>
.account-login-log {
  width: min(100%, 900px);
}
.account-login-log .toolbar {
  flex-wrap: wrap;
}
.account-login-log .table-scroll {
  margin: 14px 0;
}
.account-login-log th,
.account-login-log td {
  white-space: nowrap;
}
@media (max-width: 600px) {
  .account-login-log th,
  .account-login-log td {
    padding: 10px 6px;
    font-size: 12px;
  }
  .account-login-log td:last-child {
    white-space: normal;
    min-width: 74px;
  }
}
</style>
