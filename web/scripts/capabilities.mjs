import fs from "node:fs";
import { resourceFor } from "../src/resources.ts";
import { reportFor } from "../src/reports.ts";
import { workflowFor } from "../src/workflows.ts";
import { operationPages } from "../src/operations.ts";
import { commerceFor } from "../src/commerce.ts";
import { unavailableReasons, deferredPages } from "../src/deferred.ts";
const root = new URL("../", import.meta.url);
const known = {
  admin: new Set([
    "/dashboard",
    "/trade/order",
    "/trade/refund",
    "/transfer/list",
    "/merchant/merchant-list",
    "/merchant/record",
  ]),
  merchant: new Set([
    "/dashboard",
    "/trade/order",
    "/trade/refund",
    "/transfer",
    "/record",
  ]),
};
const all = [];
for (const area of ["admin", "merchant"]) {
  const source = JSON.parse(
    fs.readFileSync(new URL(`src/data/menu-${area}.json`, root), "utf8"),
  );
  function walk(nodes, base = "", titles = []) {
    for (const node of nodes) {
      const path = node.path.startsWith("/")
        ? node.path
        : `${base}/${node.path}`;
      const trail = [...titles, node.meta.title];
      if (node.children?.length) walk(node.children, path, trail);
      else {
        const resource = resourceFor(area, path);
        const report = reportFor(area, path),
          workflow = workflowFor(area, path),
          operation = area === "admin" ? operationPages[path] : undefined,
          account =
            (area === "merchant" &&
              ["/account/settings", "/account/api-info"].includes(path)) ||
            (area === "admin" && path === "/system/change-password"),
          commerce = commerceFor(area, path);
        const identityLookup=area === "admin" && path === "/other/get-token";
        const referral=area === "merchant" && path === "/invite";
 const subchannel=resource === "subchannels";
        const supported =
          resource ||
          report ||
          workflow ||
          operation ||
          account ||
          commerce ||
          identityLookup ||
          referral ||
          known[area].has(path);
        all.push({
          area,
          id: node.name,
          path,
          title: trail.join(" / "),
          hidden: !!node.meta.isHide,
          status: supported
            ? "partial"
            : deferredPages[area].has(path)
              ? "deferred"
              : "pending",
          resource: resource || undefined,
          capabilities: subchannel
 ? ["owned-runtime-payment-subchannel-list", "original-account-version-restore", "subchannel-status-limits-weight-cas", "merchant-global-subchannel-roll-policy", "exact-child-order-and-csv-filter", "confirmed-once-protected-payment-test", "owned-existing-collection-code-link", ...(area === "admin" ? ["super-only-encrypted-account-create-replace-archive"] : [])]
 : identityLookup
            ? ["read-session-bound-web-oauth-capabilities", "create-application-scoped-identity-authorization", "query-status-without-subject", "explicit-once-only-session-bound-reveal", "clear-sensitive-display-on-expiry-or-merchant-change", "explicit-independent-web-and-mini-application-profile", "super-own-once-only-alipay-application-authorization"]
            : referral
            ? ["owned-masked-promotion-token-and-expiry", "explicit-cas-rotate-and-revoke-once", "registration-bound-invitee-cursor-list", "original-fee-funded-reward-and-refund-derived-state", "issued-voided-refund-pending-statistics-not-balance", "promotion-prefill-stripped-before-router-explicit-otp-submit", "fee-capped-gross-basis-original-order-reward", "owned-proof-verified-daily-liability-cursor-queue"]
            : commerce
            ? commerce === "memberships"
              ? [
                  "list-membership-purchases",
                  "purchase-configured-group-with-balance",
                ]
              : commerce === "deposits"
                ? [
                    "read-deposit-balance",
                    "deposit-hold",
                    "request-reviewed-deposit-release",
                  ]
                : [
                    "list-recharge-orders",
                    "create-payment-order",
                    "open-validated-http-payment-url",
                  ]
            : report
              ? [
                  "read-report",
                  "filter-business-dates",
                  "export-current-page-csv",
                  "export-filtered-server-csv-with-bounded-download-and-abort",
                ]
              : workflow
                ? [
                    "list-ledger-workflows",
                    ...(workflow === "profitsharing"
                      ? ["server-rule-generated-workflows-only"]
                      : [
                          "read-authoritative-funds-availability",
                          "create-reservation",
                        ]),
                    ...(area === "admin" ? ["admin-review"] : []),
                    ...(area === "admin" && workflow === "settlements"
                      ? ["admin-selected-batch-review"]
                      : []),
                    ...(workflow === "settlements"
                      ? [
                          "read-original-settlement-payout-and-masked-recipient",
                          "query-original-claimed-settlement-payout",
                          ...(area === "admin"
                            ? [
                                "confirmed-wallet-channel-binding-without-second-reserve",
                                "funds-authorized-original-settlement-execute-or-proof-unsent-cancel",
                              ]
                            : []),
                        ]
                      : []),
                  ]
                : operation
                  ? [
                      "read-operation-information",
                      ...(operation === "cleanup"
                        ? [
                            "preview-expired-session-cleanup",
                            "confirmed-expired-session-cleanup",
                          ]
                        : []),
                      ...(operation === "jobs"
                        ? [
                            "retry-known-dead-notification-and-readonly-reconciliation",
                          ]
                        : []),
                    ]
                  : account
                    ? area === "admin"
                      ? [
                          "change-password-reauthenticate",
                          "totp-binding-and-disable",
                          "per-administrator-payment-password-settings",
                        ]
                      : path === "/account/settings"
                        ? [
                            "read-profile",
                            "update-profile",
                            "change-password-reauthenticate",
                            "totp-binding-and-disable",
                          ]
                        : ["read-masked-api-info", "confirmed-api-key-rotation"]
                    : resource
                      ? resource === "settlement-settings"
                        ? [
                            "read-masked-encrypted-recipient-version",
                            "confirmed-recipient-configuration-with-version-cas",
                            "clear-plaintext-draft-after-save-or-leave",
                          ]
                        : [
                            "resource-list",
                            "resource-detail",
                            "schema-permitted-create-update-delete",
                            ...(area === "admin" && resource === "merchants"
                              ? [
                                  "confirmed-superadmin-schema-permitted-merchant-delegation",
                                  "verify-merchant-session-after-unknown-delegation",
                                ]
                              : []),
                          ]
                      : known[area].has(path)
                        ? path === "/dashboard"
                          ? ["read-dashboard"]
                          : path === "/trade/order"
                            ? [
                                "read-list",
                                "export-filtered-server-csv-with-bounded-download-and-abort",
                                "read-order-provider-detail",
                                "read-preauthorization-capture-and-release-states",
                                "confirmed-server-permitted-full-authorization-release",
                                "query-original-authorization-release-without-new-intent",
                                "confirmed-server-permitted-authorization-capture-with-query-only-unknown-state",
                                "paid-order-notification-resend-with-stable-uuid-and-cooldown",
                                "verified-provider-query",
                                "confirmed-provider-close",
                                "submit-refund-with-unknown-result-handling",
                                "read-platform-income-hold-and-filter-status",
                                ...(area === "admin"
                                  ? [
                                      "confirmed-platform-income-freeze-with-stable-uuid",
                                      "confirmed-unfreeze-original-hold-cycle",
                                    ]
                                  : []),
                              ]
                            : ["/transfer", "/transfer/list"].includes(path)
                              ? [
                                  "read-list",
                                  "export-filtered-server-csv-with-bounded-download-and-abort",
                                  "read-owned-recipient-detail",
                                  "read-server-transfer-capabilities",
                                  "confirmed-capable-wallet-or-explicit-bank-transfer",
                                  "strict-alipay-private-bank-and-wechat-bank-product-capabilities",
                                  "separate-estimated-and-verified-bank-fees",
                                  "server-transfer-policy-display-and-exact-limit-validation",
                                  "separate-bank-return-lifecycle-and-immutable-ledger-history",
                                  "verified-transfer-query",
                                  "confirmed-alipay-proof-application-and-query",
                                ]
                              : [
                                  "read-list",
                                  ...([
                                    "/trade/refund",
                                    "/merchant/record",
                                    "/record",
                                  ].includes(path)
                                    ? [
                                        "export-filtered-server-csv-with-bounded-download-and-abort",
                                      ]
                                    : []),
                                ]
                        : [],
          limitations:
 subchannel
 ? "已接入微信V2/V3与支付宝受信模板的真实-2账户选择、原账户全生命周期恢复、专用字段及双角色权限。原商户不创建/删除或编辑密钥；管理员为保留原单证据采用逻辑归档。只按实际child产品能力开放，未适配服务商/进件和加密写控制器未知行为不推定支持。当前policy与历史快照分离，缺来源明确不可用。"
 : identityLookup
              ? "已接入直连微信/支付宝网页和小程序独立标识，以及super本人支付宝应用授权；均须显式应用档案与独立开关，本地simulation明确不可用。服务商/子商户、电商及原版加密行为仍未全量验收"
              : referral
              ? "支持手续费或毛额基数、原手续费封顶的即时奖励，以及原订单固定条款的次日入账队列；日结调度须独立显式启动，成功退款撤销整笔原奖励。利润基数、会员推广奖励与Pro按日重新汇总语义尚未恢复，不冒称原加密JSON完全兼容。"
              : resource === "settlement-settings"
              ? "使用专属加密收款配置接口；旧通用资料不自动接管；保存不等于上游认证或外部出款"
              : workflow === "settlements"
                ? "审批与出款结果分离；仅有费用、独立资金、收款快照且服务端明确许可的钱包或明确产品银行卡可准备；银行保留原结算费待确定退回，缺失快照历史申请不可出款；完整原版行为仍待验收"
                : resource
                  ? "已接入持久资源表单；原版的全部联动、动态按钮及业务语义仍需逐项验收"
                  : supported
                    ? "仅已列能力已接入；其余操作及原版行为对齐尚未验收"
                    : unavailableReasons[area][path] ||
                      "此功能尚未实现，仍待实施；保留原版菜单位置",
        });
      }
    }
  }
  walk(source);
}
fs.writeFileSync(
  new URL("capabilities.json", root),
  JSON.stringify(all, null, 2) + "\n",
);
console.log(
  `Recorded ${all.length} pages: ${all.filter((x) => x.status === "partial").length} partial, ${all.filter((x) => x.status === "pending").length} pending, ${all.filter((x) => x.status === "deferred").length} deferred`,
);
