import { afterEach, expect, it, vi } from "vitest";
import { mount, flushPromises, type VueWrapper } from "@vue/test-utils";
import SubchannelPage from "../src/components/SubchannelPage.vue";
import { ApiClient } from "../src/api";
import fixture from "../e2e/subchannel168b-fixture.json";

let wrapper: VueWrapper | undefined;
afterEach(() => {
  wrapper?.unmount();
  wrapper = undefined;
  document.body.innerHTML = "";
  vi.unstubAllGlobals();
});
async function click(label: string) {
  const button = [
    ...document.querySelectorAll<HTMLElement>(
      "button,[role=menuitem],[role=option]",
    ),
  ].find((node) => node.textContent?.trim() === label);
  expect(button, label).toBeTruthy();
  button!.click();
  await flushPromises();
}
async function fill(label: string, value: string) {
  const field = document.querySelector<HTMLTextAreaElement>(
    `textarea[aria-label="${label}"]`,
  )!;
  expect(field).toBeTruthy();
  field.value = value;
  field.dispatchEvent(new Event("input", { bubbles: true }));
  await flushPromises();
}
async function setup(missing = false, optional = false) {
  const row = structuredClone(fixture.admin.items[0]);
  if (missing) row.fields[1]!.present = false;
  const schema = structuredClone(fixture.schemas.items[0]);
  if (optional) schema.fields[1]!.required = false;
  const catalog = {
    items: [schema, { ...schema, parent_id: "8", fingerprint: "8".repeat(64) }],
    total: 2,
  };
  const writes: any[] = [];
  const client = new ApiClient("admin");
  client.csrfToken = "synthetic-editor244";
  vi.spyOn(client, "request").mockImplementation(async (path, options) => {
    if (options?.method === "PATCH") {
      writes.push(options.body);
      return {
        ...row,
        fingerprint: "b".repeat(64),
        revision: "2",
        account_version: "2",
      };
    }
    if (path === "/payment-subchannels/schemas") return catalog;
    if (path.startsWith("/payment-subchannels?"))
      return { ...fixture.admin, items: [row], total: 1 };
    if (path.startsWith("/payment-subchannels/policy")) return fixture.policy;
    return row;
  });
  vi.stubGlobal("crypto", {
    randomUUID: () => "12345678-1234-1234-1234-123456789012",
  });
  wrapper = mount(SubchannelPage, {
    props: { client, merchantId: "1", ownSession: true, adminClass: "super" },
    attachTo: document.body,
    global: {
      stubs: {
        RouterLink: { props: ["to"], template: '<a :href="to"><slot /></a>' },
      },
    },
  });
  await flushPromises();
  wrapper
    .get(`button[aria-label="子通道 ${row.id} 操作"]`)
    .element.dispatchEvent(new MouseEvent("click", { bubbles: true }));
  await flushPromises();
  await click("参数设置");
  return { writes, row };
}

it("edits write-only child parameters directly and preserves untouched exact-parent values", async () => {
  const { writes, row } = await setup();
  expect(document.querySelectorAll(".subchannel-param textarea")).toHaveLength(
    2,
  );
  expect(
    document.querySelector(".subchannel-param .pa-select-trigger"),
  ).toBeNull();
  expect(
    document.querySelector<HTMLTextAreaElement>(
      'textarea[aria-label="secret"]',
    )!.value,
  ).toBe("");
  expect(writes).toHaveLength(0);
  await fill("app", "replacement-app");
  await click("确认保存");
  expect(writes).toEqual([
    {
      expected_fingerprint: row.fingerprint,
      name: row.name,
      parent_id: row.parent_id,
      values: {
        app: { operation: "replace", value: "replacement-app" },
        secret: { operation: "keep" },
      },
    },
  ]);
});

it("requires a new value for a missing required child field", async () => {
  const { writes } = await setup(true);
  await click("确认保存");
  expect(document.querySelector('[role="dialog"]')!.textContent).toContain(
    "请填写新的secret",
  );
  expect(writes).toHaveLength(0);
  await fill("secret", "new-secret");
  await click("确认保存");
  expect(writes[0].values.secret).toEqual({
    operation: "replace",
    value: "new-secret",
  });
});

it("leaves an absent optional child field unset, including after canceling its new draft", async () => {
  const { writes } = await setup(true, true);
  expect(document.querySelector('button[aria-label="清除secret"]')).toBeNull();
  expect(document.querySelector('[role="dialog"]')!.textContent).not.toContain(
    "保存后将清除",
  );
  await fill("secret", "optional-draft");
  await fill("secret", "");
  await click("确认保存");
  expect(writes[0]?.values).toEqual({
    app: { operation: "keep" },
    secret: { operation: "clear" },
  });
});

it("saves untouched absent optional child fields without requiring a replacement", async () => {
  const { writes } = await setup(true, true);
  await click("确认保存");
  expect(writes[0]?.values).toEqual({
    app: { operation: "keep" },
    secret: { operation: "clear" },
  });
});

it("discards old-parent drafts and cannot silently preserve either required field after changing parent", async () => {
  const { writes } = await setup();
  await fill("secret", "old-parent-draft");
  document
    .querySelector<HTMLButtonElement>('button[aria-label="父通道"]')!
    .click();
  await flushPromises();
  await click("支付宝 #8");
  expect(
    document.querySelector<HTMLTextAreaElement>(
      'textarea[aria-label="secret"]',
    )!.value,
  ).toBe("");
  expect(document.querySelector('[role="dialog"]')!.innerHTML).not.toContain(
    "old-parent-draft",
  );
  await click("确认保存");
  expect(writes).toHaveLength(0);
  await fill("app", "new-parent-app");
  await click("确认保存");
  expect(writes).toHaveLength(0);
  // Validation deliberately clears private drafts, so refill both fields
  // after verifying that preserving the first parent's values is rejected.
  await fill("app", "new-parent-app");
  await fill("secret", "new-parent-secret");
  await click("确认保存");
  expect(writes[0]).toMatchObject({
    parent_id: "8",
    values: {
      app: { operation: "replace", value: "new-parent-app" },
      secret: { operation: "replace", value: "new-parent-secret" },
    },
  });
});
