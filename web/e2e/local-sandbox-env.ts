import fs from "node:fs";

// Live tests must explicitly name a loopback sandbox. An isolated fixture file
// avoids copying the running application's environment into source snapshots.
export function localSandboxBase(): string {
  const base = process.env.GOEPAY_WEB_URL || "";
  const match = /^http:\/\/(?:127\.0\.0\.1|localhost):([1-9][0-9]{0,4})$/.exec(
    base,
  );
  if (!match || Number(match[1]) > 65535)
    throw Error("Explicit loopback sandbox URL required");
  return base;
}

export function readLocalSandboxEnv(moduleURL: string): string {
  localSandboxBase();
  return fs.readFileSync(
    process.env.GOEPAY_LIVE_ENV_FILE || new URL("../../.env", moduleURL),
    "utf8",
  );
}
