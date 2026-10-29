import { vi } from "vitest";
import type { ApiClient } from "../src/api";

// The projection fields that put the merchant list into its "original" Pro layout.
export const originalMerchantFields = [
  { name: "name", label: "名称", type: "string" },
  { name: "effective_group_id", label: "组", type: "string", read_only: true },
  {
    name: "effective_group_name",
    label: "组名",
    type: "string",
    read_only: true,
  },
  {
    name: "available_minor",
    label: "账户余额（分）",
    type: "string",
    read_only: true,
  },
  {
    name: "frozen_minor",
    label: "冻结预留（分）",
    type: "string",
    read_only: true,
  },
  {
    name: "income_held_minor",
    label: "待解冻收入（分）",
    type: "string",
    read_only: true,
  },
  {
    name: "settlement_recipient",
    label: "结算账号与姓名",
    type: "json",
    read_only: true,
  },
  { name: "phone", label: "手机", type: "string" },
  { name: "email", label: "邮箱", type: "email" },
  { name: "created_at", label: "注册时间", type: "string", read_only: true },
  { name: "url", label: "网站", type: "string" },
  { name: "pay", label: "支付", type: "string" },
  { name: "enabled", label: "状态", type: "boolean" },
  { name: "settle", label: "结算", type: "string", read_only: true },
  {
    name: "approved_domains",
    label: "已审核域名",
    type: "json",
    read_only: true,
  },
];

export function stubMerchantList(client: ApiClient, fields: unknown[]) {
  vi.spyOn(client, "request").mockImplementation(async (path: string) => {
    if (path.endsWith("/schema"))
      return {
        title: "商户管理",
        fields,
        permissions: { create: true, update: true, delete: false },
        actions: ["sso"],
      } as any;
    return {
      items: [
        {
          id: "1000",
          name: "示例商户",
          available_minor: "1230",
          frozen_minor: "0",
          income_held_minor: "0",
          phone: "13000000000",
          email: "m@example.invalid",
          created_at: "2026-10-01T00:00:00Z",
          pay: "1",
          enabled: true,
          settle: "1",
        },
      ],
      total: 1,
    } as any;
  });
}
