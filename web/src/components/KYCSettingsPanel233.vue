<script setup lang="ts">
import { computed, onBeforeUnmount, ref, toRaw, watch } from "vue";
import { ApiClient, ApiError } from "../api";
import { useSettingsDraft212 } from "../settings-drafts212";
import {
  certificationModes233,
  emptyCredentialEdits233,
  parseConfig233,
  type CertificationConfig233,
  type CredentialKey233,
} from "../kyc233";
import KitSelect from "./KitSelect.vue";
import KitSwitch from "./KitSwitch.vue";
import KitModal from "./KitModal.vue";
import CredentialActionControl from "./CredentialActionControl.vue";

const props = withDefaults(
  defineProps<{
    client: ApiClient;
    kind: "certification" | "ocr";
    ownSession: boolean;
    adminClass?: string;
    disabled?: boolean;
    active?: boolean;
  }>(),
  { active: true },
);
const emit = defineEmits<{ busy: [boolean] }>();
const state = ref<CertificationConfig233>(),
  mode = ref("0"),
  channel = ref(""),
  corporate = ref(false),
  force = ref(false),
  money = ref("0.00"),
  provider = ref("");
const edits = ref(emptyCredentialEdits233()),
  busy = ref(false),
  loading = ref(false),
  uncertain = ref(false),
  staleDraft = ref(false),
  confirming = ref(false),
  error = ref(""),
  notice = ref(""),
  showOtherCredentials = ref(false);
const title = computed(() =>
  props.kind === "ocr" ? "OCR文字识别配置" : "实名认证配置",
);
const allowed = computed(
  () =>
    props.client.area === "admin" &&
    props.ownSession &&
    props.adminClass === "super" &&
    !!props.client.csrfToken,
);
const blocked = computed(
  () => busy.value || loading.value || uncertain.value || props.disabled,
);
const dirty = computed(
  () =>
    !!state.value &&
    (mode.value !== String(state.value.cert_open) ||
      channel.value !== state.value.cert_channel ||
      corporate.value !== state.value.cert_corpopen ||
      force.value !== state.value.cert_force ||
      money.value !== state.value.cert_money ||
      provider.value !== state.value.ocr_type ||
      Object.values(edits.value).some((e) => e.action !== "keep" || !!e.value)),
);
const { confirmDiscard } = useSettingsDraft212(
  title.value,
  () => dirty.value || uncertain.value,
);
const credentialLabels: Record<CredentialKey233, string> = {
  cert_appcode: "手机号三要素AppCode",
  cert_appcode2: "企业信息校验AppCode",
  cert_qcloudid: "腾讯云SecretId",
  cert_qcloudkey: "腾讯云SecretKey",
  cert_qcloudruleid: "腾讯云RuleId",
  cert_aliyunid: "阿里云AccessKey ID",
  cert_aliyunkey: "阿里云AccessKey Secret",
  cert_aliyunsceneid: "阿里云场景ID",
  ocr_aliyunid: "OCR阿里云AccessKey ID",
  ocr_aliyunkey: "OCR阿里云AccessKey Secret",
  ocr_baiduid: "百度云API Key",
  ocr_baidukey: "百度云Secret Key",
};
const requiredKeys = computed<CredentialKey233[]>(() =>
  props.kind === "ocr"
    ? provider.value === "aliyun"
      ? ["ocr_aliyunid", "ocr_aliyunkey"]
      : provider.value === "baidu"
        ? ["ocr_baiduid", "ocr_baidukey"]
        : []
    : [
        ...(mode.value === "2"
          ? (["cert_appcode"] as CredentialKey233[])
          : mode.value === "4"
            ? ([
                "cert_qcloudid",
                "cert_qcloudkey",
                "cert_qcloudruleid",
              ] as CredentialKey233[])
            : mode.value === "5"
              ? ([
                  "cert_aliyunid",
                  "cert_aliyunkey",
                  "cert_aliyunsceneid",
                ] as CredentialKey233[])
              : []),
        ...(corporate.value && mode.value !== "0"
          ? (["cert_appcode2"] as CredentialKey233[])
          : []),
      ],
);
const visibleKeys = computed<CredentialKey233[]>(() =>
  showOtherCredentials.value
    ? (Object.keys(credentialLabels).filter((key) =>
        key.startsWith(props.kind === "ocr" ? "ocr_" : "cert_"),
      ) as CredentialKey233[])
    : requiredKeys.value,
);
let alive = true,
  epoch = 0,
  controller: AbortController | undefined;
function context() {
  return [
    toRaw(props.client),
    props.client.area,
    props.client.csrfToken,
    props.ownSession,
    props.adminClass,
    props.kind,
  ] as const;
}
function current(run: number, c: readonly unknown[]) {
  return (
    alive &&
    epoch === run &&
    allowed.value &&
    c.every((v, i) => v === context()[i])
  );
}
function begin() {
  const run = ++epoch,
    c = context();
  controller?.abort();
  controller = new AbortController();
  return { run, c, signal: controller.signal };
}
function clearPrivate() {
  edits.value = emptyCredentialEdits233();
  confirming.value = false;
}
function setCredentialAction(
  key: CredentialKey233,
  action: "keep" | "replace" | "clear",
) {
  if (blocked.value || confirming.value || staleDraft.value) return;
  if (action === "clear" && requiredKeys.value.includes(key)) return;
  edits.value[key].action = action;
  if (action !== "replace") edits.value[key].value = "";
}
function apply(value: unknown) {
  state.value = parseConfig233(value);
  mode.value = String(state.value.cert_open);
  channel.value = state.value.cert_channel;
  corporate.value = state.value.cert_corpopen;
  force.value = state.value.cert_force;
  money.value = state.value.cert_money;
  provider.value = state.value.ocr_type;
  uncertain.value = false;
  staleDraft.value = false;
  clearPrivate();
}
async function load(forceRead = false) {
  if (
    !props.active ||
    !allowed.value ||
    busy.value ||
    (!forceRead && !(await confirmDiscard()))
  )
    return;
  const { run, c, signal } = begin();
  loading.value = true;
  error.value = "";
  notice.value = "";
  try {
    const v = await props.client.request(`/settings/${props.kind}`, { signal });
    if (current(run, c)) apply(v);
  } catch {
    if (current(run, c)) {
      state.value = undefined;
      clearPrivate();
      error.value = "读取配置失败，请检查会话并刷新页面后核对。";
    }
  } finally {
    if (current(run, c)) loading.value = false;
  }
}
function credentialReady(key: CredentialKey233) {
  return edits.value[key].action === "replace"
    ? !!edits.value[key].value.trim()
    : edits.value[key].action === "keep" &&
        !!state.value?.credentials[key].present;
}
function check() {
  if (
    !props.active ||
    !allowed.value ||
    !state.value ||
    blocked.value ||
    staleDraft.value ||
    !dirty.value
  )
    return;
  error.value = "";
  notice.value = "";
  if (!/^(0|[1-9]\d{0,8})(?:\.\d{1,2})?$/.test(money.value)) {
    error.value = "认证收费请填写非负金额，最多两位小数。";
    return;
  }
  if (
    props.kind === "certification" &&
    mode.value === "0" &&
    (force.value || corporate.value)
  ) {
    error.value = "关闭实名认证前，请关闭强制认证和企业认证。";
    return;
  }
  if (
    props.kind === "certification" &&
    ["1", "3"].includes(mode.value) &&
    !state.value.channels.some((v) => v.id === channel.value)
  ) {
    error.value = "请选择有效的支付宝通道。";
    return;
  }
  if (requiredKeys.value.some((key) => !credentialReady(key))) {
    error.value =
      "请填写所选服务所需的完整凭据；启用中的服务不能清除必要凭据。";
    return;
  }
  if (
    Object.values(edits.value).some(
      (v) => v.action === "replace" && !v.value.trim(),
    )
  ) {
    error.value = "新值不能为空；删除已有凭据请点击“清除”。";
    return;
  }
  confirming.value = true;
}
async function save() {
  if (
    !props.active ||
    !confirming.value ||
    !allowed.value ||
    !state.value ||
    blocked.value ||
    staleDraft.value
  )
    return;
  const { run, c, signal } = begin();
  busy.value = true;
  emit("busy", true);
  error.value = "";
  const body = {
    expected_revision: state.value.revision,
    cert_open: Number(mode.value),
    cert_channel: channel.value,
    cert_corpopen: corporate.value,
    cert_force: force.value,
    cert_money: money.value,
    ocr_type: provider.value,
    credentials: Object.fromEntries(
      Object.entries(edits.value).map(([key, e]) => [
        key,
        { action: e.action, value: e.action === "replace" ? e.value : "" },
      ]),
    ),
  };
  clearPrivate();
  try {
    const v = await props.client.request(`/settings/${props.kind}`, {
      method: "PUT",
      body,
      signal,
    });
    if (current(run, c)) {
      apply(v);
      notice.value = "配置已保存。";
    }
  } catch (e) {
    if (current(run, c)) {
      uncertain.value = !(
        e instanceof ApiError &&
        e.responseDecoded &&
        [400, 422].includes(e.status)
      );
      error.value = uncertain.value
        ? "保存结果尚未确认，或配置已被更新。请刷新页面后核对；不会自动重试。"
        : "配置未被接受，请核对字段和凭据后重新填写。";
    }
  } finally {
    if (current(run, c)) {
      busy.value = false;
      emit("busy", false);
    }
  }
}
watch(
  () => context(),
  () => {
    epoch++;
    controller?.abort();
    state.value = undefined;
    busy.value = false;
    loading.value = false;
    uncertain.value = false;
    staleDraft.value = false;
    error.value = "";
    notice.value = "";
    clearPrivate();
    emit("busy", false);
    if (allowed.value && props.active) void load(true);
  },
  { immediate: true },
);
watch(
  () => props.active,
  (active) => {
    if (!active) {
      notice.value = "";
      return;
    }
    if (!allowed.value || busy.value) return;
    // These two panels edit one versioned configuration. A clean cached panel
    // must refresh its whole snapshot after another panel may have saved it.
    // Never replace private input or clear an ambiguous outcome on activation.
    if (dirty.value || uncertain.value) {
      staleDraft.value = true;
      return;
    }
    void load(true);
  },
);
watch(
  () => Object.values(edits.value).map((v) => v.action),
  () => {
    for (const v of Object.values(edits.value))
      if (v.action !== "replace") v.value = "";
  },
);
onBeforeUnmount(() => {
  alive = false;
  epoch++;
  controller?.abort();
  clearPrivate();
  emit("busy", false);
});
</script>
<template>
  <section class="kyc-settings233" role="region" :aria-label="title">
    <h2>{{ title }}</h2>
    <p v-if="!allowed" class="alert">
      此配置需要本人超级管理员会话，请重新登录后操作。
    </p>
    <template v-else>
      <p v-if="error" role="alert" class="alert error">{{ error }}</p>
      <p v-if="notice" role="status" class="alert success">{{ notice }}</p>
      <p v-if="staleDraft" class="alert" role="alert">
        本页配置可能已更新，草稿已保留。请刷新页面并核对最新配置，刷新会放弃未保存内容。
      </p>

      <form class="console-config-form" v-if="state" @submit.prevent="check">
        <fieldset :disabled="blocked || confirming">
          <template v-if="kind === 'certification'">
            <label class="kyc-row233"
              ><span>是否开启实名认证</span
              ><KitSelect
                v-model="mode"
                aria-label="是否开启实名认证"
                :options="certificationModes233"
                :disabled="blocked"
            /></label>
            <p class="muted">
              实名认证扩展插件尚未接入。选择认证方式后填写对应服务商凭据；服务需在服务商平台先行开通。
            </p>
            <label v-if="mode === '1' || mode === '3'" class="kyc-row233"
              ><span>支付宝通道选择</span
              ><KitSelect
                v-model="channel"
                aria-label="支付宝通道选择"
                :options="
                  state.channels.map((v) => ({
                    value: v.id,
                    label: `${v.id} · ${v.name}`,
                  }))
                "
                placeholder="请选择支付宝通道"
                :disabled="blocked"
            /></label>
            <p
              v-if="(mode === '1' || mode === '3') && !state.channels.length"
              class="muted"
            >
              暂无可用的支付宝通道，请先在支付通道中完成配置。
            </p>
            <p v-if="mode === '3'" class="muted">
              支付宝实名信息验证需在应用中开通相关产品，并配置本站授权回调域名。
            </p>
            <label class="kyc-row233"
              ><span>开启企业认证方式</span
              ><KitSwitch
                v-model="corporate"
                aria-label="开启企业认证方式"
                :label="corporate ? '开启' : '关闭'"
                :disabled="blocked"
            /></label>
            <label class="kyc-row233"
              ><span>商户强制认证</span
              ><KitSwitch
                v-model="force"
                aria-label="商户强制认证"
                :label="force ? '开启' : '关闭'"
                :disabled="blocked"
            /></label>
            <p class="muted">
              开启商户强制认证后，未完成认证的商户无法发起收款。请先确认认证服务可用。
            </p>
            <label class="kyc-row233"
              ><span>实名认证费用</span>
              <div>
                <input
                  v-model="money"
                  aria-label="实名认证费用"
                  inputmode="decimal"
                  maxlength="12"
                  required
                  :disabled="blocked"
                /><span class="muted"> 元/次，0表示免费</span>
              </div></label
            >
            <p class="muted">
              认证成功后自动扣费，认证失败不扣费。重复查询同一次认证不会重复扣费。
            </p>
          </template>
          <template v-else>
            <label class="kyc-row233"
              ><span>文字识别接口</span
              ><KitSelect
                v-model="provider"
                aria-label="文字识别接口"
                :options="[
                  { value: '', label: '关闭' },
                  { value: 'aliyun', label: '阿里云OCR' },
                  { value: 'baidu', label: '百度云OCR' },
                ]"
                :disabled="blocked"
            /></label>
            <p class="muted">
              支持身份证正反面、护照、银行卡、营业执照及开户许可证。识别结果用于预填，需人工核对，不会直接完成实名认证。
            </p>
          </template>
          <button
            type="button"
            :disabled="blocked"
            @click="showOtherCredentials = !showOtherCredentials"
          >
            {{
              showOtherCredentials
                ? "仅显示当前服务凭据项"
                : "管理其它服务凭据项"
            }}
          </button>
          <div v-for="key in visibleKeys" :key="key" class="kyc-credential233">
            <CredentialActionControl
              :model-value="edits[key].action"
              :configured="state.credentials[key].present"
              :label="credentialLabels[key]"
              :disabled="blocked || confirming || staleDraft"
              :clear-disabled="requiredKeys.includes(key)"
              clear-hint="请先停用当前服务或切换服务，再清除必要凭据。"
              @update:model-value="setCredentialAction(key, $event)"
            />
            <label class="kyc-row233"
              ><span>{{ credentialLabels[key] }}</span
              ><input
                v-model="edits[key].value"
                :aria-label="credentialLabels[key]"
                type="password"
                autocomplete="new-password"
                maxlength="2048"
                :placeholder="
                  state.credentials[key].present ? '已保存（不回显）' : '未配置'
                "
                :disabled="blocked || edits[key].action !== 'replace'"
                :required="edits[key].action === 'replace'"
            /></label>
          </div>
          <p class="muted">
            凭据加密保存，不回显原值。需要更新时点击“修改”；未修改的凭据无需重填。保存配置不会提交认证、上传证照或调用计费接口。
          </p>
          <button type="submit" :disabled="blocked || staleDraft || !dirty">
            保存{{ kind === "ocr" ? "OCR" : "实名认证" }}配置
          </button>
        </fieldset>
      </form>
      <KitModal
        v-if="confirming"
        :title="`确认保存${title}`"
        submit-text="确认保存"
        :loading="busy"
        @close="confirming = false"
        @submit="save"
      >
        <p>
          将保存{{
            kind === "ocr" ? "OCR服务" : "实名认证规则"
          }}及填写的凭据。保存不会发起认证或识别请求。
        </p>
        <p v-if="kind === 'certification' && force">
          商户强制认证将限制尚未完成认证的商户收款。
        </p>
      </KitModal>
    </template>
  </section>
</template>
<style scoped>
.kyc-settings233 fieldset {
  margin: 0;
  padding: 0;
  border: 0;
  min-width: 0;
}
.kyc-settings233 p {
  line-height: 1.7;
}
.kyc-row233 {
  display: grid;
  grid-template-columns: 220px minmax(0, 1fr);
  gap: 18px;
  align-items: center;
  margin: 18px 0;
}
.kyc-row233 > span {
  font-weight: 500;
}
.kyc-row233 input {
  width: 100%;
  max-width: 640px;
}
.kyc-credential233 {
  border-top: 1px solid var(--border, #e8edf3);
}
@media (max-width: 760px) {
  .kyc-row233 {
    grid-template-columns: 1fr;
    gap: 8px;
  }
}
</style>
