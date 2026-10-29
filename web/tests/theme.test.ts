import { describe, expect, it } from "vitest";
import { applyTheme, saveTheme, storedTheme } from "../src/theme";

function memoryStorage(initial: Record<string, string> = {}) {
  const data = { ...initial };
  return {
    data,
    getItem: (key: string) => (key in data ? data[key] : null),
    setItem: (key: string, value: string) => {
      data[key] = value;
    },
  };
}

describe("console theme", () => {
  it("defaults to the dark theme of the UI kit", () => {
    expect(storedTheme(memoryStorage())).toBe("dark");
    expect(storedTheme(undefined)).toBe("dark");
  });

  it("remembers the light theme and ignores unknown values", () => {
    const storage = memoryStorage();
    saveTheme("light", storage);
    expect(storedTheme(storage)).toBe("light");
    expect(storedTheme(memoryStorage({ "goepay-console-theme": "neon" }))).toBe(
      "dark",
    );
  });

  it("keeps working when storage throws", () => {
    const broken = {
      getItem: () => {
        throw new Error("denied");
      },
      setItem: () => {
        throw new Error("denied");
      },
    };
    expect(storedTheme(broken)).toBe("dark");
    expect(() => saveTheme("light", broken)).not.toThrow();
  });

  it("sets data-theme on the root element", () => {
    const root = document.createElement("html");
    applyTheme("light", root);
    expect(root.dataset.theme).toBe("light");
    applyTheme("dark", root);
    expect(root.dataset.theme).toBe("dark");
  });
});
