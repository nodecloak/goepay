<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, toRaw, watch } from "vue";
import QRCode from "qrcode";
import { ApiClient, ApiError } from "../api";
import {
  parseInfo233,
  safeCertificationURL233,
  type CertificationInfo233,
} from "../kyc233";
import KitModal from "./KitModal.vue";
import KitSelect from "./KitSelect.vue";
import OCRRecognition233 from "./OCRRecognition233.vue";

const props = defineProps<{
  client: ApiClient;
  ownSession: boolean;
  merchantID: string;
}>();
const info = ref<CertificationInfo233>(),
  busy = ref(false),
  loading = ref(false),
  ocrBusy = ref(false),
  error = ref(""),
  notice = ref(""),
  uncertain = ref(false),
  upgrading = ref(false),
  consent = ref(false),
  qrcode = ref("");
const certtype = ref("0"),
  cardtype = ref("0"),
  name = ref(""),
  number = ref(""),
  companyName = ref(""),
  companyNumber = ref("");
const confirming = ref<"submit" | "geturl" | "reset">();
const allowed = computed(
  () =>
    props.ownSession &&
    props.client.area === "merchant" &&
    !!props.client.csrfToken,
);
const locked = computed(() => busy.value || loading.value || ocrBusy.value);
const providerName = computed(
  () =>
    ({
      1: "支付宝身份验证",
      2: "手机号三要素认证服务（阿里云市场）",
      3: "支付宝实名信息验证",
      4: "微信扫码实名认证（腾讯云）",
      5: "阿里云金融级实人认证",
    })[info.value?.cert_open || 0] || "认证服务",
);
const active = computed(
  () =>
    !!info.value?.attempt_id &&
    ["ready", "initializing", "pending", "unknown", "verified"].includes(
      info.value.status,
    ),
);
const formVisible = computed(
  () =>
    !!info.value?.cert_open &&
    !active.value &&
    (info.value.cert !== 1 || upgrading.value),
);
const step = computed(() =>
  info.value?.status === "passed" ||
  (info.value?.cert === 1 && !active.value && !upgrading.value)
    ? 2
    : active.value
      ? 1
      : 0,
);
const canQuery = computed(
  () =>
    !!info.value?.attempt_id &&
    (["pending", "initializing", "unknown", "verified"].includes(
      info.value.status,
    ) ||
      uncertain.value),
);
const queryClock = ref(Date.now());
const queryWait = computed(() =>
  info.value?.query_after && info.value.status !== "verified"
    ? Math.max(
        0,
        Math.ceil(
          (Date.parse(info.value.query_after) - queryClock.value) / 1000,
        ),
      )
    : 0,
);
const oauthQueryBlocked = computed(
  () =>
    info.value?.cert_open === 3 &&
    info.value.status !== "verified" &&
    (info.value.auth_code_used || !info.value.auth_code_ready),
);
const statusText: Record<string, string> = {
  none: "尚未提交认证",
  ready: "资料已提交，尚未开始核验",
  initializing: "正在建立认证，请读取最新状态",
  pending: "等待完成认证",
  unknown: "结果待核实，请查询结果",
  rejected: "认证未通过，可重新填写",
  passed: "认证成功",
  verified: "已通过服务商核验，认证费用尚未扣除；请确认可用余额后完成认证。",
  expired: "认证已过期，请重新填写",
  cancelled: "认证已取消，可重新填写",
};
const cardOptions = [
  "大陆居民身份证",
  "港澳通行证",
  "台湾通行证",
  "港澳居住证",
  "台湾居住证",
  "外国人永久居留身份证",
].map((label, i) => ({ value: String(i), label }));
let alive = true,
  epoch = 0,
  controller: AbortController | undefined,
  qrEpoch = 0,
  queryTimer: ReturnType<typeof setInterval> | undefined;
function context() {
  return [
    toRaw(props.client),
    props.client.area,
    props.client.csrfToken,
    props.ownSession,
    props.merchantID,
  ] as const;
}
function current(run: number, c: readonly unknown[]) {
  return (
    alive &&
    run === epoch &&
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
function clearIdentity() {
  name.value = "";
  number.value = "";
  companyName.value = "";
  companyNumber.value = "";
  consent.value = false;
}
function apply(value: unknown) {
  info.value = parseInfo233(value);
  uncertain.value = false;
  confirming.value = undefined;
  if (info.value.cert === 1 && !active.value) upgrading.value = false;
}
watch(
  () => info.value?.query_after,
  () => {
    clearInterval(queryTimer);
    queryClock.value = Date.now();
    if (
      info.value?.query_after &&
      Date.parse(info.value.query_after) > queryClock.value
    )
      queryTimer = setInterval(() => {
        queryClock.value = Date.now();
        if (!queryWait.value) {
          clearInterval(queryTimer);
          queryTimer = undefined;
        }
      }, 1000);
  },
);
async function load() {
  if (!allowed.value || locked.value) return;
  const { run, c, signal } = begin();
  loading.value = true;
  error.value = "";
  notice.value = "";
  try {
    const v = await props.client.request("/account/certificate/info", {
      signal,
    });
    if (current(run, c)) apply(v);
  } catch {
    if (current(run, c)) {
      info.value = undefined;
      error.value = "读取认证信息失败，请确认本人会话后重试。";
    }
  } finally {
    if (current(run, c)) loading.value = false;
  }
}
function prepareSubmit() {
  if (
    !allowed.value ||
    !formVisible.value ||
    locked.value ||
    uncertain.value ||
    !consent.value
  )
    return;
  error.value = "";
  if (
    !name.value.trim() ||
    !number.value.trim() ||
    (certtype.value === "1" &&
      (!companyName.value.trim() || !companyNumber.value.trim()))
  ) {
    error.value = "请填写完整的认证信息。";
    return;
  }
  confirming.value = "submit";
}
async function mutate(action: "submit" | "geturl" | "query" | "reset") {
  if (
    !allowed.value ||
    !info.value ||
    locked.value ||
    (action !== "query" && (uncertain.value || confirming.value !== action))
  )
    return;
  if (action === "geturl" && info.value.status !== "ready") return;
  if (info.value.configuration_changed && action === "geturl") return;
  if (action === "query" && !canQuery.value) return;
  if (action === "query" && (queryWait.value > 0 || oauthQueryBlocked.value))
    return;
  if (action === "submit" && (!consent.value || !formVisible.value)) return;
  const body =
    action === "submit"
      ? {
          expected_revision: info.value.revision,
          certtype: Number(certtype.value),
          certcardtype:
            info.value.cert_open === 1 && certtype.value === "0"
              ? Number(cardtype.value)
              : 0,
          certname: name.value.trim(),
          certno: number.value.trim(),
          certcorpname: certtype.value === "1" ? companyName.value.trim() : "",
          certcorpno: certtype.value === "1" ? companyNumber.value.trim() : "",
          consent: consent.value,
        }
      : {
          attempt_id: info.value.attempt_id,
          state: "",
          auth_code: "",
        };
  const { run, c, signal } = begin();
  busy.value = true;
  error.value = "";
  notice.value = "";
  confirming.value = undefined;
  if (action === "submit") clearIdentity();
  try {
    const v = await props.client.request(`/account/certificate/${action}`, {
      method: "POST",
      body,
      signal,
    });
    if (current(run, c)) {
      apply(v);
      notice.value =
        info.value?.status === "passed"
          ? "实名认证已完成。"
          : action === "reset"
            ? "本次认证已取消，请重新填写。"
            : action === "query"
              ? "已读取最新认证结果。"
              : action === "submit"
                ? "资料已提交，请继续开始核验。"
                : "认证已建立，请按提示完成核验。";
    }
  } catch (e) {
    if (current(run, c)) {
      uncertain.value = !(
        e instanceof ApiError &&
        e.responseDecoded &&
        [400, 422].includes(e.status)
      );
      error.value = uncertain.value
        ? "本次操作未取得可核实结果，不会自动重试。请先读取最新状态；已有认证可手动查询结果。"
        : "请求未被接受，请核对认证资料、可用余额和服务配置后处理。";
    }
  } finally {
    if (current(run, c)) busy.value = false;
  }
}
function upgrade() {
  if (
    info.value?.cert === 1 &&
    info.value.certtype === 0 &&
    info.value.cert_corpopen &&
    !locked.value &&
    !active.value
  ) {
    upgrading.value = true;
    certtype.value = "1";
    cardtype.value = "0";
    clearIdentity();
    error.value = "";
    notice.value = "";
  }
}
async function prefill(fields: Record<string, string>, type: string) {
  if (!formVisible.value || locked.value || uncertain.value) return;
  if (type === "idcard") {
    if (fields.name) name.value = fields.name;
    if (fields.id_no) number.value = fields.id_no;
    cardtype.value = "0";
  }
  if (type === "business_license" && info.value?.cert_corpopen) {
    certtype.value = "1";
    await nextTick();
    if (!allowed.value || !formVisible.value || locked.value || uncertain.value)
      return;
    if (fields.name) companyName.value = fields.name;
    if (fields.license_no) companyNumber.value = fields.license_no;
    if (fields.legal_name) name.value = fields.legal_name;
  }
  consent.value = false;
  notice.value = "识别结果已预填，请核对每个字段后再提交认证。";
}
watch(
  () => info.value?.url,
  async (url) => {
    const run = ++qrEpoch;
    qrcode.value = "";
    if (!url || !safeCertificationURL233(url) || url.length > 8000) return;
    try {
      const image = await QRCode.toDataURL(url, {
        width: 224,
        margin: 2,
        errorCorrectionLevel: "M",
      });
      if (alive && run === qrEpoch && info.value?.url === url)
        qrcode.value = image;
    } catch {
      /* Long OAuth links remain available through the explicit open action. */
    }
  },
);
watch(
  () => context(),
  () => {
    epoch++;
    qrEpoch++;
    controller?.abort();
    info.value = undefined;
    busy.value = false;
    loading.value = false;
    ocrBusy.value = false;
    error.value = "";
    notice.value = "";
    uncertain.value = false;
    upgrading.value = false;
    confirming.value = undefined;
    qrcode.value = "";
    clearIdentity();
    if (allowed.value) void load();
  },
  { immediate: true },
);
watch(certtype, () => {
  cardtype.value = "0";
  companyName.value = "";
  companyNumber.value = "";
  consent.value = false;
});
onBeforeUnmount(() => {
  clearInterval(queryTimer);
  alive = false;
  epoch++;
  qrEpoch++;
  controller?.abort();
  clearIdentity();
  qrcode.value = "";
});
</script>
<template>
  <section class="panel certification233" aria-label="实名认证">
    <h2>实名认证</h2>
    <p v-if="!allowed" class="alert">
      此功能需要商户本人登录，请使用商户账号登录后操作。
    </p>
    <template v-else>
      <p v-if="error" class="alert error" role="alert">{{ error }}</p>
      <p v-if="notice" class="alert success" role="status">{{ notice }}</p>
      <button type="button" :disabled="locked" @click="load">
        {{ loading ? "正在读取…" : "读取最新认证状态" }}
      </button>
      <template v-if="info">
        <ol class="certification-steps233" aria-label="认证进度">
          <li
            :aria-current="step === 0 ? 'step' : undefined"
            :class="{ current: step === 0 }"
          >
            1 填写认证信息
          </li>
          <li
            :aria-current="step === 1 ? 'step' : undefined"
            :class="{ current: step === 1 }"
          >
            2 完成身份核验
          </li>
          <li
            :aria-current="step === 2 ? 'step' : undefined"
            :class="{ current: step === 2 }"
          >
            3 认证完成
          </li>
        </ol>
        <p
          v-if="!info.cert_open && info.cert !== 1"
          class="alert"
          role="status"
        >
          平台尚未开启实名认证，请联系平台管理员。
        </p>
        <p class="muted" v-if="info.cert_open">
          认证成功自动扣费 ¥{{
            info.cert_money
          }}，失败不扣费；同一次认证不会重复扣费。
        </p>
        <p v-if="info.configuration_changed" class="alert" role="alert">
          认证服务配置或绑定资料已变化。尚未开始的认证需取消后重新填写；已受理的认证仍按原服务和原费用查询，不会自动提交新的认证。
        </p>
        <p v-if="info.cert_open" class="muted">
          当前认证服务：{{ providerName
          }}<template v-if="certtype === '1'"
            >；企业资料另提交至企业信息校验服务（数脉科技）</template
          >。
        </p>
        <dl
          v-if="info.cert === 1"
          class="certification-facts233"
          aria-label="已认证信息"
        >
          <dt>认证状态</dt>
          <dd>已实名认证</dd>
          <dt>认证类型</dt>
          <dd>{{ info.certtype_text || (info.certtype ? "企业" : "个人") }}</dd>
          <dt>认证方式</dt>
          <dd>{{ info.certmethod_text || "—" }}</dd>
          <dt>认证姓名</dt>
          <dd>{{ info.certname || "—" }}</dd>
          <dt>证件号码</dt>
          <dd>{{ info.certno || "—" }}</dd>
          <template v-if="info.certtype"
            ><dt>企业名称</dt>
            <dd>{{ info.certcorpname || "—" }}</dd>
            <dt>统一社会信用代码</dt>
            <dd>{{ info.certcorpno || "—" }}</dd></template
          >
          <dt>认证时间</dt>
          <dd>{{ info.certtime || "—" }}</dd>
        </dl>
        <button
          v-if="
            info.cert === 1 &&
            info.certtype === 0 &&
            info.cert_corpopen &&
            !upgrading &&
            !active
          "
          type="button"
          :disabled="locked || uncertain"
          @click="upgrade"
        >
          升级企业认证
        </button>
        <form
          v-if="formVisible"
          autocomplete="off"
          @submit.prevent="prepareSubmit"
        >
          <fieldset :disabled="locked || uncertain || !!confirming">
            <label
              v-if="info.cert_corpopen && !upgrading"
              class="certification-row233"
              ><span>认证类型</span
              ><KitSelect
                v-model="certtype"
                aria-label="认证类型"
                :options="[
                  { value: '0', label: '个人认证' },
                  { value: '1', label: '企业认证' },
                ]"
                :disabled="locked || uncertain"
            /></label>
            <template v-if="certtype === '1'"
              ><label class="certification-row233"
                ><span>企业名称</span
                ><input
                  v-model="companyName"
                  aria-label="企业名称"
                  maxlength="256"
                  required
                  autocomplete="off" /></label
              ><label class="certification-row233"
                ><span>统一社会信用代码</span
                ><input
                  v-model="companyNumber"
                  aria-label="统一社会信用代码"
                  maxlength="32"
                  required
                  autocomplete="off"
              /></label>
              <p class="muted">
                先校验企业信息，再由法定代表人完成身份核验。
              </p></template
            >
            <label
              v-if="info.cert_open === 1 && certtype === '0'"
              class="certification-row233"
              ><span>证件类型</span
              ><KitSelect
                v-model="cardtype"
                aria-label="证件类型"
                :options="cardOptions"
                :disabled="locked || uncertain"
            /></label>
            <label class="certification-row233"
              ><span>{{ certtype === "1" ? "法人姓名" : "真实姓名" }}</span
              ><input
                v-model="name"
                :aria-label="certtype === '1' ? '法人姓名' : '真实姓名'"
                maxlength="128"
                required
                autocomplete="off"
            /></label>
            <label class="certification-row233"
              ><span>{{ cardtype === "0" ? "身份证号码" : "证件号码" }}</span
              ><input
                v-model="number"
                :aria-label="cardtype === '0' ? '身份证号码' : '证件号码'"
                maxlength="64"
                required
                autocomplete="off"
            /></label>
            <template v-if="info.cert_open === 2"
              ><label class="certification-row233"
                ><span>已绑定手机号</span
                ><input
                  :value="info.phone_masked"
                  aria-label="已绑定手机号"
                  disabled
              /></label>
              <p class="muted">
                使用商户已绑定的手机号核验，不可在此修改。<RouterLink
                  to="/merchant/account/settings"
                  >修改绑定</RouterLink
                >
              </p></template
            >
            <label class="certification-consent233"
              ><input
                v-model="consent"
                type="checkbox"
                aria-label="同意实名认证及成功扣费"
                required
              />本人已核对填写的信息，同意将其发送至{{
                providerName
              }}核验<template v-if="certtype === '1'"
                >，并将企业资料提交至企业信息校验服务</template
              >，并授权认证成功后从商户余额扣除 ¥{{
                info.cert_money
              }}
              认证费用。</label
            >
            <button type="submit" :disabled="locked || uncertain || !consent">
              提交认证资料
            </button>
            <button
              v-if="upgrading"
              type="button"
              :disabled="locked || uncertain"
              @click="
                upgrading = false;
                clearIdentity();
              "
            >
              取消升级
            </button>
          </fieldset>
        </form>
        <OCRRecognition233
          v-if="formVisible && info.ocr_type"
          :client="client"
          :own-session="ownSession"
          :provider="info.ocr_type"
          :config-revision="info.ocr_config_revision"
          :disabled="busy || loading || uncertain"
          @busy="ocrBusy = $event"
          @prefill="prefill"
        />
        <section
          v-if="
            active || info.status === 'passed' || info.status === 'rejected'
          "
          class="certification-attempt233"
          aria-label="本次认证状态"
        >
          <p role="status">
            {{ statusText[info.status] || "请读取最新认证状态" }}
          </p>
          <p v-if="info.expires_at" class="muted">
            有效期至 {{ new Date(info.expires_at).toLocaleString() }}
          </p>
          <button
            v-if="info.status === 'ready'"
            type="button"
            :disabled="locked || uncertain || info.configuration_changed"
            @click="confirming = 'geturl'"
          >
            {{ info.cert_open === 2 ? "开始手机号核验" : "开始身份核验" }}
          </button>
          <template
            v-if="
              info.url &&
              safeCertificationURL233(info.url) &&
              !info.auth_code_used &&
              info.status === 'pending'
            "
          >
            <img
              v-if="qrcode"
              :src="qrcode"
              class="certification-qr233"
              alt="本次身份认证二维码"
              width="224"
              height="224"
            />
            <p>
              {{
                info.scan === "wechat"
                  ? "请使用微信扫码完成核验"
                  : info.scan === "alipay"
                    ? "请使用支付宝扫码完成核验"
                    : "请在认证服务页面完成核验"
              }}
            </p>
            <a
              class="button"
              :href="info.url"
              target="_blank"
              rel="noopener noreferrer"
              referrerpolicy="no-referrer"
              >打开认证页面</a
            >
            <p class="muted">完成后回到本页，点击“查询认证结果”。</p>
          </template>
          <button
            v-if="canQuery"
            type="button"
            :disabled="locked || queryWait > 0 || oauthQueryBlocked"
            @click="mutate('query')"
          >
            {{ info.status === "verified" ? "完成认证" : "查询认证结果" }}
          </button>
          <p v-if="queryWait > 0" class="muted" role="status">
            请等待 {{ queryWait }} 秒后手动查询，不会自动请求。
          </p>
          <p
            v-if="
              info.cert_open === 3 &&
              info.auth_code_used &&
              info.status !== 'verified' &&
              info.status !== 'passed'
            "
            class="muted"
          >
            本次授权已使用。若结果尚未确认，请读取最新状态；不要重复授权。需要重新开始时请先取消本次认证。
          </p>
          <button
            v-if="active"
            type="button"
            :disabled="locked || uncertain"
            @click="confirming = 'reset'"
          >
            重新填写认证资料
          </button>
        </section>
      </template>
      <KitModal
        v-if="confirming === 'submit'"
        title="确认提交认证资料"
        submit-text="确认提交"
        :loading="busy"
        @close="confirming = undefined"
        @submit="mutate('submit')"
        ><p>
          将保存本次认证资料。继续开始身份核验后，资料将发送至{{ providerName
          }}<template v-if="certtype === '1'"
            >，企业资料还将提交至企业信息校验服务</template
          >。认证成功扣费 ¥{{ info?.cert_money }}，失败不扣费。
        </p></KitModal
      >
      <KitModal
        v-if="confirming === 'geturl'"
        title="开始身份核验"
        submit-text="开始核验"
        :loading="busy"
        @close="confirming = undefined"
        @submit="mutate('geturl')"
        ><p>
          将向{{
            providerName
          }}提交本次信息并建立认证。认证服务可能计费，请仅提交本人或有权使用的资料。
        </p></KitModal
      >
      <KitModal
        v-if="confirming === 'reset'"
        title="重新填写认证资料"
        submit-text="取消本次并重填"
        :loading="busy"
        @close="confirming = undefined"
        @submit="mutate('reset')"
        ><p>
          取消本次待完成的认证。已提交给服务商的请求无法撤回；重新开始是新的认证请求，可能再次产生服务商费用。
          <template v-if="info?.status === 'verified'"
            >本次已通过服务商核验，取消后不会自动完成认证或扣费。</template
          >
        </p></KitModal
      >
    </template>
  </section>
</template>
<style scoped>
.certification233 fieldset {
  border: 0;
  margin: 0;
  padding: 0;
  min-width: 0;
}
.certification233 p {
  line-height: 1.7;
}
.certification233 button,
.certification233 a.button {
  margin: 8px 8px 8px 0;
}
.certification-steps233 {
  list-style: none;
  display: flex;
  gap: 12px;
  padding: 0;
  margin: 28px 0;
}
.certification-steps233 li {
  flex: 1;
  border-top: 3px solid var(--border, #e2e8f0);
  padding: 12px 0;
  color: var(--muted, #64748b);
}
.certification-steps233 li.current {
  border-color: var(--primary, #3b82f6);
  color: var(--primary, #2563eb);
  font-weight: 600;
}
.certification-row233 {
  display: grid;
  grid-template-columns: 190px minmax(0, 1fr);
  align-items: center;
  gap: 16px;
  margin: 20px 0;
}
.certification-row233 input {
  width: 100%;
  max-width: 600px;
}
.certification-consent233 {
  display: flex;
  gap: 8px;
  align-items: flex-start;
  line-height: 1.7;
  margin: 24px 0;
}
.certification-consent233 input {
  width: auto;
  margin-top: 6px;
}
.certification-facts233 {
  display: grid;
  grid-template-columns: 190px minmax(0, 1fr);
  gap: 14px;
  margin: 24px 0;
}
.certification-facts233 dd {
  margin: 0;
  overflow-wrap: anywhere;
}
.certification-attempt233 {
  padding: 20px 0;
}
.certification-qr233 {
  display: block;
  max-width: 100%;
  object-fit: contain;
  background: white;
}
@media (max-width: 760px) {
  .certification-row233 {
    grid-template-columns: 1fr;
    gap: 8px;
  }
  .certification-steps233 {
    gap: 8px;
    font-size: 13px;
  }
  .certification-facts233 {
    grid-template-columns: minmax(100px, 130px) minmax(0, 1fr);
  }
}
</style>
