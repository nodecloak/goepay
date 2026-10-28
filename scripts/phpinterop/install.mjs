// Run only after root explicitly confirms the existing test runner is idle.
import fs from "node:fs";
import path from "node:path";
import crypto from "node:crypto";
import { spawnSync } from "node:child_process";

const args = new Map();
for (let i = 2; i < process.argv.length; i += 2)
  args.set(process.argv[i], process.argv[i + 1]);
const base = args.get("--prefix"),
  archiveHash = args.get("--tar-sha256"),
  manifestHash = args.get("--manifest-sha256");
const hash = (data) => crypto.createHash("sha256").update(data).digest("hex");
const safe = (value) =>
  value &&
  !value.startsWith("/") &&
  !/[\\:\x00-\x1f]/.test(value) &&
  value.split("/").every((p) => p && p !== "." && p !== "..");
if (
  args.get("--ack-idle") !== "yes" ||
  !/^\/tmp\/goepay-phpinterop-[a-f0-9]{32}$/.test(base ?? "") ||
  ![archiveHash, manifestHash].every((v) => /^[a-f0-9]{64}$/.test(v ?? "")) ||
  process.getuid() !== 0 ||
  fs.realpathSync(base) !== base
)
  throw Error("exact owned root and explicit idle acknowledgement required");
function untar(bytes) {
  const names = new Set();
  let at = 0;
  const text = (header, start, end) =>
    header.subarray(start, end).toString("utf8").replace(/\0.*$/s, "");
  while (at + 512 <= bytes.length) {
    const header = bytes.subarray(at, at + 512);
    at += 512;
    if (header.every((b) => b === 0)) {
      if (!bytes.subarray(at).every((b) => b === 0))
        throw Error("unexpected archive trailer");
      break;
    }
    const checksum = parseInt(text(header, 148, 156).trim(), 8);
    const actual = [...header].reduce(
      (sum, b, i) => sum + (i >= 148 && i < 156 ? 32 : b),
      0,
    );
    const prefix = text(header, 345, 500),
      name = (prefix ? prefix + "/" : "") + text(header, 0, 100);
    const size = parseInt(text(header, 124, 136).trim() || "0", 8),
      mode = parseInt(text(header, 100, 108).trim() || "0", 8);
    if (
      checksum !== actual ||
      !safe(name) ||
      names.has(name) ||
      ![0, 48].includes(header[156]) ||
      !Number.isSafeInteger(size) ||
      size < 0 ||
      size > 128 * 1024 * 1024 ||
      at + size > bytes.length
    )
      throw Error("unsafe regular-only runtime tar");
    names.add(name);
    const target = path.join(base, name);
    fs.mkdirSync(path.dirname(target), { recursive: true, mode: 0o755 });
    fs.writeFileSync(target, bytes.subarray(at, at + size), {
      flag: "wx",
      mode: mode & 0o555,
    });
    at += Math.ceil(size / 512) * 512;
  }
  return names;
}
try {
  const bundle = fs.readFileSync(path.join(base, "bundle.tar"));
  if (bundle.length > 512 * 1024 * 1024 || hash(bundle) !== archiveHash)
    throw Error("runtime archive hash/size mismatch");
  const names = untar(bundle),
    manifestBytes = fs.readFileSync(path.join(base, "runtime.manifest.json"));
  if (hash(manifestBytes) !== manifestHash)
    throw Error("runtime manifest changed");
  const manifest = JSON.parse(manifestBytes.toString("utf8"));
  if (
    manifest.schema !== 1 ||
    manifest.image !==
      "sha256:2d72ff4b0cfe82e22f01f15223bc4ab4c825d318cb2e3275554cd32299a5ea50" ||
    manifest.runtime_id !== base.slice("/tmp/goepay-phpinterop-".length) ||
    manifest.namespace_isolated !== false
  )
    throw Error("pinned runtime identity mismatch");
  if (names.size !== Object.keys(manifest.files).length + 1)
    throw Error("unexpected runtime file inventory");
  for (const [name, meta] of Object.entries(manifest.files)) {
    if (!safe(name) || !names.has(name))
      throw Error("invalid prepared inventory");
    const filename = path.join(base, name),
      stat = fs.lstatSync(filename);
    if (
      !stat.isFile() ||
      stat.isSymbolicLink() ||
      fs.realpathSync(filename) !== filename ||
      stat.size !== meta.size ||
      hash(fs.readFileSync(filename)) !== meta.sha256
    )
      throw Error("prepared file changed: " + name);
  }
  fs.mkdirSync(path.join(base, "build-tmp"), { mode: 0o700 });
  fs.mkdirSync(path.join(base, "cases"), { mode: 0o700 });
  const compiled = spawnSync(
    "/usr/bin/cc",
    [
      "-shared",
      "-fPIC",
      "-nostdlib",
      "-fno-builtin",
      "-fno-stack-protector",
      "-Wl,-z,now",
      "-Wl,-z,relro",
      "-Wl,-soname,goepay-php-guard.so",
      "-o",
      path.join(base, "guard.so"),
      path.join(base, "guard.c"),
    ],
    {
      cwd: base,
      encoding: "utf8",
      timeout: 30000,
      env: {
        PATH: "/usr/bin:/bin",
        TMPDIR: path.join(base, "build-tmp"),
        LANG: "C.UTF-8",
      },
    },
  );
  if (compiled.status !== 0)
    throw Error("existing offline C compiler unavailable: " + compiled.stderr);
  fs.chmodSync(path.join(base, "guard.so"), 0o444);
  const state = {
    schema: 1,
    status: "installing",
    image: manifest.image,
    runtime_manifest_sha256: manifestHash,
    guard_sha256: hash(fs.readFileSync(path.join(base, "guard.so"))),
    executor_sha256: manifest.files["executor.mjs"].sha256,
    sandbox_sha256: manifest.files.sandbox.sha256,
    extensions_to_load: [],
  };
  const writeState = () => {
    const data = JSON.stringify(state) + "\n";
    fs.writeFileSync(path.join(base, "state.json"), data, { mode: 0o600 });
    return hash(Buffer.from(data));
  };
  let stateHash = writeState();
  const probe = spawnSync(
    process.execPath,
    [path.join(base, "executor.mjs"), "preflight", stateHash],
    {
      encoding: "utf8",
      timeout: 45000,
      maxBuffer: 4 * 1024 * 1024,
      env: { PATH: "/usr/local/bin:/usr/bin:/bin", LANG: "C.UTF-8" },
    },
  );
  let result;
  try {
    result = JSON.parse(probe.stdout);
  } catch {
    throw Error(
      "runtime preflight produced no structured receipt: " + probe.stderr,
    );
  }
  if (
    probe.status !== 0 ||
    result.status !== "ready" ||
    result.seccomp_network_denied !== true ||
    result.seccomp_exec_denied !== true ||
    result.uid !== 65534 ||
    result.namespace_isolated !== false
  )
    throw Error("PHP runtime preflight unavailable: " + JSON.stringify(result));
  state.status = "ready";
  state.extensions_to_load = result.extensions_to_load;
  state.preflight = result;
  stateHash = writeState();
  const receipt = {
    schema: 1,
    status: "ready",
    image: manifest.image,
    runtime_root: base,
    runtime_manifest_sha256: manifestHash,
    state_sha256: stateHash,
    executor_sha256: state.executor_sha256,
    sandbox_sha256: state.sandbox_sha256,
    guard_sha256: state.guard_sha256,
    php_version: result.php_version,
    seccomp_network_denied: true,
    seccomp_exec_denied: true,
    uid: 65534,
    namespace_isolated: false,
    containers_created: 0,
  };
  fs.writeFileSync(
    path.join(base, "READY.json"),
    JSON.stringify(receipt, null, 2) + "\n",
    { flag: "wx", mode: 0o600 },
  );
  process.stdout.write(JSON.stringify(receipt, null, 2) + "\n");
} catch (error) {
  const failure = {
    schema: 1,
    status: "unavailable",
    error: String(error.message ?? error),
    containers_created: 0,
    namespace_isolated: false,
  };
  fs.writeFileSync(
    path.join(base, "UNAVAILABLE.json"),
    JSON.stringify(failure, null, 2) + "\n",
    { flag: "wx", mode: 0o600 },
  );
  process.stdout.write(JSON.stringify(failure, null, 2) + "\n");
  process.exitCode = 125;
}
