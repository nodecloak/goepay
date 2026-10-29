import { it, expect, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";

const task = {
  task: "order",
  name: "系统每日数据维护",
  description: "清理过期数据、重置通道状态",
  available: true,
  configured: true,
  enabled: true,
  control_mode: "managed",
  can_update: true,
  can_run: true,
  revision: "1",
  fingerprint: "fp",
  last_run_time: null,
  next_run_time: null,
  last_status: "never_run",
  unavailable_reason: "",
  schedule: {
    frequency: "day",
    interval: 1,
    execute_at: "2026-10-29T00:00:00Z",
  },
  params: {},
  parameter_definitions: [],
};

// Mock the strict validators so the component gets a usable task without
// hand-crafting the exact wire shape they parse.
vi.mock("../src/scheduled-tasks", async () => {
  const actual = await vi.importActual<typeof import("../src/scheduled-tasks")>(
    "../src/scheduled-tasks",
  );
  return {
    ...actual,
    readScheduledCatalog: () => [task],
    readScheduledTask: () => task,
    scheduledTaskModeLabel: () => "已接管",
  };
});

import TaskSchedulerPage from "../src/components/TaskSchedulerPage.vue";

function client() {
  return {
    area: "admin",
    csrfToken: "t",
    request: vi.fn().mockResolvedValue({}),
  } as unknown as import("../src/api").ApiClient;
}

it("cron config dialog uses the kit switch (开启此任务) and kit frequency select", async () => {
  const wrapper = mount(TaskSchedulerPage, {
    props: { client: client(), allowed: true },
    global: {
      stubs: { ShellIcon: true, ResourcePage: true, OperationsPage: true },
    },
  });
  await flushPromises();
  // task card rendered from the mocked catalog
  expect(wrapper.text()).toContain("系统每日数据维护");
  const configBtn = wrapper.findAll("button").find((b) => b.text() === "配置");
  expect(configBtn).toBeTruthy();
  await configBtn!.trigger("click");
  await flushPromises();
  // kit switch + kit select present in the config dialog
  expect(wrapper.find(".pa-switch").exists()).toBe(true);
  expect(wrapper.find(".pa-select-trigger").exists()).toBe(true);
  expect(wrapper.text()).toContain("开启此任务");
});
