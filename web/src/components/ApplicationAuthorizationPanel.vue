<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from "vue";
import type { ApiClient } from "../api";
import type { Area } from "../menu";
import {
  applicationAuthorizationID,
  parseApplicationAuthorizationCapabilities,
  parseApplicationAuthorizationCreated,
  parseApplicationAuthorizationRequest,
  parseApplicationAuthorizationResult,
  type ApplicationAuthorizationProfile,
  type ApplicationAuthorizationRequest,
  type ApplicationAuthorizationResult,
} from "../application-authorization";

const props = defineProps<{
  client: ApiClient;
  area: Area;
  ownSession: boolean;
  adminClass?: string;
}>();
const allowed = computed(
  () =>
    props.area === "admin" && props.ownSession && props.adminClass === "super",
);
const merchant = ref(""),
  selected = ref(""),
  profiles = ref<ApplicationAuthorizationProfile[]>([]),
  authorizationOrigin = ref(""),
  available = ref(false),
  reason = ref(""),
  error = ref(""),
  busy = ref(false),
  confirmation = ref(false),
  launchKind = ref("browser"),
  clock = ref(Date.now());
const current = ref<ApplicationAuthorizationRequest | null>(null),
  snapshot = ref<ApplicationAuthorizationProfile | null>(null),
  authorization = ref(""),
  result = ref<ApplicationAuthorizationResult | null>(null),
  creationUncertain = ref(false),
  revealAttempted = ref(false),
  revealUncertain = ref(false),
  launched = ref(false);
const selectedProfile = computed(() =>
  profiles.value.find((p) => p.id === selected.value),
);
const expired = computed(
  () => !!current.value && Date.parse(current.value.expires_at) <= clock.value,
);
const statusLabel = computed(() =>
  expired.value
    ? "已过期"
    : current.value
      ? {
          pending: "等待授权",
          processing: "正在处理",
          verified: "授权已验证",
          failed: "授权失败",
          revealed: "已单次领取",
        }[current.value.status]
      : "",
);
const canCreate = computed(
  () =>
    allowed.value &&
    available.value &&
    !!selectedProfile.value &&
    !busy.value &&
    !creationUncertain.value &&
    !current.value,
);
let epoch = 0,
  active = true,
  controller: AbortController | undefined;

function clearSecrets() {
  authorization.value = "";
  result.value = null;
}
function reset(clearProfiles = false, explicit = false) {
  epoch++;
  controller?.abort();
  busy.value = false;
  confirmation.value = false;
  current.value = null;
  snapshot.value = null;
  clearSecrets();
  revealAttempted.value = false;
  revealUncertain.value = false;
  launched.value = false;
  error.value = "";
  if (explicit) creationUncertain.value = false;
  if (clearProfiles) {
    profiles.value = [];
    selected.value = "";
    authorizationOrigin.value = "";
    available.value = false;
    reason.value = "";
  }
}
watch(
  () => [
    props.client,
    props.area,
    props.ownSession,
    props.adminClass,
    merchant.value,
  ],
  () => reset(true),
  { flush: "sync" },
);
watch(
  selected,
  () => {
    reset();
    launchKind.value = "browser";
  },
  { flush: "sync" },
);
watch(launchKind, () => {
  confirmation.value = false;
});
async function run(
  action: (signal: AbortSignal, ticket: number) => Promise<void>,
  failureMessage: string,
) {
  if (!allowed.value || busy.value) return;
  controller?.abort();
  controller = new AbortController();
  const ticket = ++epoch;
  busy.value = true;
  error.value = "";
  try {
    await action(controller.signal, ticket);
  } catch {
    if (active && ticket === epoch) {
      clearSecrets();
      error.value = failureMessage;
    }
  } finally {
    if (active && ticket === epoch) busy.value = false;
  }
}
async function capabilities() {
  if (!allowed.value) return;
  if (!applicationAuthorizationID(merchant.value)) {
    error.value = "请输入规范商户ID";
    return;
  }
  if (current.value) return;
  // Clear a selected old profile before acquiring a request ticket. The sync
  // selection watcher must not cancel this capability read itself.
  selected.value = "";
  profiles.value = [];
  authorizationOrigin.value = "";
  available.value = false;
  reason.value = "";
  const mid = merchant.value;
  await run(async (signal, ticket) => {
    const data = await props.client.request(
      `/application-authorizations/capabilities?merchant_id=${encodeURIComponent(mid)}`,
      { signal },
    );
    if (!active || ticket !== epoch) return;
    const parsed = parseApplicationAuthorizationCapabilities(data, mid);
    profiles.value = parsed.profiles;
    authorizationOrigin.value = parsed.authorization_origin;
    available.value = parsed.available;
    reason.value = parsed.reason;
  }, "无法读取应用授权档案，请核对当前会话与商户");
}
function review() {
  if (canCreate.value) confirmation.value = true;
}
async function create() {
  const p = selectedProfile.value;
  if (!canCreate.value || !confirmation.value || !p) return;
  confirmation.value = false;
  const mid = merchant.value,
    selectedLaunch = launchKind.value,
    originalOrigin = authorizationOrigin.value;
  creationUncertain.value = true;
  await run(async (signal, ticket) => {
    const data = await props.client.request("/application-authorizations", {
      method: "POST",
      body: {
        merchant_id: mid,
        profile_id: p.id,
        profile_revision: p.revision,
      },
      signal,
    });
    if (!active || ticket !== epoch) return;
    const created = parseApplicationAuthorizationCreated(
      data,
      p,
      mid,
      originalOrigin,
    );
    snapshot.value = { ...p, app_types: [...p.app_types] };
    current.value = created.request;
    creationUncertain.value = false;
    clock.value = Date.now();
    if (!expired.value)
      authorization.value =
        selectedLaunch === "wallet" ? created.walletURL : created.browserURL;
  }, "创建结果不确定，请勿重复创建；清除并重新选择后再操作");
}
function launch(event: MouseEvent) {
  if (launched.value || busy.value || expired.value || !authorization.value) {
    event.preventDefault();
    return;
  }
  launched.value = true;
  // Let this explicit click open its one selected local launch route, then
  // remove the route from this panel. No automatic window or second launch.
  authorization.value = "";
}
async function query() {
  const q = current.value,
    p = snapshot.value;
  if (!q || !p || expired.value) return;
  await run(async (signal, ticket) => {
    const data = await props.client.request(
      `/application-authorizations/${q.request_id}`,
      { signal },
    );
    if (!active || ticket !== epoch) return;
    const parsed = parseApplicationAuthorizationRequest(
      data,
      p,
      q.merchant_id,
      q,
    );
    current.value = parsed;
    clock.value = Date.now();
    if (parsed.status !== "pending") authorization.value = "";
    if (expired.value || parsed.status === "failed") clearSecrets();
    // A metadata response never reverses a locally attempted one-time reveal.
  }, "无法读取本次授权状态，请核对原会话与档案");
}
async function reveal() {
  const q = current.value;
  if (
    !q ||
    q.status !== "verified" ||
    revealAttempted.value ||
    expired.value ||
    busy.value
  )
    return;
  revealAttempted.value = true;
  revealUncertain.value = true;
  clearSecrets();
  await run(async (signal, ticket) => {
    const data = await props.client.request(
      `/application-authorizations/${q.request_id}/reveal`,
      { method: "POST", body: {}, signal },
    );
    if (!active || ticket !== epoch) return;
    clock.value = Date.now();
    if (expired.value) return;
    result.value = parseApplicationAuthorizationResult(data, q);
    revealUncertain.value = false;
    current.value = { ...q, status: "revealed" };
  }, "读取结果不确定，本次领取已按消耗处理，请勿重试");
}
const timer = setInterval(() => {
  clock.value = Date.now();
  if (expired.value && (authorization.value || result.value || busy.value)) {
    epoch++;
    controller?.abort();
    busy.value = false;
    confirmation.value = false;
    clearSecrets();
  }
}, 1000);
onBeforeUnmount(() => {
  active = false;
  clearInterval(timer);
  reset(true, true);
});
</script>

<template>
  <section
    class="panel application-authorization"
    aria-label="支付宝应用授权工具"
  >
    <h2>支付宝应用授权</h2>
    <p class="intro">
      获取商家授予ISV应用的授权Token。此结果是应用凭证，独立于用户标识查询。
    </p>
    <p v-if="!allowed" role="status">仅本人超级管理员会话可使用此工具。</p>
    <template v-else>
      <div class="app-form">
        <label
          >商户ID<input
            v-model="merchant"
            aria-label="应用授权商户ID"
            inputmode="numeric"
            autocomplete="off"
            spellcheck="false"
        /></label>
        <button
          data-action="app-capabilities"
          :disabled="busy || !!current"
          @click="capabilities"
        >
          读取应用授权档案
        </button>
        <label
          >应用授权档案<select
            v-model="selected"
            aria-label="应用授权档案"
            :disabled="busy || !available || !!current"
          >
            <option value="">请选择档案</option>
            <option v-for="p in profiles" :key="p.id" :value="p.id">
              {{
                p.launch_kind === "basic" ? "基础应用授权" : "指定应用授权"
              }}
              · #{{ p.id }} · 版本{{ p.revision }}
            </option>
          </select></label
        >
      </div>
      <p v-if="reason" role="status">{{ reason }}</p>
      <div v-if="selectedProfile" class="app-binding">
        <div>
          <span>ISV应用AppID</span
          ><code>{{ selectedProfile.issuer_app_id }}</code>
        </div>
        <div>
          <span>预期授权应用AppID</span
          ><code>{{ selectedProfile.expected_auth_app_id }}</code>
        </div>
        <p v-if="selectedProfile.app_types.length">
          允许的应用类型：{{ selectedProfile.app_types.join("、") }}
        </p>
      </div>
      <label
        v-if="selectedProfile?.launch_kind === 'assigned'"
        class="launch-choice"
        >打开方式<select
          v-model="launchKind"
          aria-label="应用授权打开方式"
          :disabled="busy || !!current"
        >
          <option value="browser">浏览器授权</option>
          <option value="wallet">支付宝钱包授权</option>
        </select></label
      >
      <div class="app-actions">
        <button
          class="primary"
          data-action="app-review"
          :disabled="!canCreate"
          @click="review"
        >
          核对并创建应用授权
        </button>
        <button
          data-action="app-reset"
          :disabled="busy"
          @click="reset(true, true)"
        >
          清除并重新选择
        </button>
      </div>
      <p v-if="creationUncertain && !busy" role="alert">
        创建结果不确定，尚未自动重试。请先清除并重新选择。
      </p>
      <p v-if="error" role="alert">{{ error }}</p>
      <div
        v-if="confirmation && selectedProfile"
        class="app-confirmation"
        role="dialog"
        aria-modal="false"
        aria-label="确认创建应用授权"
      >
        <h3>确认本次应用授权</h3>
        <p>
          商户{{ merchant }} · 档案#{{ selectedProfile.id }} · 版本{{
            selectedProfile.revision
          }}
        </p>
        <p>
          ISV应用：<code>{{ selectedProfile.issuer_app_id }}</code
          ><br />授权应用：<code>{{
            selectedProfile.expected_auth_app_id
          }}</code>
        </p>
        <p>
          {{
            selectedProfile.launch_kind === "basic"
              ? "基础应用授权"
              : "指定应用授权"
          }}
          ·
          {{
            launchKind === "wallet" ? "支付宝钱包授权" : "浏览器授权"
          }}。创建后仍需本人在支付宝完成授权。
        </p>
        <div class="app-actions">
          <button
            class="primary"
            data-action="app-confirm-create"
            :disabled="busy"
            @click="create"
          >
            确认创建应用授权</button
          ><button :disabled="busy" @click="confirmation = false">
            返回核对
          </button>
        </div>
      </div>
      <div v-if="current" class="app-request">
        <h3>本次授权请求</h3>
        <p role="status">状态：{{ statusLabel }}</p>
        <p>
          本次显示截止：<time>{{ current.expires_at }}</time>
        </p>
        <a
          v-if="authorization && !expired"
          data-action="app-launch"
          :href="authorization"
          target="_blank"
          rel="noopener noreferrer"
          referrerpolicy="no-referrer"
          @click="launch"
          >{{
            launchKind === "wallet"
              ? "打开本次支付宝钱包授权"
              : "打开本次浏览器授权"
          }}</a
        >
        <p v-if="launched && !expired">
          已打开所选授权方式，请完成后手动查询状态。
        </p>
        <div class="app-actions">
          <button
            data-action="app-query"
            :disabled="busy || expired"
            @click="query"
          >
            查询本次应用授权状态</button
          ><button
            v-if="current.status === 'verified' && !revealAttempted && !expired"
            data-action="app-reveal"
            :disabled="busy"
            @click="reveal"
          >
            单次查看应用授权Token
          </button>
        </div>
        <p v-if="revealUncertain && !busy" role="alert">
          读取结果不确定，本次领取已按消耗处理，查询状态不会恢复领取。
        </p>
        <div v-if="result && !expired" class="app-result">
          <p>
            授权商户UID：<code>{{ result.user_id }}</code>
          </p>
          <label
            >应用授权Token（单次领取）<input
              aria-label="应用授权Token"
              type="password"
              :value="result.app_auth_token"
              readonly
              autocomplete="off"
              spellcheck="false"
              autocapitalize="off"
          /></label>
          <p>
            {{
              result.token_expires_in_seconds === "0"
                ? "厂商有效期未知"
                : `厂商返回Token有效期：${result.token_expires_in_seconds}秒`
            }}。本页显示截止时间独立于厂商凭证有效期。
          </p>
          <button data-action="app-clear-token" @click="result = null">
            清除Token显示
          </button>
        </div>
      </div>
      <p class="app-note">
        Token仅在本页内存中单次显示，切换商户、档案、会话或离开页面后清除。此工具不自动绑定收款通道。
      </p>
    </template>
  </section>
</template>

<style scoped>
.application-authorization {
  padding: 24px;
  margin-top: 16px;
  overflow-wrap: anywhere;
}
.intro,
.app-note {
  color: var(--muted);
  line-height: 1.7;
}
.app-form {
  display: grid;
  grid-template-columns: minmax(120px, 1fr) auto minmax(180px, 2fr);
  align-items: end;
  gap: 16px;
}
.app-form label,
.launch-choice {
  margin: 12px 0;
}
.app-form button {
  margin-bottom: 12px;
}
select {
  width: 100%;
  margin-top: 9px;
  min-height: 42px;
}
.app-binding {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  border-left: 3px solid var(--accent);
  background: var(--bg-raised);
  padding: 16px;
  gap: 16px;
  margin: 16px 0;
}
.app-binding span {
  display: block;
  color: var(--muted);
  margin-bottom: 8px;
}
.app-binding p {
  grid-column: 1 / -1;
  margin: 0;
}
code {
  font-family: Consolas, "SFMono-Regular", monospace;
  font-size: 13px;
  overflow-wrap: anywhere;
}
.app-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  margin: 16px 0;
}
.app-confirmation,
.app-request {
  border-top: 1px solid var(--line);
  margin-top: 22px;
  padding-top: 20px;
}
.app-confirmation {
  background: var(--bg-raised);
  padding: 18px;
  border: 1px solid var(--line);
  border-radius: 6px;
}
.app-result {
  border: 1px solid var(--line);
  padding: 16px;
  border-radius: 6px;
}
.app-note {
  margin: 24px 0 0;
  font-size: 12px;
}
@media (max-width: 700px) {
  .application-authorization {
    padding: 16px;
  }
  .app-form,
  .app-binding {
    grid-template-columns: minmax(0, 1fr);
  }
  .app-form {
    gap: 0;
  }
  .app-form button {
    justify-self: start;
    margin-top: 0;
  }
  .app-actions button {
    min-height: 42px;
  }
}
</style>
