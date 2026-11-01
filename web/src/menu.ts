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

// Per-menu administrator permissions (migration 000132). A restricted operator
// carries a grant set of menu node ids; it sees only leaves whose id is granted
// (group nodes survive when any descendant does). A super administrator and an
// unrestricted operator (permissions undefined) are not narrowed — the server
// remains the enforcement authority; this only drives navigation visibility.
export const permissionRestricted = (
  role: string,
  adminClass: string | undefined,
  permissions: string[] | undefined,
): boolean =>
  role === "admin" && adminClass !== "super" && Array.isArray(permissions);

const granted = (node: MenuNode, keys: Set<string>): boolean =>
  node.children.length
    ? node.children.some((c) => granted(c, keys))
    : keys.has(node.id);

const permittedMenuSet = (nodes: MenuNode[], keys: Set<string>): MenuNode[] =>
  nodes
    .filter((n) => granted(n, keys))
    .map((n) => ({
      ...n,
      children: n.children.length ? permittedMenuSet(n.children, keys) : [],
    }));

export const permittedMenu = (
  nodes: MenuNode[],
  permissions: string[],
): MenuNode[] => permittedMenuSet(nodes, new Set(permissions));

// navigationFor applies both the role filter and, for a restricted operator, the
// per-menu grant set.
export const navigationFor = (
  area: Area,
  role: string,
  adminClass: string | undefined,
  permissions: string[] | undefined,
  features?: Record<string, boolean>,
): MenuNode[] => {
  const byRole = featureFilteredMenu(
    area,
    visibleMenu(navigationMenus(area), menuRole(role, adminClass)),
    features,
  );
  if (!permissionRestricted(role, adminClass, permissions)) return byRole;
  return permittedMenu(byRole, permissions as string[]);
};

// merchantFeatureMenu maps a merchant feature toggle key to the merchant menu
// node id whose entry it governs. A feature that is off (explicit false) hides
// that node; a missing/true feature leaves it shown. internal_transfer /
// recharge / onecode default ON; complain_open / applyments_open / mchrisk_open
// default OFF — both are decided server-side and delivered as merchant_features.
const merchantFeatureMenu: Record<string, string> = {
  internal_transfer: "InternalTransfer",
  recharge: "Recharge",
  onecode: "Onecode",
  complain_open: "Complain",
  applyments_open: "ApplymentsManage",
  mchrisk_open: "MchRisk",
  user_transfer_batch: "MerchantTransferBatches",
  user_deposit: "Deposit",
  subchannel_open: "SubChannel",
};

// featureHiddenNodeIds collects the menu node ids to hide for the merchant area
// given the session feature flags. Only an explicit false hides a node.
const featureHiddenNodeIds = (
  area: Area,
  features: Record<string, boolean> | undefined,
): Set<string> => {
  const hidden = new Set<string>();
  if (area !== "merchant" || !features) return hidden;
  for (const [key, nodeId] of Object.entries(merchantFeatureMenu))
    if (features[key] === false) hidden.add(nodeId);
  return hidden;
};

// featureFilteredMenu drops any node whose id is a disabled merchant feature,
// then prunes a group left with no children.
const featureFilteredMenu = (
  area: Area,
  nodes: MenuNode[],
  features: Record<string, boolean> | undefined,
): MenuNode[] => {
  const hidden = featureHiddenNodeIds(area, features);
  if (!hidden.size) return nodes;
  const prune = (list: MenuNode[]): MenuNode[] =>
    list
      .filter((n) => !hidden.has(n.id))
      .map((n) => ({ ...n, children: prune(n.children) }))
      .filter((n) => !(n.children.length === 0 && wasGroup(n)));
  const wasGroup = (n: MenuNode): boolean =>
    menus[area]
      .concat(supplementalMenus[area])
      .some((top) => top.id === n.id && top.children.length > 0);
  return prune(nodes);
};

// pageAuthorizedFor decides whether a page's route may be opened: the role must
// match and, for a restricted operator, the page's menu id must be granted. A
// page with no menu id (hidden/utility pages like change-password) is allowed so
// self-service remains reachable. A merchant page whose feature is off is refused.
export const pageAuthorizedFor = (
  area: Area,
  path: string,
  role: string,
  adminClass: string | undefined,
  permissions: string[] | undefined,
  features?: Record<string, boolean>,
): boolean => {
  const page = resolvePage(area, path);
  if (!page) return false;
  const roleKey = menuRole(role, adminClass);
  if (page.roles.length && !page.roles.includes(roleKey)) return false;
  // A closed batch feature hides its create entry, while the original route
  // still provides owner-bound history, query and proven-unsent cancellation.
  if (
    featureHiddenNodeIds(area, features).has(page.id) &&
    !(
      area === "merchant" &&
      role === "merchant" &&
      ["/transfer/batches", "/account/deposit", "/account/recharge"].includes(
        path,
      )
    )
  )
    return false;
  if (!permissionRestricted(role, adminClass, permissions)) return true;
  const keys = permissions as string[];
  // Always-allowed self-service pages carry no grantable menu key.
  if (alwaysAllowedPage(path)) return true;
  return keys.includes(page.id);
};

// alwaysAllowedPage lists utility pages a restricted operator may always open.
const alwaysAllowedPage = (path: string): boolean =>
  path === "/system/change-password" || path === "system/change-password";
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
  | "complain"
  | "unavailable";
export function viewKind(area: Area, path: string): ViewKind {
  if (
    (area === "admin" &&
      ["/complain/list", "/complain/charts", "/complain/task"].includes(
        path,
      )) ||
    (area === "merchant" && path === "/complain")
  )
    return "complain";
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
