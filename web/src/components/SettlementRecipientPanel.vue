<script setup lang="ts">
import { ref, computed, watch, onBeforeUnmount } from "vue";
import { ApiClient, ApiError } from "../api";
import {
  readSettlementRecipient,
  recipientConfigurationBody,
  type SettlementRecipient,
  type RecipientDraft,
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
const current = ref<SettlementRecipient | null>(null),
  loading = ref(false),
  saving = ref(false),
  editing = ref(false),
  error = ref(""),
  notice = ref("");
// Settlement account types the platform allows now (settle_* switches). Types
// turned off are hidden from the 收款方式 picker; the server still refuses them.
const allowedTypes = ref<string[]>(["alipay", "wxpay", "bank"]);
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
  controller: AbortController | undefined;
function clear() {
  epoch++;
  controller?.abort();
  current.value = null;
  draft.value = empty();
  editing.value = false;
  loading.value = false;
  error.value = "";
  notice.value = "";
  emit("change", null);
}
async function load() {
  clear();
  if (props.admin && !/^[1-9]\d*$/.test(props.merchantID || "")) return;
  const generation = epoch;
  loading.value = true;
  controller = new AbortController();
  try {
    const q = props.admin
      ? "?merchant_id=" + encodeURIComponent(props.merchantID!)
      : "";
    const raw = await props.client.request(
      "/workflows/settlement-recipient" + q,
      { signal: controller.signal },
    );
    if (generation !== epoch) return;
    try {
      const types = await props.client.request<{ settlement?: string[] }>(
        "/workflows/settlement-recipient/allowed-types",
        { signal: controller.signal },
      );
      if (generation !== epoch) return;
      if (Array.isArray(types?.settlement) && types.settlement.length)
        allowedTypes.value = types.settlement;
    } catch {
      // Allowed-types is advisory for the form; the server still enforces on save.
    }
    current.value = readSettlementRecipient(raw);
    emit("change", current.value);
  } catch (e) {
    if (generation === epoch)
      error.value = e instanceof Error ? e.message : "收款配置读取失败";
  } finally {
    if (generation === epoch) loading.value = false;
  }
}
function edit() {
  draft.value = empty();
  if (
    !allowedTypes.value.includes(draft.value.type) &&
    allowedTypes.value.length
  )
    draft.value.type = allowedTypes.value.includes("bank")
      ? allowedTypes.value.find((t) => t !== "qqpay") || allowedTypes.value[0]!
      : allowedTypes.value[0]!;
  editing.value = true;
  notice.value = "";
  emit("change", null);
}
function cancel() {
  draft.value = empty();
  editing.value = false;
  emit("change", current.value);
}
async function save() {
  if (saving.value || !current.value) return;
  error.value = "";
  const generation = epoch;
  try {
    const body = recipientConfigurationBody(
      draft.value,
      current.value,
      props.admin ? props.merchantID : undefined,
    );
    saving.value = true;
    const raw = await props.client.request("/workflows/settlement-recipient", {
      method: "POST",
      body,
    });
    if (generation !== epoch) return;
    current.value = readSettlementRecipient(raw);
    draft.value = empty();
    editing.value = false;
    notice.value = "已记录新收款版本；旧申请的收款快照不变";
    emit("change", current.value);
  } catch (e) {
    if (generation !== epoch) return;
    error.value =
      e instanceof Error ? e.message : "收款资料保存结果未确认，请刷新核对";
    if (e instanceof ApiError && e.status === 409) {
      await load();
      error.value = "收款版本已变化，请核对当前版本后重新编辑";
    }
  } finally {
    saving.value = false;
  }
}
watch(() => [props.admin, props.merchantID], load, { immediate: true });
watch(
  () => draft.value.type,
  () => {
    draft.value.bank_code = "";
    draft.value.app_id = "";
  },
);
onBeforeUnmount(clear);
</script>
<template>
  <fieldset class="settlement-recipient" :disabled="disabled || saving">
    <legend>结算收款资料</legend>
    <p v-if="loading" role="status">正在读取收款配置…</p>
    <p v-if="error" class="alert error" role="alert">{{ error }}</p>
    <p v-if="notice" class="muted" role="status">{{ notice }}</p>
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
        尚未配置收款资料；请先配置。历史缺失快照的申请不能出款。
      </p>
      <div v-if="!editing" class="row-actions">
        <button type="button" @click="edit">
          {{
            current.state === "missing" ? "配置收款资料" : "修改收款资料"
          }}</button
        ><button type="button" @click="load">刷新收款版本</button>
      </div>
      <template v-else>
        <p class="muted">
          请完整输入新收款资料。此操作只更新后续申请，不更改旧快照，也不会汇款。
        </p>
        <div class="recipient-form">
          <KitField label="收款方式">
            <KitSelect
              v-model="draft.type"
              :options="typeOptions"
              aria-label="收款方式"
            />
          </KitField>
          <KitField label="收款账号">
            <KitInput
              v-model="draft.account"
              :maxlength="128"
              autocomplete="off"
              aria-label="收款账号"
            />
          </KitField>
          <KitField label="真实姓名">
            <KitInput
              v-model="draft.name"
              :maxlength="128"
              autocomplete="off"
              aria-label="真实姓名"
            />
          </KitField>
          <KitField v-if="draft.type === 'wxpay'" label="收款所属 AppID">
            <KitInput
              v-model="draft.app_id"
              :maxlength="128"
              autocomplete="off"
              aria-label="收款所属 AppID"
            />
          </KitField>
          <KitField v-if="draft.type === 'bank'" label="银行代码（按渠道要求）">
            <KitInput
              v-model="draft.bank_code"
              :maxlength="32"
              autocomplete="off"
              aria-label="银行代码"
            />
          </KitField>
        </div>
        <div class="row-actions">
          <button type="button" class="pa-btn" @click="cancel">
            取消修改收款资料</button
          ><button type="button" class="pa-btn pa-btn--primary" @click="save">
            {{ saving ? "正在保存…" : "保存收款版本" }}
          </button>
        </div>
      </template>
    </template>
    <button
      v-else-if="!loading && (!admin || merchantID)"
      type="button"
      @click="load"
    >
      重试读取收款配置
    </button>
    <p v-else-if="admin && !merchantID" class="muted">
      输入付款商户 ID 后读取其收款配置。
    </p>
  </fieldset>
</template>
<style scoped>
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
