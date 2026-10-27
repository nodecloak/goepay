import { ApiClient } from "./api";

type NativeClient202 = "wechat" | "alipay" | "browser";
interface WechatConfig202 {
  debug: boolean;
  appId: string;
  timestamp: number;
  nonceStr: string;
  signature: string;
  jsApiList: string[];
}
interface WechatSDK202 {
  config(value: WechatConfig202): void;
  ready(callback: () => void): void;
  error(callback: () => void): void;
  scanQRCode(options: {
    needResult: number;
    scanType: string[];
    success(value: { resultStr?: unknown }): void;
    fail(): void;
    cancel(): void;
  }): void;
}
interface AlipaySDK202 {
  scan(options: {
    type: string;
    success(value: Record<string, unknown>): void;
    fail(value?: { error?: unknown; errorMessage?: unknown }): void;
  }): void;
}
type SDKWindow202 = Window & {
  wx?: WechatSDK202;
  ap?: AlipaySDK202;
};

export function nativeClient202(
  userAgent = navigator.userAgent,
): NativeClient202 {
  if (/MicroMessenger/i.test(userAgent)) return "wechat";
  if (/AlipayClient/i.test(userAgent)) return "alipay";
  return "browser";
}

function aborted202() {
  return new DOMException("Native scan cancelled", "AbortError");
}
function active202(signal: AbortSignal) {
  if (signal.aborted) throw aborted202();
}

const loading202 = new Map<NativeClient202, Promise<void>>();
async function load202(kind: "wechat" | "alipay", signal: AbortSignal) {
  active202(signal);
  const sdk = window as SDKWindow202;
  if (kind === "wechat" ? sdk.wx : sdk.ap) return;
  let load = loading202.get(kind);
  if (!load) {
    load = new Promise<void>((resolve, reject) => {
      const script = document.createElement("script");
      script.src =
        kind === "wechat"
          ? "https://res.wx.qq.com/open/js/jweixin-1.6.3.js"
          : "https://gw.alipayobjects.com/as/g/h5-lib/alipayjsapi/3.1.1/alipayjsapi.min.js";
      script.async = true;
      const finish = (error?: Error) => {
        clearTimeout(timer);
        script.onload = null;
        script.onerror = null;
        if (error) {
          script.remove();
          loading202.delete(kind);
          reject(error);
        } else resolve();
      };
      const timer = setTimeout(
        () => finish(new Error("原生扫码组件加载超时，可手动输入付款码")),
        15000,
      );
      script.onload = () =>
        finish(
          (kind === "wechat" ? sdk.wx : sdk.ap)
            ? undefined
            : new Error("原生扫码组件不可用，可手动输入付款码"),
        );
      script.onerror = () =>
        finish(new Error("原生扫码组件加载失败，可手动输入付款码"));
      document.head.appendChild(script);
    });
    loading202.set(kind, load);
  }
  await load;
  active202(signal);
}

function scanResult202(
  signal: AbortSignal,
  start: (
    done: (value: string | null, error?: Error) => void,
    pending: () => boolean,
  ) => void,
): Promise<string | null> {
  return new Promise((resolve, reject) => {
    let settled = false;
    const done = (value: string | null, error?: Error) => {
      if (settled) return;
      settled = true;
      clearTimeout(timer);
      signal.removeEventListener("abort", abort);
      if (signal.aborted) reject(aborted202());
      else if (error) reject(error);
      else resolve(value);
    };
    const abort = () => done(null, aborted202());
    const timer = setTimeout(
      () => done(null, new Error("原生扫码未完成，请重试或输入付款码")),
      90000,
    );
    signal.addEventListener("abort", abort, { once: true });
    if (signal.aborted) return abort();
    try {
      start(done, () => !settled && !signal.aborted);
    } catch {
      done(null, new Error("无法启动原生扫码，可手动输入付款码"));
    }
  });
}

// The SDK only returns a bearer code to the existing prepare/confirm workflow.
// It never submits a charge and never persists the returned code.
export async function nativeScan202(
  client: ApiClient,
  signal: AbortSignal,
): Promise<string | null> {
  const kind = nativeClient202();
  if (kind === "browser") throw new Error("当前浏览器请使用摄像头或输入付款码");
  await load202(kind, signal);
  const sdk = window as SDKWindow202;
  if (kind === "alipay") {
    return scanResult202(signal, (done) =>
      sdk.ap!.scan({
        type: "bar",
        success: (value) => {
          const raw = [
            value.code,
            value.barCode,
            value.qrCode,
            value.result,
          ].find((v) => typeof v === "string" && v.trim() !== "");
          if (typeof raw !== "string")
            done(null, new Error("未识别到付款码，请重试"));
          else done(raw.trim());
        },
        fail: () =>
          done(null, new Error("支付宝扫码未完成，可重试或输入付款码")),
      }),
    );
  }
  const config = await client.request<WechatConfig202>("/scanpay/jsconfig", {
    method: "POST",
    body: { url: location.href.split("#")[0] },
    signal,
  });
  active202(signal);
  if (
    !config ||
    typeof config.debug !== "boolean" ||
    typeof config.appId !== "string" ||
    !config.appId ||
    !Number.isSafeInteger(config.timestamp) ||
    config.timestamp <= 0 ||
    typeof config.nonceStr !== "string" ||
    !config.nonceStr ||
    typeof config.signature !== "string" ||
    !/^[a-f0-9]{40}$/.test(config.signature) ||
    !Array.isArray(config.jsApiList) ||
    config.jsApiList.length !== 1 ||
    config.jsApiList[0] !== "scanQRCode"
  ) {
    throw new Error("微信扫码配置无法确认，请重新进入收款页");
  }
  return scanResult202(signal, (done, pending) => {
    let started = false;
    sdk.wx!.error(() =>
      done(null, new Error("微信扫码配置未通过，请重新进入收款页")),
    );
    sdk.wx!.config(config);
    sdk.wx!.ready(() => {
      if (!pending() || started) return;
      started = true;
      sdk.wx!.scanQRCode({
        needResult: 1,
        scanType: ["qrCode", "barCode"],
        success: (value) =>
          typeof value.resultStr === "string"
            ? done(value.resultStr.trim())
            : done(null, new Error("未识别到付款码，请重试")),
        fail: () => done(null, new Error("微信扫码未完成，可重试或输入付款码")),
        cancel: () => done(null),
      });
    });
  });
}
