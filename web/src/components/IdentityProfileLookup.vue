<script setup lang="ts">
import { ref, computed, watch, onMounted, onBeforeUnmount } from "vue";
import QRCode from "qrcode";
import type { ApiClient } from "../api";
import type { Area } from "../menu";
import {
  parseIdentityProfile,
  parseProfileRequest,
  parseProfileLaunch,
  parseProfileIdentityLink,
  profilePositive,
  type IdentityProfile,
  type ProfileRequest,
  type ProfileSDKLaunch,
  type ProfileIdentityLink,
} from "../identity-profile";
const props = defineProps<{
  client: ApiClient;
  area: Area;
  ownSession: boolean;
}>();
const merchant = ref(""),
  mid = ref(""),
  profiles = ref<IdentityProfile[]>([]),
  selected = ref(""),
  current = ref<ProfileRequest | null>(null),
  snapshot = ref<IdentityProfile | null>(null),
  launch = ref<ProfileSDKLaunch | null>(null),
  authorization = ref(""),
  code = ref(""),
  subject = ref(""),
  label = ref(""),
  reason = ref(""),
  error = ref(""),
  busy = ref(false),
  unknown = ref(false),
  clock = ref(Date.now());
const intent = ref<{ state: string; code: string; id: string } | null>(null);
const link = ref<ProfileIdentityLink | null>(null),
  linkAttempted = ref(false),
  linkUncertain = ref(false),
  linkQR = ref("");
let qrEpoch = 0;
let epoch = 0,
  controller: AbortController | undefined,
  active = true;
const selectedProfile = computed(() =>
  profiles.value.find((p) => p.id === selected.value),
);
const expired = computed(
  () => !!current.value && Date.parse(current.value.expires_at) <= clock.value,
);
const openRequest = computed(
  () =>
    current.value &&
    !expired.value &&
    ["pending", "processing", "verified"].includes(current.value.status),
);
const linkAvailable = computed(
  () =>
    !!current.value &&
    current.value.status === "pending" &&
    current.value.provider === "wxpay" &&
    current.value.kind === "mini" &&
    snapshot.value?.url_link_available === true,
);
const linkWindow = () =>
  !!current.value &&
  Math.floor(Date.parse(current.value.expires_at) / 1000) * 1000 - Date.now() >
    70000;
const canGenerateLink = computed(
  () =>
    linkAvailable.value &&
    !!launch.value &&
    !busy.value &&
    !unknown.value &&
    !linkAttempted.value &&
    !!current.value &&
    Math.floor(Date.parse(current.value.expires_at) / 1000) * 1000 -
      clock.value >
      70000,
);
function clearLinkURL() {
  qrEpoch++;
  linkQR.value = "";
  if (link.value) link.value = { ...link.value, url_link: undefined };
}
function clearSecrets() {
  launch.value = null;
  authorization.value = "";
  code.value = "";
  subject.value = "";
  label.value = "";
  intent.value = null;
  clearLinkURL();
}
function reset(clearProfiles = false) {
  epoch++;
  controller?.abort();
  busy.value = false;
  current.value = null;
  snapshot.value = null;
  clearSecrets();
  error.value = "";
  unknown.value = false;
  link.value = null;
  linkAttempted.value = false;
  linkUncertain.value = false;
  if (clearProfiles) {
    profiles.value = [];
    selected.value = "";
    mid.value = "";
    reason.value = "";
  }
}
watch(
  () => [props.client, props.area, props.ownSession, merchant.value],
  () => reset(true),
  { flush: "sync" },
);
watch(selected, () => reset(), { flush: "sync" });
async function run(fn: (signal: AbortSignal, ticket: number) => Promise<void>) {
  if (busy.value || !props.ownSession) return;
  controller?.abort();
  controller = new AbortController();
  const ticket = ++epoch;
  busy.value = true;
  error.value = "";
  try {
    await fn(controller.signal, ticket);
  } catch (e) {
    if (active && ticket === epoch)
      error.value = e instanceof Error ? e.message : "独立授权失败";
  } finally {
    if (active && ticket === epoch) busy.value = false;
  }
}
async function capabilities() {
  if (props.area === "admin" && !profilePositive(merchant.value)) {
    error.value = "请输入规范商户ID";
    return;
  }
  reset(true);
  await run(async (signal, ticket) => {
    const out = await props.client.request<any>(
      "/identity-profile-lookups/capabilities" +
        (props.area === "admin" ? `?merchant_id=${merchant.value}` : ""),
      { signal },
    );
    if (!active || ticket !== epoch) return;
    if (
      !profilePositive(out?.merchant_id) ||
      (props.area === "admin" && out.merchant_id !== merchant.value) ||
      typeof out.available !== "boolean" ||
      typeof out.reason !== "string" ||
      !Array.isArray(out.profiles) ||
      typeof out.url_link_available !== "boolean"
    )
      throw Error("应用能力响应未通过校验");
    const parsed = out.profiles.map(parseIdentityProfile);
    if (
      new Set(parsed.map((p: IdentityProfile) => p.id)).size !==
        parsed.length ||
      out.available !== parsed.length > 0 ||
      out.url_link_available !==
        parsed.some((p: IdentityProfile) => p.url_link_available === true)
    )
      throw Error("应用能力状态不一致");
    mid.value = out.merchant_id;
    profiles.value = parsed;
    reason.value = out.reason;
  });
}
async function create() {
  const p = selectedProfile.value;
  if (!p || openRequest.value || unknown.value) return;
  clearSecrets();
  current.value = null;
  link.value = null;
  linkAttempted.value = false;
  linkUncertain.value = false;
  await run(async (signal, ticket) => {
    unknown.value = true;
    const out = await props.client.request<any>("/identity-profile-lookups", {
      method: "POST",
      body: {
        profile_id: p.id,
        profile_revision: p.revision,
        ...(props.area === "admin" ? { merchant_id: mid.value } : {}),
      },
      signal,
    });
    if (!active || ticket !== epoch) return;
    const q = parseProfileRequest(out?.request, p, mid.value);
    if (
      q.status !== "pending" ||
      out.url_link_available !== (p.url_link_available === true)
    )
      throw Error("创建响应不是独立待授权请求");
    let sdk: ProfileSDKLaunch | null = null,
      url = "";
    if (q.kind === "mini") sdk = parseProfileLaunch(out.launch, q);
    else {
      const u = new URL(out.authorization_url);
      if (
        u.protocol !== "https:" ||
        u.username ||
        u.password ||
        u.search ||
        u.hash ||
        !/^\/identity\/profile\/authorize\/[a-f0-9]{64}$/.test(u.pathname)
      )
        throw Error("授权地址不是独立查询入口");
      url = u.href;
    }
    snapshot.value = { ...p };
    current.value = q;
    launch.value = sdk;
    authorization.value = url;
    clock.value = Date.now();
    unknown.value = false;
  });
}
async function generateLink() {
  const q = current.value,
    sdk = launch.value;
  if (!q || !sdk || !canGenerateLink.value || !linkWindow()) return;
  linkAttempted.value = true;
  linkUncertain.value = true;
  link.value = null;
  await run(async (signal, ticket) => {
    try {
      const out = await props.client.request(
        `/identity-profile-lookups/${q.request_id}/url-link`,
        { method: "POST", body: { state: sdk.body.state }, signal },
      );
      if (!active || ticket !== epoch) return;
      const parsed = parseProfileIdentityLink(out, q);
      clock.value = Date.now();
      if (expired.value) return;
      link.value = parsed;
      linkUncertain.value = parsed.status === "unknown";
    } catch {
      if (active && ticket === epoch) {
        clearLinkURL();
        throw Error(
          "URL Link生成结果不确定，本次请求不会再次生成；请手动查询已有结果",
        );
      }
    }
  });
}
async function queryLink() {
  const q = current.value;
  if (!q || !linkAvailable.value || !linkAttempted.value || expired.value)
    return;
  await run(async (signal, ticket) => {
    qrEpoch++;
    linkQR.value = "";
    try {
      const out = await props.client.request(
        `/identity-profile-lookups/${q.request_id}/url-link`,
        { signal },
      );
      if (!active || ticket !== epoch) return;
      const parsed = parseProfileIdentityLink(out, q, link.value ?? undefined);
      clock.value = Date.now();
      if (expired.value) return;
      link.value = parsed;
      linkUncertain.value = parsed.status === "unknown";
      // A stored read can recover a ready URL, never a new generation attempt.
    } catch {
      if (active && ticket === epoch) {
        clearLinkURL();
        linkUncertain.value = true;
        throw Error("URL Link本地结果暂不可读；本次请求不会再次生成");
      }
    }
  });
}
async function showLinkQR() {
  const q = current.value,
    target = link.value?.url_link;
  if (!q || !target || !linkAvailable.value || expired.value || busy.value)
    return;
  const ticket = epoch,
    localTicket = ++qrEpoch;
  try {
    const image = await QRCode.toDataURL(target, { width: 240, margin: 2 });
    if (
      active &&
      ticket === epoch &&
      localTicket === qrEpoch &&
      !expired.value &&
      current.value?.request_id === q.request_id &&
      link.value?.url_link === target
    )
      linkQR.value = image;
  } catch {
    if (active && ticket === epoch && localTicket === qrEpoch)
      error.value = "本地二维码生成失败；可继续使用返回链接";
  }
}
function openLink(event: MouseEvent) {
  if (
    !link.value?.url_link ||
    busy.value ||
    !current.value ||
    Date.parse(current.value.expires_at) <= Date.now()
  )
    event.preventDefault();
}
function checkComplete() {
  if (
    !current.value ||
    !launch.value ||
    expired.value ||
    current.value.status !== "pending" ||
    !code.value ||
    code.value.length > 4096 ||
    !/^[-A-Za-z0-9_.~]+$/.test(code.value)
  ) {
    error.value = "请输入钱包SDK返回的本次临时code";
    return;
  }
  intent.value = {
    id: current.value.request_id,
    state: launch.value.body.state,
    code: code.value,
  };
}
async function complete() {
  if (!intent.value || !current.value || expired.value) return;
  const send = { ...intent.value };
  intent.value = null;
  current.value = { ...current.value, status: "processing" };
  clearSecrets();
  await run(async (signal, ticket) => {
    const out = await props.client.request<any>(
      `/identity-profile-lookups/${send.id}/complete`,
      { method: "POST", body: { state: send.state, code: send.code }, signal },
    );
    if (!active || ticket !== epoch) return;
    if (out?.request_id !== send.id || out.status !== "verified")
      throw Error("兑换响应未知，请查询原请求，禁止重复提交");
    if (current.value) current.value = { ...current.value, status: "verified" };
  });
}
async function query() {
  const q = current.value,
    p = snapshot.value;
  if (!q || !p || expired.value) return;
  await run(async (signal, ticket) => {
    try {
      const out = await props.client.request(
        `/identity-profile-lookups/${q.request_id}`,
        { signal },
      );
      if (!active || ticket !== epoch) return;
      const next = parseProfileRequest(out, p, q.merchant_id);
      if (
        next.request_id !== q.request_id ||
        (p.url_link_available === true &&
          Date.parse(next.expires_at) !== Date.parse(q.expires_at))
      )
        throw Error("原授权请求不一致");
      current.value = next;
      clock.value = Date.now();
      if (next.status !== "pending") clearSecrets();
    } catch (e) {
      if (active && ticket === epoch) clearLinkURL();
      throw e;
    }
  });
}
async function reveal() {
  const q = current.value;
  if (!q || q.status !== "verified" || expired.value) return;
  current.value = { ...q, status: "revealed" };
  clearSecrets();
  await run(async (signal, ticket) => {
    const out = await props.client.request<any>(
      `/identity-profile-lookups/${q.request_id}/reveal`,
      { method: "POST", body: {}, signal },
    );
    if (!active || ticket !== epoch || expired.value) return;
    if (
      out?.provider !== q.provider ||
      out.app_id !== q.app_id ||
      typeof out.subject_id !== "string" ||
      out.subject_id.length < 1 ||
      out.subject_id.length > 128 ||
      /[\u0000-\u001f\u007f]/.test(out.subject_id) ||
      !["微信OpenID", "支付宝用户标识"].includes(out.label)
    )
      throw Error("单次结果响应无效；不会自动再次读取");
    subject.value = out.subject_id;
    label.value = out.label;
  });
}
const timer = setInterval(() => {
  clock.value = Date.now();
  if (expired.value) {
    epoch++;
    controller?.abort();
    busy.value = false;
    clearSecrets();
  }
}, 1000);
onMounted(() => {
  if (props.ownSession && props.area === "merchant") void capabilities();
});
onBeforeUnmount(() => {
  active = false;
  clearInterval(timer);
  reset(true);
});
</script>
<template>
  <section class="panel" aria-label="独立应用用户标识">
    <h2>独立应用用户标识</h2>
    <p>
      结果仅属于所选授权AppID；可与收款AppID不同。此流程只交换用户标识，不创建订单或发起付款。
    </p>
    <p v-if="!ownSession" role="alert">
      仅原始本人会话可使用；代登录不可发起或读取授权。
    </p>
    <template v-else>
      <div class="form-grid">
        <label v-if="area === 'admin'"
          >独立查询商户ID<input
            v-model.trim="merchant"
            inputmode="numeric"
            autocomplete="off" /></label
        ><button :disabled="busy" @click="capabilities">读取独立应用</button
        ><label
          >授权应用档案<select
            v-model="selected"
            :disabled="busy || !profiles.length"
          >
            <option value="">请选择独立应用</option>
            <option v-for="p in profiles" :key="p.id" :value="p.id">
              {{ p.provider === "wxpay" ? "微信" : "支付宝" }} /
              {{ p.kind === "mini" ? "小程序" : "网页" }} / {{ p.app_id }} / #{{
                p.channel_id
              }}
              · v{{ p.revision }}
            </option>
          </select></label
        ><button
          :disabled="busy || !selectedProfile || !!openRequest || unknown"
          @click="create"
        >
          创建独立授权请求
        </button>
      </div>
      <p v-if="reason" role="status">{{ reason }}</p>
      <p v-if="error" role="alert">{{ error }}</p>
      <p v-if="unknown" role="alert">
        创建响应未知，请重新读取应用后再明确操作；不会自动重发。
      </p>
      <div v-if="current">
        <p>
          独立请求状态：{{ expired ? "已过期" : current.status }} · 应用
          {{ current.app_id }} ·
          {{ current.kind === "mini" ? "小程序" : "网页" }}
        </p>
        <p>
          档案修订 {{ current.profile_revision }} · 到期
          {{ current.expires_at }}
        </p>
        <a
          v-if="authorization && !expired"
          :href="authorization"
          target="_blank"
          rel="noopener noreferrer"
          referrerpolicy="no-referrer"
          >打开独立网页授权</a
        >
        <template v-if="launch && !expired"
          ><h3>SDK对接资料</h3>
          <p v-if="!snapshot?.url_link_available">
            当前未提供厂商URL
            Link；以下是独立查询SDK回调资料，不是支付二维码。请在所示小程序中使用钱包SDK取得本次code。
          </p>
          <p v-else>
            此档案支持尝试生成独立小程序URL
            Link；也可继续使用以下SDK回调资料取得本次code。
          </p>
          <label
            >固定SDK回调资料<textarea
              readonly
              rows="8"
              :value="JSON.stringify(launch, null, 2)"
              autocomplete="off"
              spellcheck="false"
            /></label
          ><label
            >本次SDK临时code<input
              v-model="code"
              type="password"
              maxlength="4096"
              autocomplete="off"
              spellcheck="false"
              :disabled="busy || !!intent" /></label
          ><button :disabled="busy || !code || !!intent" @click="checkComplete">
            检查并兑换本次临时码
          </button></template
        >
        <section
          v-if="linkAvailable && !expired"
          class="profile-url-link"
          aria-label="微信独立小程序URL Link"
        >
          <h3>微信独立小程序URL Link</h3>
          <p>按需为本次原始请求生成，打开后仍须由所示小程序完成SDK身份交换。</p>
          <button
            data-action="profile-link-generate"
            :disabled="!canGenerateLink"
            @click="generateLink"
          >
            生成本次小程序URL Link
          </button>
          <button
            v-if="linkAttempted"
            data-action="profile-link-query"
            :disabled="busy"
            @click="queryLink"
          >
            查询已有URL Link结果
          </button>
          <p v-if="!linkAttempted && !canGenerateLink && !busy" role="status">
            生成前须保留超过70秒；当前原请求剩余时间不足。
          </p>
          <p v-if="linkUncertain && !busy" role="alert">
            URL Link生成结果不确定，本次请求不会再次生成；仅可手动查询已有结果。
          </p>
          <template v-if="link">
            <p role="status">
              链接状态：{{
                {
                  creating: "生成处理中",
                  ready: "已取得返回链接",
                  failed: "生成失败",
                  unknown: "结果不确定",
                }[link.status]
              }}
            </p>
            <p>
              请求截止值：{{
                link.requested_expires_at
              }}。此值不是厂商返回的实际有效期证明。
            </p>
            <a
              v-if="link.status === 'ready' && link.url_link"
              data-action="profile-link-open"
              :href="link.url_link"
              target="_blank"
              rel="noopener noreferrer"
              referrerpolicy="no-referrer"
              @click="openLink"
              >打开本次微信小程序链接</a
            >
            <button
              v-if="link.status === 'ready' && link.url_link"
              data-action="profile-link-qr"
              :disabled="busy"
              @click="showLinkQR"
            >
              显示本次身份查询二维码
            </button>
            <img
              v-if="linkQR && link.status === 'ready' && link.url_link"
              :src="linkQR"
              alt="本次微信小程序身份查询二维码"
              width="240"
              height="240"
            />
          </template>
        </section>
        <div v-if="intent" role="dialog" aria-label="确认独立身份交换">
          <p>
            确认仅为应用
            {{ current.app_id }}
            兑换本次SDK临时码？不会创建订单或付款。未知结果只能查询原请求。
          </p>
          <button :disabled="busy" @click="complete">确认兑换标识</button
          ><button :disabled="busy" @click="intent = null">取消兑换</button>
        </div>
        <p v-if="current.status === 'processing'" role="status">
          兑换处理中或响应未确定，请查询原请求，禁止重复提交临时码。
        </p>
        <button :disabled="busy || expired" @click="query">
          查询独立授权状态</button
        ><button
          v-if="current.status === 'verified' && !expired"
          :disabled="busy"
          @click="reveal"
        >
          单次查看独立标识
        </button>
        <div v-if="subject" role="status">
          <p>
            {{ label }}：<output>{{ subject }}</output>
          </p>
          <p>所属AppID：{{ current.app_id }}</p>
          <button @click="clearSecrets">清除独立标识显示</button>
        </div>
      </div>
    </template>
  </section>
</template>
<style scoped>
textarea {
  width: 100%;
  max-width: 100%;
  box-sizing: border-box;
  font-family: monospace;
}
input {
  max-width: 100%;
  box-sizing: border-box;
}
[role="dialog"] {
  padding: 1rem;
  border: 1px solid #007c91;
  border-radius: 8px;
  margin: 1rem 0;
}
a {
  display: inline-block;
  margin: 1rem 0;
  overflow-wrap: anywhere;
}
.profile-url-link {
  border-top: 1px solid var(--line);
  margin: 1rem 0;
  padding-top: 1rem;
  overflow-wrap: anywhere;
}
.profile-url-link button {
  margin: 0 0.5rem 0.5rem 0;
}
.profile-url-link img {
  display: block;
  max-width: 100%;
  height: auto;
}
</style>
