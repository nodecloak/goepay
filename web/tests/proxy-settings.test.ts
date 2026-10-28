import { expect, it } from "vitest";
import {
  settingDefinition,
  settingStorage,
  settingsTabs,
} from "../src/settings";

it("shows the original 中转代理 settings on the proxy tab", () => {
  expect(settingsTabs.find((t) => t[0] === "proxy")?.[1]).toBe("中转代理");
  const proxy = settingDefinition("proxy");
  expect([proxy.tab, proxy.label]).toEqual(["proxy", "中转代理开关"]);
  expect(proxy.options?.map((o) => [o.value, o.label])).toEqual([
    ["0", "关闭"],
    ["1", "代理服务器"],
    ["2", "代理API"],
  ]);
  const type = settingDefinition("proxy_type");
  expect(type.options?.map((o) => o.value)).toEqual([
    "http",
    "https",
    "sock4",
    "sock5",
    "sock5h",
  ]);
  for (const key of [
    "proxy_server",
    "proxy_port",
    "proxy_user",
    "proxy_apiurl",
  ])
    expect(settingDefinition(key).tab).toBe("proxy");
  expect(settingStorage(settingDefinition("proxy_port"), "8080")).toBe("8080");
  expect(() =>
    settingStorage(settingDefinition("proxy_port"), "70000"),
  ).toThrow();
  expect(() =>
    settingStorage(settingDefinition("proxy_server"), "has space"),
  ).toThrow();
  expect(
    settingStorage(
      settingDefinition("proxy_apiurl"),
      "https://relay.example.net/sub",
    ),
  ).toBe("https://relay.example.net/sub");
  expect(() =>
    settingStorage(settingDefinition("proxy_apiurl"), "relay.example.net"),
  ).toThrow();
});
