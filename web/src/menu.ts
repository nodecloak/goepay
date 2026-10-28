import admin from "./data/menu-admin.json";
import merchant from "./data/menu-merchant.json";
import { resourceFor } from "./resources";
import { reportFor } from "./reports";
import { workflowFor } from "./workflows";
import { operationPages } from "./operations";
import { commerceFor } from "./commerce";
export type Area = "admin" | "merchant";
// Only an explicit super administrator class receives privileged navigation.
// Missing or unknown administrator classes fall back to the operator role.
export const menuRole = (role: string, adminClass?: string): string =>
  role === "admin" ? (adminClass === "super" ? "R_SUPER" : "R_ADMIN") : role;
interface RawNode {
  path: string;
  name: string;
  meta: {
    title: string;
    roles?: string[];
    isHide?: boolean;
    isHideTab?: boolean;
    hidden?: boolean;
    hideInMenu?: boolean;
  };
  children?: RawNode[];
}
export interface MenuNode {
  id: string;
  title: string;
  path: string;
  hidden: boolean;
  roles: string[];
  children: MenuNode[];
}
function normalize(nodes: RawNode[], base = ""): MenuNode[] {
  return nodes.map((n) => {
    const path = n.path.startsWith("/") ? n.path : `${base}/${n.path}`;
    return {
      id: n.name,
      title: n.meta.title,
      path,
      hidden: !!(n.meta.isHide || n.meta.hidden || n.meta.hideInMenu),
      roles: n.meta.roles || [],
      children: normalize(n.children || [], path),
    };
  });
}
export const menus: Record<Area, MenuNode[]> = {
  admin: normalize(admin as RawNode[]),
  merchant: normalize(merchant as RawNode[]),
};
export const supplementalMenus: Record<Area, MenuNode[]> = {
  admin: [
    {
      id: "MerchantNotice",
      title: "商户消息提醒",
      path: "/merchant/notice",
      hidden: false,
      roles: ["R_SUPER", "R_ADMIN"],
      children: [],
    },
    {
      id: "PlatformPrinting",
      title: "订单打印配置",
      path: "/system/printing",
      hidden: false,
      roles: ["R_SUPER"],
      children: [],
    },
    {
      id: "SettlementPlans",
      title: "结算计划",
      path: "/settle/plans",
      hidden: false,
      roles: ["R_SUPER"],
      children: [],
    },
    {
      id: "ReferralPolicies",
      title: "邀请返现政策",
      path: "/system/referral-policies",
      hidden: false,
      roles: ["R_SUPER"],
      children: [],
    },
    {
      id: "Redpacks",
      title: "创建红包",
      path: "/transfer/red",
      hidden: false,
      roles: ["R_SUPER", "R_ADMIN"],
      children: [],
    },
    {
      id: "TransferBatches",
      title: "批量付款",
      path: "/transfer/batches",
      hidden: false,
      roles: ["R_SUPER", "R_ADMIN"],
      children: [],
    },
    {
      id: "OrderSubjectPolicies",
      title: "订单名称限制策略",
      path: "/system/order-subject-policies",
      hidden: false,
      roles: ["R_SUPER"],
      children: [],
    },
    {
      id: "InternalProfitsharingOrders",
      title: "站内分账审批",
      path: "/profitsharing/internal-orders",
      hidden: false,
      roles: ["R_SUPER", "R_ADMIN"],
      children: [],
    },
    {
      id: "DepositReview",
      title: "保证金审批",
      path: "/merchant/deposit-review",
      hidden: false,
      roles: ["R_SUPER", "R_ADMIN"],
      children: [],
    },
  ],
  merchant: [
    {
      id: "MerchantNotice",
      title: "消息提醒设置",
      path: "/account/notice",
      hidden: false,
      roles: [],
      children: [],
    },
    {
      id: "MerchantPrinting",
      title: "订单打印设置",
      path: "/account/printing",
      hidden: false,
      roles: [],
      children: [],
    },
    {
      id: "MerchantSettlementSchedule",
      title: "自动结算参与设置",
      path: "/account/settlement-schedule",
      hidden: false,
      roles: [],
      children: [],
    },
    {
      id: "MerchantRedpacks",
      title: "创建红包",
      path: "/transfer/red",
      hidden: false,
      roles: [],
      children: [],
    },
    {
      id: "MerchantTransferBatches",
      title: "批量付款",
      path: "/transfer/batches",
      hidden: false,
      roles: [],
      children: [],
    },
  ],
};
export const navigationMenus = (area: Area) =>
  menus[area].map((node) =>
    supplementalMenus[area].some((extra) =>
      extra.path.startsWith(node.path + "/"),
    )
      ? {
          ...node,
          children: [
            ...(node.children.length
              ? []
              : [
                  {
                    ...node,
                    id: node.id + "Single",
                    title: "单笔付款",
                    children: [],
                  },
                ]),
            ...node.children,
            ...supplementalMenus[area].filter((extra) =>
              extra.path.startsWith(node.path + "/"),
            ),
          ],
        }
      : node,
  );
export const flattenMenu = (nodes: MenuNode[]): MenuNode[] =>
  nodes.flatMap((n) => (n.children.length ? flattenMenu(n.children) : [n]));
export const visibleMenu = (nodes: MenuNode[], role: string): MenuNode[] =>
  nodes
    .filter((n) => !n.hidden && (!n.roles.length || n.roles.includes(role)))
    .map((n) => ({ ...n, children: visibleMenu(n.children, role) }));
export const resolvePage = (area: Area, path: string) =>
  flattenMenu([...menus[area], ...supplementalMenus[area]]).find(
    (n) => n.path === path,
  );
export type ViewKind =
  | "dashboard"
  | "orders"
  | "refunds"
  | "transfers"
  | "transfer-batches"
  | "redpacks"
  | "merchants"
  | "ledger"
  | "resource"
  | "report"
  | "workflow"
  | "account"
  | "operation"
  | "commerce"
  | "group-sales"
  | "settings"
  | "invitations"
  | "collection-styles"
  | "collection-generator"
  | "identity-lookup"
  | "channel-sharing"
  | "referral"
  | "referral-policy"
  | "settlement-plans"
  | "settlement-batches"
  | "settlement-schedule"
  | "printing"
  | "merchant-notice"
  | "template"
  | "app-store"
  | "unavailable";
export function viewKind(area: Area, path: string): ViewKind {
  if (area === "merchant" && path === "/onecode") return "collection-generator";
  if (
    area === "admin" &&
    ["/other/onecode", "/other/onecode/edit"].includes(path)
  )
    return "collection-styles";
  if (area === "admin" && path === "/merchant/invite-code")
    return "invitations";
  if (area === "admin" && path === "/system/settings") return "settings";
  if (area === "admin" && path === "/merchant/group-buy") return "group-sales";
  if (area === "admin" && path === "/settle/batch") return "settlement-batches";
  if (
    (area === "merchant" && path === "/account/notice") ||
    (area === "admin" && path === "/merchant/notice")
  )
    return "merchant-notice";
  if (area === "admin" && path === "/app/template") return "template";
  if (area === "admin" && path === "/app/app-store") return "app-store";
  if (
    (area === "admin" && path === "/system/printing") ||
    (area === "merchant" && path === "/account/printing")
  )
    return "printing";
  if (area === "admin" && path === "/settle/plans") return "settlement-plans";
  if (area === "merchant" && path === "/account/settlement-schedule")
    return "settlement-schedule";
  if (area === "merchant" && path === "/invite") return "referral";
  if (area === "admin" && path === "/system/referral-policies")
    return "referral-policy";
  if (["/profitsharing/receiver", "/profitsharing/order"].includes(path))
    return "channel-sharing";
  if (area === "admin" && path === "/other/get-token") return "identity-lookup";
  if (path === "/transfer/batches") return "transfer-batches";
  if (path === "/transfer/red") return "redpacks";
  if (commerceFor(area, path)) return "commerce";
  if (area === "admin" && path === "/system/change-password") return "account";
  if (area === "admin" && operationPages[path]) return "operation";
  if (
    area === "merchant" &&
    ["/account/api-info", "/account/settings"].includes(path)
  )
    return "account";
  if (resourceFor(area, path)) return "resource";
  if (reportFor(area, path)) return "report";
  if (workflowFor(area, path)) return "workflow";
  if (path === "/dashboard") return "dashboard";
  if (path === "/trade/order") return "orders";
  if (path === "/trade/refund") return "refunds";
  if (
    path === "/transfer/list" ||
    (area === "merchant" && path === "/transfer")
  )
    return "transfers";
  if (area === "admin" && path === "/merchant/merchant-list")
    return "merchants";
  if (path === "/merchant/record" || path === "/record") return "ledger";
  return "unavailable";
}
