<script setup lang="ts">
import { computed, nextTick, ref, useId, watch } from "vue";
import type { ResourceField } from "../resources";
import { readonly } from "../resources";
import { merchantRecipientLabel } from "../pro-console-fields";
import ResourceFields from "./ResourceFields.vue";
import MerchantGroupPicker from "./MerchantGroupPicker.vue";
import AdminMerchantCredentials from "./AdminMerchantCredentials.vue";
import MerchantSignaturePolicy from "./MerchantSignaturePolicy.vue";
import type { SignaturePolicy } from "../merchant-signature-policy";
import type { ApiClient } from "../api";
const props = defineProps<{
  client: ApiClient;
  fields: ResourceField[];
  values: Record<string, unknown>;
  errors: Record<string, string>;
  editing: boolean;
  disabled: boolean;
  recipient?: unknown;
  canEditRecipient: boolean;
  merchantID?: string;
  canManageCredentials?: boolean;
  signaturePolicy?: unknown;
}>();
const emit = defineEmits<{
  change: [name: string, value: unknown];
  recipient: [];
  credentialBusy: [merchantID: string, value: boolean];
  credentialDraftsDiscard: [merchantID: string];
  signaturePolicyObserved: [state: SignaturePolicy];
  legacyAlgorithmDiscard: [merchantID: string, algorithm: "MD5" | "RSA"];
}>();
const tab = ref("basic"),
  root = ref<HTMLElement>(),
  id = useId();
const apiNames = new Set([
    "api_key",
    "signature_algorithm",
    "rsa_public_key",
    "keytype",
  ]),
  statusNames = new Set(["enabled", "pay"]),
  payoutNames = new Set(["transfer_api_enabled", "transfer_rate", "settle"]);
const apiFields = computed(() =>
    props.fields.filter(
      (f) =>
        apiNames.has(f.name) &&
        !(
          props.editing &&
          (props.signaturePolicy === 0 || props.signaturePolicy === 1) &&
          f.name === "signature_algorithm"
        ),
    ),
  ),
  tabs = computed(() => [
    { name: "basic", label: "基本信息" },
    ...(apiFields.value.some((f) => !readonly(f, props.editing))
      ? [{ name: "api", label: "密钥信息" }]
      : []),
  ]);
const groups = computed(() =>
  [
    {
      name: "basic",
      label: "基本资料",
      fields: props.fields.filter(
        (f) =>
          !apiNames.has(f.name) &&
          !statusNames.has(f.name) &&
          !payoutNames.has(f.name),
      ),
    },
    {
      name: "payout",
      label: "结算与代付",
      fields: props.fields.filter((f) => payoutNames.has(f.name)),
    },
    {
      name: "status",
      label: "状态设置",
      fields: props.fields.filter((f) => statusNames.has(f.name)),
    },
  ].filter((g) => g.fields.some((f) => !readonly(f, props.editing))),
);
function groupPickerFields(fields: ResourceField[]) {
  return fields.some(
    (f) => f.name === "group_id" && !readonly(f, props.editing),
  );
}
function ordinaryFields(fields: ResourceField[]) {
  return groupPickerFields(fields)
    ? fields.filter(
        (f) => !["group_id", "confirm_group_assignment"].includes(f.name),
      )
    : fields;
}
watch(
  () => props.errors,
  async (errors) => {
    const first = props.fields.find(
      (f) => errors[f.name] && !readonly(f, props.editing),
    );
    if (!first) return;
    tab.value = apiNames.has(first.name) ? "api" : "basic";
    await nextTick();
    const field = Array.from(
      root.value?.querySelectorAll<HTMLElement>("[id]") || [],
    ).find((e) => e.id === "field-" + first.name);
    field?.focus();
  },
  { flush: "post" },
);
function keydown(event: KeyboardEvent) {
  if (
    props.disabled ||
    !["ArrowLeft", "ArrowRight", "Home", "End"].includes(event.key)
  )
    return;
  event.preventDefault();
  const index = tabs.value.findIndex((t) => t.name === tab.value),
    direction = event.key === "ArrowLeft" ? -1 : 1;
  const target =
    event.key === "Home"
      ? 0
      : event.key === "End"
        ? tabs.value.length - 1
        : (index + direction + tabs.value.length) % tabs.value.length;
  tab.value = tabs.value[target]!.name;
  void nextTick(() =>
    root.value
      ?.querySelector<HTMLElement>('[role="tab"][aria-selected="true"]')
      ?.focus(),
  );
}
</script>
<template>
  <div ref="root" class="merchant-editor">
    <div
      role="tablist"
      aria-label="商户信息分类"
      class="merchant-editor-tabs"
      @keydown="keydown"
    >
      <button
        v-for="t in tabs"
        :id="id + '-' + t.name + '-tab'"
        :key="t.name"
        type="button"
        role="tab"
        :disabled="disabled"
        :aria-selected="tab === t.name"
        :aria-controls="id + '-' + t.name + '-panel'"
        :tabindex="tab === t.name ? 0 : -1"
        @click="tab = t.name"
      >
        {{ t.label }}
      </button>
    </div>
    <fieldset :disabled="disabled">
      <div
        v-show="tab === 'basic'"
        :id="id + '-basic-panel'"
        role="tabpanel"
        :aria-labelledby="id + '-basic-tab'"
      >
        <p class="muted merchant-editor-hint">
          手机、邮箱和网站是联系资料。登录账号及已审核支付域名分别管理。
        </p>

        <section
          v-for="g in groups"
          :key="g.name"
          :aria-label="g.label"
          class="merchant-editor-group"
        >
          <h3>{{ g.label }}</h3>
          <ResourceFields
            :fields="ordinaryFields(g.fields)"
            :values="values"
            :errors="errors"
            :editing="editing"
            @change="(name, value) => emit('change', name, value)"
          />
          <div v-if="groupPickerFields(g.fields)" class="merchant-group-fields">
            <MerchantGroupPicker
              :client="client"
              :value="values.group_id"
              :disabled="disabled"
              :error="errors.group_id"
              @change="(value) => emit('change', 'group_id', value)"
            />
            <ResourceFields
              class="merchant-group-confirmation"
              :fields="
                g.fields.filter((f) => f.name === 'confirm_group_assignment')
              "
              :values="values"
              :errors="errors"
              :editing="editing"
              @change="(name, value) => emit('change', name, value)"
            />
          </div>
        </section>
        <section
          v-if="editing && canEditRecipient"
          aria-label="结算收款信息"
          class="merchant-editor-group"
        >
          <h3>结算收款信息</h3>
          <p>{{ merchantRecipientLabel(recipient) }}</p>
          <button type="button" @click="emit('recipient')">
            修改结算收款资料
          </button>
        </section>
      </div>
      <div
        v-if="tabs.length > 1"
        v-show="tab === 'api'"
        :id="id + '-api-panel'"
        role="tabpanel"
        :aria-labelledby="id + '-api-tab'"
      >
        <p class="muted merchant-editor-hint">
          已有密钥不回显；编辑时留空保留原值。修改后请同步商户支付配置。
        </p>
        <p v-if="editing && (signaturePolicy === 0 || signaturePolicy === 1)">
          当前签名方式：{{
            signaturePolicy === 0 ? "MD5+RSA兼容" : "仅RSA"
          }}。历史算法不决定当前方式。
        </p>
        <ResourceFields
          :fields="apiFields"
          :values="values"
          :errors="errors"
          :editing="editing"
          @change="(name, value) => emit('change', name, value)"
        />
        <AdminMerchantCredentials
          v-if="editing && canManageCredentials && merchantID && tab === 'api'"
          :key="merchantID"
          :client="client"
          :merchant-i-d="merchantID"
          :disabled="disabled"
          @busy="(id, value) => emit('credentialBusy', id, value)"
          @discard-drafts="(id) => emit('credentialDraftsDiscard', id)"
        />
        <MerchantSignaturePolicy
          v-if="editing && canManageCredentials && merchantID && tab === 'api'"
          :key="merchantID + '-policy'"
          :client="client"
          :merchant-i-d="merchantID"
          :reload="0"
          :disabled="disabled"
          @busy="(id, value) => emit('credentialBusy', id, value)"
          @observed="(state) => emit('signaturePolicyObserved', state)"
          @discard-legacy-algorithm="
            (id, algorithm) => emit('legacyAlgorithmDiscard', id, algorithm)
          "
        />
      </div>
    </fieldset>
  </div>
</template>
<style scoped>
.merchant-editor-tabs {
  display: flex;
  border-bottom: 1px solid var(--border, #e5e7eb);
  gap: 24px;
  margin: 12px 0 18px;
}
.merchant-editor-tabs button {
  border: 0;
  border-radius: 0;
  background: transparent;
  color: var(--muted, #64748b);
  padding: 10px 0 12px;
  margin-bottom: -1px;
  border-bottom: 2px solid transparent;
}
.merchant-editor-tabs button[aria-selected="true"] {
  color: var(--primary, #409eff);
  border-bottom-color: currentColor;
}
.merchant-editor-tabs button:focus-visible {
  outline: 2px solid var(--primary, #409eff);
  outline-offset: 3px;
}
fieldset {
  margin: 0;
  padding: 0;
  border: 0;
  min-width: 0;
}
.merchant-editor-group {
  border: 0;
  margin: 0 0 18px;
  padding: 0;
}
.merchant-editor-group h3 {
  font-size: 14px;
  font-weight: 600;
  color: var(--primary, #409eff);
  margin: 0 0 10px;
}
.merchant-editor-hint {
  font-size: 13px;
  margin: 0 0 16px;
  line-height: 1.7;
}
.merchant-group-fields {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 0 22px;
  align-items: start;
}
.merchant-group-fields :deep(.merchant-group-confirmation) {
  grid-template-columns: minmax(0, 1fr);
}
@media (max-width: 640px) {
  .merchant-group-fields {
    grid-template-columns: minmax(0, 1fr);
  }
}
.merchant-editor-group :deep(.resource-fields) {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 0 22px;
}
.merchant-editor-group :deep(.resource-fields label) {
  margin: 10px 0;
  min-width: 0;
}
@media (max-width: 640px) {
  .merchant-editor-group :deep(.resource-fields) {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
