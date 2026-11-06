<script setup lang="ts">
import { ref, nextTick, onMounted, onBeforeUnmount } from "vue";
import { ApiClient } from "../api";
import SettlementAutoAuthorization from "./SettlementAutoAuthorization.vue";
import KitField from "./KitField.vue";
import KitInput from "./KitInput.vue";
import KitSelect from "./KitSelect.vue";
import KitHelpPopover from "./KitHelpPopover.vue";
import {
  planBody,
  authorizationBody,
  type SettlementPlan,
  type PlanDraft,
  type AuthorizationDraft,
  type PlanRun,
} from "../settlement-plans";
const props = defineProps<{
  client: ApiClient;
  adminClass?: string;
  ownSession: boolean;
}>();
const plans = ref<SettlementPlan[]>([]),
  next = ref(""),
  selected = ref<SettlementPlan | null>(null),
  runs = ref<PlanRun[]>([]),
  runNext = ref(""),
  run = ref<PlanRun | null>(null);
const loading = ref(false),
  busy = ref(false),
  error = ref(""),
  notice = ref(""),
  editing = ref(false),
  creating = ref(false),
  confirming = ref(""),
  uncertain = ref(false);
const fresh = (): PlanDraft => ({
  mode: "generate_only",
  name: "",
  frequency: "day",
  interval: "1",
  anchor_at: "",
  execute_hour: "9",
  execute_minute: "0",
  execute_second: "0",
  merchant_ids: "",
});
const draft = ref(fresh()),
  auth = ref<AuthorizationDraft>({
    valid_from: "",
    expires_at: "",
    max_item_gross: "",
    max_run_gross: "",
    max_day_gross: "",
    max_day_items: "",
  });
let controller: AbortController | undefined,
  active = true,
  frozen: Record<string, unknown> | null = null;
const allowed = () =>
  props.client.area === "admin" &&
  props.adminClass === "super" &&
  props.ownSession;
const validID = (v: string) => /^[1-9]\d*$/.test(v),
  validRev = (v: string) => /^[1-9]\d*$/.test(v);
function clearReview() {
  confirming.value = "";
  frozen = null;
}
async function list(after = "0") {
  if (!allowed() || !active) {
    error.value = "仅本人超级管理员可配置结算计划";
    return;
  }
  controller?.abort();
  const c = new AbortController();
  controller = c;
  selected.value = null;
  runs.value = [];
  run.value = null;
  editing.value = false;
  creating.value = false;
  loading.value = true;
  error.value = "";
  clearReview();
  try {
    const data = await props.client.request<{
      items: SettlementPlan[];
      next_cursor: string;
    }>(`/settlement-plans?after_id=${after}&limit=20`, {
      signal: c.signal,
    });
    if (!active || c.signal.aborted) return;
    plans.value = data.items;
    next.value = data.next_cursor;
    uncertain.value = false;
  } catch (e) {
    if (active && !c.signal.aborted)
      error.value = e instanceof Error ? e.message : "计划读取失败";
  } finally {
    if (active && controller === c) loading.value = false;
  }
}
async function select(id: string) {
  if (!allowed() || !validID(id) || !active || busy.value) return;
  controller?.abort();
  const c = new AbortController();
  controller = c;
  selected.value = null;
  runs.value = [];
  run.value = null;
  loading.value = true;
  clearReview();
  error.value = "";
  editing.value = false;
  creating.value = false;
  try {
    const [p, page] = await Promise.all([
      props.client.request<SettlementPlan>(`/settlement-plans/${id}`, {
        signal: c.signal,
      }),
      props.client.request<{ items: PlanRun[]; next_cursor: string }>(
        `/settlement-plans/${id}/runs?after_id=0&limit=20`,
        { signal: c.signal },
      ),
    ]);
    if (!active || c.signal.aborted) return;
    if (
      p.id !== id ||
      !["generate_only", "balance_threshold"].includes(p.mode) ||
      !validRev(p.revision) ||
      !Array.isArray(p.merchant_ids)
    )
      throw new Error("计划身份或模式不匹配，请刷新页面后核对");
    selected.value = p;
    runs.value = page.items;
    runNext.value = page.next_cursor;
    uncertain.value = false;
  } catch (e) {
    if (active && !c.signal.aborted)
      error.value = e instanceof Error ? e.message : "计划读取失败";
  } finally {
    if (active && controller === c) loading.value = false;
  }
}
const editorElement = ref<HTMLElement | null>(null);
let editTrigger: HTMLElement | null = null;
function cancelEdit() {
  if (busy.value) return;
  clearReview();
  draft.value = fresh();
  editing.value = false;
  creating.value = false;
  void nextTick(() => {
    if (active && allowed() && editTrigger?.isConnected) editTrigger.focus();
  });
}
function edit(create = false, event?: Event) {
  if (
    !active ||
    !allowed() ||
    busy.value ||
    loading.value ||
    uncertain.value ||
    (!create && !selected.value)
  )
    return;
  editTrigger =
    event?.currentTarget instanceof HTMLElement ? event.currentTarget : null;
  clearReview();
  creating.value = create;
  editing.value = true;
  notice.value = "";
  draft.value = create
    ? fresh()
    : {
        mode: selected.value!.mode,
        name: selected.value!.name,
        frequency: selected.value!.schedule.frequency,
        interval: String(selected.value!.schedule.interval),
        anchor_at: selected.value!.schedule.anchor_at,
        execute_hour: String(selected.value!.schedule.execute_hour),
        execute_minute: String(selected.value!.schedule.execute_minute),
        execute_second: String(selected.value!.schedule.execute_second),
        merchant_ids: selected.value!.merchant_ids.join(","),
      };
  void nextTick(() => {
    if (active && allowed() && editing.value)
      editorElement.value?.querySelector<HTMLInputElement>("input")?.focus();
  });
}
function review(kind: string) {
  if (!allowed() || busy.value || (uncertain.value && kind !== "revoke"))
    return;
  error.value = "";
  try {
    if (kind === "definition")
      frozen = planBody(
        draft.value,
        creating.value ? undefined : selected.value!.revision,
      );
    else if (kind === "authorization")
      frozen = authorizationBody(
        selected.value!.revision,
        selected.value!.last_authorization_id,
        auth.value,
      );
    else if (kind === "scan")
      frozen = { expected_revision: selected.value!.revision };
    else if (kind === "revoke") {
      if (!validID(selected.value!.last_authorization_id))
        throw new Error("无可撤销的原授权");
      frozen = {};
    }
    confirming.value = kind;
  } catch (e) {
    error.value = e instanceof Error ? e.message : "请核对计划";
  }
}
async function submit() {
  if (
    !allowed() ||
    !frozen ||
    !confirming.value ||
    busy.value ||
    (uncertain.value && confirming.value !== "revoke")
  )
    return;
  const kind = confirming.value,
    p = selected.value,
    body = frozen,
    isCreate = creating.value;
  clearReview();
  busy.value = true;
  error.value = "";
  notice.value = "";
  const target =
    kind === "definition"
      ? isCreate
        ? "/settlement-plans"
        : `/settlement-plans/${p!.id}`
      : kind === "authorization"
        ? `/settlement-plans/${p!.id}/authorizations`
        : kind === "scan"
          ? `/settlement-plans/${p!.id}/runs`
          : `/settlement-plan-authorizations/${p!.last_authorization_id}/revoke`;
  try {
    const result = await props.client.requestResult<SettlementPlan | PlanRun>(
      target,
      {
        method: kind === "definition" && !isCreate ? "PATCH" : "POST",
        body,
        signal: controller?.signal,
      },
    );
    if (!active) return;
    if (kind === "scan") {
      if (
        result.status !== 202 ||
        !validID((result.data as PlanRun).id) ||
        (result.data as PlanRun).plan_id !== p!.id
      )
        throw new Error("扫描响应未核实原计划运行，请查询原记录");
      run.value = result.data as PlanRun;
      notice.value = "扫描已受理，仅核对原到期时段；生成完成不代表审批或付款。";
    } else {
      notice.value =
        kind === "definition"
          ? "计划定义已保存且关闭，需要单独确认新授权。"
          : kind === "authorization"
            ? "原计划授权已保存，后续每次生成仍重新核实范围、参与和收款配置。"
            : "原授权已撤销，后续生成停止；既有申请仍按原审批处理。";
      editing.value = false;
      creating.value = false;
      if (
        kind === "definition" &&
        isCreate &&
        validID((result.data as SettlementPlan).id)
      )
        selected.value = result.data as SettlementPlan;
      else if (p) selected.value = null;
    }
  } catch (e) {
    if (active) {
      uncertain.value = true;
      error.value = e instanceof Error ? e.message : "操作结果未知";
      notice.value = "不会自动重复提交。请刷新页面后核对。";
    }
  } finally {
    if (active) busy.value = false;
  }
}
async function moreRuns() {
  if (!selected.value || !runNext.value || busy.value) return;
  const c = controller,
    planID = selected.value.id;
  try {
    const page = await props.client.request<{
      items: PlanRun[];
      next_cursor: string;
    }>(
      `/settlement-plans/${selected.value.id}/runs?after_id=${runNext.value}&limit=20`,
      { signal: controller?.signal },
    );
    if (active && !c?.signal.aborted && selected.value?.id === planID) {
      runs.value = page.items;
      runNext.value = page.next_cursor;
    }
  } catch (e) {
    if (active)
      error.value = e instanceof Error ? e.message : "运行记录读取失败";
  }
}
async function detail(id: string) {
  if (!validID(id) || !selected.value) return;
  const c = controller,
    planID = selected.value.id;
  try {
    const data = await props.client.request<PlanRun>(
      `/settlement-plan-runs/${id}`,
      { signal: controller?.signal },
    );
    if (
      active &&
      !c?.signal.aborted &&
      selected.value?.id === planID &&
      data.id === id &&
      data.plan_id === planID
    )
      run.value = data;
  } catch (e) {
    if (active)
      error.value = e instanceof Error ? e.message : "运行详情读取失败";
  }
}
const modeOptions = [
  { value: "generate_only", label: "普通结算申请" },
  { value: "balance_threshold", label: "商户余额自动转账（整余额阈值）" },
];
function frequencyOptions(mode: string) {
  return [
    ...(mode === "balance_threshold"
      ? [
          { value: "minute", label: "按分钟" },
          { value: "second", label: "按秒（至少10秒）" },
        ]
      : []),
    { value: "day", label: "按天" },
    { value: "hour", label: "按小时" },
    { value: "once", label: "单次" },
  ];
}
const state = (s: string) =>
  ({
    pending: "待扫描",
    running: "扫描中",
    completed: "扫描完成",
    partial: "部分处理",
    blocked: "受阻",
    revoked: "授权已撤销",
  })[s] || "未识别状态，请核对";
onMounted(() => list());
onBeforeUnmount(() => {
  active = false;
  controller?.abort();
  frozen = null;
  selected.value = null;
  plans.value = [];
  run.value = null;
});
</script>
<template>
  <section class="panel" aria-label="结算计划">
    <div class="plans-head">
      <KitHelpPopover>
        <p>
          计划生成授权仅生成待审核申请并冻结对应资金。自动审批和付款必须另行签发独立自动出款授权；普通计划任务资料不触发资金执行。
        </p>
        <p>
          按UTC+08业务日及原D0/D1配置处理可结算资金。自动付款还要求独立执行器、明确钱包产品与商户通道准入。
        </p>
      </KitHelpPopover>
    </div>
    <p v-if="error" role="alert" class="alert error">{{ error }}</p>
    <p v-if="notice" role="status">{{ notice }}</p>
    <template v-if="allowed()">
      <div class="toolbar">
        <button
          :disabled="busy || loading || uncertain"
          @click="edit(true, $event)"
        >
          新建关闭的计划
        </button>
      </div>
      <p v-if="loading" role="status">正在读取结算计划…</p>
      <p
        v-else-if="!error && !plans.length && !editing && !selected"
        role="status"
        class="muted"
      >
        暂无结算计划，可新建一个默认关闭的计划；新建后需单独核对授权，当前不会生成申请或发起付款。
      </p>
      <ul>
        <li v-for="p in plans" :key="p.id">
          {{ p.name }} · 修订{{ p.revision }} ·
          {{ p.enabled ? "启用记录（执行仍核验）" : "关闭" }}
          <button
            :disabled="busy"
            :aria-label="'查看计划 ' + p.id"
            @click="select(p.id)"
          >
            查看
          </button>
        </li>
      </ul>
      <button v-if="next" :disabled="busy || loading" @click="list(next)">
        下一页计划
      </button>
      <section v-if="selected" class="case-panel">
        <h3>计划 {{ selected.id }} · 当前修订 {{ selected.revision }}</h3>
        <p v-if="selected.mode === 'balance_threshold'">
          按余额自动结算：按商户号从大到小，每次最多选 5
          个可用余额超过“自动结算金额”的商户，为其申请结算全部余额；所在用户组已关闭结算的商户会被跳过，名额不由其他商户补上。手续费只按用户组费率和全局手续费上限计算；同一天的新批次沿用当日已用额度，超出额度的付款不拆分。付款固定使用“支付宝转账”设置的通道，并且仍需单独授权自动出款。
        </p>
        <p>
          仅生成申请。商户范围 {{ selected.merchant_ids.join("、") }}；频率
          {{ selected.schedule.frequency }}，间隔
          {{ selected.schedule.interval }}；锚点
          {{ selected.schedule.anchor_at }}。原授权ID
          {{ selected.last_authorization_id }}。
        </p>
        <div class="toolbar">
          <button :disabled="busy || uncertain" @click="edit(false, $event)">
            修改计划定义</button
          ><button :disabled="busy || uncertain" @click="review('scan')">
            核对到期扫描</button
          ><button
            :disabled="busy || selected.last_authorization_id === '0'"
            @click="review('revoke')"
          >
            紧急撤销原授权
          </button>
        </div>
        <h3>重新授权当前修订</h3>
        <p>
          修改定义或商户参与/收款配置等变化后，旧授权失效。授权始终要求本人资金密码确认；输入的毛额上限单位为分，不是元。
        </p>
        <fieldset
          class="plan-form"
          :disabled="busy || !!confirming || uncertain"
        >
          <KitField label="授权开始 RFC3339">
            <KitInput
              v-model="auth.valid_from"
              placeholder="2026-10-24T00:00:00+08:00"
              aria-label="授权开始 RFC3339"
            />
          </KitField>
          <KitField label="授权截止 RFC3339">
            <KitInput v-model="auth.expires_at" aria-label="授权截止 RFC3339" />
          </KitField>
          <KitField label="单项毛额上限（分）">
            <KitInput
              v-model="auth.max_item_gross"
              inputmode="numeric"
              aria-label="单项毛额上限（分）"
            />
          </KitField>
          <KitField label="每次毛额上限（分）">
            <KitInput
              v-model="auth.max_run_gross"
              inputmode="numeric"
              aria-label="每次毛额上限（分）"
            />
          </KitField>
          <KitField label="每日毛额上限（分）">
            <KitInput
              v-model="auth.max_day_gross"
              inputmode="numeric"
              aria-label="每日毛额上限（分）"
            />
          </KitField>
          <KitField label="每日条数上限">
            <KitInput
              v-model="auth.max_day_items"
              inputmode="numeric"
              aria-label="每日条数上限"
            />
          </KitField>
          <button
            class="pa-btn pa-btn--primary"
            @click="review('authorization')"
          >
            核对计划授权
          </button>
        </fieldset>
        <h3>原运行记录</h3>
        <ul>
          <li v-for="r in runs" :key="r.id">
            <button :disabled="busy" @click="detail(r.id)">
              运行 {{ r.id }}
            </button>
            {{ r.business_date }} · {{ state(r.state) }} · 生成{{
              r.created_count
            }}/跳过{{ r.skipped_count }}/受阻{{ r.blocked_count }}/错误{{
              r.error_count
            }}
          </li>
        </ul>
        <button v-if="runNext" @click="moreRuns">下一页运行记录</button>
        <SettlementAutoAuthorization
          :client="client"
          :admin-class="adminClass"
          :own-session="ownSession"
          :plan="selected"
        />
      </section>
      <section v-if="editing" ref="editorElement" class="case-panel">
        <h3>
          {{ creating ? "新建计划（默认关闭）" : "修改定义后关闭旧授权" }}
        </h3>
        <p v-if="!creating && selected?.last_authorization_id !== '0'">
          已授权计划的业务模式固定。切换业务时新建关闭计划并重新核对授权。
        </p>
        <fieldset
          class="plan-form"
          :disabled="busy || !!confirming || uncertain"
        >
          <KitField label="计划名称">
            <KitInput
              v-model="draft.name"
              :maxlength="100"
              aria-label="计划名称"
            />
          </KitField>
          <KitField label="业务模式">
            <KitSelect
              :model-value="draft.mode ?? 'generate_only'"
              :options="modeOptions"
              aria-label="计划业务模式"
              :disabled="!creating && selected?.last_authorization_id !== '0'"
              @update:model-value="(v) => (draft.mode = v)"
            />
          </KitField>
          <KitField label="频率">
            <KitSelect
              v-model="draft.frequency"
              :options="frequencyOptions(draft.mode ?? 'generate_only')"
              aria-label="计划执行频率"
            />
          </KitField>
          <KitField label="间隔（单次为0）">
            <KitInput
              v-model="draft.interval"
              inputmode="numeric"
              aria-label="间隔（单次为0）"
            />
          </KitField>
          <KitField label="锚点 RFC3339">
            <KitInput
              v-model="draft.anchor_at"
              placeholder="2026-10-24T00:00:00+08:00"
              aria-label="锚点 RFC3339"
            />
          </KitField>
          <KitField label="执行小时（非按天为0）">
            <KitInput
              v-model="draft.execute_hour"
              inputmode="numeric"
              aria-label="执行小时（非按天为0）"
            />
          </KitField>
          <KitField label="执行分钟（非按天为0）">
            <KitInput
              v-model="draft.execute_minute"
              inputmode="numeric"
              aria-label="执行分钟（非按天为0）"
            />
          </KitField>
          <KitField label="执行秒（非按天为0）">
            <KitInput
              v-model="draft.execute_second"
              inputmode="numeric"
              aria-label="执行秒（非按天为0）"
            />
          </KitField>
          <KitField label="商户ID范围（逗号分隔，最多100）">
            <textarea
              v-model="draft.merchant_ids"
              class="pa-input pa-textarea"
              aria-label="商户ID范围（逗号分隔，最多100）"
            />
          </KitField>
          <button class="pa-btn pa-btn--primary" @click="review('definition')">
            核对计划定义
          </button>
          <button type="button" :disabled="busy" @click="cancelEdit">
            取消修改计划
          </button>
        </fieldset>
      </section>
      <section
        v-if="confirming"
        class="case-panel"
        role="region"
        aria-label="计划操作确认"
      >
        <h3>确认原计划操作</h3>
        <pre>{{ JSON.stringify(frozen, null, 2) }}</pre>
        <p>
          仅扫描或生成待审核申请，不执行外部付款。授权必须另行资金密码确认。
        </p>
        <button :disabled="busy" @click="submit">
          {{
            confirming === "authorization"
              ? "确认授权并验证资金密码"
              : confirming === "scan"
                ? "确认扫描当前到期时段"
                : confirming === "revoke"
                  ? "确认紧急撤销"
                  : "确认保存关闭的定义"
          }}</button
        ><button @click="clearReview">取消核对</button>
      </section>
      <section v-if="run" class="case-panel">
        <h3>运行 {{ run.id }} · {{ state(run.state) }}</h3>
        <p>
          生成{{ run.created_count }}，跳过{{ run.skipped_count }}，受阻{{
            run.blocked_count
          }}，错误{{ run.error_count }}。生成成功仅代表待审核申请；不代表付款。
        </p>
        <ul>
          <li v-for="item in run.items || []" :key="String(item.id)">
            商户{{ item.merchant_id }} · {{ item.state }} · {{ item.reason }} ·
            申请{{ item.request_id }} · 毛额{{ item.gross }}分 / 费用{{
              item.fee
            }}分 / 净额{{ item.net }}分
          </li>
        </ul>
      </section>
    </template>
  </section>
</template>
<style scoped>
.plans-head {
  display: flex;
  align-items: center;
  gap: var(--space-3, 12px);
  flex-wrap: wrap;
}
.plan-form {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
  gap: var(--space-3) var(--space-4);
  border: 0;
  padding: var(--space-3) 0;
  margin: 0;
}
.plan-form > button {
  grid-column: 1 / -1;
  justify-self: start;
}
.pa-textarea {
  display: block;
  width: 100%;
  min-height: 72px;
  padding: var(--space-2) var(--space-3);
  box-sizing: border-box;
  resize: vertical;
}
</style>
