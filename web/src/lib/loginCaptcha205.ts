export interface LoginChallenge205 {
  version: 0 | 1;
  gt: string;
  challenge: string;
  new_captcha: boolean;
  success: 1;
  captcha_token: string;
}
interface Widget {
  onReady(callback: () => void): Widget;
  onSuccess(callback: () => void): Widget;
  onError(callback: () => void): Widget;
  onClose?(callback: () => void): Widget;
  getValidate(): unknown;
  showCaptcha?(): void;
  verify?(): void;
  destroy?(): void;
}
type SDK = Window & {
  initGeetest?: (
    options: Record<string, unknown>,
    callback: (widget: Widget) => void,
  ) => void;
  initGeetest4?: (
    options: Record<string, unknown>,
    callback: (widget: Widget) => void,
  ) => void;
};
function record(value: unknown, keys: string[]): Record<string, unknown> {
  if (!value || typeof value !== "object" || Array.isArray(value))
    throw Error("验证码数据格式无法核对");
  const v = value as Record<string, unknown>;
  if (
    Object.keys(v).length !== keys.length ||
    keys.some((key) => !Object.hasOwn(v, key))
  )
    throw Error("验证码数据格式无法核对");
  return v;
}
function string(value: unknown, max: number): value is string {
  return (
    typeof value === "string" &&
    value.length > 0 &&
    value.length <= max &&
    value.trim() === value &&
    !/[\u0000-\u001f\u007f-\u009f]/.test(value)
  );
}
export function parseLoginChallenge205(value: unknown): LoginChallenge205 {
  const v = record(value, [
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
    !/^[0-9a-f]{32}$/.test(v.gt) ||
    v.success !== 1 ||
    typeof v.captcha_token !== "string" ||
    !/^[A-Za-z0-9_-]{43}$/.test(v.captcha_token) ||
    (v.version === 0
      ? typeof v.challenge !== "string" ||
        !/^[0-9a-f]{32}$/.test(v.challenge) ||
        v.new_captcha !== true
      : v.challenge !== "" || v.new_captcha !== false)
  )
    throw Error("登录验证码上下文无法核对");
  return v as unknown as LoginChallenge205;
}
function parseProof(
  data: LoginChallenge205,
  value: unknown,
): Record<string, string> {
  const keys =
    data.version === 1
      ? ["captcha_id", "lot_number", "pass_token", "gen_time", "captcha_output"]
      : ["geetest_challenge", "geetest_validate", "geetest_seccode"];
  const raw = record(value, keys),
    result: Record<string, string> = { captcha_token: data.captcha_token };
  for (const key of keys) {
    const limit =
      key === "captcha_output" ? 4096 : key === "geetest_seccode" ? 1024 : 2048;
    if (!string(raw[key], limit)) throw Error("验证码结果格式无法核对");
    result[key] = raw[key] as string;
  }
  if (
    data.version === 1
      ? result.captcha_id !== data.gt ||
        !/^[0-9a-f]{32}$/.test(result.lot_number!) ||
        !/^[0-9]{1,12}$/.test(result.gen_time!)
      : result.geetest_challenge !== data.challenge ||
        !/^[0-9a-f]{32}$/.test(result.geetest_validate!)
  )
    throw Error("验证码结果与原上下文不一致");
  return result;
}

// Only fixed official scripts are loaded. CSP is controlled by the host; a
// blocked SDK is a closed login gate, never a local/offline verification path.
async function loadSDK(
  data: LoginChallenge205,
  signal: AbortSignal,
): Promise<void> {
  const sdk = window as SDK;
  if (data.version === 1 ? sdk.initGeetest4 : sdk.initGeetest) return;
  await new Promise<void>((resolve, reject) => {
    const script = document.createElement("script");
    script.src =
      data.version === 1
        ? "https://static.geetest.com/v4/gt4.js"
        : "https://static.geetest.com/static/tools/gt.js";
    script.async = true;
    script.referrerPolicy = "no-referrer";
    const finish = (error?: Error) => {
      clearTimeout(timer);
      signal.removeEventListener("abort", aborted);
      script.remove();
      script.onload = script.onerror = null;
      error ? reject(error) : resolve();
    };
    const aborted = () => finish(Error("登录验证码上下文已变化"));
    const timer = window.setTimeout(
      () => finish(Error("验证码加载超时")),
      8000,
    );
    signal.addEventListener("abort", aborted, { once: true });
    script.onload = () => finish();
    script.onerror = () => finish(Error("验证码加载失败，请稍后重试"));
    if (signal.aborted) aborted();
    else document.head.append(script);
  });
}
export async function verifyLoginChallenge205(
  data: LoginChallenge205,
  signal: AbortSignal,
): Promise<Record<string, string>> {
  await loadSDK(data, signal);
  if (signal.aborted) throw Error("登录验证码上下文已变化");
  return new Promise((resolve, reject) => {
    let instance: Widget | undefined,
      finished = false,
      ready = false;
    const finish = (error?: Error, proof?: Record<string, string>) => {
      if (finished) return;
      finished = true;
      clearTimeout(timer);
      signal.removeEventListener("abort", aborted);
      try {
        instance?.destroy?.();
      } catch {
        /* SDK cleanup cannot authorize a login. */
      }
      if (error || !proof) reject(error ?? Error("验证码已取消"));
      else resolve(proof);
    };
    const aborted = () => finish(Error("登录验证码上下文已变化"));
    let timer = window.setTimeout(
      () => finish(Error("验证码组件加载超时")),
      8000,
    );
    signal.addEventListener("abort", aborted, { once: true });
    const sdk = window as SDK,
      initialize = data.version === 1 ? sdk.initGeetest4 : sdk.initGeetest;
    if (!initialize) {
      finish(Error("验证码组件不可用"));
      return;
    }
    try {
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
              new_captcha: true,
              product: "bind",
              offline: false,
            },
        (widget) => {
          if (finished || signal.aborted) {
            widget?.destroy?.();
            return;
          }
          if (
            !widget ||
            typeof widget.onReady !== "function" ||
            typeof widget.onSuccess !== "function" ||
            typeof widget.onError !== "function" ||
            typeof widget.getValidate !== "function"
          ) {
            finish(Error("验证码组件格式无效"));
            return;
          }
          instance = widget;
          widget.onSuccess(() => {
            if (signal.aborted) {
              aborted();
              return;
            }
            try {
              finish(undefined, parseProof(data, widget.getValidate()));
            } catch {
              finish(Error("验证码结果无法核对，请重新验证"));
            }
          });
          widget.onError(() => finish(Error("验证码服务不可用，请稍后重试")));
          widget.onClose?.(() => finish(Error("验证码已取消")));
          widget.onReady(() => {
            if (finished || signal.aborted) {
              aborted();
              return;
            }
            if (ready) return;
            ready = true;
            clearTimeout(timer);
            timer = window.setTimeout(
              () => finish(Error("验证码验证超时，请重新验证")),
              120000,
            );
            try {
              if (data.version === 1) {
                if (!widget.showCaptcha) throw Error();
                widget.showCaptcha();
              } else {
                if (!widget.verify) throw Error();
                widget.verify();
              }
            } catch {
              finish(Error("验证码组件不可用"));
            }
          });
        },
      );
    } catch {
      finish(Error("验证码组件不可用"));
    }
  });
}
