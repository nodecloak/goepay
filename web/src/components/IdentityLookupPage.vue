<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import QRCode from "qrcode";
import type { ApiClient } from "../api";
import type { Area } from "../menu";
import IdentityProfileLookup from "./IdentityProfileLookup.vue";
import ApplicationAuthorizationPanel from "./ApplicationAuthorizationPanel.vue";
const props = defineProps<{
  client: ApiClient;
  area: Area;
  ownSession?: boolean;
  adminClass?: string;
}>();
const tab = ref<"legacy" | "profiles" | "application">("legacy");
const applicationAllowed = computed(
  () =>
    props.area === "admin" &&
    props.ownSession === true &&
    props.adminClass === "super",
);
type Channel = {
  channel_id: string;
  provider: "wxpay" | "alipay";
  app_id: string;
};
type Lookup = Channel & {
  request_id: string;
  merchant_id: string;
  scope: string;
  status: string;
  expires_at: string;
};
const merchant = ref(""),
  resolvedMerchant = ref(""),
  selected = ref(""),
  channels = ref<Channel[]>([]),
  available = ref(false),
  reason = ref(""),
  error = ref(""),
  busy = ref(false);
const current = ref<Lookup>(),
  authorization = ref(""),
  qr = ref(""),
  subject = ref(""),
  label = ref(""),
  clock = ref(Date.now());
let epoch = 0,
  controller: AbortController | undefined;
const expired = computed(
  () => !!current.value && Date.parse(current.value.expires_at) <= clock.value,
);
const selectedChannel = computed(() =>
  channels.value.find((c) => c.channel_id === selected.value),
);
const safeText = (s: unknown, max = 256): s is string =>
  typeof s === "string" &&
  s.length > 0 &&
  s.length <= max &&
  !/[\u0000-\u001f\u007f]/.test(s);
const positive = (s: unknown): s is string =>
  typeof s === "string" &&
  /^[1-9][0-9]{0,18}$/.test(s) &&
  BigInt(s) <= 9223372036854775807n;
function clearMaterial() {
  authorization.value = "";
  qr.value = "";
  subject.value = "";
  label.value = "";
}
function reset(clearChannels = false) {
  epoch++;
  controller?.abort();
  busy.value = false;
  current.value = undefined;
  clearMaterial();
  error.value = "";
  if (clearChannels) {
    channels.value = [];
    available.value = false;
    reason.value = "";
    selected.value = "";
  }
}
watch(tab, () => reset());
watch(
  () => [props.client, props.area, merchant.value],
  () => reset(true),
);
watch(selected, () => reset());
function parseChannel(v: any): Channel {
  if (
    !v ||
    !positive(v.channel_id) ||
    !["wxpay", "alipay"].includes(v.provider) ||
    !safeText(v.app_id)
  )
    throw Error("授权通道返回格式错误");
  return { channel_id: v.channel_id, provider: v.provider, app_id: v.app_id };
}
function parseLookup(v: any, expected: Channel, mid: string): Lookup {
  const c = parseChannel(v);
  if (
    !v ||
    !/^[a-f0-9]{64}$/.test(v.request_id) ||
    v.merchant_id !== mid ||
    c.channel_id !== expected.channel_id ||
    c.provider !== expected.provider ||
    c.app_id !== expected.app_id ||
    v.scope !== (c.provider === "wxpay" ? "snsapi_base" : "auth_base") ||
    !["pending", "processing", "verified", "failed", "revealed"].includes(
      v.status,
    ) ||
    typeof v.expires_at !== "string" ||
    !Number.isFinite(Date.parse(v.expires_at)) ||
    Date.parse(v.expires_at) > Date.now() + 310000 ||
    "subject_id" in v
  )
    throw Error("授权请求返回格式错误");
  return {
    ...c,
    request_id: v.request_id,
    merchant_id: mid,
    scope: v.scope,
    status: v.status,
    expires_at: v.expires_at,
  };
}
async function run(
  action: (signal: AbortSignal, ticket: number) => Promise<void>,
) {
  if (busy.value) return;
  const ticket = ++epoch;
  controller?.abort();
  controller = new AbortController();
  busy.value = true;
  error.value = "";
  try {
    await action(controller.signal, ticket);
  } catch (e) {
    if (ticket === epoch) {
      clearMaterial();
      error.value = e instanceof Error ? e.message : "操作失败";
    }
  } finally {
    if (ticket === epoch) busy.value = false;
  }
}
async function capabilities() {
  if (props.area === "admin" && !positive(merchant.value)) {
    error.value = "请输入有效商户 ID";
    return;
  }
  await run(async (signal, ticket) => {
    const data = await props.client.request<any>(
      "/identity-lookups/capabilities" +
        (props.area === "admin"
          ? `?merchant_id=${encodeURIComponent(merchant.value)}`
          : ""),
      { signal },
    );
    if (ticket !== epoch) return;
    if (
      !data ||
      !positive(data.merchant_id) ||
      (props.area === "admin" && data.merchant_id !== merchant.value) ||
      typeof data.available !== "boolean" ||
      typeof data.reason !== "string" ||
      !Array.isArray(data.channels)
    )
      throw Error("授权能力返回格式错误");
    const parsed = data.channels.map(parseChannel);
    if (
      new Set(parsed.map((c: Channel) => c.channel_id)).size !==
        parsed.length ||
      (!data.available && parsed.length)
    )
      throw Error("授权能力返回格式错误");
    resolvedMerchant.value = data.merchant_id;
    channels.value = parsed;
    available.value = data.available;
    reason.value = data.reason;
  });
}
async function create() {
  const c = selectedChannel.value,
    mid = props.area === "admin" ? merchant.value : resolvedMerchant.value;
  if (!available.value || !c) return;
  clearMaterial();
  current.value = undefined;
  await run(async (signal, ticket) => {
    const data = await props.client.request<any>("/identity-lookups", {
      method: "POST",
      body: {
        ...(props.area === "admin" ? { merchant_id: mid } : {}),
        channel_id: c.channel_id,
      },
      signal,
    });
    if (ticket !== epoch) return;
    const parsed = parseLookup(data, c, mid),
      url = new URL(data.authorization_url);
    if (
      parsed.status !== "pending" ||
      url.protocol !== "https:" ||
      url.username ||
      url.password ||
      url.search ||
      url.hash ||
      !/^\/identity\/authorize\/[a-f0-9]{64}$/.test(url.pathname)
    )
      throw Error("授权地址返回格式错误");
    const image = await QRCode.toDataURL(url.href, { width: 240, margin: 2 });
    if (ticket !== epoch) return;
    current.value = parsed;
    clock.value = Date.now();
    if (!expired.value) {
      authorization.value = url.href;
      qr.value = image;
    }
  });
}
async function query() {
  const previous = current.value;
  if (!previous || expired.value) return;
  await run(async (signal, ticket) => {
    const data = await props.client.request<any>(
      `/identity-lookups/${previous.request_id}`,
      { signal },
    );
    if (ticket !== epoch) return;
    const parsed = parseLookup(data, previous, previous.merchant_id);
    if (parsed.request_id !== previous.request_id)
      throw Error("授权请求不匹配");
    current.value = parsed;
    clock.value = Date.now();
    if (parsed.status !== "pending") {
      authorization.value = "";
      qr.value = "";
    }
  });
}
async function reveal() {
  const previous = current.value;
  if (!previous || previous.status !== "verified" || expired.value) return;
  // A lost reply must not cause an automatic second reveal.
  current.value = { ...previous, status: "revealed" };
  clearMaterial();
  await run(async (signal, ticket) => {
    const data = await props.client.request<any>(
      `/identity-lookups/${previous.request_id}/reveal`,
      { method: "POST", body: {}, signal },
    );
    if (ticket !== epoch) return;
    clock.value = Date.now();
    if (expired.value) return;
    if (
      !data ||
      data.provider !== previous.provider ||
      data.app_id !== previous.app_id ||
      !safeText(data.subject_id, 512) ||
      !safeText(data.label, 80)
    )
      throw Error("授权结果返回格式错误");
    subject.value = data.subject_id;
    label.value = data.label;
  });
}
const timer = setInterval(() => {
  clock.value = Date.now();
  if (expired.value) {
    epoch++;
    controller?.abort();
    busy.value = false;
    clearMaterial();
  }
}, 1000);
onMounted(() => {
  if (props.area === "merchant") void capabilities();
});
onBeforeUnmount(() => {
  clearInterval(timer);
  reset(true);
});
</script>
<template>
  <nav aria-label="用户标识查询用途">
    <button :aria-pressed="tab === 'legacy'" @click="tab = 'legacy'">
      原收款应用网页授权</button
    ><button :aria-pressed="tab === 'profiles'" @click="tab = 'profiles'">
      独立应用与小程序
    </button>
    <button
      v-if="applicationAllowed"
      :aria-pressed="tab === 'application'"
      @click="tab = 'application'"
    >
      支付宝应用授权
    </button>
  </nav>
  <ApplicationAuthorizationPanel
    v-if="tab === 'application' && applicationAllowed"
    :client="client"
    :area="area"
    :own-session="ownSession === true"
    :admin-class="adminClass"
  />
  <IdentityProfileLookup
    v-else-if="tab === 'profiles'"
    :client="client"
    :area="area"
    :own-session="ownSession === true"
  />
  <section v-else class="panel" aria-label="获取支付用户标识">
    <h2>获取支付用户标识</h2>
    <p>
      选择商户及应用，使用微信或支付宝完成授权。授权结果只允许发起会话读取一次，五分钟内有效。
    </p>
    <div class="form-grid">
      <label v-if="area === 'admin'"
        >商户 ID<input
          v-model.trim="merchant"
          aria-label="商户 ID"
          inputmode="numeric"
          autocomplete="off"
      /></label>
      <button data-action="capabilities" :disabled="busy" @click="capabilities">
        读取授权通道
      </button>
      <label
        >应用通道<select v-model="selected" :disabled="busy || !available">
          <option value="">请选择通道</option>
          <option
            v-for="c in channels"
            :key="c.channel_id"
            :value="c.channel_id"
          >
            {{ c.provider === "wxpay" ? "微信" : "支付宝" }} · {{ c.app_id }} ·
            #{{ c.channel_id }}
          </option>
        </select></label
      >
      <button
        data-action="create"
        :disabled="busy || !available || !selectedChannel"
        @click="create"
      >
        创建授权请求
      </button>
    </div>
    <p v-if="reason" role="status">{{ reason }}</p>
    <p v-if="error" role="alert">{{ error }}</p>
    <div v-if="current">
      <p>
        请求状态：{{ expired ? "已过期" : current.status }} · 应用：{{
          current.app_id
        }}
      </p>
      <p>到期时间：{{ current.expires_at }}</p>
      <template v-if="authorization && !expired"
        ><img
          v-if="qr"
          :src="qr"
          width="240"
          height="240"
          alt="支付平台授权二维码"
        />
        <p>
          <a
            data-action="authorize"
            :href="authorization"
            target="_blank"
            rel="noopener noreferrer"
            referrerpolicy="no-referrer"
            >打开支付平台授权</a
          >
        </p></template
      >
      <button data-action="query" :disabled="busy || expired" @click="query">
        查询授权状态
      </button>
      <button
        v-if="current.status === 'verified' && !expired"
        data-action="reveal"
        :disabled="busy"
        @click="reveal"
      >
        读取一次授权结果
      </button>
      <div v-if="subject" role="status">
        <p>
          {{ label }}：<output>{{ subject }}</output>
        </p>
        <button data-action="clear-result" @click="clearMaterial">
          清除显示
        </button>
      </div>
    </div>
  </section>
</template>
