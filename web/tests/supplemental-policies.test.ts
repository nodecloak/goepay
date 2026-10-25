import { expect, it } from "vitest";
import {
  flattenMenu,
  menus,
  navigationMenus,
  visibleMenu,
  resolvePage,
  viewKind,
} from "../src/menu";
import { resourceFor } from "../src/resources";
it("adds subject policies as a separate super-only resource without claiming original menu parity", () => {
  expect(flattenMenu(menus.admin)).toHaveLength(54);
  expect(flattenMenu(menus.merchant)).toHaveLength(28);
  expect(resolvePage("admin", "/system/order-subject-policies")?.roles).toEqual(
    ["R_SUPER"],
  );
  expect(resourceFor("admin", "/system/order-subject-policies")).toBe(
    "order-subject-policies",
  );
  expect(viewKind("admin", "/system/order-subject-policies")).toBe("resource");
  expect(
    flattenMenu(visibleMenu(navigationMenus("admin"), "R_SUPER")).some(
      (x) => x.path === "/system/order-subject-policies",
    ),
  ).toBe(true);
  expect(
    flattenMenu(visibleMenu(navigationMenus("admin"), "R_ADMIN")).some(
      (x) => x.path === "/system/order-subject-policies",
    ),
  ).toBe(false);
});
