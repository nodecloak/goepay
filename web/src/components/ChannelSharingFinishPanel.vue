<script setup lang="ts">
import { nextTick, onBeforeUnmount, ref, watch } from "vue";
import { ApiError, type ApiClient } from "../api";
import type { Area } from "../menu";
const props = defineProps<{
  client: ApiClient;
  area: Area;
  merchantId: string;
  enabled: boolean;
}>();
type Finish = {
  id: string;
  order_id: string;
  merchant_id: string;
  trade_no: string;
  external_no: string;
  out_no: string;
  state: string;
  source: string;
  gross_amount: string;
  merchant_net: string;
  shared_amount: string;
  expected_unsplit: string;
  release_amount: string;
  funds_released: boolean;
  previous_failed_id: string;
};
const trade = ref(""),
  external = ref(""),
  preview = ref<Finish>(),
  rows = ref<Finish[]>([]),
  busy = ref(false),
  locked = ref(false),
  confirming = ref(false),
  error = ref("");
let epoch = 0,
  controller: AbortController | undefined;
const unsigned = (v: unknown): v is string =>
  typeof v === "string" &&
  /^(0|[1-9][0-9]{0,18})$/.test(v) &&
  BigInt(v) <= 9223372036854775807n;
const positive = (v: unknown): v is string => unsigned(v) && v !== "0";
const text = (v: unknown): v is string =>
  typeof v === "string" && v.length <= 256 && !/[\u0000-\u001f\u007f]/.test(v);
const money = (v: string) =>
  `${BigInt(v) / 100n}.${(BigInt(v) % 100n).toString().padStart(2, "0")}`;
const states: Record<string, string> = {
  eligible: "可单独申请",
  unsent: "未发送",
  claimed: "已受理",
  pending: "处理中",
  unknown: "结果未知",
  failed: "失败",
  succeeded: "成功",
};
const owner = () =>
  props.area === "admin" ? { merchant_id: props.merchantId } : {};
const query = (path: string, limit = false) => {
  const values = [
    ...(props.area === "admin" ? [`merchant_id=${props.merchantId}`] : []),
    ...(limit ? ["limit=100"] : []),
  ];
  return path + (values.length ? "?" + values.join("&") : "");
};
function parse(v: any): Finish {
  if (
    !v ||
    !unsigned(v.id) ||
    !positive(v.order_id) ||
    v.merchant_id !== props.merchantId ||
    !text(v.trade_no) ||
    !text(v.external_no) ||
    !text(v.out_no) ||
    !Object.hasOwn(states, v.state) ||
    !["finish", "sharing"].includes(v.source) ||
    typeof v.funds_released !== "boolean" ||
    !unsigned(v.previous_failed_id ?? "0") ||
    ![
      "gross_amount",
      "merchant_net",
      "shared_amount",
      "expected_unsplit",
      "release_amount",
    ].every((k) => unsigned(v[k]))
  )
    throw Error("原完结响应格式错误");
  return {
    id: v.id,
    order_id: v.order_id,
    merchant_id: v.merchant_id,
    trade_no: v.trade_no,
    external_no: v.external_no,
    out_no: v.out_no,
    state: v.state,
    source: v.source,
    gross_amount: v.gross_amount,
    merchant_net: v.merchant_net,
    shared_amount: v.shared_amount,
    expected_unsplit: v.expected_unsplit,
    release_amount: v.release_amount,
    funds_released: v.funds_released,
    previous_failed_id: v.previous_failed_id ?? "0",
  };
}
function reset() {
  epoch++;
  controller?.abort();
  busy.value = false;
  trade.value = "";
  external.value = "";
  preview.value = undefined;
  rows.value = [];
  locked.value = false;
  confirming.value = false;
  error.value = "";
}
async function run(action: (v: number, signal: AbortSignal) => Promise<void>) {
  if (busy.value) return;
  const version = epoch,
    c = new AbortController();
  controller = c;
  busy.value = true;
  error.value = "";
  try {
    await action(version, c.signal);
  } catch (e) {
    if (version === epoch && !c.signal.aborted)
      error.value = e instanceof Error ? e.message : "原完结操作失败";
  } finally {
    if (version === epoch) busy.value = false;
  }
}
async function load() {
  if (!positive(props.merchantId)) return;
  await run(async (v, signal) => {
    const result: any = await props.client.request(
      query("/channel-sharing/finishes", true),
      { signal },
    );
    if (v !== epoch) return;
    if (!Array.isArray(result?.items)) throw Error("原完结列表格式错误");
    rows.value = result.items.map(parse);
  });
}
watch(
  () => [props.client, props.area, props.merchantId],
  () => {
    reset();
    void load();
  },
  { immediate: true },
);
watch(trade, () => {
  preview.value = undefined;
  external.value = "";
  locked.value = false;
  confirming.value = false;
});
watch(external, () => {
  confirming.value = false;
});
onBeforeUnmount(reset);
async function inspect() {
  if (!trade.value) return;
  await run(async (v, signal) => {
    const result = parse(
      await props.client.request(
        query(
          `/channel-sharing/finish-preview/${encodeURIComponent(trade.value)}`,
        ),
        { signal },
      ),
    );
    if (v !== epoch) return;
    if (result.trade_no !== trade.value) throw Error("原订单号不一致");
    preview.value = result;
    external.value =
      result.external_no ||
      "finish_" +
        Array.from(crypto.getRandomValues(new Uint8Array(12)), (n) =>
          n.toString(16).padStart(2, "0"),
        ).join("");
    locked.value = result.id !== "0" || result.source === "sharing";
    if (result.id !== "0")
      rows.value = [result, ...rows.value.filter((i) => i.id !== result.id)];
  });
}
async function submit() {
  const original = preview.value;
  if (
    !original ||
    original.source !== "finish" ||
    original.state !== "eligible" ||
    locked.value ||
    !props.enabled
  )
    return;
  const payload = {
    ...owner(),
    trade_no: original.trade_no,
    external_no: external.value,
  };
  confirming.value = false;
  await run(async (v, signal) => {
    locked.value = true;
    try {
      const result = parse(
        await props.client.request("/channel-sharing/finishes", {
          method: "POST",
          body: payload,
          signal,
        }),
      );
      if (v === epoch) {
        preview.value = result;
        rows.value = [result, ...rows.value.filter((i) => i.id !== result.id)];
      }
    } catch (e) {
      if (v === epoch && !signal.aborted) {
        try {
          const original = parse(
            await props.client.request(
              query(
                `/channel-sharing/finishes/${encodeURIComponent(payload.external_no)}`,
              ),
              { signal },
            ),
          );
          if (v === epoch) {
            preview.value = original;
            rows.value = [
              original,
              ...rows.value.filter((i) => i.id !== original.id),
            ];
          }
        } catch (readError) {
          if (
            v === epoch &&
            readError instanceof ApiError &&
            readError.status === 404
          )
            locked.value = false;
        }
      }
      throw e;
    }
  });
}
async function queryOriginal(row: Finish) {
  await run(async (v, signal) => {
    const result = parse(
      await props.client.request(
        `/channel-sharing/finishes/${encodeURIComponent(row.external_no)}/query`,
        { method: "POST", body: owner(), signal },
      ),
    );
    if (v === epoch) {
      rows.value = rows.value.map((i) => (i.id === result.id ? result : i));
      if (preview.value?.id === result.id) preview.value = result;
    }
  });
}
async function prepareRetry(row: Finish) {
  trade.value = row.trade_no;
  await nextTick();
  await inspect();
}
</script>
<template>
  <section aria-label="渠道独立完结" class="finish-panel">
    <h3>原订单独立完结</h3>
    <p class="muted">
      用于尚未执行分账或已可信未受理的微信原订单。已完成的自动完结只返回原结果；支付宝独立完结暂不支持。受理成功不等于资金已释放。
    </p>
    <p v-if="error" role="alert" class="error">{{ error }}</p>
    <form aria-label="核对渠道独立完结" @submit.prevent="inspect">
      <label
        >完结平台订单号<input
          v-model="trade"
          aria-label="完结平台订单号"
          maxlength="64"
          autocomplete="off"
          :disabled="busy" /></label
      ><button :disabled="busy || !trade">核对原订单完结状态</button>
      <template v-if="preview"
        ><p class="full">
          {{
            preview.source === "sharing"
              ? "原分账已可信完结，无须新建完结请求"
              : states[preview.state]
          }}
          · 上游原金额 {{ money(preview.gross_amount) }} 元 · 已分
          {{ money(preview.shared_amount) }} 元 · 应解冻
          {{ money(preview.expected_unsplit) }} 元 · 可释放原商户净额
          {{ money(preview.release_amount) }} 元
        </p>
        <p
          v-if="
            preview.previous_failed_id !== '0' && preview.state === 'eligible'
          "
          class="full"
        >
          原完结失败证据已核实。本次创建新完结意图，使用新业务号并重新核对资金授权；旧请求保留历史。
        </p>
        <label v-if="preview.source === 'finish'"
          >完结业务请求号<input
            v-model="external"
            aria-label="完结业务请求号"
            :readonly="locked"
            maxlength="128" /></label
        ><button
          v-if="preview.source === 'finish'"
          type="button"
          data-action="submit-finish"
          class="primary"
          :disabled="
            busy ||
            locked ||
            !enabled ||
            preview.state !== 'eligible' ||
            !external
          "
          @click="confirming = true"
        >
          核对并申请独立完结
        </button></template
      >
    </form>
    <section aria-label="独立完结记录">
      <header>
        <h3>独立完结记录</h3>
        <button :disabled="busy" @click="load">刷新原完结记录</button>
      </header>
      <p v-if="!rows.length">暂无独立完结记录</p>
      <article v-for="row in rows" :key="row.id">
        <strong>{{ row.external_no }}</strong>
        <p>
          原订单 {{ row.trade_no }} · {{ states[row.state] }} ·
          {{ row.funds_released ? "原余款已释放" : "原资金仍保留" }}
        </p>
        <p>
          应解冻 {{ money(row.expected_unsplit) }} 元 · 原商户净额
          {{ money(row.release_amount) }} 元
        </p>
        <p class="muted">上游请求号 {{ row.out_no }}</p>
        <button
          data-action="query-finish"
          :disabled="busy || row.funds_released || row.state === 'failed'"
          @click="queryOriginal(row)"
        >
          查询原完结请求
        </button>
        <button
          v-if="row.state === 'failed'"
          data-action="prepare-finish-retry"
          :disabled="busy || !enabled"
          @click="prepareRetry(row)"
        >
          核对并创建新完结意图
        </button>
      </article>
    </section>
    <div
      v-if="confirming"
      role="dialog"
      aria-modal="true"
      aria-label="确认渠道完结"
      class="finish-confirm"
    >
      <p>
        确认按核对的原订单金额申请独立完结？首次受理后仅查询原请求，可信终态前不释放资金。
      </p>
      <button class="primary" @click="submit">确认申请完结</button
      ><button @click="confirming = false">取消</button>
    </div>
  </section>
</template>
<style scoped>
.finish-panel {
  display: grid;
  gap: 14px;
  border-top: 1px solid var(--line);
  padding-top: 20px;
}
.finish-panel h3 {
  margin: 0;
}
.finish-panel form {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
  gap: 14px;
  border: 1px solid var(--line);
  padding: 16px;
  border-radius: 6px;
}
.finish-panel label {
  display: grid;
  gap: 6px;
  margin: 0;
}
.finish-panel label > input {
  margin-top: 0;
}
.finish-panel button {
  align-self: end;
}
.full {
  grid-column: 1/-1;
}
.finish-panel article {
  margin-top: 14px;
  padding: 16px;
  border: 1px solid var(--line);
  border-radius: 6px;
  overflow-wrap: anywhere;
}
.finish-panel header {
  display: flex;
  gap: 12px;
  justify-content: space-between;
  align-items: center;
}
.finish-confirm {
  position: fixed;
  inset: auto 18px 24px;
  max-width: 620px;
  margin: auto;
  padding: 20px;
  background: var(--bg-card);
  box-shadow: var(--shadow-modal);
  border: 1px solid var(--line);
  border-radius: var(--radius-lg);
  z-index: 30;
}
.finish-confirm button + button {
  margin-left: 10px;
}
</style>
