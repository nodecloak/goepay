import type { Area } from "./menu";
export const deferredPages: Record<Area, ReadonlySet<string>> = {
  admin: new Set<string>(),
  merchant: new Set<string>(),
};
export const unavailableReasons: Record<Area, Record<string, string>> = {
  admin: {},
  merchant: {},
};
