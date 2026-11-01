// @vitest-environment jsdom
import { expect, it, vi } from "vitest";
import { flushPromises, mount } from "@vue/test-utils";
import ResourcePage from "../src/components/ResourcePage.vue";
import PayRollCreateForm from "../src/components/PayRollCreateForm.vue";
import PayRollChannelEditor from "../src/components/PayRollChannelEditor.vue";
import { ApiClient } from "../src/api";

async function fixture(draft = false) {
  const client = new ApiClient("admin");
  client.csrfToken = "synthetic-roll-parent";
  const record = {
    id: "3",
    name: "Synthetic roll",
    channel_ids: draft ? "" : "7:1",
    merchant_id: "0",
    type: "alipay",
    strategy: "round_robin",
    enabled: false,
    default_route: false,
    limits: "{}",
    rules: "{}",
  };
  const request = vi
    .spyOn(client, "request")
    .mockImplementation(async (path) => {
      if (path.endsWith("/schema"))
        return {
          title: "通道轮询",
          permissions: { create: true, update: true, delete: false },
          fields: [
            { name: "name", label: "名称", type: "string", required: true },
            {
              name: "channel_ids",
              label: "通道ID:权重，逗号分隔",
              type: "string",
              required: true,
            },
          ],
        } as never;
      if (path.endsWith("/3")) return record as never;
      return {
        items: [record],
        total: 1,
      } as never;
    });
  const w = mount(ResourcePage, {
    props: {
      client,
      resource: "pay-rolls",
      ownSession: true,
      adminClass: "super",
    },
    global: {
      stubs: {
        PayRollCreateForm: {
          props: ["client", "disabled"],
          emits: ["busy", "saved", "recovery-required"],
          template:
            '<form aria-label="轮询组新增表单"><input aria-label="显示名称"></form>',
        },
        PayRollChannelEditor: {
          props: ["client", "rollID", "disabled"],
          emits: ["busy", "saved"],
          template: '<div aria-label="轮询组通道表格"></div>',
        },
      },
    },
    attachTo: document.body,
  });
  await flushPromises();
  return { w, request };
}

it("uses the three-field creation flow and never submits a second generic resource write", async () => {
  const { w, request } = await fixture();
  const opener = w.findAll("button").find((b) => b.text() === "新增轮询配置")!;
  (opener.element as HTMLButtonElement).focus();
  await opener.trigger("click");
  await flushPromises();
  const child = w.findComponent(PayRollCreateForm);
  expect(child.exists()).toBe(true);
  expect(w.find("#field-channel_ids").exists()).toBe(false);
  expect(w.get('[role="dialog"]').attributes("aria-label")).toBe("新增轮询组");
  child.vm.$emit("busy", true);
  await flushPromises();
  expect(
    w.get('button[aria-label="关闭"]').attributes("disabled"),
  ).toBeDefined();
  child.vm.$emit("busy", false);
  child.vm.$emit("saved", { id: "4" });
  await flushPromises();
  expect(w.find('[role="dialog"]').exists()).toBe(false);
  expect(request.mock.calls.some(([, o]) => o?.method === "POST")).toBe(false);
  expect(document.activeElement).toBe(opener.element);
  w.unmount();
});

it("opens channel configuration separately from the advanced edit form", async () => {
  const { w } = await fixture();
  const opener = w.findAll("button").find((b) => b.text() === "配置通道")!;
  (opener.element as HTMLButtonElement).focus();
  await opener.trigger("click");
  await flushPromises();
  const child = w.findComponent(PayRollChannelEditor);
  expect(child.exists()).toBe(true);
  expect(child.props("rollID")).toBe("3");
  expect(w.find("#field-channel_ids").exists()).toBe(false);
  expect(w.get('[role="dialog"]').attributes("aria-label")).toBe("配置通道");
  await w.get('button[aria-label="关闭"]').trigger("click");
  await flushPromises();
  expect(document.activeElement).toBe(opener.element);
  w.unmount();
});

it("returns focus to the refreshed row after channel configuration replaces the table", async () => {
  const { w } = await fixture();
  const opener = w.findAll("button").find((b) => b.text() === "配置通道")!;
  (opener.element as HTMLButtonElement).focus();
  await opener.trigger("click");
  await flushPromises();
  w.findComponent(PayRollChannelEditor).vm.$emit("saved", { id: "3" });
  await flushPromises();
  const refreshed = w.findAll("button").find((b) => b.text() === "配置通道")!;
  expect(refreshed.element).not.toBe(opener.element);
  expect(document.activeElement).toBe(refreshed.element);
  w.unmount();
});

it("returns to create when the original configured row is absent from the refreshed list", async () => {
  const { w, request } = await fixture();
  const opener = w.findAll("button").find((b) => b.text() === "配置通道")!;
  (opener.element as HTMLButtonElement).focus();
  await opener.trigger("click");
  await flushPromises();
  request.mockResolvedValueOnce({ items: [], total: 0 } as never);
  w.findComponent(PayRollChannelEditor).vm.$emit("saved", { id: "3" });
  await flushPromises();
  expect(w.findAll("button").some((b) => b.text() === "配置通道")).toBe(false);
  const create = w.findAll("button").find((b) => b.text() === "新增轮询配置")!;
  expect(document.activeElement).toBe(create.element);
  w.unmount();
});

it("allows renaming an inert draft through the advanced editor without requiring channels", async () => {
  const { w, request } = await fixture(true);
  await w
    .findAll("button")
    .find((b) => b.text() === "编辑")!
    .trigger("click");
  await flushPromises();
  expect(
    w.get("#field-channel_ids").attributes("aria-required"),
  ).toBeUndefined();
  await w.get("#field-name").setValue("Renamed draft");
  await w.get("form").trigger("submit");
  await flushPromises();
  expect(
    request.mock.calls.filter(([, o]) => o?.method === "PATCH"),
  ).toHaveLength(1);
  expect(w.find('[role="dialog"]').exists()).toBe(false);
  w.unmount();
});
