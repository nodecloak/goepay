// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { flushPromises, mount, type VueWrapper } from "@vue/test-utils";
import { ApiClient } from "../src/api";
import RefundAttestationDialog from "../src/components/RefundAttestationDialog.vue";
import {
  isAdminFundsAction,
  needsFundsAuthorization,
} from "../src/funds-authorization";

const views: VueWrapper[] = [];
afterEach(() => {
  views.splice(0).forEach((view) => view.unmount());
  vi.restoreAllMocks();
});

function attestation(overrides: Record<string, unknown> = {}) {
  return {
    refund_id: "12",
    refund_no: "RF-12",
    trade_no: "202611060000000000000000001",
    merchant_id: "2",
    amount: "1.00",
    status: "pending",
    sent: true,
    channel_id: "7",
    eligible: true,
    reason: "",
    states: ["succeeded", "failed"],
    attestation: null,
    ...overrides,
  };
}

function mountDialog(responses: unknown[]) {
  const client = new ApiClient("admin");
  client.csrfToken = "synthetic-csrf";
  const request = vi
    .spyOn(client, "request")
    .mockImplementation(async () => responses.shift() as never);
  const view = mount(RefundAttestationDialog, {
    props: { client, refundId: "12", ownSession: true, adminClass: "super" },
    attachTo: document.body,
  });
  views.push(view);
  return { view, request };
}

it("treats attestation as a mandatory funds action", () => {
  expect(isAdminFundsAction("/refunds/12/attest-result", "POST")).toBe(true);
  expect(isAdminFundsAction("/refunds/12/attestation", "GET")).toBe(false);
  expect(
    needsFundsAuthorization(
      "/console/v1/admin/refunds/12/attest-result",
      false,
    ),
  ).toBe(true);
});

it("records a verified result only after the operator confirms the check", async () => {
  const { view, request } = mountDialog([
    attestation(),
    attestation({ status: "succeeded", eligible: false, reason: "final" }),
  ]);
  await flushPromises();
  const submit = () =>
    view.findAll("button").find((b) => b.text() === "确认并提交")!;
  expect(submit().attributes("disabled")).toBeDefined();
  await view.find('input[value="succeeded"]').setValue(true);
  await view.find('input[maxlength="128"]').setValue("ALI-REFUND-1");
  await view.find("textarea").setValue("商家后台退款成功");
  expect(submit().attributes("disabled")).toBeDefined();
  await view.find('input[type="checkbox"]').setValue(true);
  await submit().trigger("click");
  await flushPromises();
  expect(request).toHaveBeenLastCalledWith(
    "/refunds/12/attest-result",
    expect.objectContaining({
      method: "POST",
      body: {
        state: "succeeded",
        provider_reference: "ALI-REFUND-1",
        note: "商家后台退款成功",
      },
    }),
  );
  expect(view.text()).toContain("已记录核实结果：退款成功");
  expect(view.emitted("updated")).toHaveLength(1);
});

it("explains why a refund cannot be closed by hand", async () => {
  const { view } = mountDialog([
    attestation({ eligible: false, reason: "automatic_query", states: [] }),
  ]);
  await flushPromises();
  expect(view.text()).toContain("系统会自动核对");
  expect(view.findAll("button").some((b) => b.text() === "确认并提交")).toBe(
    false,
  );
});

it("only offers failure for a reservation that was never sent", async () => {
  const { view } = mountDialog([
    attestation({ sent: false, states: ["failed"] }),
  ]);
  await flushPromises();
  expect(view.findAll('input[type="radio"]')).toHaveLength(1);
  expect(view.text()).toContain("只能核实为失败");
});

it("shows a recorded attestation in Beijing time and offers no new one", async () => {
  const { view } = mountDialog([
    attestation({
      status: "succeeded",
      eligible: false,
      reason: "final",
      states: [],
      attestation: {
        state: "succeeded",
        provider_reference: "ALI-REFUND-1",
        note: "商家后台显示退款成功",
        admin_username: "admin",
        created_at: "2026-11-06T09:12:33.123456Z",
      },
    }),
  ]);
  await flushPromises();
  expect(view.text()).toContain(
    "已于 2026-11-06 17:12:33 由 admin 核实为“退款成功”",
  );
  expect(view.text()).not.toContain("2026-11-06T09:12:33");
  expect(view.findAll('input[type="radio"]')).toHaveLength(0);
});
