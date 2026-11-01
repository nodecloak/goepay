import { flushPromises, mount } from "@vue/test-utils";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { ApiClient } from "../src/api";

const state = vi.hoisted(() => ({ load: vi.fn() }));
vi.mock("vue", async (importOriginal) => {
  const actual = await importOriginal<typeof import("vue")>();
  return {
    ...actual,
    defineAsyncComponent: (
      source: Parameters<typeof actual.defineAsyncComponent>[0],
    ) =>
      actual.defineAsyncComponent({
        ...(typeof source === "function" ? { loader: source } : source),
        loader: state.load,
      }),
  };
});
beforeEach(() => {
  vi.resetModules();
  state.load.mockReset();
});
afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  vi.useRealTimers();
});

it("ends a stalled chunk load with a bounded error state rather than leaving the page blank", async () => {
  vi.useFakeTimers();
  state.load.mockImplementation(() => new Promise(() => {}));
  const ReportPage = (await import("../src/components/ReportPage.vue")).default;
  const client = new ApiClient("admin"),
    request = vi.spyOn(client, "request");
  const w = mount(ReportPage, {
    props: { client, report: "provinces" },
    global: { config: { errorHandler: vi.fn() } },
  });
  await flushPromises();
  expect(w.get('[role="status"]').text()).toContain("加载地区统计");
  await vi.advanceTimersByTimeAsync(20000);
  expect(w.get('[role="alert"]').text()).toContain("地区统计加载失败");
  expect(w.find('[role="status"]').exists()).toBe(false);
  expect(state.load).toHaveBeenCalledTimes(1);
  expect(request).not.toHaveBeenCalled();
  w.unmount();
});

it("shows visible loading feedback while the province chunk is pending and replaces it after success", async () => {
  let resolve!: (value: any) => void;
  state.load.mockImplementation(
    () =>
      new Promise((done) => {
        resolve = done;
      }),
  );
  const ReportPage = (await import("../src/components/ReportPage.vue")).default;
  const client = new ApiClient("admin"),
    request = vi.spyOn(client, "request");
  const w = mount(ReportPage, { props: { client, report: "provinces" } });
  await flushPromises();
  expect(w.get('[role="status"]').text()).toContain("加载地区统计");
  expect(request).not.toHaveBeenCalled();
  resolve({ template: "<div data-province-loaded>地区地图已加载</div>" });
  await flushPromises();
  expect(w.find('[role="status"]').exists()).toBe(false);
  expect(w.find("[data-province-loaded]").exists()).toBe(true);
  expect(state.load).toHaveBeenCalledTimes(1);
  w.unmount();
});

it("shows a retry control after chunk rejection without retrying imports or business requests automatically", async () => {
  let reject!: (error: Error) => void;
  state.load.mockImplementation(
    () =>
      new Promise((_done, fail) => {
        reject = fail;
      }),
  );
  const ReportPage = (await import("../src/components/ReportPage.vue")).default;
  const client = new ApiClient("admin"),
    request = vi.spyOn(client, "request"),
    errorHandler = vi.fn();
  const reload = vi.fn();
  vi.stubGlobal("location", { reload });
  const w = mount(ReportPage, {
    props: { client, report: "provinces" },
    global: { config: { errorHandler } },
  });
  await flushPromises();
  reject(
    new Error(
      "Failed to fetch dynamically imported module: internal-chunk-detail",
    ),
  );
  await flushPromises();
  expect(w.get('[role="alert"]').text()).toContain("地区统计加载失败");
  expect(w.text()).not.toContain("internal-chunk-detail");
  expect(w.find('[role="status"]').exists()).toBe(false);
  expect(state.load).toHaveBeenCalledTimes(1);
  expect(request).not.toHaveBeenCalled();
  expect(reload).not.toHaveBeenCalled();
  await w.get("button").trigger("click");
  expect(reload).toHaveBeenCalledTimes(1);
  expect(request).not.toHaveBeenCalled();
  expect(state.load).toHaveBeenCalledTimes(1);
  w.unmount();
});
