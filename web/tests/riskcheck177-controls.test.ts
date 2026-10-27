import { it, expect, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import { ApiClient } from "../src/api";
import CoreRiskGroupPanel from "../src/components/CoreRiskGroupPanel.vue";
import CoreRiskActions from "../src/components/CoreRiskActions.vue";
it("the real ApiClient serializes the group mutation once as a JSON object", async () => {
  const wire: unknown[] = [];
  const fetch = vi
    .spyOn(globalThis, "fetch")
    .mockImplementation(async (_input, init) => {
      if (init?.method === "PATCH") {
        const body = JSON.parse(String(init.body));
        wire.push(body);
        if (typeof body !== "object" || body === null)
          return new Response(
            JSON.stringify({
              code: -1,
              message: "object required",
              data: null,
            }),
            { status: 400, headers: { "Content-Type": "application/json" } },
          );
        return new Response(
          JSON.stringify({
            code: 0,
            message: "",
            data: {
              ...group("1"),
              config: { pay_block_oversea: "", check_pay_regoin: "2" },
              effective_scan_region: "2",
            },
          }),
          { headers: { "Content-Type": "application/json" } },
        );
      }
      return new Response(
        JSON.stringify({ code: 0, message: "", data: group() }),
        { headers: { "Content-Type": "application/json" } },
      );
    });
  const c = new ApiClient("admin");
  c.csrfToken = "synthetic-only";
  const w = mount(CoreRiskGroupPanel, {
    props: {
      client: c,
      groupID: group().group_id,
      allowed: true,
      disabled: false,
    },
  });
  try {
    await flushPromises();
    await w.findAll("select")[1]!.setValue("2");
    await w
      .findAll("button")
      .find((b) => b.text() === "核对并保存用户组风控")!
      .trigger("click");
    await w
      .findAll("button")
      .find((b) => b.text() === "确认保存用户组风控")!
      .trigger("click");
    await flushPromises();
    expect(typeof wire[0]).toBe("object");
    expect(w.text()).toContain("用户组风控配置已保存");
  } finally {
    w.unmount();
    fetch.mockRestore();
  }
});
const group = (revision = "0") => ({
  group_id: "9007199254741773",
  group_name: "合成会员组",
  configured: revision !== "0",
  revision,
  fingerprint: (revision === "0" ? "a" : "b").repeat(64),
  config: { pay_block_oversea: "", check_pay_regoin: "" },
  effective_oversea: "0",
  effective_scan_region: "0",
  geo_configured: true,
});
function api(request: ReturnType<typeof vi.fn>) {
  const c = new ApiClient("admin");
  c.csrfToken = "synthetic-risk177-controls";
  c.request = request as ApiClient["request"];
  return c;
}
it("group config confirms both original fields, holds parent busy and checks the committed revision without key-order dependence", async () => {
  let resolve!: (v: unknown) => void;
  const request = vi.fn(
    (_: string, options?: { method?: string; body?: unknown }) =>
      options?.method === "PATCH"
        ? new Promise((r) => (resolve = r))
        : Promise.resolve(group()),
  );
  const w = mount(CoreRiskGroupPanel, {
    props: {
      client: api(request),
      groupID: group().group_id,
      allowed: true,
      disabled: false,
    },
  });
  await flushPromises();
  await w.findAll("select")[1]!.setValue("2");
  await w
    .findAll("button")
    .find((b) => b.text() === "核对并保存用户组风控")!
    .trigger("click");
  expect(
    request.mock.calls.filter(([, o]) => o?.method === "PATCH"),
  ).toHaveLength(0);
  await w
    .findAll("button")
    .find((b) => b.text() === "确认保存用户组风控")!
    .trigger("click");
  expect(w.emitted("busy")?.at(-1)).toEqual([true]);
  const sent = request.mock.calls.find(([, o]) => o?.method === "PATCH")![1]!
    .body;
  expect(sent).toEqual({
    expected_fingerprint: "a".repeat(64),
    config: { pay_block_oversea: "", check_pay_regoin: "2" },
  });
  resolve({
    ...group("1"),
    config: { check_pay_regoin: "2", pay_block_oversea: "" },
    effective_scan_region: "2",
  });
  await flushPromises();
  expect(w.text()).toContain("用户组风控配置已保存");
  expect(w.emitted("busy")?.at(-1)).toEqual([false]);
  w.unmount();
});
it("group malformed commit stays uncertain and never retries the mutation", async () => {
  const request = vi.fn(
    async (_: string, options?: { method?: string; body?: unknown }) =>
      options?.method === "PATCH"
        ? {
            ...group("1"),
            config: { pay_block_oversea: "", check_pay_regoin: "0" },
          }
        : group(),
  );
  const w = mount(CoreRiskGroupPanel, {
    props: {
      client: api(request),
      groupID: group().group_id,
      allowed: true,
      disabled: false,
    },
  });
  await flushPromises();
  await w.findAll("select")[1]!.setValue("2");
  await w
    .findAll("button")
    .find((b) => b.text() === "核对并保存用户组风控")!
    .trigger("click");
  await w
    .findAll("button")
    .find((b) => b.text() === "确认保存用户组风控")!
    .trigger("click");
  await flushPromises();
  expect(w.text()).toContain("保存结果尚未核实");
  expect(
    w.findAll("select").every((s) => s.attributes("disabled") !== undefined),
  ).toBe(true);
  expect(
    request.mock.calls.filter(([, o]) => o?.method === "PATCH"),
  ).toHaveLength(1);
  w.unmount();
});
const action = {
  id: "9007199254741774",
  kind: "merchant",
  target_id: "9007199254741773",
  revision: "1",
  rule: "auto_check_sucrate",
  active: true,
  until: null,
  changed_at: "2026-10-26T00:00:00Z",
  source_count: 1,
  source_order_ids: ["9007199254741775"],
  fingerprint: "c".repeat(64),
};
it("risk release preserves full IDs and sends both policy and action fingerprints only after confirmation", async () => {
  const request = vi.fn(
    async (_: string, o?: { method?: string; body?: unknown }) =>
      o?.method === "POST"
        ? {
            ...action,
            active: false,
            revision: "2",
            fingerprint: "d".repeat(64),
          }
        : { items: [action], total: 1 },
  );
  const w = mount(CoreRiskActions, {
    props: {
      client: api(request),
      allowed: true,
      blocked: false,
      policyFingerprint: "a".repeat(64),
    },
  });
  await flushPromises();
  expect(w.text()).toContain(action.id);
  await w
    .findAll("button")
    .find((b) => b.text().includes("解除"))!
    .trigger("click");
  expect(
    request.mock.calls.filter(([, o]) => o?.method === "POST"),
  ).toHaveLength(0);
  await w
    .findAll("button")
    .find((b) => b.text() === "确认解除此项")!
    .trigger("click");
  await flushPromises();
  const call = request.mock.calls.find(([, o]) => o?.method === "POST")!;
  expect(call[0]).toContain(action.id + "/release");
  expect(call[1]!.body).toEqual({
    expected_fingerprint: action.fingerprint,
    expected_policy_fingerprint: "a".repeat(64),
  });
  w.unmount();
});
