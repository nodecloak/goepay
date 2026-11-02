import { expect, it, vi } from "vitest";
import { flushPromises, mount } from "@vue/test-utils";
import ResourceFields from "../src/components/ResourceFields.vue";
import TaskSchedulerPage from "../src/components/TaskSchedulerPage.vue";
import SystemUpdatePage from "../src/components/SystemUpdatePage.vue";
import KitSelect from "../src/components/KitSelect.vue";
import { ApiClient } from "../src/api";
import { readScheduledTask, scheduledTaskChange } from "../src/scheduled-tasks";
import { scheduler171Task } from "./scheduler171-fixtures";
import actualUpdate from "../e2e/systemupdate183-http-fixture.json";
import type { ResourceField } from "../src/resources";

const permission: ResourceField = {
  name: "permissions",
  label: "菜单权限",
  type: "permission_tree",
};
it.each([undefined, "super"])(
  "hides inapplicable permission selection for role %s",
  (role) => {
    const w = mount(ResourceFields, {
      props: {
        fields: [permission],
        values: { admin_class: role, permissions: ["OrderList"] },
        errors: {},
        editing: false,
      },
    });
    expect(w.find(".admin-permission-tree").exists()).toBe(false);
    w.unmount();
  },
);
it("shows operator grants without nesting labels or changing the saved selection on role switches", async () => {
  const permissions = ["OrderList"];
  const w = mount(ResourceFields, {
    props: {
      fields: [permission],
      values: { admin_class: "operator", permissions },
      errors: {},
      editing: true,
    },
  });
  expect(w.find(".admin-permission-tree").exists()).toBe(true);
  expect(w.find("label .admin-permission-tree").exists()).toBe(false);
  await w.setProps({ values: { admin_class: "super", permissions } });
  await w.setProps({ values: { admin_class: "operator", permissions } });
  expect(w.find(".admin-permission-tree").exists()).toBe(true);
  expect(w.emitted("change")).toBeUndefined();
  expect(permissions).toEqual(["OrderList"]);
  w.unmount();
});

it("switching a periodic task to once emits the required canonical interval rather than validating a hidden field", () => {
  const task = readScheduledTask(scheduler171Task());
  const change = scheduledTaskChange(task, {
    enabled: false,
    frequency: "once",
    interval: "45",
    executeAt: "2026-11-03T14:00:00",
    params: { limit: "20" },
  });
  expect(change.schedule).toEqual({
    frequency: "once",
    interval: 1,
    execute_at: "2026-11-03T06:00:00.000Z",
  });
  expect(change.expected_fingerprint).toBe(task.fingerprint);
});
it("task cards explain the business action and one-time forms hide interval while keeping cancel read-only", async () => {
  const raw = scheduler171Task({ description: "原奖励结算，不产生新付款请求" });
  const client = new ApiClient("admin");
  client.csrfToken = "synthetic-tasks229";
  const request = vi
    .spyOn(client, "request")
    .mockImplementation(
      async (p) =>
        (p === "/scheduled-tasks" ? { items: [raw], total: 1 } : raw) as never,
    );
  const w = mount(TaskSchedulerPage, {
    props: { client, allowed: true },
    global: { stubs: { ShellIcon: true } },
  });
  await flushPromises();
  expect(w.get(".job-card").text()).toContain(raw.description);
  await w
    .findAll("button")
    .find((b) => b.text() === "配置")!
    .trigger("click");
  await flushPromises();
  w.findComponent(KitSelect).vm.$emit("update:modelValue", "once");
  await flushPromises();
  expect(w.find("#scheduler171-interval").exists()).toBe(false);
  expect(w.text()).toContain("执行时间（UTC+8）");
  await w
    .findAll("button")
    .find((b) => b.text() === "关闭")!
    .trigger("click");
  expect(
    request.mock.calls.filter(([, o]) => o?.method && o.method !== "GET"),
  ).toHaveLength(0);
  w.unmount();
});

it("update action names the real preparation step and opening then canceling performs no POST", async () => {
  const client = new ApiClient("admin");
  client.csrfToken = "synthetic-update229";
  const request = vi
    .spyOn(client, "request")
    .mockResolvedValue(actualUpdate.available);
  const w = mount(SystemUpdatePage, {
    props: { client, ownSession: true, adminClass: "super" },
    global: { stubs: { Teleport: true } },
  });
  await flushPromises();
  expect(w.get('[data-action="prepare"]').text()).toBe("准备更新包");
  await w.get('[data-action="prepare"]').trigger("click");
  expect(w.get('[role="dialog"]').text()).toContain("确认准备更新包");
  await w
    .findAll("button")
    .find((b) => b.text() === "返回")!
    .trigger("click");
  await flushPromises();
  expect(
    request.mock.calls.filter(([, o]) => o?.method === "POST"),
  ).toHaveLength(0);
  w.unmount();
});
