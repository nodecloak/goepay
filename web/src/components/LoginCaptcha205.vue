<script setup lang="ts">
import { nextTick, onBeforeUnmount, ref, watch } from "vue";
import {
  parseLoginChallenge205,
  verifyLoginChallenge205,
} from "../lib/loginCaptcha205";

const props = withDefaults(
  defineProps<{
    active?: boolean;
    username?: string;
    contextKey?: unknown;
    disabled?: boolean;
  }>(),
  { active: false, username: "", disabled: false },
);
const enabled = ref<boolean>(),
  loading = ref(false),
  verifying = ref(false),
  error = ref("");
const alert = ref<HTMLElement>();
let alive = true,
  epoch = 0,
  metadataEpoch = 0,
  operation: AbortController | undefined,
  metadata: AbortController | undefined;
function current(run: number, controller: AbortController) {
  return alive && props.active && run === epoch && !controller.signal.aborted;
}
function invalidate() {
  epoch++;
  operation?.abort();
  operation = undefined;
  verifying.value = false;
}
async function request(
  path: string,
  controller: AbortController,
  body?: Record<string, string>,
): Promise<unknown> {
  const transport = new AbortController(),
    cancel = () => transport.abort();
  controller.signal.addEventListener("abort", cancel, { once: true });
  const timeout = window.setTimeout(cancel, 10000);
  try {
    const response = await fetch(path, {
      method: body ? "POST" : "GET",
      credentials: "same-origin",
      cache: "no-store",
      redirect: "error",
      referrerPolicy: "no-referrer",
      signal: transport.signal,
      ...(body
        ? {
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify(body),
          }
        : {}),
    });
    let envelope: unknown;
    try {
      envelope = await response.json();
    } catch {
      throw Error("验证码响应无法核对，请重新读取");
    }
    if (!envelope || typeof envelope !== "object" || Array.isArray(envelope))
      throw Error("验证码响应无法核对，请重新读取");
    const value = envelope as Record<string, unknown>;
    if (!response.ok || value.code !== 0)
      throw Error(
        response.status === 409
          ? "验证码配置已变化，请重新核对"
          : "验证码服务不可用，请稍后重试",
      );
    return value.data;
  } finally {
    clearTimeout(timeout);
    controller.signal.removeEventListener("abort", cancel);
  }
}
async function reload() {
  invalidate();
  metadata?.abort();
  const run = ++metadataEpoch;
  enabled.value = undefined;
  error.value = "";
  loading.value = false;
  if (!alive || !props.active) return;
  const controller = new AbortController();
  metadata = controller;
  loading.value = true;
  try {
    const data = await request(
      "/console/v1/public/login-captcha?role=merchant&mode=password",
      controller,
    );
    if (
      !alive ||
      !props.active ||
      run !== metadataEpoch ||
      controller.signal.aborted
    )
      return;
    if (
      !data ||
      typeof data !== "object" ||
      Array.isArray(data) ||
      Object.keys(data).length !== 1 ||
      typeof (data as Record<string, unknown>).enabled !== "boolean"
    )
      throw Error("登录验证码设置格式无法核对");
    enabled.value = (data as { enabled: boolean }).enabled;
  } catch {
    if (
      alive &&
      props.active &&
      run === metadataEpoch &&
      !controller.signal.aborted
    )
      error.value = "登录验证码设置无法读取，请重新核对后登录";
  } finally {
    if (alive && run === metadataEpoch && !controller.signal.aborted)
      loading.value = false;
  }
}
async function verify(): Promise<Record<string, string>> {
  if (
    !alive ||
    !props.active ||
    loading.value ||
    verifying.value ||
    enabled.value === undefined
  )
    throw Error("请先核对登录验证码设置");
  if (enabled.value === false) return {};
  const username = props.username;
  if (
    !username ||
    username.trim() !== username ||
    username.length > 128 ||
    /[\u0000-\u001f\u007f-\u009f]/.test(username)
  )
    throw Error("请先填写登录账号");
  invalidate();
  const run = epoch,
    controller = new AbortController();
  operation = controller;
  verifying.value = true;
  error.value = "";
  try {
    const data = parseLoginChallenge205(
      await request("/console/v1/public/login-captcha/init", controller, {
        username,
      }),
    );
    if (!current(run, controller) || username !== props.username)
      throw Error("登录验证码上下文已变化");
    const proof = await verifyLoginChallenge205(data, controller.signal);
    if (!current(run, controller) || username !== props.username)
      throw Error("登录验证码上下文已变化");
    return proof;
  } catch (reason) {
    if (current(run, controller)) {
      error.value =
        reason instanceof Error
          ? reason.message
          : "验证码服务不可用，请重新验证";
      await nextTick();
      if (current(run, controller)) alert.value?.focus();
    }
    throw reason;
  } finally {
    if (current(run, controller)) {
      verifying.value = false;
      operation = undefined;
    }
  }
}
watch(
  () => [props.active, props.contextKey],
  () => {
    void reload();
  },
  { immediate: true },
);
watch(
  () => props.username,
  () => {
    invalidate();
    error.value = "";
  },
);
onBeforeUnmount(() => {
  alive = false;
  invalidate();
  metadataEpoch++;
  metadata?.abort();
});
defineExpose({ verify, invalidate, reload });
</script>

<template>
  <div
    v-if="active"
    class="login-captcha205"
    role="group"
    aria-label="登录验证码"
    :aria-busy="loading || verifying"
  >
    <p aria-live="polite">
      {{
        loading
          ? "正在核对登录验证码设置…"
          : verifying
            ? "请完成人机验证…"
            : enabled === true
              ? "登录时需要完成人机验证"
              : enabled === false
                ? "当前登录无需人机验证"
                : "请核对登录验证码设置"
      }}
    </p>
    <p
      v-if="error"
      ref="alert"
      class="login-captcha205-error"
      role="alert"
      tabindex="-1"
    >
      {{ error }}
    </p>
    <button
      type="button"
      :disabled="disabled || loading || verifying"
      @click="reload"
    >
      重新核对验证码
    </button>
  </div>
</template>

<style scoped>
.login-captcha205 {
  display: grid;
  gap: 0.5rem;
  min-width: 0;
  margin-block: 0.75rem;
  font-size: 0.875rem;
}
.login-captcha205 p {
  margin: 0;
  overflow-wrap: anywhere;
}
.login-captcha205 button {
  justify-self: start;
  max-width: 100%;
  min-height: 2.75rem;
  padding: 0.5rem 0.75rem;
  border: 1px solid currentColor;
  border-radius: 0.375rem;
  background: transparent;
  color: inherit;
  cursor: pointer;
}
.login-captcha205 button:focus-visible {
  outline: 2px solid var(--color-primary, #2563eb);
  outline-offset: 3px;
}
.login-captcha205 button:disabled {
  cursor: default;
  opacity: 0.55;
}
.login-captcha205-error {
  color: var(--color-danger, #b91c1c);
}
</style>
