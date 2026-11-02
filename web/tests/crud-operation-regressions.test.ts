import { expect, it } from "vitest";
import { ApiError } from "../src/api";
import { buildPayload, type ResourceSchema } from "../src/resources";
import { listPageWithinTotal } from "../src/list-page";
import { uncertainCreationResult } from "../src/mutation-result";

it("submitting super omits the old restricted grant set without losing the operator draft", () => {
  const schema: ResourceSchema = {
    title: "管理员",
    permissions: { create: true, update: true, delete: true },
    fields: [
      { name: "admin_class", label: "权限等级", type: "select" },
      { name: "permissions", label: "菜单权限", type: "permission_tree" },
    ],
  };
  const draft = Object.freeze({
    admin_class: "super",
    permissions: Object.freeze(["OrderList"]),
  });
  expect(buildPayload(schema, draft, true)).toEqual({
    admin_class: "super",
    permissions: [],
  });
  expect(draft.permissions).toEqual(["OrderList"]);
  expect(
    buildPayload(schema, { ...draft, admin_class: "operator" }, true),
  ).toEqual({ admin_class: "operator", permissions: ["OrderList"] });
});

it.each([
  [2, 20, 20, 1],
  [3, 21, 20, 2],
  [2, 0, 20, 1],
  [1, 40, 20, 1],
  [2, 12, 12, 1],
  [2, 13, 12, 2],
])(
  "page %i with total %i and size %i stays on valid page %i",
  (page, total, size, expected) => {
    expect(listPageWithinTotal(page, total, size)).toBe(expected);
  },
);

it.each([400, 401, 403, 409, 422])(
  "a decoded rejection %i permits input correction",
  (status) => {
    expect(uncertainCreationResult(new ApiError("rejected", status, -1))).toBe(
      false,
    );
  },
);
it.each([408, 429, 500, 502, 503])(
  "reply %i cannot prove a dispatched creation was not committed",
  (status) => {
    expect(uncertainCreationResult(new ApiError("unknown", status, -1))).toBe(
      true,
    );
  },
);
it("network and malformed replies cannot justify a second creation", () => {
  expect(uncertainCreationResult(new TypeError("lost reply"))).toBe(true);
  expect(
    uncertainCreationResult(
      new ApiError("unparsed", 400, -1, undefined, false),
    ),
  ).toBe(true);
});
