<script setup lang="ts">
import {
  computed,
  nextTick,
  onBeforeUnmount,
  onMounted,
  ref,
  toRaw,
  watch,
} from "vue";
import { ApiError, type ApiClient } from "../api";
import { useSettingsDraft212 } from "../settings-drafts212";
import CoreRiskActions from "./CoreRiskActions.vue";
import {
  coreRiskFields,
  canonicalCoreRiskDraft,
  readCoreRiskPolicy,
  readCoreRiskCheckResult,
  validCoreRiskDraft,
  type CoreRiskPolicy,
} from "../core-payment-risk";
const props = defineProps<{ client: ApiClient; allowed: boolean }>();
const emit = defineEmits<{ busy: [boolean] }>();
const current = ref<CoreRiskPolicy>(),
  actionBusy = ref(false),
  pendingRun = ref<{ key: string; revision: string }>(),
  draft = ref<Record<string, string>>({}),
  loading = ref(false),
  busy = ref(false),
  uncertain = ref(false),
  error = ref(""),
  notice = ref(""),
  confirm = ref<"save" | "check">(),
  prepare = ref<HTMLButtonElement>(),
  dialog = ref<HTMLElement>();
let alive = true,
  epoch = 0,
  abort: AbortController | undefined,
  client = toRaw(props.client),
  csrf = client.csrfToken;
function reset() {
  ++epoch;
  abort?.abort();
  current.value = undefined;
  pendingRun.value = undefined;
  draft.value = {};
  confirm.value = undefined;
  busy.value = false;
  loading.value = false;
  uncertain.value = false;
  notice.value = "";
  emit("busy", false);
  client = toRaw(props.client);
  csrf = client.csrfToken;
}
function context() {
  if (client === toRaw(props.client) && csrf === toRaw(props.client).csrfToken)
    return true;
  reset();
  error.value = "会话已变化，请刷新页面后重新核对。";
  return false;
}
function valid(stamp: number, c: AbortController) {
  return (
    alive && stamp === epoch && !c.signal.aborted && props.allowed && context()
  );
}
const dirty = computed(
  () =>
    !!current.value &&
    JSON.stringify(draft.value) !== JSON.stringify(current.value.settings),
);
const { confirmDiscard: confirmDraftDiscard } = useSettingsDraft212(
  "核心支付风控",
  () =>
    dirty.value ||
    !!confirm.value ||
    !!pendingRun.value ||
    uncertain.value ||
    busy.value ||
    actionBusy.value,
);
const fields = computed(() =>
  coreRiskFields.filter((f) => !f.parent || draft.value[f.parent] === "1"),
);
async function load() {
  if (!props.allowed || busy.value || actionBusy.value) return;
  const discardEpoch = epoch;
  if (
    !(await confirmDraftDiscard()) ||
    discardEpoch !== epoch ||
    !props.allowed ||
    busy.value ||
    actionBusy.value
  )
    return;
  context();
  abort?.abort();
  const c = new AbortController();
  abort = c;
  const stamp = ++epoch;
  loading.value = true;
  error.value = "";
  confirm.value = undefined;
  try {
    const v = readCoreRiskPolicy(
      await client.request("/payment-risk", { signal: c.signal }),
    );
    if (!valid(stamp, c)) return;
    current.value = v;
    draft.value = { ...v.settings };
    uncertain.value = !!pendingRun.value;
    notice.value = "已读取当前生效设置。";
  } catch {
    if (valid(stamp, c)) {
      current.value = undefined;
      error.value = "无法确认当前风控设置，请刷新核对。";
    }
  } finally {
    if (valid(stamp, c)) loading.value = false;
  }
}
async function begin(kind: "save" | "check") {
  if (actionBusy.value) return;
  if (
    !context() ||
    !props.allowed ||
    !current.value ||
    busy.value ||
    loading.value ||
    uncertain.value
  )
    return;
  if (kind === "save" && (!dirty.value || !validCoreRiskDraft(draft.value)))
    return;
  if (kind === "check" && (!current.value.configured || dirty.value)) return;
  if (
    kind === "save" &&
    !current.value.geo_configured &&
    (draft.value.pay_region_block ||
      draft.value.pay_block_oversea === "1" ||
      draft.value.check_pay_regoin !== "0")
  ) {
    error.value = "请先配置本地地址库，再开启地区保护。";
    return;
  }
  confirm.value = kind;
  await nextTick();
  dialog.value?.focus();
}
function actionBusyChanged(value: boolean) {
  actionBusy.value = value;
  emit("busy", busy.value || value || !!pendingRun.value);
}
async function cancel() {
  if (busy.value) return;
  confirm.value = undefined;
  await nextTick();
  prepare.value?.focus();
}
async function submit() {
  if (
    !context() ||
    !props.allowed ||
    !current.value ||
    !confirm.value ||
    busy.value
  )
    return;
  const kind = confirm.value,
    original = current.value,
    settings = canonicalCoreRiskDraft(draft.value),
    c = new AbortController(),
    stamp = ++epoch;
  abort = c;
  busy.value = true;
  emit("busy", true);
  error.value = "";
  notice.value = "";
  try {
    if (kind === "save") {
      const v = readCoreRiskPolicy(
        await client.request("/payment-risk", {
          method: "PATCH",
          signal: c.signal,
          body: {
            settings,
            expected_fingerprint: original.fingerprint,
          },
        }),
      );
      if (!valid(stamp, c)) return;
      if (
        v.revision !== String(BigInt(original.revision) + 1n) ||
        !v.configured ||
        v.fingerprint === original.fingerprint ||
        coreRiskFields.some((f) => v.settings[f.key] !== settings[f.key])
      )
        throw Error("提交结果无法核对");
      current.value = v;
      draft.value = { ...v.settings };
      notice.value = "风控设置已保存。";
    } else {
      const run = {
        key: "riskcheck-" + crypto.randomUUID(),
        revision: original.revision,
      };
      pendingRun.value = run;
      const v = readCoreRiskCheckResult(
        await client.request("/payment-risk/check", {
          method: "POST",
          signal: c.signal,
          body: {
            expected_fingerprint: original.fingerprint,
            external_no: run.key,
          },
        }),
        run.key,
        run.revision,
      );
      if (!valid(stamp, c)) return;
      if (
        !Number.isSafeInteger(v.actions) ||
        !Number.isSafeInteger(v.scanned) ||
        v.actions < 0 ||
        v.scanned < 0
      )
        throw Error("检查结果无法核对");
      notice.value = `检查完成：统计 ${v.scanned} 笔订单，新增 ${v.actions} 项阻断。`;
      pendingRun.value = undefined;
    }
    confirm.value = undefined;
    await nextTick();
    prepare.value?.focus();
  } catch (e) {
    if (valid(stamp, c)) {
      uncertain.value = true;
      confirm.value = undefined;
      error.value =
        e instanceof ApiError && e.status === 409
          ? "风控设置或事实已变化，请刷新页面后重新确认。"
          : "结果无法确认。请刷新核对，不会自动重复提交或运行检查。";
    }
  } finally {
    if (valid(stamp, c)) {
      busy.value = false;
      emit("busy", !!pendingRun.value);
    }
  }
}
async function recoverRun() {
  if (!context() || !props.allowed || busy.value || !pendingRun.value) return;
  const run = { ...pendingRun.value },
    c = new AbortController(),
    stamp = ++epoch;
  abort = c;
  busy.value = true;
  emit("busy", true);
  error.value = "";
  try {
    const result = readCoreRiskCheckResult(
      await client.request(
        "/payment-risk/check-runs/" + encodeURIComponent(run.key),
        { signal: c.signal },
      ),
      run.key,
      run.revision,
    );
    if (!valid(stamp, c)) return;
    pendingRun.value = undefined;
    uncertain.value = false;
    notice.value = `已核对原检查结果：统计 ${result.scanned} 笔订单，新增 ${result.actions} 项阻断。`;
  } catch {
    if (valid(stamp, c))
      error.value = "原检查结果仍无法确认。可再次只读核对，不会重新运行检查。";
  } finally {
    if (valid(stamp, c)) {
      busy.value = false;
      emit("busy", !!pendingRun.value);
    }
  }
}
function keydown(e: KeyboardEvent) {
  if (e.key === "Escape") {
    e.preventDefault();
    void cancel();
  }
  if (e.key === "Tab" && dialog.value) {
    const items = Array.from(
      dialog.value.querySelectorAll<HTMLElement>("button:not(:disabled)"),
    );
    if (!items.length) {
      e.preventDefault();
      return;
    }
    const first = items[0],
      last = items.at(-1);
    if (e.shiftKey && document.activeElement === first) {
      e.preventDefault();
      last?.focus();
    } else if (!e.shiftKey && document.activeElement === last) {
      e.preventDefault();
      first?.focus();
    }
  }
}
watch(
  () => [props.client, props.client.csrfToken, props.allowed],
  () => {
    reset();
    error.value = "会话或权限已变化，请刷新页面后重新核对。";
  },
);
onMounted(load);
onBeforeUnmount(() => {
  alive = false;
  ++epoch;
  abort?.abort();
  emit("busy", false);
});
</script>
<template>
  <section class="core-risk" aria-label="核心付款风控">
    <div class="toolbar">
      <h3>核心付款风控</h3>
    </div>
    <p>
      仅限制新的首次付款。原订单查询、回调和未决发送恢复继续执行。旧通用设置记录不会自动启用这些规则。
    </p>
    <p v-if="error" role="alert" class="alert error">{{ error }}</p>
    <p v-if="notice" role="status">{{ notice }}</p>
    <p v-if="pendingRun" role="status">
      上次检查结果尚未核实。<button
        :disabled="busy || loading || !allowed"
        @click="recoverRun"
      >
        核对上次检查结果
      </button>
    </p>
    <p v-if="loading" role="status">正在读取风控设置…</p>
    <template v-if="current"
      ><p>
        状态：{{ current.configured ? "已配置" : "尚未配置，默认关闭" }} · 修订
        {{ current.revision }}
      </p>
      <p class="muted">
        {{
          current.geo_configured
            ? "已指定本地地址库，付款时会校验查询结果。"
            : "尚未指定本地地址库，请先完成配置后开启地区保护。"
        }}
      </p>
      <form @submit.prevent="begin('save')">
        <div class="risk-grid">
          <div v-for="f in fields" :key="f.key">
            <label :for="'risk177-' + f.key">{{ f.label }}</label
            ><select
              v-if="f.kind === 'flag' || f.kind === 'scan'"
              :id="'risk177-' + f.key"
              v-model="draft[f.key]"
              :disabled="busy || loading || uncertain || !allowed"
            >
              <option value="0">否</option>
              <option value="1">
                {{ f.kind === "scan" ? "不一致时禁止支付" : "是" }}
              </option>
              <option v-if="f.kind === 'scan'" value="2">
                不一致时提示
              </option></select
            ><input
              v-else
              :id="'risk177-' + f.key"
              v-model="draft[f.key]"
              :inputmode="
                f.kind === 'money'
                  ? 'decimal'
                  : f.kind === 'integer'
                    ? 'numeric'
                    : undefined
              "
              :disabled="busy || loading || uncertain || !allowed"
              maxlength="8192"
            /><small v-if="f.hint" class="muted">{{ f.hint }}</small>
          </div>
        </div>
        <div class="toolbar">
          <button
            ref="prepare"
            class="primary"
            :disabled="
              !allowed ||
              busy ||
              loading ||
              uncertain ||
              !dirty ||
              !validCoreRiskDraft(draft)
            "
          >
            核对并保存风控设置</button
          ><button
            type="button"
            :disabled="busy || loading || uncertain || !dirty"
            @click="draft = { ...current.settings }"
          >
            撤销编辑</button
          ><button
            type="button"
            :disabled="
              !allowed ||
              busy ||
              loading ||
              uncertain ||
              dirty ||
              !current.configured
            "
            @click="begin('check')"
          >
            运行一次核心检查
          </button>
        </div>
      </form></template
    >
    <p class="muted">
      投诉、进件相关功能暂不启用。检查结果只影响新的付款，消息发送状态请在通知记录核对。
    </p>
    <div v-if="confirm" class="modal-backdrop">
      <section
        ref="dialog"
        role="dialog"
        aria-modal="true"
        aria-labelledby="risk177-confirm-title"
        tabindex="-1"
        class="modal"
        @keydown="keydown"
      >
        <h3 id="risk177-confirm-title">
          {{ confirm === "save" ? "确认风控设置" : "确认运行核心检查" }}
        </h3>
        <p>
          {{
            confirm === "save"
              ? "保存后立即影响新付款；已有订单不重新计费。关闭自动检查不会解除既有阻断。"
              : "检查会按已保存规则统计订单，并产生真实的新付款阻断；不会再次发起支付。"
          }}
        </p>
        <dl v-if="confirm === 'save'">
          <template
            v-for="f in coreRiskFields.filter(
              (f) => draft[f.key] !== current?.settings[f.key],
            )"
            :key="f.key"
            ><dt>{{ f.label }}</dt>
            <dd>{{ draft[f.key] || "关闭 / 未配置" }}</dd></template
          >
        </dl>
        <div class="toolbar">
          <button :disabled="busy" @click="cancel">取消</button
          ><button class="primary" :disabled="busy" @click="submit">
            {{ busy ? "正在确认…" : "确认执行" }}
          </button>
        </div>
      </section>
    </div>
    <CoreRiskActions
      v-if="current && allowed"
      :client="client"
      :allowed="allowed"
      :blocked="busy || loading || uncertain"
      :policy-fingerprint="current.fingerprint"
      @busy="actionBusyChanged"
    />
  </section>
</template>
<style scoped>
.core-risk {
  margin: 20px 0;
  min-width: 0;
}
.risk-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(min(100%, 300px), 1fr));
  gap: 20px;
}
.risk-grid label,
.risk-grid small {
  display: block;
  margin: 6px 0;
}
.risk-grid input,
.risk-grid select {
  width: 100%;
  min-width: 0;
}
.core-risk form > .toolbar {
  margin-top: 24px;
}
.modal {
  max-height: 85dvh;
  overflow: auto;
  max-width: min(600px, calc(100vw - 28px));
}
dd {
  overflow-wrap: anywhere;
  margin-left: 0;
}
</style>
