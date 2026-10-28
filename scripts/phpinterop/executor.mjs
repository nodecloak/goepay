// Invoked only in the existing, explicitly prepared idle runner. No Docker API.
import fs from "node:fs";
import path from "node:path";
import crypto from "node:crypto";
import { spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";

const base = path.dirname(fileURLToPath(import.meta.url));
const hash = (bytes) => crypto.createHash("sha256").update(bytes).digest("hex");
const safe = (name) =>
  typeof name === "string" &&
  name &&
  !name.startsWith("/") &&
  !/[\\:\x00-\x1f]/.test(name) &&
  name.split("/").every((part) => part && part !== "." && part !== "..");
function regular(filename, digest) {
  const stat = fs.lstatSync(filename);
  if (
    !stat.isFile() ||
    stat.isSymbolicLink() ||
    fs.realpathSync(filename) !== filename ||
    hash(fs.readFileSync(filename)) !== digest
  )
    throw Error("runtime file integrity failure: " + path.basename(filename));
}
function loadState(expected) {
  if (
    !/^\/tmp\/goepay-phpinterop-[a-f0-9]{32}$/.test(base) ||
    !/^[a-f0-9]{64}$/.test(expected)
  )
    throw Error("owned prepared runtime required");
  regular(path.join(base, "state.json"), expected);
  const state = JSON.parse(
    fs.readFileSync(path.join(base, "state.json"), "utf8"),
  );
  regular(
    path.join(base, "runtime.manifest.json"),
    state.runtime_manifest_sha256,
  );
  const manifest = JSON.parse(
    fs.readFileSync(path.join(base, "runtime.manifest.json"), "utf8"),
  );
  if (
    manifest.image !==
      "sha256:2d72ff4b0cfe82e22f01f15223bc4ab4c825d318cb2e3275554cd32299a5ea50" ||
    manifest.schema !== 1 ||
    manifest.architecture !== "linux-amd64"
  )
    throw Error("pinned runtime mismatch");
  for (const [name, meta] of Object.entries(manifest.files)) {
    if (!safe(name)) throw Error("unsafe runtime inventory");
    regular(path.join(base, name), meta.sha256);
  }
  regular(path.join(base, "guard.so"), state.guard_sha256);
  return { state, manifest };
}
const disabled = [
  "curl_exec",
  "curl_multi_exec",
  "fsockopen",
  "pfsockopen",
  "stream_socket_client",
  "stream_socket_server",
  "stream_socket_accept",
  "stream_socket_pair",
  "socket_create",
  "socket_create_pair",
  "socket_connect",
  "socket_bind",
  "socket_listen",
  "socket_accept",
  "socket_send",
  "socket_sendto",
  "socket_sendmsg",
  "socket_recv",
  "socket_recvfrom",
  "socket_recvmsg",
  "socket_import_stream",
  "mail",
  "exec",
  "shell_exec",
  "system",
  "passthru",
  "popen",
  "proc_open",
  "pcntl_exec",
  "pcntl_fork",
  "dl",
  "putenv",
];
function requestValid(request) {
  if (
    !request ||
    !/^[a-f0-9]{32}$/.test(request.nonce) ||
    !Array.isArray(request.files) ||
    request.files.length > 100 ||
    !Array.isArray(request.args) ||
    !request.args.length ||
    request.args.length > 3
  )
    throw Error("invalid bounded PHP request");
  if (request.args[0] === "-r") {
    if (
      request.args.length !== 2 ||
      typeof request.args[1] !== "string" ||
      request.args[1].length > 256 * 1024
    )
      throw Error("bounded inline PHP required");
  } else if (
    request.args.some(
      (arg) =>
        typeof arg !== "string" ||
        !safe(arg.slice(1)) ||
        !/^\/(?:work|reference)(?:\/[^\x00-\x20\\:]*)?$|^\/(?:runner|original|synthetic)\.php$/.test(
          arg,
        ),
    )
  )
    throw Error("exact virtual PHP paths required");
  let total = 0;
  const names = new Set();
  for (const file of request.files) {
    if (
      !safe(file.path) ||
      !(
        /^(?:work|reference)\//.test(file.path) ||
        /^(?:runner|original|synthetic)\.php$/.test(file.path)
      ) ||
      names.has(file.path) ||
      !/^([A-Za-z0-9+/]{4})*([A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$/.test(
        file.data,
      )
    )
      throw Error("unsafe case file");
    const bytes = Buffer.from(file.data, "base64");
    total += bytes.length;
    if (total > 16 * 1024 * 1024 || hash(bytes) !== file.sha256)
      throw Error("case file hash/size mismatch");
    names.add(file.path);
  }
  if (
    typeof request.input !== "string" ||
    request.input.length > 24 * 1024 * 1024
  )
    throw Error("bounded stdin required");
}
function execute(request, state, manifest, extensions) {
  requestValid(request);
  const caseBase = path.join(base, "cases", request.nonce),
    root = path.join(caseBase, "rootfs");
  fs.mkdirSync(caseBase, { mode: 0o700 });
  fs.mkdirSync(root, { mode: 0o755 });
  const add = (name, bytes, mode) => {
    if (!safe(name)) throw Error("unsafe child path");
    const target = path.join(root, name);
    fs.mkdirSync(path.dirname(target), { recursive: true, mode: 0o755 });
    fs.writeFileSync(target, bytes, { flag: "wx", mode });
  };
  try {
    for (const [name] of Object.entries(manifest.files))
      if (name.startsWith("rootfs/")) {
        const relative = name.slice(7),
          target = path.join(root, relative);
        fs.mkdirSync(path.dirname(target), { recursive: true, mode: 0o755 });
        fs.linkSync(path.join(base, name), target);
      }
    add(
      "goepay-php-guard.so",
      fs.readFileSync(path.join(base, "guard.so")),
      0o444,
    );
    add(
      "goepay-bootstrap.php",
      fs.readFileSync(path.join(base, "bootstrap.php")),
      0o444,
    );
    for (const file of request.files)
      add(file.path, Buffer.from(file.data, "base64"), 0o444);
    for (const dir of ["work", "reference", "tmp"])
      fs.mkdirSync(path.join(root, dir), { recursive: true, mode: 0o555 });
    const options = [
      "-n",
      "-d",
      "date.timezone=Asia/Shanghai",
      "-d",
      "display_errors=stderr",
      "-d",
      "log_errors=0",
      "-d",
      "memory_limit=128M",
      "-d",
      "allow_url_fopen=0",
      "-d",
      "allow_url_include=0",
      "-d",
      "ffi.enable=0",
      "-d",
      "disable_functions=" + disabled.join(","),
      "-d",
      "open_basedir=/work:/reference:/runner.php:/original.php:/synthetic.php:/goepay-bootstrap.php",
    ];
    for (const extension of extensions) {
      if (!Object.values(manifest.dynamic_extensions).includes(extension))
        throw Error("unapproved dynamic extension");
      options.push("-d", "extension=" + extension);
    }
    const child = spawnSync(
      path.join(base, "sandbox"),
      [
        root,
        "--",
        "/usr/local/bin/php",
        ...options,
        "/goepay-bootstrap.php",
        ...request.args,
      ],
      {
        input: Buffer.from(request.input, "base64"),
        encoding: null,
        maxBuffer: 4 * 1024 * 1024,
        timeout: Math.min(Math.max(request.timeout_ms || 30000, 1000), 60000),
        killSignal: "SIGKILL",
        env: {
          PATH: "/usr/local/bin:/usr/bin:/bin",
          LANG: "C.UTF-8",
          PHP_INI_SCAN_DIR: "",
          OPENSSL_CONF: "/etc/ssl/openssl.cnf",
          OPENSSL_MODULES: "/usr/lib/x86_64-linux-gnu/ossl-modules",
          LD_PRELOAD: "/goepay-php-guard.so",
          GOEPAY_PHP_GUARD_NONCE: request.nonce,
          GOEPAY_PHP_GUARD_STATE: "pending",
        },
      },
    );
    const stderr = (child.stderr ?? Buffer.alloc(0)).toString("utf8");
    const proof =
      "GOEPAY_PHP_GUARD_V1 " +
      request.nonce +
      " seccomp=2 network=denied exec=denied uid=65534\n";
    const proofCount = stderr.split(proof).length - 1;
    const rest = stderr.replace(proof, "");
    const unavailable =
      proofCount !== 1 || child.status === 125 || child.status === 126;
    const result = {
      schema: 1,
      status: unavailable ? "unavailable" : "executed",
      exit_code: child.status,
      signal: child.signal,
      error: child.error?.message,
      guard_valid: proofCount === 1,
      namespace_isolated: false,
      network_restriction: "child-seccomp-syscall-denial",
      exec_restriction: "pre-main-seccomp",
      uid: 65534,
      stdout_base64: (child.stdout ?? Buffer.alloc(0)).toString("base64"),
      stderr: rest,
      file_hashes: request.files.map(({ path, sha256 }) => ({ path, sha256 })),
    };
    fs.writeFileSync(
      path.join(caseBase, "receipt.json"),
      JSON.stringify(
        {
          ...result,
          stdout_base64: undefined,
          stdout_sha256: hash(child.stdout ?? Buffer.alloc(0)),
        },
        null,
        2,
      ) + "\n",
      { flag: "wx", mode: 0o600 },
    );
    return result;
  } finally {
    if (fs.realpathSync(root) !== root || !root.startsWith(base + "/cases/"))
      throw Error("refusing unsafe case cleanup");
    fs.rmSync(root, { recursive: true });
  }
}

try {
  const mode = process.argv[2],
    { state, manifest } = loadState(process.argv[3]);
  if (mode === "preflight") {
    const code =
      "echo json_encode(['version'=>PHP_VERSION,'extensions'=>array_combine(['openssl','bcmath','mbstring'],array_map('extension_loaded',['openssl','bcmath','mbstring'])),'functions'=>array_combine(['openssl_sign','openssl_verify','openssl_public_encrypt','bcadd','bcmul','bcpow','mb_convert_encoding'],array_map('function_exists',['openssl_sign','openssl_verify','openssl_public_encrypt','bcadd','bcmul','bcpow','mb_convert_encoding']))],JSON_THROW_ON_ERROR);";
    const probe = (extensions) =>
      execute(
        {
          nonce: crypto.randomBytes(16).toString("hex"),
          files: [],
          args: ["-r", code],
          input: "",
          timeout_ms: 10000,
        },
        state,
        manifest,
        extensions,
      );
    let result = probe([]);
    if (result.status !== "executed" || result.exit_code !== 0 || result.stderr)
      throw Error("sandbox preflight unavailable: " + result.stderr);
    let php = JSON.parse(
      Buffer.from(result.stdout_base64, "base64").toString("utf8"),
    );
    const extensions = Object.entries(php.extensions)
      .filter(([, loaded]) => !loaded)
      .map(([name]) => {
        if (!manifest.dynamic_extensions[name])
          throw Error("required PHP extension unavailable: " + name);
        return manifest.dynamic_extensions[name];
      });
    if (extensions.length) {
      result = probe(extensions);
      if (
        result.status !== "executed" ||
        result.exit_code !== 0 ||
        result.stderr
      )
        throw Error("extension preflight unavailable: " + result.stderr);
      php = JSON.parse(
        Buffer.from(result.stdout_base64, "base64").toString("utf8"),
      );
    }
    if (
      php.version !== "8.2.35" ||
      Object.values(php.extensions).some((v) => v !== true) ||
      Object.values(php.functions).some((v) => v !== true)
    )
      throw Error("required PHP functions/version unavailable");
    process.stdout.write(
      JSON.stringify({
        status: "ready",
        extensions_to_load: extensions,
        php_version: php.version,
        seccomp_network_denied: true,
        seccomp_exec_denied: true,
        uid: 65534,
        namespace_isolated: false,
      }),
    );
  } else if (mode === "run" && state.status === "ready") {
    const input = fs.readFileSync(0);
    if (input.length > 32 * 1024 * 1024) throw Error("request too large");
    process.stdout.write(
      JSON.stringify(
        execute(
          JSON.parse(input.toString("utf8")),
          state,
          manifest,
          state.extensions_to_load,
        ),
      ),
    );
  } else throw Error("prepared ready runtime required");
} catch (error) {
  process.stdout.write(
    JSON.stringify({
      schema: 1,
      status: "unavailable",
      error: String(error.message ?? error),
      namespace_isolated: false,
    }),
  );
  process.exitCode = 125;
}
