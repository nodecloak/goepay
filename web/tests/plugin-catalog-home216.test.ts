// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { flushPromises, mount, type VueWrapper } from "@vue/test-utils";
import market from "../src/data/plugin-catalog.json";
import registryData from "../src/data/builtin-templates.json";
import { withBuiltinTemplates, type CatalogEntry } from "../src/plugin-catalog";
import AppStorePage from "../src/components/AppStorePage.vue";

const names = [
  "default",
  "index1",
  "index2",
  "index3",
  "index4",
  "index5",
  "index6",
  "index7",
  "index8",
  "index9",
  "index10",
  "minimal",
];
type RegistryTemplate = {
  type: string;
  name: string;
  title: string;
  description: string;
  version?: string;
};
const registry = registryData as RegistryTemplate[];
const original = market as CatalogEntry[];
const mounted: VueWrapper[] = [];
afterEach(() => {
  for (const w of mounted.splice(0)) w.unmount();
  document.body.innerHTML = "";
  vi.restoreAllMocks();
});

it.each(names)(
  "shows current registry metadata for home/%s, including original market entries",
  (name) => {
    const source = registry.find((x) => x.type === "home" && x.name === name)!;
    expect(source, `registry home/${name}`).toBeDefined();
    const row = withBuiltinTemplates(original).find(
      (x) => x.type === "template" && x.sub === "home" && x.name === name,
    )!;
    expect(row, `market home/${name}`).toBeDefined();
    expect(row.desc).toBe(source.description);
    expect(row.version).toBe("3");
    expect(source.version).toBe("3");
    const captured = original.find(
      (x) => x.type === "template" && x.sub === "home" && x.name === name,
    );
    expect(row.title).toBe(captured?.title ?? source.title);
  },
);

it("overlays only the known home description/version while preserving original metadata and other categories", () => {
  const known = original.find(
    (x) => x.type === "template" && x.sub === "home" && x.name === "index7",
  )!;
  const custom = Object.freeze({
    ...known,
    title: "保留原市场标题",
    author: "原市场作者",
    desc: "旧简介",
    version: "legacy",
  });
  const foreign: CatalogEntry = Object.freeze({
    ...known,
    name: "unported-home",
    title: "未移植模板",
    desc: "原版说明",
    version: "legacy",
  });
  const other = original.filter(
    (x) => x.type !== "template" || x.sub !== "home",
  );
  const input = [custom, foreign, ...other];
  const before = JSON.stringify(input);
  const result = withBuiltinTemplates(input);
  const home = result.find(
    (x) => x.type === "template" && x.sub === "home" && x.name === "index7",
  )!;
  const source = registry.find(
    (x) => x.type === "home" && x.name === "index7",
  )!;
  expect(home).toEqual({ ...custom, desc: source.description, version: "3" });
  expect(
    result.find((x) => x.type === "template" && x.name === foreign.name),
  ).toBe(foreign);
  for (const entry of other)
    expect(
      result.find(
        (x) =>
          x.type === entry.type && x.sub === entry.sub && x.name === entry.name,
      ),
    ).toBe(entry);
  expect(result.filter((x) => x.type !== "template")).toEqual(
    other.filter((x) => x.type !== "template"),
  );
  expect(JSON.stringify(input)).toBe(before);
});

it("uses serialized registry versions for added templates and remains idempotent", () => {
  const generated = withBuiltinTemplates([]);
  expect(generated).toHaveLength(16);
  for (const row of generated) {
    expect(row.version).toBe(row.sub === "home" ? "3" : "1");
    const source = registry.find(
      (x) => x.type === row.sub && x.name === row.name,
    )!;
    expect(row.version).toBe(source.version);
  }
  expect(withBuiltinTemplates(generated)).toEqual(generated);
  const all = withBuiltinTemplates(original);
  expect(withBuiltinTemplates(all)).toEqual(all);
});

it("renders the updated homepage description and version in the real app market and detail drawer", async () => {
  const w = mount(AppStorePage, { attachTo: document.body });
  mounted.push(w);
  await vi.waitFor(() => expect(w.text()).not.toContain("正在读取应用列表"));
  await flushPromises();
  await w
    .findAll('[role="tab"]')
    .find((x) => x.text() === "模板")!
    .trigger("click");
  const row = w
    .findAll("tbody tr")
    .find(
      (x) =>
        x.findAll("td")[1]?.text() === "default" &&
        x.text().includes("默认首页模板"),
    )!;
  const source = registry.find(
    (x) => x.type === "home" && x.name === "default",
  )!;
  expect(row.find(".plugin-desc").text()).toBe(source.description);
  expect(row.findAll("td")[5].text()).toBe("3");
  await row.get('button[aria-label="应用详情 默认首页模板"]').trigger("click");
  const drawer = w.get('[role="dialog"][aria-label="应用详情"]');
  expect(drawer.text()).toContain(source.description);
  expect(drawer.findAll("dd")[3].text()).toBe("3");
});
