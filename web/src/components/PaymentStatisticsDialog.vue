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
import PaymentAmountTrend from "./PaymentAmountTrend.vue";
import {
  readMerchantPaymentStatistics,
  merchantStatisticsID,
  type MerchantPaymentStatistics,
} from "../merchant-payment-statistics";
import type { ApiClient } from "../api";
import { groupUsersHref } from "../group-directory";
import { readGroupStatistics, type GroupStatistics } from "../group-statistics";
const props = defineProps<{
    client: ApiClient;
    subjectId: string;
    kind: "group" | "merchant";
    ownSession: boolean;
  }>(),
  emit = defineEmits<{ close: [] }>();
const data = ref<GroupStatistics | MerchantPaymentStatistics | null>(null),
  tab = ref<"today" | "yesterday">("today"),
  loading = ref(false),
  error = ref(""),
  closeButton = ref<HTMLButtonElement>(),
  dialog = ref<HTMLElement>(),
  id = useId();
const allowed = computed(
    () =>
      props.ownSession &&
      props.client.area === "admin" &&
      (props.kind === "group"
        ? !!groupUsersHref(props.subjectId)
        : merchantStatisticsID(props.subjectId)),
  ),
  day = computed(() => data.value?.days[tab.value]);
let epoch = 0,
  controller: AbortController | undefined;
async function load() {
  const focusWithin = !!dialog.value?.contains(document.activeElement);
  controller?.abort();
  const current = ++epoch;
  data.value = null;
  error.value = "";
  if (!allowed.value) {
    error.value =
      (props.kind === "group" ? "用户组" : "商户") +
      "统计仅管理员本人会话可查看";
    loading.value = false;
    return;
  }
  const c = new AbortController();
  controller = c;
  loading.value = true;
  void nextTick(() => {
    if (
      current === epoch &&
      focusWithin &&
      (!dialog.value?.contains(document.activeElement) ||
        (document.activeElement instanceof HTMLButtonElement &&
          document.activeElement.disabled))
    )
      closeButton.value?.focus();
  });
  try {
    const result = await props.client.request(
      "/reports/" +
        (props.kind === "group" ? "groups?" : "merchant-payments?") +
        new URLSearchParams({
          [props.kind === "group" ? "gid" : "uid"]: props.subjectId,
        }),
      { signal: c.signal },
    );
    if (current !== epoch || c.signal.aborted) return;
    data.value =
      props.kind === "group"
        ? readGroupStatistics(result, props.subjectId)
        : readMerchantPaymentStatistics(result, props.subjectId);
  } catch (e) {
    if (current === epoch && !c.signal.aborted)
      error.value = e instanceof Error ? e.message : "读取统计失败";
  } finally {
    if (current === epoch) loading.value = false;
  }
}
watch(
  () => [
    props.client,
    props.client.area,
    props.client.csrfToken,
    props.subjectId,
    props.kind,
    props.ownSession,
  ],
  () => {
    tab.value = "today";
    void load();
  },
  { immediate: true, flush: "sync" },
);
function keydown(e: KeyboardEvent) {
  if (e.key === "Escape") {
    e.preventDefault();
    emit("close");
    return;
  }
  if (e.key !== "Tab") return;
  const buttons = Array.from(
      dialog.value?.querySelectorAll<HTMLElement>(
        'button:not(:disabled),[tabindex="0"]',
      ) || [],
    ).filter((el) => el.tabIndex >= 0 && el.getClientRects().length),
    first = buttons[0],
    last = buttons.at(-1);
  if (e.shiftKey && document.activeElement === first) {
    e.preventDefault();
    last?.focus();
  } else if (!e.shiftKey && document.activeElement === last) {
    e.preventDefault();
    first?.focus();
  }
}
function tabKey(e: KeyboardEvent) {
  if (!["ArrowLeft", "ArrowRight", "Home", "End"].includes(e.key)) return;
  e.preventDefault();
  tab.value =
    e.key === "Home"
      ? "today"
      : e.key === "End"
        ? "yesterday"
        : tab.value === "today"
          ? "yesterday"
          : "today";
  void nextTick(() =>
    dialog.value
      ?.querySelector<HTMLButtonElement>('[role="tab"][aria-selected="true"]')
      ?.focus(),
  );
}
onMounted(() => void nextTick(() => closeButton.value?.focus()));
onBeforeUnmount(() => {
  ++epoch;
  controller?.abort();
  data.value = null;
});
</script>
<template>
  <div class="modal-backdrop" @click.self="emit('close')">
    <section
      ref="dialog"
      role="dialog"
      aria-modal="true"
      :aria-labelledby="id + '-title'"
      class="modal panel group-statistics"
      :class="{ 'merchant-statistics': kind === 'merchant' }"
      @keydown="keydown"
    >
      <div class="toolbar">
        <h2 :id="id + '-title'">
          {{
            kind === "group"
              ? "用户组 " +
                (data && "group_name" in data
                  ? data.group_name
                  : "#" + subjectId) +
                " 支付统计"
              : "商户 " + subjectId + " 支付统计"
          }}
        </h2>
        <button ref="closeButton" type="button" @click="emit('close')">
          关闭统计
        </button>
      </div>
      <PaymentAmountTrend
        v-if="kind === 'merchant' && data && 'trend' in data"
        :key="subjectId"
        :days="data.trend"
      />
      <div
        role="tablist"
        aria-label="统计日期"
        class="toolbar statistics-tabs"
        @keydown="tabKey"
      >
        <button
          v-for="choice in ['today', 'yesterday'] as const"
          :id="id + '-' + choice"
          :key="choice"
          type="button"
          role="tab"
          :aria-selected="tab === choice"
          :tabindex="tab === choice ? 0 : -1"
          :aria-controls="id + '-panel'"
          @click="tab = choice"
        >
          {{ choice === "today" ? "今日数据" : "昨日数据" }}
        </button>
      </div>
      <p class="muted">
        {{
          kind === "group" ? "按当前有效用户组统计，" : ""
        }}日期为北京时间订单创建日。金额为已支付订单原金额，数量为全部订单，成功率为已支付笔数／全部笔数。
      </p>
      <button type="button" :disabled="loading || !allowed" @click="load">
        刷新统计
      </button>
      <p v-if="loading" role="status">正在读取统计…</p>
      <p v-if="error" role="alert" class="alert error">{{ error }}</p>
      <div
        v-if="day"
        :id="id + '-panel'"
        role="tabpanel"
        :aria-labelledby="id + '-' + tab"
      >
        <p>{{ day.date }}</p>
        <div class="table-scroll">
          <table>
            <thead>
              <tr>
                <th scope="col">支付方式</th>
                <th scope="col">订单金额</th>
                <th scope="col">订单数量</th>
                <th scope="col">成功率</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="row in day.items" :key="row.type">
                <td>{{ row.showname }}</td>
                <td>¥{{ row.money }}</td>
                <td>{{ row.count }}</td>
                <td>{{ row.rate }}%</td>
              </tr>
            </tbody>
            <tfoot>
              <tr>
                <th scope="row">总计</th>
                <td>¥{{ day.summary.money }}</td>
                <td>{{ day.summary.count }}</td>
                <td>{{ day.summary.rate }}%</td>
              </tr>
            </tfoot>
          </table>
        </div>
      </div>
    </section>
  </div>
</template>
<style scoped>
.group-statistics {
  width: min(100%, 700px);
}
.merchant-statistics {
  width: min(100%, 800px);
}
.group-statistics .statistics-tabs {
  justify-content: flex-start;
  gap: 8px;
}
.group-statistics h2 {
  overflow-wrap: anywhere;
}
.group-statistics .toolbar {
  flex-wrap: wrap;
}
.group-statistics table {
  min-width: 430px;
}
.group-statistics td:not(:first-child),
.group-statistics th:not(:first-child) {
  text-align: right;
}
.group-statistics [role="tab"][aria-selected="true"] {
  color: var(--primary);
  border-color: currentColor;
}
</style>
