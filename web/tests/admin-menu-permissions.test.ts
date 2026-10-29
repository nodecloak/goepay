import { describe, it, expect } from "vitest";
import {
  navigationFor,
  pageAuthorizedFor,
  permittedMenu,
  permissionRestricted,
  flattenMenu,
  menus,
} from "../src/menu";
import {
  buildPayload,
  validateFields,
  formValues,
  type ResourceSchema,
} from "../src/resources";

describe("admin per-menu permissions (000132)", () => {
  it("super and unrestricted operator are not narrowed", () => {
    expect(permissionRestricted("admin", "super", ["OrderList"])).toBe(false);
    expect(permissionRestricted("admin", "operator", undefined)).toBe(false);
    // A restricted operator carries an explicit (possibly empty) array.
    expect(permissionRestricted("admin", "operator", [])).toBe(true);
    expect(permissionRestricted("admin", "operator", ["OrderList"])).toBe(true);
  });

  it("navigation shows only granted menu leaves for a restricted operator", () => {
    const nav = navigationFor("admin", "admin", "operator", [
      "OrderList",
      "SystemLog",
    ]);
    const leaves = flattenMenu(nav).map((n) => n.id);
    expect(leaves).toContain("OrderList");
    expect(leaves).toContain("SystemLog");
    expect(leaves).not.toContain("MerchantList");
    expect(leaves).not.toContain("AdminUser");
    // Parent groups with no granted child are dropped entirely.
    expect(nav.some((n) => n.id === "AppManage")).toBe(false);
  });

  it("navigation is unchanged for an unrestricted operator", () => {
    const restricted = navigationFor("admin", "admin", "operator", [
      "OrderList",
    ]);
    const unrestricted = navigationFor("admin", "admin", "operator", undefined);
    expect(flattenMenu(unrestricted).length).toBeGreaterThan(
      flattenMenu(restricted).length,
    );
  });

  it("permittedMenu keeps a group when any descendant is granted", () => {
    const trade = menus.admin.filter((n) => n.id === "TradeManage");
    const kept = permittedMenu(trade, ["DailyStat"]);
    expect(kept).toHaveLength(1);
    expect(flattenMenu(kept).map((n) => n.id)).toEqual(["DailyStat"]);
  });

  it("route authorization enforces the grant set and allows self-service", () => {
    const perms = ["OrderList"];
    expect(
      pageAuthorizedFor("admin", "/trade/order", "admin", "operator", perms),
    ).toBe(true);
    expect(
      pageAuthorizedFor(
        "admin",
        "/merchant/group-list",
        "admin",
        "operator",
        perms,
      ),
    ).toBe(false);
    // Self-service change-password is always reachable.
    expect(
      pageAuthorizedFor(
        "admin",
        "/system/change-password",
        "admin",
        "operator",
        [],
      ),
    ).toBe(true);
    // Super reaches a super-only page an operator never could.
    expect(
      pageAuthorizedFor(
        "admin",
        "/system/admin-user",
        "admin",
        "super",
        undefined,
      ),
    ).toBe(true);
    expect(
      pageAuthorizedFor(
        "admin",
        "/system/admin-user",
        "admin",
        "operator",
        undefined,
      ),
    ).toBe(false);
  });
});

describe("permission_tree field round-trips as a string array", () => {
  const schema: ResourceSchema = {
    name: "admin-users",
    title: "管理员列表",
    permissions: { create: true, update: true, delete: false },
    fields: [
      { name: "username", label: "账号", type: "string", required: true },
      { name: "admin_class", label: "权限等级", type: "select" },
      { name: "permissions", label: "菜单权限", type: "permission_tree" },
    ],
  };
  it("builds an array payload and never a string", () => {
    const payload = buildPayload(
      schema,
      {
        username: "ops",
        admin_class: "operator",
        permissions: ["OrderList", "DailyStat"],
      },
      false,
    );
    expect(payload.permissions).toEqual(["OrderList", "DailyStat"]);
  });
  it("does not error-validate the tree field", () => {
    const errors = validateFields(
      schema,
      { username: "ops", admin_class: "operator", permissions: [] },
      false,
    );
    expect(errors.permissions).toBeUndefined();
  });
  it("defaults the tree to an empty array in formValues", () => {
    const values = formValues(schema, {});
    expect(values.permissions).toEqual([]);
  });
});
