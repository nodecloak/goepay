"""Host-only cached-image preparation. Never enters or creates a container."""
import argparse
import gzip
import hashlib
import io
import json
import pathlib
import posixpath
import re
import struct
import subprocess
import tarfile

IMAGE = "sha256:2d72ff4b0cfe82e22f01f15223bc4ab4c825d318cb2e3275554cd32299a5ea50"
HERE = pathlib.Path(__file__).resolve().parent
SOURCE = HERE.parent.parent
OCI_INDEX = "application/vnd.oci.image.index.v1+json"
OCI_MANIFEST = "application/vnd.oci.image.manifest.v1+json"
OCI_CONFIG = "application/vnd.oci.image.config.v1+json"
OCI_LAYER = "application/vnd.oci.image.layer.v1.tar"
OCI_GZIP_LAYER = OCI_LAYER + "+gzip"
DIGEST = re.compile(r"sha256:[a-f0-9]{64}\Z")


def safe_name(name):
    if not isinstance(name, str) or not name or name.startswith(("/", "\\")) or any(ord(c) < 32 for c in name) or "\\" in name or ":" in name:
        raise ValueError("unsafe archive path")
    parts = name.split("/")
    if ".." in parts:
        raise ValueError("archive traversal")
    result = "/".join(p for p in parts if p and p != ".")
    if not result:
        raise ValueError("empty archive path")
    return result


def virtual_name(name):
    """Image-only POSIX key, never a host/output pathname.

    Debian uses ':' in dpkg filenames. It is safe in this in-memory tree but
    remains forbidden by safe_name at every host/runtime archive boundary.
    """
    if not isinstance(name, str) or not name or name.startswith(("/", "\\")) or re.match(r"^[A-Za-z]:", name) or "\\" in name or any(ord(c) < 32 or 127 <= ord(c) <= 159 for c in name):
        raise ValueError("unsafe virtual image path")
    parts = name.split("/")
    if ".." in parts:
        raise ValueError("virtual image traversal")
    result = "/".join(part for part in parts if part and part != ".")
    if not result:
        raise ValueError("empty virtual image path")
    return result


def digest(path):
    h = hashlib.sha256()
    with open(path, "rb") as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            h.update(chunk)
    return h.hexdigest()


def check_digest(path, expected):
    if expected != "sha256:" + digest(path):
        raise ValueError("digest mismatch: " + str(path))


def image_archive_members(archive):
    # Docker save supplies ordinary files and directories. Never extract its
    # names or links onto the Windows host, and do not silently accept aliases.
    members, seen, total = {}, set(), 0
    for member in archive:
        name = safe_name(member.name)
        if name in seen or not (member.isfile() or member.isdir()) or member.size < 0:
            raise ValueError("duplicate/unsupported image archive entry")
        seen.add(name)
        if member.isfile():
            total += member.size
            if len(seen) > 10000 or total > 4 * 1024 ** 3:
                raise ValueError("image archive inventory exceeds offline preparation limit")
            members[name] = member
    return members


def strict_json(data):
    def object_pairs(pairs):
        result = {}
        for key, value in pairs:
            if key in result:
                raise ValueError("duplicate JSON metadata key")
            result[key] = value
        return result
    return json.loads(data, object_pairs_hook=object_pairs)


def archive_json(archive, members, name):
    member = members.get(name)
    if member is None or member.size > 16 * 1024 ** 2:
        raise ValueError("missing/oversized archive JSON: " + name)
    return strict_json(archive.extractfile(member).read())


def descriptor_blob(archive, members, descriptor, media_types, metadata=False):
    """Verify selected OCI content before using any descriptor-supplied data."""
    if not isinstance(descriptor, dict):
        raise ValueError("OCI descriptor object required")
    value, size = descriptor.get("digest"), descriptor.get("size")
    limit = 16 * 1024 ** 2 if metadata else 2 * 1024 ** 3
    if not isinstance(value, str) or not DIGEST.fullmatch(value) or type(size) is not int or not 0 < size <= limit or descriptor.get("mediaType") not in media_types or descriptor.get("urls") or descriptor.get("data"):
        raise ValueError("invalid OCI descriptor digest/size/media type or external content")
    name = "blobs/sha256/" + value[7:]
    member = members.get(name)
    if member is None or member.size != size:
        raise ValueError("missing OCI blob or descriptor size mismatch: " + value)
    sha = hashlib.sha256()
    content = [] if metadata else None
    with archive.extractfile(member) as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            sha.update(chunk)
            if metadata:
                content.append(chunk)
    if "sha256:" + sha.hexdigest() != value:
        raise ValueError("OCI blob digest mismatch: " + value)
    return strict_json(b"".join(content)) if metadata else name


def image_chain(archive, members, inspected, expected_image):
    """Bind the pinned multi-platform index to the one selected amd64 image."""
    if len(inspected) != 1 or inspected[0].get("Id") != expected_image or inspected[0].get("Os") != "linux" or inspected[0].get("Architecture") != "amd64":
        raise ValueError("exact cached Linux/amd64 image inspection required")
    inspected = inspected[0]
    if archive_json(archive, members, "oci-layout") != {"imageLayoutVersion": "1.0.0"}:
        raise ValueError("OCI image layout 1.0.0 required")
    wrapper = archive_json(archive, members, "index.json")
    if not isinstance(wrapper, dict) or wrapper.get("schemaVersion") != 2 or wrapper.get("mediaType") != OCI_INDEX or not isinstance(wrapper.get("manifests"), list) or len(wrapper["manifests"]) != 1:
        raise ValueError("single OCI archive root required")
    root = wrapper["manifests"][0]
    inspected_root = inspected.get("Descriptor")
    if not isinstance(root, dict) or root.get("digest") != expected_image or not isinstance(inspected_root, dict) or any(root.get(key) != inspected_root.get(key) for key in ("mediaType", "digest", "size")):
        raise ValueError("archive root does not match pinned image index descriptor")
    index = descriptor_blob(archive, members, root, {OCI_INDEX}, metadata=True)
    if not isinstance(index, dict) or index.get("schemaVersion") != 2 or index.get("mediaType") != OCI_INDEX or not isinstance(index.get("manifests"), list):
        raise ValueError("OCI image index required")
    selected = [item for item in index["manifests"] if isinstance(item, dict) and isinstance(item.get("platform"), dict) and item["platform"].get("os") == "linux" and item["platform"].get("architecture") == "amd64"]
    if len(selected) != 1 or selected[0]["platform"].get("variant") not in (None, ""):
        raise ValueError("exactly one unambiguous Linux/amd64 manifest required")
    selected = selected[0]
    manifest = descriptor_blob(archive, members, selected, {OCI_MANIFEST}, metadata=True)
    if not isinstance(manifest, dict) or manifest.get("schemaVersion") != 2 or manifest.get("mediaType") != OCI_MANIFEST or not isinstance(manifest.get("layers"), list) or not 0 < len(manifest["layers"]) <= 128:
        raise ValueError("selected OCI image manifest/layers required")
    config_descriptor = manifest.get("config")
    config = descriptor_blob(archive, members, config_descriptor, {OCI_CONFIG}, metadata=True)
    if not isinstance(config, dict) or config.get("os") != "linux" or config.get("architecture") != "amd64" or config.get("variant") not in (None, ""):
        raise ValueError("selected config platform mismatch")
    rootfs = config.get("rootfs")
    inspected_rootfs = inspected.get("RootFS")
    if not isinstance(rootfs, dict) or rootfs.get("type") != "layers" or not isinstance(rootfs.get("diff_ids"), list) or not isinstance(inspected_rootfs, dict) or inspected_rootfs.get("Type") != "layers" or rootfs["diff_ids"] != inspected_rootfs.get("Layers") or len(rootfs["diff_ids"]) != len(manifest["layers"]) or any(not isinstance(value, str) or not DIGEST.fullmatch(value) for value in rootfs["diff_ids"]):
        raise ValueError("selected config/inspection DiffID inventory mismatch")
    layers = []
    for item, diff_id in zip(manifest["layers"], rootfs["diff_ids"]):
        name = descriptor_blob(archive, members, item, {OCI_LAYER, OCI_GZIP_LAYER})
        layers.append({"name": name, "digest": item["digest"], "size": item["size"], "media_type": item["mediaType"], "diff_id": diff_id})
    # Docker's convenience manifest is not a trust anchor. Require it to agree
    # exactly with the independently verified OCI chain and its layer order.
    legacy = archive_json(archive, members, "manifest.json")
    if not isinstance(legacy, list) or len(legacy) != 1 or not isinstance(legacy[0], dict) or legacy[0].get("Config") != "blobs/sha256/" + config_descriptor["digest"][7:] or legacy[0].get("Layers") != [item["name"] for item in layers]:
        raise ValueError("Docker convenience manifest disagrees with OCI chain")
    proof = {"platform": "linux/amd64", "index_digest": root["digest"], "index_size": root["size"], "manifest_digest": selected["digest"], "manifest_size": selected["size"], "config_digest": config_descriptor["digest"], "config_size": config_descriptor["size"], "layer_digests": [item["digest"] for item in layers], "diff_ids": rootfs["diff_ids"], "layers": layers}
    return proof, layers


def unpack_layers(archive, members, layers, directory):
    paths, total = [], 0
    for i, layer in enumerate(layers):
        with archive.extractfile(members[layer["name"]]) as stream:
            data = io.BufferedReader(stream)
            compressed = layer["media_type"] == OCI_GZIP_LAYER
            if compressed != (data.peek(2)[:2] == b"\x1f\x8b"):
                raise ValueError("OCI layer encoding/media type mismatch")
            source = gzip.GzipFile(fileobj=data) if compressed else data
            target = directory / (str(i) + ".tar")
            with target.open("xb") as writer:
                size = 0
                for chunk in iter(lambda: source.read(1024 * 1024), b""):
                    size += len(chunk)
                    total += len(chunk)
                    if size > 2 * 1024 ** 3 or total > 4 * 1024 ** 3:
                        raise ValueError("uncompressed image layers exceed offline limit")
                    writer.write(chunk)
        check_digest(target, layer["diff_id"])
        paths.append(target)
    return paths


def merge_layers(paths):
    index = {}
    for path in paths:
        entries = []
        seen = set()
        with tarfile.open(path, "r:") as archive:
            for item in archive:
                if item.name in (".", "./") and item.isdir():
                    continue
                name = virtual_name(item.name)
                if name in seen or len(seen) > 1000000:
                    raise ValueError("duplicate/oversized image layer inventory")
                seen.add(name)
                entries.append((name, item))
        # Whiteouts apply to lower layers, irrespective of tar member order.
        for name, _ in entries:
            parent, base = posixpath.split(name)
            if base == ".wh..wh..opq":
                index = {k: v for k, v in index.items() if not k.startswith(parent + "/")} if parent else {}
            elif base.startswith(".wh."):
                if base == ".wh.":
                    raise ValueError("invalid empty whiteout target")
                target = posixpath.join(parent, base[4:])
                index = {k: v for k, v in index.items() if k != target and not k.startswith(target + "/")}
        for name, item in entries:
            if posixpath.basename(name).startswith(".wh."):
                continue
            if not item.isdir():
                index = {k: v for k, v in index.items() if not k.startswith(name + "/")}
            index[name] = {"layer": str(path), "offset": item.offset_data, "size": item.size, "mode": item.mode, "type": item.type, "link": item.linkname}
    return index


def image_entry(index, name):
    pending = virtual_name(name).split("/")
    resolved = []
    links = 0
    while pending:
        component = pending.pop(0)
        if component in ("", "."):
            continue
        if component == "..":
            if not resolved:
                raise ValueError("virtual image symlink escapes root")
            resolved.pop()
            continue
        candidate = "/".join(resolved + [component])
        entry = index.get(candidate)
        if entry and entry["type"] in (tarfile.SYMTYPE, tarfile.LNKTYPE):
            links += 1
            if links > 40:
                raise ValueError("virtual image symlink cycle")
            target = entry["link"]
            if not target or "\\" in target or re.match(r"^[A-Za-z]:", target) or any(ord(c) < 32 or 127 <= ord(c) <= 159 for c in target):
                raise ValueError("invalid virtual image link")
            if target.startswith("/") or entry["type"] == tarfile.LNKTYPE:
                resolved = []
            pending = target.split("/") + pending
        else:
            resolved.append(component)
    actual = "/".join(resolved)
    if actual not in index:
        raise FileNotFoundError(actual)
    return actual, index[actual]


def image_bytes(index, name):
    _, entry = image_entry(index, name)
    if entry["type"] not in (tarfile.REGTYPE, tarfile.AREGTYPE) or entry["size"] > 128 * 1024 * 1024:
        raise ValueError("selected image file is not bounded regular content")
    with open(entry["layer"], "rb") as stream:
        stream.seek(entry["offset"])
        result = stream.read(entry["size"])
    if len(result) != entry["size"]:
        raise ValueError("truncated layer content")
    return result


def elf_info(data):
    if len(data) < 64 or data[:6] != b"\x7fELF\x02\x01" or struct.unpack_from("<H", data, 18)[0] != 62:
        raise ValueError("Linux amd64 ELF required")
    offset = struct.unpack_from("<Q", data, 32)[0]
    size, count = struct.unpack_from("<HH", data, 54)
    if size < 56 or count > 256 or offset + size * count > len(data):
        raise ValueError("invalid ELF program headers")
    loads, dynamic, interpreter = [], None, None
    for i in range(count):
        kind, _, file_at, virtual, _, file_size, _, _ = struct.unpack_from("<IIQQQQQQ", data, offset + i * size)
        if file_at + file_size > len(data):
            raise ValueError("invalid ELF segment")
        if kind == 1:
            loads.append((virtual, file_at, file_size))
        elif kind == 2:
            dynamic = (file_at, file_size)
        elif kind == 3:
            interpreter = data[file_at:file_at+file_size].rstrip(b"\0").decode("ascii")
    needed, paths, strings = [], [], None
    if dynamic:
        entries = []
        for at in range(dynamic[0], dynamic[0] + dynamic[1], 16):
            tag, value = struct.unpack_from("<QQ", data, at)
            if tag == 0:
                break
            entries.append((tag, value))
        for tag, value in entries:
            if tag == 5:
                for virtual, file_at, length in loads:
                    if virtual <= value < virtual + length:
                        strings = file_at + value - virtual
        if strings is None and entries:
            raise ValueError("ELF dynamic string table missing")
        for tag, value in entries:
            if tag in (1, 15, 29):
                end = data.find(b"\0", strings + value)
                if end < 0:
                    raise ValueError("ELF string not terminated")
                text = data[strings+value:end].decode("ascii")
                (needed if tag == 1 else paths).append(text)
    return {"interpreter": interpreter, "needed": needed, "search": paths}


def references(root):
    root = pathlib.Path(root).resolve(strict=True)
    expected = json.loads((HERE / "references.json").read_text("utf8"))
    if len(expected) != 15:
        raise ValueError("exact reference catalog required")
    for name, wanted in expected.items():
        target = root / safe_name(name)
        actual = target.resolve(strict=True)
        if not actual.is_relative_to(root) or target.is_symlink() or not actual.is_file() or digest(actual) != wanted:
            raise ValueError("original reference changed: " + name)
    return expected


def runtime_files(index):
    selected, visited = {}, set()
    standard = ["lib/x86_64-linux-gnu", "usr/lib/x86_64-linux-gnu", "usr/local/lib", "lib64", "usr/lib64", "lib", "usr/lib"]
    dynamic_extensions = {}
    def include(name, execute=False):
        name = safe_name(name)
        if name in visited:
            return
        visited.add(name)
        data = image_bytes(index, name)
        selected["rootfs/"+name] = (data, 0o555 if execute else 0o444)
        if not data.startswith(b"\x7fELF"):
            return
        info = elf_info(data)
        if info["interpreter"]:
            include(info["interpreter"].lstrip("/"), True)
        search = []
        for value in info["search"]:
            for component in value.split(":"):
                expanded = component.replace("${ORIGIN}", "/"+posixpath.dirname(name)).replace("$ORIGIN", "/"+posixpath.dirname(name))
                if not expanded.startswith("/") or "$" in expanded:
                    raise ValueError("unsupported ELF search path")
                search.append(safe_name(posixpath.normpath(expanded).lstrip("/")))
        for library in info["needed"]:
            if "/" in library or "\\" in library:
                raise ValueError("unexpected ELF dependency name")
            found = False
            for directory in search + standard:
                candidate = directory + "/" + library
                try:
                    image_entry(index, candidate)
                except FileNotFoundError:
                    continue
                include(candidate)
                found = True
                break
            if not found:
                raise ValueError("missing image dependency: " + library)
    include("usr/local/bin/php", True)
    for name in sorted(index):
        match = re.fullmatch(r"usr/local/lib/php/extensions/[^/]+/(openssl|bcmath|mbstring)\.so", name)
        if match:
            if match[1] in dynamic_extensions:
                raise ValueError("ambiguous PHP extension ABI")
            dynamic_extensions[match[1]] = "/"+name
            include(name)
        if name.startswith("etc/ssl/") or "/ossl-modules/" in name:
            if index[name]["type"] != tarfile.DIRTYPE:
                try:
                    include(name)
                except FileNotFoundError:
                    # Public CA aliases may point to packages outside the image;
                    # they are not used by the selected signing-only functions.
                    if not name.startswith("etc/ssl/certs/"):
                        raise
    return selected, dynamic_extensions


def prepare(args):
    workspace = pathlib.Path(args.workspace).resolve(strict=True)
    output = pathlib.Path(args.output).absolute()
    parent = output.parent.resolve(strict=True)
    if parent != (workspace / ".tools").resolve(strict=True) or not re.fullmatch(r"phpinterop-prepared-[a-f0-9]{32}", output.name) or output.exists():
        raise ValueError("new owned workspace/.tools/phpinterop-prepared-UUID required")
    refs = references(args.reference_root)
    sandbox = pathlib.Path(args.sandbox).resolve(strict=True)
    if digest(sandbox) != args.sandbox_sha256:
        raise ValueError("sandbox binary digest mismatch")
    binary_info = elf_info(sandbox.read_bytes())
    if binary_info["interpreter"] or binary_info["needed"]:
        raise ValueError("CGO_ENABLED=0 static sandbox required")
    inspected = json.loads(subprocess.check_output(["docker", "image", "inspect", IMAGE], text=True))
    if len(inspected) != 1 or inspected[0]["Id"] != IMAGE or inspected[0]["Os"] != "linux" or inspected[0]["Architecture"] != "amd64":
        raise ValueError("exact cached PHP image required; no pull is performed")
    output.mkdir(mode=0o700)
    (output/"image-inspect.json").write_text(json.dumps(inspected, indent=2)+"\n", "utf8")
    image = output/"image.tar"
    subprocess.run(["docker", "image", "save", "--output", str(image), IMAGE], check=True)
    layers_dir = output/"layers"
    layers_dir.mkdir()
    with tarfile.open(image, "r:") as archive:
        members = image_archive_members(archive)
        chain, layers = image_chain(archive, members, inspected, IMAGE)
        paths = unpack_layers(archive, members, layers, layers_dir)
    selected, extensions = runtime_files(merge_layers(paths))
    policy = json.loads((SOURCE/"internal/testphp/sandbox/policy.json").read_text("utf8"))
    denied = policy["denied"]
    if policy["schema"] != 1 or policy["architecture"] != "linux-amd64" or len(denied) != len(set(denied.values())) or len(denied) < 35 or any(not isinstance(n, int) or n <= 0 for n in denied.values()):
        raise ValueError("invalid syscall policy")
    numbers = sorted(denied.values())
    header = "#define GOEPAY_POLICY_COUNT "+str(len(numbers))+"\nstatic const unsigned goepay_denied[]={"+",".join(map(str,numbers))+"};\n"
    selected["policy.h"] = (header.encode(), 0o444)
    selected["policy.json"] = ((SOURCE/"internal/testphp/sandbox/policy.json").read_bytes(),0o444)
    for name in ["guard.c", "bootstrap.php", "executor.mjs", "install.mjs"]:
        selected[name] = ((HERE/name).read_bytes(),0o444)
    selected["sandbox"] = (sandbox.read_bytes(),0o555)
    inventory = {name:{"sha256":hashlib.sha256(data).hexdigest(),"size":len(data),"mode":mode} for name,(data,mode) in selected.items()}
    runtime_manifest = {"schema":1,"image":IMAGE,"image_chain":chain,"architecture":"linux-amd64","runtime_id":output.name.removeprefix("phpinterop-prepared-"),"dynamic_extensions":extensions,"references":refs,"files":inventory,"namespace_isolated":False,"required_child_guards":["uid65534","seccomp-network","seccomp-exec","php-ini"],"preflight_executed":False}
    raw_manifest = (json.dumps(runtime_manifest, sort_keys=True, separators=(",", ":"))+"\n").encode()
    (output/"runtime.manifest.json").write_bytes(raw_manifest)
    with tarfile.open(output/"runtime.tar", "w", format=tarfile.USTAR_FORMAT) as archive:
        all_files = {**selected,"runtime.manifest.json":(raw_manifest,0o444)}
        for name,(data,mode) in sorted(all_files.items()):
            item=tarfile.TarInfo(safe_name(name));item.size=len(data);item.mode=mode;item.uid=0;item.gid=0;item.mtime=0
            archive.addfile(item,io.BytesIO(data))
    if references(args.reference_root) != refs:
        raise ValueError("references changed during preparation")
    receipt={"schema":1,"status":"prepared-not-executed","image":IMAGE,"image_chain":chain,"image_archive_sha256":digest(image),"runtime_tar_sha256":digest(output/"runtime.tar"),"runtime_manifest_sha256":hashlib.sha256(raw_manifest).hexdigest(),"sandbox_sha256":digest(sandbox),"runtime_id":runtime_manifest["runtime_id"],"references":refs,"containers_created":0,"runner_entered":False,"php_executed":False,"namespace_isolated":False}
    (output/"PREPARED.json").write_text(json.dumps(receipt,indent=2)+"\n","utf8")
    print(json.dumps(receipt,indent=2))


if __name__ == "__main__":
    parser=argparse.ArgumentParser()
    for option in ["workspace","output","reference-root","sandbox","sandbox-sha256"]:
        parser.add_argument("--"+option,required=True)
    prepare(parser.parse_args())
