<script setup lang="ts">
import { nextTick, onBeforeUnmount, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
const route = useRoute(),
  router = useRouter();
const prefix = "/console/v1/public/testpay";
interface PayType {
  id: string;
  name: "alipay" | "wxpay";
  showname: string;
}
interface Initial {
  csrf_token: string;
  trade_no: string;
  money: string;
  paytype: PayType[];
  sitename: string;
  captcha_open: 0 | 1;
}
interface CaptchaData {
  version: 0 | 1;
  gt: string;
  challenge: string;
  new_captcha: boolean;
  success: 1;
  captcha_token: string;
}
interface CaptchaInstance {
  onReady(callback: () => void): CaptchaInstance;
  onSuccess(callback: () => void): CaptchaInstance;
  onError(callback: (error?: unknown) => void): CaptchaInstance;
  onClose?(callback: () => void): CaptchaInstance;
  getValidate(): unknown;
  showCaptcha?(): void;
  verify?(): void;
  reset?(): void;
  destroy?(): void;
}
type BrowserSDK = Window & {
  initGeetest?: (
    options: Record<string, unknown>,
    callback: (captcha: CaptchaInstance) => void,
  ) => void;
  initGeetest4?: (
    options: Record<string, unknown>,
    callback: (captcha: CaptchaInstance) => void,
  ) => void;
};
class PublicError extends Error {
  constructor(
    message: string,
    public status: number,
    public data: unknown,
  ) {
    super(message);
  }
}
const state = ref<Initial>(),
  money = ref("1.00"),
  paidMoney = ref(""),
  siteName = ref("测试支付");
const loading = ref(false),
  busy = ref(false),
  unknown = ref(false),
  error = ref(""),
  ready = ref(false);
const moneyInput = ref<HTMLInputElement>(),
  errorBox = ref<HTMLElement>();
let active = true,
  epoch = 0,
  controller: AbortController | undefined,
  captcha: CaptchaInstance | undefined,
  captchaData: CaptchaData | undefined;
let stopCaptcha: (() => void) | undefined,
  hasWrite = false,
  originalTrade = "";
const scripts = new Set<HTMLScriptElement>();
function record(raw: unknown, keys: string[]): Record<string, unknown> {
  if (!raw || typeof raw !== "object" || Array.isArray(raw))
    throw Error("公开测试数据格式无法核对");
  const v = raw as Record<string, unknown>;
  if (
    Object.keys(v).length !== keys.length ||
    keys.some((key) => !Object.hasOwn(v, key))
  )
    throw Error("公开测试数据格式无法核对");
  return v;
}
function text(v: unknown, max: number): v is string {
  return (
    typeof v === "string" &&
    v.length > 0 &&
    Array.from(v).length <= max &&
    v.trim() === v &&
    !/[\u0000-\u001f\u007f-\u009f]/.test(v)
  );
}
function amount(value: string): string | undefined {
  const raw = value.trim();
  if (!/^(0|[1-9][0-9]{0,6})(\.[0-9]{1,2})?$/.test(raw)) return;
  const [whole, fraction = ""] = raw.split(".");
  const cents = BigInt(whole) * 100n + BigInt(fraction.padEnd(2, "0"));
  if (cents <= 0n || cents > 99999999n) return;
  return whole + "." + fraction.padEnd(2, "0");
}
function parseInitial(raw: unknown): Initial {
  const v = record(raw, [
    "csrf_token",
    "trade_no",
    "money",
    "paytype",
    "sitename",
    "captcha_open",
  ]);
  if (
    typeof v.csrf_token !== "string" ||
    !/^[A-Za-z0-9_-]{43}$/.test(v.csrf_token) ||
    typeof v.trade_no !== "string" ||
    !/^[A-Za-z0-9_-]{1,128}$/.test(v.trade_no) ||
    typeof v.money !== "string" ||
    amount(v.money) !== v.money ||
    !text(v.sitename, 64) ||
    (v.captcha_open !== 0 && v.captcha_open !== 1) ||
    !Array.isArray(v.paytype) ||
    v.paytype.length > 100
  )
    throw Error("公开测试初始化格式无法核对");
  const ids = new Set<string>();
  const types = v.paytype.map((raw) => {
    const t = record(raw, ["id", "name", "showname"]);
    if (
      typeof t.id !== "string" ||
      !/^[1-9][0-9]{0,18}$/.test(t.id) ||
      BigInt(t.id) > 9223372036854775807n ||
      ids.has(t.id) ||
      (t.name !== "alipay" && t.name !== "wxpay") ||
      !text(t.showname, 100)
    )
      throw Error("公开支付方式格式无法核对");
    ids.add(t.id);
    return { id: t.id, name: t.name, showname: t.showname } as PayType;
  });
  return {
    csrf_token: v.csrf_token,
    trade_no: v.trade_no,
    money: v.money,
    paytype: types,
    sitename: v.sitename,
    captcha_open: v.captcha_open,
  };
}
function parseCaptcha(raw: unknown): CaptchaData {
  const v = record(raw, [
    "version",
    "gt",
    "challenge",
    "new_captcha",
    "success",
    "captcha_token",
  ]);
  if (
    (v.version !== 0 && v.version !== 1) ||
    typeof v.gt !== "string" ||
    !/^[a-f0-9]{32}$/.test(v.gt) ||
    typeof v.captcha_token !== "string" ||
    !/^[A-Za-z0-9_-]{43}$/.test(v.captcha_token) ||
    v.success !== 1 ||
    typeof v.challenge !== "string" ||
    typeof v.new_captcha !== "boolean" ||
    (v.version === 0 &&
      (!/^[a-f0-9]{32}$/.test(v.challenge) || v.new_captcha !== true)) ||
    (v.version === 1 && (v.challenge !== "" || v.new_captcha !== false))
  )
    throw Error("验证码初始化格式无法核对");
  return v as unknown as CaptchaData;
}
async function request(
  path: string,
  c: AbortController,
  body?: unknown,
): Promise<unknown> {
  const response = await fetch(path, {
    method: body === undefined ? "GET" : "POST",
    credentials: "same-origin",
    headers: {
      Accept: "application/json",
      ...(body === undefined ? {} : { "Content-Type": "application/json" }),
    },
    signal: c.signal,
    ...(body === undefined ? {} : { body: JSON.stringify(body) }),
  });
  let payload: { code?: unknown; data?: unknown; message?: unknown };
  try {
    payload = await response.json();
  } catch {
    throw new PublicError(
      "服务器返回格式无法核对，请核对原测试订单",
      response.status,
      null,
    );
  }
  if (!response.ok || payload.code !== 0)
    throw new PublicError(
      typeof payload.message === "string"
        ? payload.message
        : "测试支付服务不可用",
      response.status,
      payload.data,
    );
  return payload.data;
}
function publicPath() {
  return route.path === "/test" || route.path === "/test/";
}
function valid(run: number, c: AbortController) {
  return active && run === epoch && !c.signal.aborted && publicPath();
}
function clear() {
  ++epoch;
  controller?.abort();
  stopCaptcha?.();
  stopCaptcha = undefined;
  try {
    captcha?.destroy?.();
  } catch {}
  captcha = undefined;
  captchaData = undefined;
  ready.value = false;
  state.value = undefined;
  money.value = "1.00";
  paidMoney.value = "";
  error.value = "";
  loading.value = false;
  busy.value = false;
  unknown.value = false;
  hasWrite = false;
  originalTrade = "";
  for (const script of scripts) script.remove();
  scripts.clear();
}
async function script(url: string, run: number, c: AbortController) {
  await new Promise<void>((resolve, reject) => {
    const element = document.createElement("script");
    element.src = url;
    element.async = true;
    element.referrerPolicy = "no-referrer";
    scripts.add(element);
    const timeout = window.setTimeout(
      () => reject(Error("验证码加载超时")),
      8000,
    );
    c.signal.addEventListener(
      "abort",
      () => {
        clearTimeout(timeout);
        reject(Error("验证码上下文已变化"));
      },
      { once: true },
    );
    element.onload = () => {
      clearTimeout(timeout);
      valid(run, c) ? resolve() : reject(Error("验证码上下文已变化"));
    };
    element.onerror = () => {
      clearTimeout(timeout);
      reject(Error("验证码加载失败"));
    };
    document.head.appendChild(element);
  });
}
async function loadCaptcha(run: number, c: AbortController) {
  const data = parseCaptcha(
    await request("/console/v1/public/captcha?purpose=testpay", c),
  );
  if (!valid(run, c)) return;
  captchaData = data;
  const sdk = window as BrowserSDK;
  const source =
    data.version === 1
      ? "https://static.geetest.com/v4/gt4.js"
      : "https://static.geetest.com/static/tools/gt.js";
  if (data.version === 1 ? !sdk.initGeetest4 : !sdk.initGeetest)
    await script(source, run, c);
  if (!valid(run, c)) return;
  await new Promise<void>((resolve, reject) => {
    const timeout = window.setTimeout(
      () => reject(Error("验证码加载超时")),
      8000,
    );
    c.signal.addEventListener(
      "abort",
      () => {
        clearTimeout(timeout);
        reject(Error("验证码上下文已变化"));
      },
      { once: true },
    );
    const initialize = data.version === 1 ? sdk.initGeetest4 : sdk.initGeetest;
    if (!initialize) {
      clearTimeout(timeout);
      reject(Error("验证码组件不可用"));
      return;
    }
    initialize(
      data.version === 1
        ? {
            captchaId: data.gt,
            product: "bind",
            protocol: "https://",
            riskType: "slide",
            hideSuccess: true,
          }
        : {
            gt: data.gt,
            challenge: data.challenge,
            new_captcha: data.new_captcha,
            product: "bind",
            offline: false,
          },
      (instance) => {
        if (
          !instance ||
          typeof instance.onReady !== "function" ||
          typeof instance.onError !== "function" ||
          typeof instance.onSuccess !== "function" ||
          typeof instance.getValidate !== "function"
        ) {
          clearTimeout(timeout);
          reject(Error("验证码组件格式无效"));
          return;
        }
        if (!valid(run, c)) {
          instance.destroy?.();
          clearTimeout(timeout);
          reject(Error("验证码上下文已变化"));
          return;
        }
        captcha = instance;
        instance
          .onReady(() => {
            clearTimeout(timeout);
            if (valid(run, c)) {
              ready.value = true;
              resolve();
            } else reject(Error("验证码上下文已变化"));
          })
          .onError(() => {
            clearTimeout(timeout);
            reject(Error("验证码加载失败"));
          });
      },
    );
  });
}
async function showCaptcha(
  run: number,
  c: AbortController,
): Promise<Record<string, string> | undefined> {
  if (!captcha || !captchaData || !ready.value) return;
  const instance = captcha,
    data = captchaData;
  return new Promise((resolve, reject) => {
    let finished = false;
    const finish = (proof?: Record<string, string>) => {
      if (finished) return;
      finished = true;
      stopCaptcha = undefined;
      resolve(proof);
    };
    stopCaptcha = () => finish();
    instance.onSuccess(() => {
      if (!valid(run, c)) {
        finish();
        return;
      }
      try {
        const keys =
          data.version === 1
            ? [
                "captcha_id",
                "lot_number",
                "pass_token",
                "gen_time",
                "captcha_output",
              ]
            : ["geetest_challenge", "geetest_validate", "geetest_seccode"];
        const raw = record(instance.getValidate(), keys);
        const proof: Record<string, string> = {
          captcha_token: data.captcha_token,
        };
        for (const key of keys) {
          if (!text(raw[key], key === "captcha_output" ? 4096 : 2048))
            throw Error("验证码结果格式无法核对");
          proof[key] = raw[key] as string;
        }
        if (
          (data.version === 1 && proof.captcha_id !== data.gt) ||
          (data.version === 0 && proof.geetest_challenge !== data.challenge)
        )
          throw Error("验证码上下文不一致");
        finish(proof);
      } catch (reason) {
        finished = true;
        stopCaptcha = undefined;
        reject(reason);
      }
    });
    instance.onClose?.(() => finish());
    instance.onError(() => {
      if (finished) return;
      finished = true;
      stopCaptcha = undefined;
      reject(Error("验证码服务不可用"));
    });
    try {
      if (data.version === 1) {
        if (!instance.showCaptcha) throw Error("验证码组件不可用");
        instance.showCaptcha();
      } else {
        if (!instance.verify) throw Error("验证码组件不可用");
        instance.verify();
      }
    } catch (reason) {
      finished = true;
      stopCaptcha = undefined;
      reject(reason);
    }
  });
}
function checkout(raw: unknown): string {
  const v = record(raw, ["url"]);
  if (
    typeof v.url !== "string" ||
    v.url.length > 2048 ||
    !v.url.startsWith("/checkout/") ||
    v.url.startsWith("//") ||
    /[\u0000-\u0020\u007f\\#]/.test(v.url)
  )
    throw Error("支付跳转地址无法核对");
  const u = new URL(v.url, window.location.origin);
  if (
    u.origin !== window.location.origin ||
    !/^\/checkout\/[A-Za-z0-9_-]+$/.test(u.pathname) ||
    u.searchParams.getAll("expires").length !== 1 ||
    u.searchParams.getAll("token").length !== 1 ||
    Array.from(u.searchParams.keys()).some(
      (key) => key !== "expires" && key !== "token",
    ) ||
    !/^\d{10,12}$/.test(u.searchParams.get("expires") ?? "") ||
    !/^[A-Za-z0-9_-]{43}$/.test(u.searchParams.get("token") ?? "")
  )
    throw Error("支付跳转地址无法核对");
  return v.url;
}
async function original(c: AbortController, run: number, trade: string) {
  const raw = record(
    await request(prefix + "/success?trade_no=" + encodeURIComponent(trade), c),
    ["money", "sitename"],
  );
  if (
    typeof raw.money !== "string" ||
    amount(raw.money) !== raw.money ||
    !text(raw.sitename, 64)
  )
    throw Error("原订单支付结果格式无法核对");
  if (valid(run, c)) {
    paidMoney.value = raw.money;
    siteName.value = raw.sitename;
    unknown.value = false;
    state.value = undefined;
    error.value = "";
  }
}
async function load() {
  clear();
  if (!publicPath()) return;
  const run = epoch,
    c = new AbortController();
  controller = c;
  loading.value = true;
  try {
    const ok = route.query.ok,
      trade = route.query.trade_no;
    if (ok !== undefined && trade !== undefined) {
      if (
        typeof ok !== "string" ||
        typeof trade !== "string" ||
        !/^[A-Za-z0-9_-]{1,128}$/.test(trade)
      )
        throw Error("测试返回参数无效");
      try {
        await original(c, run, trade);
        if (paidMoney.value) return;
      } catch {
        if (!valid(run, c)) return;
      }
    }
    const next = parseInitial(await request(prefix + "/init", c));
    if (!valid(run, c)) return;
    state.value = next;
    money.value = next.money;
    siteName.value = next.sitename;
    originalTrade = next.trade_no;
    if (next.captcha_open === 1) await loadCaptcha(run, c);
  } catch (reason) {
    if (valid(run, c)) {
      if (reason instanceof PublicError && reason.status === 409) {
        try {
          const data = record(reason.data, ["trade_no"]);
          if (
            typeof data.trade_no === "string" &&
            /^[A-Za-z0-9_-]{1,128}$/.test(data.trade_no)
          ) {
            originalTrade = data.trade_no;
            unknown.value = true;
            hasWrite = true;
          }
        } catch {}
      }
      error.value =
        reason instanceof Error ? reason.message : "测试支付读取失败";
    }
  } finally {
    if (valid(run, c)) loading.value = false;
  }
}
async function pay(type: PayType) {
  if (
    !state.value ||
    loading.value ||
    busy.value ||
    unknown.value ||
    (state.value.captcha_open === 1 && !ready.value)
  )
    return;
  const value = amount(money.value);
  if (!value) {
    error.value = "请输入大于0、最多两位小数的有效金额";
    return;
  }
  const snapshot = state.value,
    run = epoch,
    c = new AbortController();
  controller?.abort();
  controller = c;
  busy.value = true;
  error.value = "";
  let sent = false;
  try {
    let proof: Record<string, string> | undefined = {};
    if (snapshot.captcha_open === 1) {
      proof = await showCaptcha(run, c);
      if (!proof) return;
    }
    if (!valid(run, c) || state.value !== snapshot) return;
    sent = true;
    hasWrite = true;
    const raw = await request(prefix + "/submit", c, {
      money: value,
      typeid: type.id,
      csrf_token: snapshot.csrf_token,
      ...proof,
    });
    if (!valid(run, c)) return;
    const target = checkout(raw);
    if (valid(run, c)) window.location.assign(target);
  } catch (reason) {
    if (!valid(run, c)) return;
    const known =
      reason instanceof PublicError && [400, 422].includes(reason.status);
    if (sent && !known) {
      if (reason instanceof PublicError && reason.status === 409) {
        try {
          const data = record(reason.data, ["trade_no"]);
          if (
            typeof data.trade_no === "string" &&
            /^[A-Za-z0-9_-]{1,128}$/.test(data.trade_no)
          )
            originalTrade = data.trade_no;
        } catch {
          /* Keep the already bound reference when no exact original is returned. */
        }
      }
      unknown.value = true;
      error.value = "原测试订单结果未知，请只读核对，不要重复支付。";
    } else {
      hasWrite = false;
      error.value =
        reason instanceof Error ? reason.message : "验证码或支付服务不可用";
      captcha?.reset?.();
    }
  } finally {
    if (valid(run, c)) {
      busy.value = false;
      if (error.value)
        void nextTick(() => {
          if (valid(run, c)) errorBox.value?.focus();
        });
    }
  }
}
async function recheck() {
  if (busy.value || loading.value || !originalTrade) return;
  const run = epoch,
    c = new AbortController();
  controller?.abort();
  controller = c;
  busy.value = true;
  try {
    await original(c, run, originalTrade);
  } catch (reason) {
    if (valid(run, c)) {
      unknown.value = true;
      error.value =
        reason instanceof Error ? reason.message : "原测试订单结果尚未核实";
    }
  } finally {
    if (valid(run, c)) busy.value = false;
  }
}
async function returnTest() {
  const previous = route.fullPath;
  await router.replace({ path: "/test", query: {} });
  if (route.fullPath === previous) void load();
}
function pageshow(event: PageTransitionEvent) {
  if (event.persisted) {
    if (hasWrite || unknown.value) void recheck();
    else void load();
  }
}
window.addEventListener("pageshow", pageshow);
watch(() => route.fullPath, load, { immediate: true, flush: "sync" });
onBeforeUnmount(() => {
  active = false;
  clear();
  window.removeEventListener("pageshow", pageshow);
});
</script>
<template>
  <main class="public-testpay203" aria-label="测试支付">
    <header>
      <h1>{{ siteName }} - 测试支付</h1>
      <RouterLink to="/">返回首页</RouterLink>
    </header>
    <section class="panel testpay-card">
      <template v-if="paidMoney"
        ><h2>订单已支付成功！</h2>
        <p>支付金额：¥ {{ paidMoney }}</p>
        <button class="primary" type="button" @click="returnTest">
          返回测试
        </button></template
      >
      <template v-else
        ><h2>测试支付</h2>
        <p v-if="loading" role="status">正在加载测试支付…</p>
        <p
          v-if="error"
          ref="errorBox"
          class="alert error"
          role="alert"
          tabindex="-1"
        >
          {{ error }}
        </p>
        <form v-if="state" novalidate @submit.prevent>
          <label
            >商户订单号<input
              :value="state.trade_no"
              name="trade_no"
              readonly
              autocomplete="off"
          /></label>
          <label
            >商品名称<input value="支付测试" readonly autocomplete="off"
          /></label>
          <label
            >付款金额
            <div class="testpay-money">
              <span aria-hidden="true">¥</span
              ><input
                ref="moneyInput"
                v-model="money"
                name="money"
                inputmode="decimal"
                maxlength="10"
                autocomplete="off"
                :disabled="loading || busy || unknown"
              /></div
          ></label>
          <p v-if="state.captcha_open === 1" class="muted">
            请点击下方支付方式，完成滑动验证后即可跳转支付
          </p>
          <p v-if="state.paytype.length === 0" class="muted">
            暂无可用支付方式，请稍后重试。
          </p>
          <div class="testpay-methods">
            <button
              v-for="type in state.paytype"
              :key="type.id"
              type="button"
              class="primary"
              :disabled="
                loading ||
                busy ||
                unknown ||
                (state.captcha_open === 1 && !ready)
              "
              @click="pay(type)"
            >
              {{ type.showname }}
            </button>
          </div>
        </form>
        <button
          v-if="unknown"
          type="button"
          :disabled="busy || loading"
          @click="recheck"
        >
          核对原测试订单
        </button>
        <button
          v-else-if="!state && !loading"
          type="button"
          :disabled="busy"
          @click="load"
        >
          重新读取测试
        </button>
      </template>
      <footer>
        {{ siteName }} © {{ new Date().getFullYear() }} All Rights Reserved.
      </footer>
    </section>
  </main>
</template>
<style scoped>
.public-testpay203 {
  width: min(560px, calc(100% - 32px));
  margin: 40px auto;
  color: var(--text, var(--ink));
}
.public-testpay203 header {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 16px;
  margin-bottom: 20px;
  flex-wrap: wrap;
}
.public-testpay203 h1 {
  font-size: 20px;
  margin: 0;
}
.public-testpay203 header a {
  font-size: 14px;
}
.testpay-card {
  padding: 28px;
}
.testpay-card h2 {
  font-size: 20px;
  margin: 0 0 24px;
}
.testpay-card label {
  display: grid;
  gap: 8px;
  margin: 20px 0;
  min-width: 0;
}
.testpay-card input {
  width: 100%;
  min-width: 0;
  box-sizing: border-box;
}
.testpay-card input[readonly] {
  background: var(--surface-alt, var(--bg-raised));
  color: var(--muted, var(--ink));
}
.testpay-money {
  display: flex;
  align-items: center;
  gap: 10px;
}
.testpay-methods {
  display: flex;
  gap: 12px;
  flex-wrap: wrap;
  margin: 24px 0;
}
.testpay-methods button {
  flex: 1;
  min-width: 100px;
}
.testpay-card p {
  line-height: 1.7;
}
.testpay-card footer {
  text-align: center;
  font-size: 12px;
  color: var(--muted, var(--ink));
  margin-top: 32px;
  overflow-wrap: anywhere;
}
.testpay-card input:focus-visible,
.testpay-card button:focus-visible,
.testpay-card [role="alert"]:focus {
  outline: 2px solid var(--primary, var(--focus));
  outline-offset: 3px;
}
@media (max-width: 600px) {
  .public-testpay203 {
    margin: 24px auto;
  }
  .testpay-card {
    padding: 20px;
  }
}
</style>
