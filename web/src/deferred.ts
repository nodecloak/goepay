import type { Area } from "./menu";
export const deferredPages: Record<Area, ReadonlySet<string>> = {
  admin: new Set(["/app/app-store"]),
  merchant: new Set(["/account/certificate"]),
};
export const unavailableReasons: Record<Area, Record<string, string>> = {
  admin: {
    "/app/app-store":
      "应用市场依赖外部市场服务及插件授权，本轮按约定暂缓接入。本地扩展资料与实际运行能力可在“本地插件”中查看。",
  },
  merchant: {
    "/account/certificate":
      "实名认证依赖身份核验服务插件，本轮按约定暂缓接入。",
  },
};
