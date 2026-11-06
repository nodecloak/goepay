import { existsSync, readdirSync, readFileSync, statSync } from "node:fs";
import { join, resolve } from "node:path";
import { describe, expect, it } from "vitest";
import { ApiError } from "../src/api";
import {
  hasServerMessageTranslation,
  localizeServerMessage,
} from "../src/server-messages";

const internal = resolve(__dirname, "../../internal");
function goFiles(dir: string): string[] {
  return readdirSync(dir).flatMap((name) => {
    const path = join(dir, name);
    if (statSync(path).isDirectory()) return goFiles(path);
    return name.endsWith(".go") && !name.endsWith("_test.go") ? [path] : [];
  });
}
// Literal messages handed straight to the console JSON writers.
const writer =
  /(?:\bresponse\(w, [^,()]+, -1, |\bwrite\(w, [^,()]+, (?:nil|map\[[^\]]+\][a-z]+\{[^}]*\}|[a-zA-Z_.]+), )"((?:[^"\\\n]|\\.)*)"/g;

describe("server message localization", () => {
  // Runs from the repository checkout; copies of web/ without the Go sources
  // report this check as skipped rather than passed.
  it.skipIf(!existsSync(internal))(
    "translates every English console message written by a Go handler",
    () => {
      const missing = new Set<string>();
      for (const file of goFiles(internal)) {
        if (file.includes(`${join("internal", "adapters")}`)) continue;
        for (const match of readFileSync(file, "utf8").matchAll(writer)) {
          const text = match[1];
          if (!text || /[㐀-鿿]/.test(text) || !/[a-zA-Z]{2}/.test(text))
            continue;
          // Single lowercase identifiers are response data keys, not messages.
          if (/^[a-z_]+$/.test(text)) continue;
          if (!hasServerMessageTranslation(text)) missing.add(text);
        }
      }
      expect([...missing].sort()).toEqual([]);
    },
  );

  it("shows Chinese text and keeps the original for program logic", () => {
    const e = new ApiError("invalid credentials", 401, -1);
    expect(e.message).toBe("账号、密码或密钥错误");
    expect(e.serverMessage).toBe("invalid credentials");
    const own = new ApiError("own merchant session required", 403, -1);
    expect(own.message).toBe(
      "该功能需要商户本人登录，请使用商户账号登录后操作。",
    );
  });

  it("explains unknown English messages by status and keeps Chinese text", () => {
    expect(localizeServerMessage("brand new failure", 422)).toBe(
      "输入内容或当前状态不允许此操作（brand new failure）",
    );
    expect(localizeServerMessage("", 503)).toBe("服务暂时不可用，请稍后重试");
    expect(localizeServerMessage("商户尚未完成实名认证", 422)).toBe(
      "商户尚未完成实名认证",
    );
  });

  it("adds the Retry-After wait to throttled requests", () => {
    expect(localizeServerMessage("too many attempts", 429, 900)).toBe(
      "登录尝试次数过多，请约 15 分钟后再试",
    );
    expect(localizeServerMessage("image upload rate limited", 429, 900)).toBe(
      "图片上传过于频繁，请稍后再试",
    );
  });
});
