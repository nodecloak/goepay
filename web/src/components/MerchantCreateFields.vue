<script setup lang="ts">
import {
  computed,
  ref,
  onBeforeUnmount,
  onMounted,
  nextTick,
  watch,
} from "vue";
import type { ApiClient } from "../api";
import type { ResourceField } from "../resources";
import ResourceFields from "./ResourceFields.vue";
import MerchantGroupPicker from "./MerchantGroupPicker.vue";

const props = defineProps<{
  client: ApiClient;
  fields: ResourceField[];
  values: Record<string, unknown>;
  errors: Record<string, string>;
  disabled: boolean;
}>();
const emit = defineEmits<{
  change: [name: string, value: unknown];
  "settlement-validity": [message: string];
}>();
const root = ref<HTMLElement>();
const account = computed(() => props.values.creation_mode !== "api");
const allowedTypes = ref<string[] | null>(null),
  recipientWritePermission = ref<boolean | null>(null),
  typeError = ref(""),
  typesLoading = ref(false);
const hasInitialRecipient = computed(() =>
  ["settlement_account", "settlement_name", "settlement_app_id"].some((key) =>
    String(props.values[key] || "").trim(),
  ),
);
const settlementIssue = computed(() => {
  if (!hasInitialRecipient.value) return "";
  if (typesLoading.value) return "正在读取当前用户组的结算方式，请稍候。";
  if (recipientWritePermission.value !== true)
    return "初始结算资料配置权限未核实或当前只读，请重新读取或清空初始结算资料。";
  if (typeError.value || !allowedTypes.value)
    return "结算方式尚未核实，请重新读取或清空初始结算资料。";
  if (!allowedTypes.value.includes(String(props.values.settlement_type || "")))
    return "当前用户组不允许该结算方式，请重新选择用户组、收款方式或清空初始结算资料。";
  return "";
});
watch(settlementIssue, (message) => emit("settlement-validity", message), {
  immediate: true,
  flush: "sync",
});
let typesEpoch = 0,
  typesController: AbortController | undefined,
  alive = true;
async function loadTypes() {
  if (!props.fields.some((f) => f.name === "settlement_type")) return;
  const epoch = ++typesEpoch,
    client = props.client,
    csrf = client.csrfToken;
  typesController?.abort();
  const controller = new AbortController();
  typesController = controller;
  typesLoading.value = true;
  recipientWritePermission.value = null;
  typeError.value = "";
  try {
    const groupQuery = props.fields.some((f) => f.name === "group_id")
      ? "?group_id=" + encodeURIComponent(String(props.values.group_id || "0"))
      : "";
    const data = await client.request<{
      settlement: unknown;
      can_write?: unknown;
    }>("/workflows/settlement-recipient/allowed-types" + groupQuery, {
      signal: controller.signal,
    });
    if (
      !alive ||
      epoch !== typesEpoch ||
      client !== props.client ||
      csrf !== client.csrfToken
    )
      return;
    const types = data.settlement;
    recipientWritePermission.value =
      typeof data.can_write === "boolean" ? data.can_write : null;
    if (
      !Array.isArray(types) ||
      types.some(
        (v) =>
          typeof v !== "string" ||
          !["alipay", "wxpay", "qqpay", "bank"].includes(v),
      ) ||
      new Set(types).size !== types.length
    )
      throw Error();
    allowedTypes.value = types;
    if (
      !types.includes(String(props.values.settlement_type || "")) &&
      !props.values.settlement_account &&
      !props.values.settlement_name
    )
      emit(
        "change",
        "settlement_type",
        ["alipay", "wxpay", "bank"].find((t) => types.includes(t)) || "",
      );
  } catch {
    if (alive && epoch === typesEpoch && !controller.signal.aborted)
      typeError.value = "结算方式读取失败，请重试；草稿已保留。";
  } finally {
    if (alive && epoch === typesEpoch) typesLoading.value = false;
  }
}
watch(
  () => [props.client, props.client.csrfToken, props.values.group_id],
  () => {
    allowedTypes.value = null;
    recipientWritePermission.value = null;
    void loadTypes();
  },
  { immediate: true, flush: "sync" },
);
onBeforeUnmount(() => {
  alive = false;
  ++typesEpoch;
  typesController?.abort();
});
const labels: Record<string, string> = {
  phone: "手机号(登录账号)",
  email: "邮箱(登录账号)",
  login_password: "登录密码",
  enabled: "商户状态",
  pay: "支付权限",
};
function fields(keys: string[]): ResourceField[] {
  return keys.flatMap<ResourceField>((name) => {
    const field = props.fields.find((f) => f.name === name);
    if (!field) return [];
    if (name.startsWith("settlement_")) {
      const options =
        props.fields
          .find((f) => f.name === "settlement_type")
          ?.options?.filter((o) =>
            allowedTypes.value?.includes(
              String(typeof o === "string" ? o : o.value),
            ),
          ) || [];
      if (!options.length && !hasInitialRecipient.value) return [];
      if (name === "settlement_type") return [{ ...field, options }];
    }
    if (name === "email")
      return [
        {
          ...field,
          label: account.value ? labels[name] : "联系邮箱",
          max_length: account.value ? 100 : field.max_length,
        },
      ];
    if (name === "phone" && !account.value)
      return [{ ...field, label: "联系手机" }];
    if (
      name === "settlement_app_id" &&
      props.values.settlement_type !== "wxpay"
    )
      return [];
    if (name === "enabled")
      return [
        {
          ...field,
          type: "select",
          label: "商户状态",
          options: [
            { value: "true", label: "1_正常" },
            { value: "false", label: "0_封禁" },
          ],
        },
      ];
    if (name === "pay")
      return [
        {
          ...field,
          label: "支付权限",
          options: [
            { value: "1", label: "1_开启" },
            { value: "0", label: "0_关闭" },
            { value: "2", label: "2_待审核" },
          ],
        },
      ];
    return [{ ...field, label: labels[name] || field.label }];
  });
}
function change(name: string, value: unknown) {
  if (name.startsWith("settlement_") && recipientWritePermission.value !== true)
    return;
  if (name === "settlement_type" && value !== props.values.settlement_type) {
    for (const key of [
      "settlement_account",
      "settlement_name",
      "settlement_app_id",
    ])
      emit("change", key, "");
  }
  emit(
    "change",
    name,
    name === "enabled" ? value === true || value === "true" : value,
  );
}
function clearInitialRecipient() {
  for (const key of [
    "settlement_account",
    "settlement_name",
    "settlement_app_id",
  ])
    emit("change", key, "");
  emit("change", "settlement_type", allowedTypes.value?.[0] || "");
}
function mode(event: Event) {
  const api = (event.target as HTMLInputElement).checked;
  emit("change", "creation_mode", api ? "api" : "account");
  for (const name of ["login_username", "login_password", "id", "name"])
    emit("change", name, "");
}
function randomPassword() {
  const bytes = new Uint8Array(20);
  crypto.getRandomValues(bytes);
  const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789";
  emit(
    "change",
    "login_password",
    Array.from(bytes, (b) => alphabet[b % alphabet.length]).join(""),
  );
}
const basic = computed(() =>
  fields(account.value ? ["phone", "email"] : ["id", "name", "phone", "email"]),
);
function focusField(errorsOnly = false) {
  if (!alive || props.disabled || !root.value?.isConnected) return;
  const fields = Array.from(
    root.value.querySelectorAll<HTMLElement>(
      'input[id^="field-"],select[id^="field-"],textarea[id^="field-"]',
    ),
  );
  fields
    .find(
      (field) =>
        !field.matches(":disabled") &&
        !field.closest("details:not([open])") &&
        (!errorsOnly || props.errors[field.id.slice(6)]),
    )
    ?.focus();
}
onMounted(async () => {
  await nextTick();
  focusField(Object.keys(props.errors).length > 0);
});
watch(
  () => props.errors,
  async (errors) => {
    if (!Object.keys(errors).length) return;
    await nextTick();
    focusField(true);
  },
  { flush: "post" },
);
</script>
<template>
  <fieldset ref="root" :disabled="disabled" class="merchant-create-fields">
    <section class="merchant-editor-group" aria-label="基本信息">
      <h3>基本信息</h3>
      <ResourceFields
        :fields="basic"
        :values="values"
        :errors="errors"
        :editing="false"
        @change="change"
      />
      <template v-if="account">
        <label for="field-login_password"
          ><span class="field-label field-label--req">登录密码</span></label
        >
        <div class="password-row">
          <input
            id="field-login_password"
            type="password"
            :value="values.login_password"
            autocomplete="new-password"
            minlength="12"
            maxlength="1024"
            :aria-required="true"
            :aria-invalid="!!errors.login_password"
            @input="
              change(
                'login_password',
                ($event.target as HTMLInputElement).value,
              )
            "
          /><button type="button" @click="randomPassword">随机</button>
        </div>
        <small v-if="errors.login_password" role="alert" class="field-error">{{
          errors.login_password
        }}</small>
        <p class="muted">
          手机号或邮箱至少填写一项。两项均填写时，使用手机号登录；登录密码至少12个字符。
        </p>
      </template>
      <MerchantGroupPicker
        v-if="props.fields.some((f) => f.name === 'group_id')"
        :client="client"
        :value="values.group_id"
        :disabled="disabled"
        :error="errors.group_id"
        :creating="true"
        @change="change('group_id', $event)"
      />
      <ResourceFields
        :fields="fields(['qq', 'url'])"
        :values="values"
        :errors="errors"
        :editing="false"
        @change="change"
      />
    </section>
    <p v-if="typesLoading" role="status">正在读取结算方式…</p>
    <p
      v-else-if="allowedTypes && recipientWritePermission !== true"
      role="status"
      class="muted"
    >
      初始结算资料配置权限未核实或当前只读，可先创建商户，之后由有权限的管理员配置。
    </p>
    <div v-if="settlementIssue" role="alert" class="field-error">
      {{ settlementIssue
      }}<button
        type="button"
        :disabled="disabled"
        @click="clearInitialRecipient"
      >
        清空初始结算资料
      </button>
    </div>
    <div v-if="typeError" role="alert">
      {{ typeError
      }}<button type="button" :disabled="typesLoading" @click="loadTypes">
        重新读取结算方式
      </button>
    </div>
    <p v-if="allowedTypes && !fields(['settlement_type']).length" class="muted">
      平台或当前用户组暂未开启结算方式，商户创建后可再配置收款资料。
    </p>
    <fieldset
      v-if="fields(['settlement_type']).length"
      class="merchant-editor-group"
      aria-label="结算信息"
      :disabled="
        recipientWritePermission !== true || typesLoading || !!typeError
      "
    >
      <h3>结算信息</h3>
      <ResourceFields
        :fields="
          fields([
            'settlement_type',
            'settlement_account',
            'settlement_name',
            'settlement_app_id',
          ])
        "
        :values="values"
        :errors="errors"
        :editing="false"
        @change="change"
      />
    </fieldset>
    <section class="merchant-editor-group" aria-label="功能开关">
      <h3>功能开关</h3>
      <ResourceFields
        :fields="fields(['fee_mode', 'enabled', 'pay', 'settle'])"
        :values="values"
        :errors="errors"
        :editing="false"
        @change="change"
      />
    </section>
    <details>
      <summary>高级选项</summary>
      <label
        ><input
          type="checkbox"
          :checked="!account"
          @change="mode"
        />仅API（不创建后台登录账号）</label
      >
      <p class="muted">
        仅API模式使用明确的商户编号和名称，支付密钥由系统生成。
      </p>
    </details>
  </fieldset>
</template>
<style scoped>
.merchant-create-fields {
  border: 0;
  padding: 0;
  margin: 0;
  min-width: 0;
}
.merchant-editor-group {
  border: 0;
  padding: 0;
  min-width: 0;
  margin-inline: 0;
  margin-bottom: 18px;
}
.merchant-editor-group h3 {
  color: var(--primary-text);
  font-size: 15px;
  margin: 12px 0;
}
.password-row {
  display: flex;
  gap: 8px;
  min-width: 0;
}
.password-row input {
  flex: 1;
  min-width: 0;
}
.merchant-create-fields details {
  margin-top: 16px;
  font-size: 13px;
}
.merchant-create-fields summary {
  cursor: pointer;
}
.field-error {
  display: block;
  color: var(--danger);
}
</style>
