<script setup lang="ts">
import { ref, computed, watch, onBeforeUnmount, nextTick } from "vue";
import { ApiClient, ApiError } from "../api";
import {
  readSettlementRecipient,
  recipientConfigurationBody,
  type SettlementRecipient,
  type RecipientDraft,
  type RecipientFieldErrors,
  recipientFieldErrors,
  recipientBankOptions,
} from "../settlement-recipient";
import KitField from "./KitField.vue";
import KitInput from "./KitInput.vue";
import KitSelect from "./KitSelect.vue";
const props = defineProps<{
  client: ApiClient;
  merchantID?: string;
  admin: boolean;
  disabled: boolean;
}>();
const emit = defineEmits<{ change: [SettlementRecipient | null] }>();
const panel = ref<HTMLElement>(),
  editTrigger = ref<HTMLButtonElement>(),
  refreshTrigger = ref<HTMLButtonElement>(),
  fieldErrors = ref<RecipientFieldErrors>({}),
  manualBankCode = ref(false);
const current = ref<SettlementRecipient | null>(null),
  loading = ref(false),
  saving = ref(false),
  editing = ref(false),
  error = ref(""),
  notice = ref("");
// Settlement account types the platform allows now (settle_* switches). Types
// turned off are hidden from the 收款方式 picker; the server still refuses them.
const allowedTypes = ref<string[]>([]),
  typesState = ref<"loading" | "ready" | "unavailable">("loading"),
  recipientWritePermission = ref<boolean | null>(null),
  typesWritePermission = ref<boolean | null>(null),
  unknownSave = ref(false);
const settlementTypeLabel: Record<string, string> = {
  alipay: "支付宝",
  wxpay: "微信",
  qqpay: "QQ钱包",
  bank: "银行卡",
};
// Pro order 支付宝 / 微信 / 银行卡; only platform-enabled rails shown.
const typeOptions = computed(() =>
  ["alipay", "wxpay", "bank"]
    .filter((t) => allowedTypes.value.includes(t))
    .map((t) => ({ value: t, label: settlementTypeLabel[t]! })),
);
const empty = (): RecipientDraft => ({
  type: "alipay",
  account: "",
  name: "",
  bank_code: "",
  app_id: "",
});
const draft = ref(empty());
let epoch = 0,
  controller: AbortController | undefined,
  writeController: AbortController | undefined,
  active = true;
const context = () => ({
  client: props.client,
  csrf: props.client.csrfToken,
  area: props.client.area,
  admin: props.admin,
  merchantID: props.merchantID,
});
type Context = ReturnType<typeof context>;
function sameContext(scope: Context) {
  return (
    scope.client === props.client &&
    scope.csrf === props.client.csrfToken &&
    scope.area === props.client.area &&
    scope.admin === props.admin &&
    scope.merchantID === props.merchantID
  );
}
function currentContext(generation: number, scope: Context) {
  return active && generation === epoch && sameContext(scope);
}
const canEdit = computed(
  () =>
    !!current.value &&
    recipientWritePermission.value === true &&
    typesWritePermission.value === true &&
    typesState.value === "ready" &&
    typeOptions.value.length > 0 &&
    !unknownSave.value &&
    !loading.value &&
    !saving.value &&
    !props.disabled,
);
function focusRemainsInPanel() {
  const focused = document.activeElement;
  return (
    !focused ||
    focused === document.body ||
    focused === document.documentElement ||
    !!panel.value?.contains(focused)
  );
}
function focusEditor(generation: number, scope: Context) {
  if (!currentContext(generation, scope) || !editing.value || !canEdit.value)
    return;
  const target = panel.value?.querySelector<HTMLElement>(
    '[data-recipient-field] button:not([disabled]),[data-recipient-field] input:not([disabled]):not([type="hidden"])',
  );
  if (
    focusRemainsInPanel() &&
    target?.isConnected &&
    !target.matches(":disabled")
  )
    target.focus();
}
function restoreEditorFocus(generation: number, scope: Context) {
  if (
    !currentContext(generation, scope) ||
    editing.value ||
    loading.value ||
    saving.value
  )
    return;
  const target =
    editTrigger.value && !editTrigger.value.matches(":disabled")
      ? editTrigger.value
      : refreshTrigger.value;
  if (
    focusRemainsInPanel() &&
    target?.isConnected &&
    !target.matches(":disabled")
  )
    target.focus();
}
function clear(resetCurrent = true) {
  epoch++;
  controller?.abort();
  writeController?.abort();
  if (resetCurrent) {
    current.value = null;
    unknownSave.value = false;
  }
  draft.value = empty();
  fieldErrors.value = {};
  manualBankCode.value = false;
  editing.value = false;
  loading.value = false;
  saving.value = false;
  allowedTypes.value = [];
  recipientWritePermission.value = null;
  typesWritePermission.value = null;
  typesState.value = "loading";
  error.value = "";
  notice.value = "";
  emit("change", null);
}
async function load(resetCurrent = false, explicitReadback = false) {
  clear(resetCurrent);
  const scope = context();
  if (
    scope.admin &&
    (!/^[1-9]\d{0,18}$/.test(scope.merchantID || "") ||
      BigInt(scope.merchantID!) > 9223372036854775807n)
  )
    return null;
  const generation = epoch;
  loading.value = true;
  controller = new AbortController();
  try {
    const q = scope.admin
      ? "?merchant_id=" + encodeURIComponent(scope.merchantID!)
      : "";
    const raw = await scope.client.request<Record<string, unknown>>(
      "/workflows/settlement-recipient" + q,
      { signal: controller.signal },
    );
    if (!currentContext(generation, scope)) return null;
    current.value = readSettlementRecipient(raw);
    recipientWritePermission.value =
      typeof raw.can_write === "boolean" ? raw.can_write : null;
    if (explicitReadback) unknownSave.value = false;
    try {
      const types = await scope.client.request<{
        settlement?: unknown;
        can_write?: unknown;
      }>("/workflows/settlement-recipient/allowed-types" + q, {
        signal: controller.signal,
      });
      if (!currentContext(generation, scope)) return null;
      typesWritePermission.value =
        typeof types?.can_write === "boolean" ? types.can_write : null;
      const values = types?.settlement;
      if (
        !Array.isArray(values) ||
        values.length > 3 ||
        values.some(
          (type) =>
            typeof type !== "string" ||
            !["alipay", "wxpay", "bank"].includes(type),
        ) ||
        new Set(values).size !== values.length
      )
        throw new Error("收款方式未核实");
      allowedTypes.value = values;
      typesState.value = "ready";
    } catch {
      if (!currentContext(generation, scope)) return null;
      allowedTypes.value = [];
      typesState.value = "unavailable";
    }
    if (!unknownSave.value && typesState.value === "ready")
      emit("change", current.value);
    return generation;
  } catch (e) {
    if (currentContext(generation, scope)) {
      error.value = e instanceof Error ? e.message : "收款配置读取失败";
      typesState.value = "unavailable";
    }
    return null;
  } finally {
    if (currentContext(generation, scope)) loading.value = false;
  }
}
const refresh = () => load(false, true);
function edit() {
  if (!canEdit.value) return;
  const generation = epoch,
    scope = context();
  draft.value = empty();
  if (current.value?.type && allowedTypes.value.includes(current.value.type)) {
    draft.value.type = current.value.type;
    if (current.value.type === "wxpay")
      draft.value.app_id = current.value.app_id;
  }
  if (
    !allowedTypes.value.includes(draft.value.type) &&
    allowedTypes.value.length
  )
    draft.value.type = allowedTypes.value.includes("bank")
      ? allowedTypes.value.find((t) => t !== "qqpay") || allowedTypes.value[0]!
      : allowedTypes.value[0]!;
  editing.value = true;
  fieldErrors.value = {};
  manualBankCode.value = false;
  notice.value = "";
  emit("change", null);
  void nextTick(() => focusEditor(generation, scope));
}
function cancel() {
  if (saving.value) return;
  const generation = epoch,
    scope = context();
  draft.value = empty();
  editing.value = false;
  fieldErrors.value = {};
  if (!unknownSave.value && typesState.value === "ready")
    emit("change", current.value);
  void nextTick(() => restoreEditorFocus(generation, scope));
}
async function save() {
  if (!editing.value || !canEdit.value || !current.value) return;
  const generation = epoch,
    scope = context();
  error.value = "";
  fieldErrors.value = recipientFieldErrors(draft.value);
  if (Object.keys(fieldErrors.value).length) {
    error.value = "请核对标记的收款资料";
    await nextTick();
    if (!currentContext(generation, scope)) return;
    const field = ["type", "account", "name", "app_id", "bank_code"].find(
      (key) => fieldErrors.value[key as keyof RecipientFieldErrors],
    );
    panel.value
      ?.querySelector<HTMLElement>(
        `[data-recipient-field="${field}"] input, [data-recipient-field="${field}"] button`,
      )
      ?.focus();
    return;
  }
  if (!allowedTypes.value.includes(draft.value.type)) return;
  try {
    const body = recipientConfigurationBody(
      draft.value,
      current.value,
      scope.admin ? scope.merchantID : undefined,
    );
    saving.value = true;
    writeController = new AbortController();
    const raw = await scope.client.request("/workflows/settlement-recipient", {
      method: "POST",
      body,
      signal: writeController.signal,
    });
    if (!currentContext(generation, scope)) return;
    current.value = readSettlementRecipient(raw);
    draft.value = empty();
    editing.value = false;
    notice.value = "已记录新收款版本；旧申请的收款快照不变";
    emit("change", current.value);
  } catch (e) {
    if (!currentContext(generation, scope)) return;
    error.value =
      e instanceof Error ? e.message : "收款资料保存结果未确认，请刷新核对";
    if (e instanceof ApiError && e.responseDecoded && e.status === 422) return;
    draft.value = empty();
    editing.value = false;
    fieldErrors.value = {};
    manualBankCode.value = false;
    emit("change", null);
    if (e instanceof ApiError && e.responseDecoded && e.status === 409) {
      const readbackRequest = load(false, true);
      const readbackGeneration = epoch;
      const readback = await readbackRequest;
      if (readback !== null && currentContext(readback, scope))
        error.value = "收款版本已变化，请核对当前版本后重新编辑";
      void nextTick(() => restoreEditorFocus(readbackGeneration, scope));
    } else if (!(
      e instanceof ApiError &&
      e.responseDecoded &&
      e.status >= 400 &&
      e.status < 500
    )) {
      unknownSave.value = true;
      error.value = "";
    } else {
      recipientWritePermission.value = null;
      typesWritePermission.value = null;
      typesState.value = "unavailable";
    }
  } finally {
    if (currentContext(generation, scope)) {
      saving.value = false;
      if (!editing.value)
        void nextTick(() => restoreEditorFocus(generation, scope));
    }
  }
}
watch(
  () => [
    props.client,
    props.client.csrfToken,
    props.client.area,
    props.admin,
    props.merchantID,
  ],
  () => load(true),
  { immediate: true },
);
watch(
  () => draft.value.type,
  (type) => {
    draft.value.bank_code = "";
    draft.value.app_id =
      type === "wxpay" && current.value?.type === "wxpay"
        ? current.value.app_id
        : "";
    fieldErrors.value = {};
    manualBankCode.value = false;
  },
);
onBeforeUnmount(() => {
  active = false;
  clear();
});
</script>
<template>
  <fieldset
    ref="panel"
    class="settlement-recipient"
    :disabled="disabled || saving"
  >
    <legend>结算收款资料</legend>
    <p v-if="loading" role="status">正在读取收款配置…</p>
    <p v-if="error" class="alert error" role="alert">{{ error }}</p>
    <p v-if="notice" class="muted" role="status">{{ notice }}</p>
    <p
      v-if="
        current &&
        typesState === 'ready' &&
        (recipientWritePermission !== true || typesWritePermission !== true)
      "
      class="alert"
      role="status"
    >
      {{
        recipientWritePermission === false || typesWritePermission === false
          ? "当前仅可查看收款资料；修改需商户管理权限。"
          : "收款资料修改权限尚未核实；当前仅可查看，请刷新收款版本重试。"
      }}
    </p>
    <p v-if="unknownSave" class="alert error" role="alert">
      保存结果尚未确认，已清除新收款资料；请刷新收款版本核对后再编辑。
    </p>
    <p
      v-if="current && typesState === 'unavailable'"
      class="alert"
      role="status"
    >
      暂无法核实该商户可用的收款方式；原收款资料仍可查看，暂不可编辑。请刷新收款版本重试。
    </p>
    <p
      v-else-if="current && typesState === 'ready' && !typeOptions.length"
      class="alert"
      role="status"
    >
      当前没有可用的结算收款方式；原资料仍可查看，暂不可新增或修改。
    </p>
    <template v-if="current">
      <p
        v-if="current.state === 'configured'"
        data-testid="settlement-recipient-current"
      >
        当前收款：{{
          current.type === "alipay"
            ? "支付宝"
            : current.type === "wxpay"
              ? "微信"
              : "银行卡"
        }}
        {{ current.account_masked }} · 版本 {{ current.config_id
        }}<span v-if="current.app_id"> · AppID {{ current.app_id }}</span>
      </p>
      <p v-else class="alert" role="status">
        尚未配置收款资料；{{
          canEdit ? "请先配置。" : "请联系有商户管理权限的管理员配置。"
        }}历史缺失快照的申请不能出款。
      </p>
      <div v-if="!editing" class="row-actions">
        <button
          ref="editTrigger"
          type="button"
          :disabled="!canEdit"
          @click="edit"
        >
          {{
            current.state === "missing" ? "配置收款资料" : "修改收款资料"
          }}</button
        ><button
          ref="refreshTrigger"
          type="button"
          :disabled="loading"
          @click="refresh"
        >
          刷新收款版本
        </button>
      </div>
      <template v-else>
        <p class="muted">
          请完整输入新收款资料。此操作只更新后续申请，不更改旧快照，也不会汇款。
        </p>
        <div class="recipient-form">
          <KitField label="收款方式" data-recipient-field="type">
            <KitSelect
              v-model="draft.type"
              :options="typeOptions"
              aria-label="收款方式"
            />
          </KitField>
          <KitField label="收款账号" data-recipient-field="account" required>
            <KitInput
              v-model="draft.account"
              :maxlength="128"
              autocomplete="off"
              aria-label="收款账号"
              :error="!!fieldErrors.account"
            />
            <span
              v-if="fieldErrors.account"
              class="recipient-field-error"
              role="alert"
              >{{ fieldErrors.account }}</span
            >
          </KitField>
          <KitField label="真实姓名" data-recipient-field="name" required>
            <KitInput
              v-model="draft.name"
              :maxlength="128"
              autocomplete="off"
              aria-label="真实姓名"
              :error="!!fieldErrors.name"
            />
            <span
              v-if="fieldErrors.name"
              class="recipient-field-error"
              role="alert"
              >{{ fieldErrors.name }}</span
            >
          </KitField>
          <KitField
            v-if="draft.type === 'wxpay'"
            label="收款所属 AppID"
            data-recipient-field="app_id"
            required
          >
            <KitInput
              v-model="draft.app_id"
              :maxlength="128"
              autocomplete="off"
              aria-label="收款所属 AppID"
              :error="!!fieldErrors.app_id"
            />
            <span class="muted recipient-field-help"
              >收款账号应为该应用下的OpenID；AppID与OpenID必须属于同一应用。不清楚时请向平台管理员获取，不能填写微信号。</span
            >
            <span
              v-if="fieldErrors.app_id"
              class="recipient-field-error"
              role="alert"
              >{{ fieldErrors.app_id }}</span
            >
          </KitField>
          <KitField
            v-if="draft.type === 'bank'"
            label="开户银行（按渠道要求）"
            data-recipient-field="bank_code"
          >
            <KitSelect
              v-if="!manualBankCode"
              v-model="draft.bank_code"
              :options="recipientBankOptions"
              aria-label="选择开户银行"
            />
            <KitInput
              v-else
              v-model="draft.bank_code"
              :maxlength="32"
              autocomplete="off"
              aria-label="银行代码"
              inputmode="numeric"
              :error="!!fieldErrors.bank_code"
            />
            <span class="muted recipient-field-help"
              >列表复用平台已有微信银行编号。仅通道要求银行代码时选择；其他渠道代码请向平台管理员获取。</span
            >
            <button
              type="button"
              class="link-button recipient-bank-mode"
              @click="
                manualBankCode = !manualBankCode;
                draft.bank_code = '';
                fieldErrors.bank_code = undefined;
              "
            >
              {{
                manualBankCode
                  ? "使用已有银行编号"
                  : "按管理员提供的代码手动填写"
              }}
            </button>
            <span
              v-if="fieldErrors.bank_code"
              class="recipient-field-error"
              role="alert"
              >{{ fieldErrors.bank_code }}</span
            >
          </KitField>
        </div>
        <div class="row-actions">
          <button type="button" class="pa-btn" @click="cancel">
            取消修改收款资料</button
          ><button
            type="button"
            class="pa-btn pa-btn--primary"
            :disabled="!canEdit"
            @click="save"
          >
            {{ saving ? "正在保存…" : "保存收款版本" }}
          </button>
        </div>
      </template>
    </template>
    <button
      v-else-if="!loading && (!admin || merchantID)"
      type="button"
      @click="refresh"
    >
      重试读取收款配置
    </button>
    <p v-else-if="admin && !merchantID" class="muted">
      输入付款商户 ID 后读取其收款配置。
    </p>
  </fieldset>
</template>
<style scoped>
.recipient-field-error {
  color: var(--danger-text, #c62828);
  font-size: 12px;
}
.recipient-field-help {
  display: block;
  font-size: 12px;
  font-weight: normal;
  line-height: 1.5;
}
.recipient-bank-mode {
  justify-self: start;
  text-align: left;
  white-space: normal;
}
.recipient-form {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
  gap: var(--space-3, 12px) var(--space-4, 16px);
  margin: var(--space-3, 12px) 0;
}
.settlement-recipient {
  border: 0;
  padding: 0;
  margin: 0;
}
</style>
