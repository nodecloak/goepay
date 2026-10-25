import type { Area } from "./menu";
export const workflowPages: Record<
  Area,
  Record<string, "settlements" | "internal-transfers" | "profitsharing">
> = {
  admin: {
    "/settle/list": "settlements",
    "/merchant/internal-transfer": "internal-transfers",
    "/profitsharing/internal-orders": "profitsharing",
  },
  merchant: {
    "/settle/list": "settlements",
    "/settle/apply": "settlements",
    "/account/internal-transfer": "internal-transfers",
  },
};
export const workflowFor = (area: Area, path: string) =>
  workflowPages[area][path];
