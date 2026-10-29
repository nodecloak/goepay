import type { Area } from "./menu";
export const deferredPages: Record<Area, ReadonlySet<string>> = {
  admin: new Set<string>(),
  merchant: new Set(["/account/certificate"]),
};
export const unavailableReasons: Record<Area, Record<string, string>> = {
  admin: {},
  merchant: {
    "/account/certificate":
      "实名认证依赖身份核验服务插件，本轮按约定暂缓接入。",
  },
};
