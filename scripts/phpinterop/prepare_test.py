"""Pure fixture tests: no Docker, PHP, process execution or network."""
import hashlib
import gzip
import copy
import importlib.util
import io
import json
import pathlib
import shutil
import struct
import tarfile
import tempfile
import types
import unittest
from contextlib import redirect_stdout
from unittest import mock

spec = importlib.util.spec_from_file_location("prepare_php", pathlib.Path(__file__).with_name("prepare.py"))
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)


def layer(path, entries):
    with tarfile.open(path, "w") as archive:
        for name, value in entries:
            member = tarfile.TarInfo(name)
            if isinstance(value, tuple):
                member.type = tarfile.SYMTYPE
                member.linkname = value[0]
                archive.addfile(member)
            else:
                member.size = len(value)
                archive.addfile(member, io.BytesIO(value))


def oci_fixture(change=None, count=1):
    """Small complete OCI/Docker-save archive, with distinct index/config IDs."""
    files = {}
    def alter(stage, value):
        if change:
            change(stage, value)
        return value
    def raw(value):
        return json.dumps(value, sort_keys=True, separators=(",", ":")).encode()
    def hashed(data):
        return "sha256:" + hashlib.sha256(data).hexdigest()
    def blob(data, media, **extra):
        value = hashed(data)
        files["blobs/sha256/" + value[7:]] = data
        return {"mediaType": media, "digest": value, "size": len(data), **extra}
    descriptors, diff_ids = [], []
    for number in range(count):
        layer_bytes = io.BytesIO()
        with tarfile.open(fileobj=layer_bytes, mode="w") as archive:
            member = tarfile.TarInfo("fixture/original-" + str(number) + ".txt")
            content = b"untouched fixture bytes " + str(number).encode()
            member.size = len(content)
            archive.addfile(member, io.BytesIO(content))
        uncompressed = layer_bytes.getvalue()
        descriptors.append(alter("layer_descriptor", blob(gzip.compress(uncompressed, mtime=0), "application/vnd.oci.image.layer.v1.tar+gzip")))
        diff_ids.append(hashed(uncompressed))
    config = alter("config", {"architecture": "amd64", "os": "linux", "rootfs": {"type": "layers", "diff_ids": diff_ids}})
    config_descriptor = blob(raw(config), "application/vnd.oci.image.config.v1+json")
    manifest = alter("manifest", {"schemaVersion": 2, "mediaType": "application/vnd.oci.image.manifest.v1+json", "config": config_descriptor, "layers": descriptors})
    manifest_descriptor = alter("manifest_descriptor", blob(raw(manifest), manifest["mediaType"], platform={"architecture": "amd64", "os": "linux"}))
    # A Docker partial save may omit other architecture/attestation blobs. Only
    # the one selected platform's content is required, with no network fallback.
    other = {"digest": "sha256:" + "b" * 64, "size": 123, "mediaType": manifest["mediaType"], "platform": {"architecture": "arm64", "os": "linux"}}
    index = alter("index", {"schemaVersion": 2, "mediaType": "application/vnd.oci.image.index.v1+json", "manifests": [manifest_descriptor, other]})
    index_descriptor = blob(raw(index), index["mediaType"])
    files["oci-layout"] = raw({"imageLayoutVersion": "1.0.0"})
    files["index.json"] = raw(alter("wrapper", {"schemaVersion": 2, "mediaType": index["mediaType"], "manifests": [copy.deepcopy(index_descriptor)]}))
    files["manifest.json"] = raw(alter("legacy", [{"Config": "blobs/sha256/" + config_descriptor["digest"][7:], "RepoTags": None, "Layers": ["blobs/sha256/" + item["digest"][7:] for item in descriptors]}]))
    inspected = alter("inspection", [{"Id": index_descriptor["digest"], "Os": "linux", "Architecture": "amd64", "Descriptor": copy.deepcopy(index_descriptor), "RootFS": {"Type": "layers", "Layers": copy.deepcopy(config["rootfs"]["diff_ids"])}}])
    return {"files": files, "inspect": inspected, "index": index_descriptor, "manifest": manifest_descriptor, "config": config_descriptor, "layers": descriptors, "diff_ids": config["rootfs"]["diff_ids"]}


class OCIArchiveChain(unittest.TestCase):
    def run_preparation(self, fixture, directory, selected_files=None):
        directory = pathlib.Path(directory)
        tools = directory / ".tools"
        tools.mkdir()
        fixture_archive = directory / "fixture-image.tar"
        layer(fixture_archive, list(fixture["files"].items()))
        sandbox = tools / "fixture-static-sandbox"
        header = bytearray(64)
        header[:6] = b"\x7fELF\x02\x01"
        struct.pack_into("<H", header, 18, 62)
        struct.pack_into("<Q", header, 32, 64)
        struct.pack_into("<HH", header, 54, 56, 0)
        sandbox.write_bytes(header)
        output = tools / ("phpinterop-prepared-" + "a" * 32)
        args = types.SimpleNamespace(workspace=str(directory), output=str(output), reference_root="fixture-only", sandbox=str(sandbox), sandbox_sha256=hashlib.sha256(header).hexdigest())
        def save(command, **kwargs):
            self.assertEqual(command, ["docker", "image", "save", "--output", str(output / "image.tar"), fixture["inspect"][0]["Id"]])
            self.assertEqual(kwargs, {"check": True})
            shutil.copyfile(fixture_archive, command[4])
        if selected_files is None:
            selected_files = {"rootfs/usr/local/bin/php": (b"fixture-only-not-executable", 0o555)}
        with mock.patch.object(module, "IMAGE", fixture["inspect"][0]["Id"]), mock.patch.object(module, "references", return_value={"fixture.php": "0" * 64}), mock.patch.object(module.subprocess, "check_output", return_value=json.dumps(fixture["inspect"])), mock.patch.object(module.subprocess, "run", side_effect=save), mock.patch.object(module, "runtime_files", return_value=(selected_files, {})), redirect_stdout(io.StringIO()):
            module.prepare(args)
        return output

    def test_real_oci_index_to_config_chain_is_not_config_equals_image_id(self):
        fixture = oci_fixture()
        self.assertNotEqual(fixture["index"]["digest"], fixture["config"]["digest"])
        with tempfile.TemporaryDirectory() as directory:
            output = self.run_preparation(fixture, directory)
            receipt = json.loads((output / "PREPARED.json").read_text())
            chain = receipt["image_chain"]
            self.assertEqual(chain["index_digest"], fixture["index"]["digest"])
            self.assertEqual(chain["manifest_digest"], fixture["manifest"]["digest"])
            self.assertEqual(chain["config_digest"], fixture["config"]["digest"])
            self.assertEqual(chain["layer_digests"], [item["digest"] for item in fixture["layers"]])
            self.assertEqual(chain["diff_ids"], fixture["diff_ids"])
            self.assertEqual(receipt["status"], "prepared-not-executed")
            self.assertFalse(receipt["runner_entered"])
            self.assertEqual(receipt["containers_created"], 0)

    def reject(self, fixture, reason):
        with tempfile.TemporaryDirectory() as directory, self.assertRaisesRegex(ValueError, reason):
            self.run_preparation(fixture, directory)

    def test_every_selected_blob_is_digest_bound_even_when_size_is_unchanged(self):
        for selected in ["index", "manifest", "config", "layer"]:
            with self.subTest(selected=selected):
                fixture = oci_fixture()
                descriptor = fixture["layers"][0] if selected == "layer" else fixture[selected]
                name = "blobs/sha256/" + descriptor["digest"][7:]
                data = bytearray(fixture["files"][name])
                data[-1] ^= 1
                fixture["files"][name] = bytes(data)
                self.reject(fixture, "OCI blob digest mismatch")

    def test_missing_selected_blob_does_not_use_legacy_manifest_or_external_io(self):
        fixture = oci_fixture()
        fixture["files"].pop("blobs/sha256/" + fixture["manifest"]["digest"][7:])
        self.reject(fixture, "missing OCI blob")

    def test_platform_descriptor_and_inventory_tampering_is_rejected(self):
        cases = [
            ("wrapper", lambda value: value["manifests"][0].update(digest="sha256:" + "f" * 64), "pinned image index descriptor"),
            ("inspection", lambda value: value[0]["Descriptor"].update(size=1), "pinned image index descriptor"),
            ("index", lambda value: value["manifests"][0]["platform"].update(architecture="arm64"), "unambiguous Linux/amd64"),
            ("index", lambda value: value["manifests"].append(copy.deepcopy(value["manifests"][0])), "unambiguous Linux/amd64"),
            ("manifest_descriptor", lambda value: value.update(mediaType="application/vnd.oci.image.index.v1+json"), "invalid OCI descriptor"),
            ("manifest_descriptor", lambda value: value.update(urls=["https://unused.invalid/blob"]), "external content"),
            ("layer_descriptor", lambda value: value.update(size=value["size"] + 1), "descriptor size mismatch"),
            ("layer_descriptor", lambda value: value.update(mediaType="application/vnd.oci.image.layer.v1.tar"), "encoding/media type mismatch"),
            ("manifest", lambda value: value["config"].update(mediaType="application/vnd.oci.image.index.v1+json"), "invalid OCI descriptor"),
            ("config", lambda value: value.update(architecture="arm64"), "config platform mismatch"),
            ("inspection", lambda value: value[0]["RootFS"].update(Layers=["sha256:" + "f" * 64]), "DiffID inventory mismatch"),
            ("legacy", lambda value: value[0].update(Config="unverified-config.json"), "convenience manifest disagrees"),
        ]
        for stage, mutate, reason in cases:
            with self.subTest(stage=stage, reason=reason):
                fixture = oci_fixture(lambda current, value: mutate(value) if current == stage else None)
                self.reject(fixture, reason)

    def test_uncompressed_diffid_is_checked_in_addition_to_compressed_digest(self):
        def change(stage, value):
            if stage == "config":
                value["rootfs"]["diff_ids"] = ["sha256:" + "f" * 64]
        self.reject(oci_fixture(change), "digest mismatch")

    def test_convenience_manifest_cannot_reorder_verified_layers(self):
        def change(stage, value):
            if stage == "legacy":
                value[0]["Layers"].reverse()
        self.reject(oci_fixture(change, count=2), "convenience manifest disagrees")

    def test_colon_path_cannot_be_exported_even_if_runtime_selection_is_bypassed(self):
        with tempfile.TemporaryDirectory() as directory:
            with self.assertRaisesRegex(ValueError, "unsafe archive path"):
                self.run_preparation(oci_fixture(), directory, {"rootfs/var/lib/dpkg/info/libc6:amd64.list": (b"virtual-only", 0o444)})
            output = pathlib.Path(directory) / ".tools" / ("phpinterop-prepared-" + "a" * 32)
            with tarfile.open(output / "runtime.tar", "r:") as archive:
                self.assertTrue(all(":" not in member.name for member in archive))


class PreparationSafety(unittest.TestCase):
    def test_only_root_directory_metadata_is_ignored(self):
        for name in [".", "./"]:
            with self.subTest(name=name), tempfile.TemporaryDirectory() as directory:
                path = pathlib.Path(directory) / "layer.tar"
                with tarfile.open(path, "w") as archive:
                    member = tarfile.TarInfo(name)
                    member.type = tarfile.DIRTYPE
                    archive.addfile(member)
                self.assertEqual(module.merge_layers([path]), {})

    def test_root_non_directory_entries_must_not_be_silently_ignored(self):
        for name in [".", "./"]:
            for kind in [tarfile.REGTYPE, tarfile.SYMTYPE, tarfile.LNKTYPE, tarfile.CHRTYPE]:
                with self.subTest(name=name, kind=kind), tempfile.TemporaryDirectory() as directory:
                    path = pathlib.Path(directory) / "layer.tar"
                    with tarfile.open(path, "w") as archive:
                        member = tarfile.TarInfo(name)
                        member.type = kind
                        member.linkname = "outside"
                        archive.addfile(member)
                    with self.assertRaises(ValueError):
                        module.merge_layers([path])

    def test_legal_linux_colon_metadata_stays_virtual_and_is_not_selected(self):
        with tempfile.TemporaryDirectory() as directory:
            path = pathlib.Path(directory) / "layer.tar"
            header = bytearray(64)
            header[:6] = b"\x7fELF\x02\x01"
            struct.pack_into("<H", header, 18, 62)
            struct.pack_into("<Q", header, 32, 64)
            struct.pack_into("<HH", header, 54, 56, 0)
            name = "var/lib/dpkg/info/libc6:amd64.list"
            layer(path, [("usr/local/bin/php", bytes(header)), (name, b"Linux metadata only")])
            index = module.merge_layers([path])
            self.assertEqual(module.image_bytes(index, name), b"Linux metadata only")
            selected, _ = module.runtime_files(index)
            self.assertEqual(set(selected), {"rootfs/usr/local/bin/php"})
            self.assertFalse(any(":" in name for name in selected))
            with self.assertRaises(ValueError):
                module.safe_name(name)

    def test_virtual_layer_paths_still_reject_escape_and_windows_names(self):
        for name in ["/absolute", "../escape", "a/../escape", "C:/escape", "C:escape", "a\\escape", "a\x01b", "a\x7fb", "a\x85b"]:
            with self.subTest(name=name), tempfile.TemporaryDirectory() as directory:
                path = pathlib.Path(directory) / "layer.tar"
                layer(path, [(name, b"unsafe")])
                with self.assertRaises(ValueError):
                    module.merge_layers([path])

    def test_colon_symlink_targets_remain_virtual_and_cannot_escape(self):
        with tempfile.TemporaryDirectory() as directory:
            path = pathlib.Path(directory) / "layer.tar"
            layer(path, [("metadata/pkg:amd64", b"virtual bytes"), ("virtual/alias", ("../metadata/pkg:amd64",)), ("virtual/escape", ("../../host",)), ("virtual/drive", ("C:/host",))])
            index = module.merge_layers([path])
            self.assertEqual(module.image_bytes(index, "virtual/alias"), b"virtual bytes")
            for name in ["virtual/escape", "virtual/drive"]:
                with self.subTest(name=name), self.assertRaises(ValueError):
                    module.image_bytes(index, name)

    def test_colon_in_a_selected_runtime_path_is_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            path = pathlib.Path(directory) / "layer.tar"
            header = bytearray(64)
            header[:6] = b"\x7fELF\x02\x01"
            struct.pack_into("<H", header, 18, 62)
            struct.pack_into("<Q", header, 32, 64)
            struct.pack_into("<HH", header, 54, 56, 0)
            layer(path, [("usr/local/bin/php", bytes(header)), ("etc/ssl/certs/not-for-export:amd64", b"blocked")])
            index = module.merge_layers([path])
            with self.assertRaisesRegex(ValueError, "unsafe archive path"):
                module.runtime_files(index)

    def test_rejects_escape_and_windows_paths(self):
        for bad in ["/etc/passwd", "../escape", "a/../escape", "C:/escape", "a\\escape", "a\x00b"]:
            with self.subTest(bad=bad), self.assertRaises(ValueError):
                module.safe_name(bad)

    def test_whiteout_and_opaque_layers_do_not_preserve_deleted_files(self):
        with tempfile.TemporaryDirectory() as directory:
            a, b = pathlib.Path(directory)/"a.tar", pathlib.Path(directory)/"b.tar"
            layer(a, [("usr/lib/old", b"old"), ("usr/lib/keep", b"keep"), ("etc/removed", b"gone")])
            layer(b, [("usr/lib/.wh..wh..opq", b""), ("usr/lib/new", b"new"), ("etc/.wh.removed", b"")])
            index = module.merge_layers([a, b])
            self.assertNotIn("usr/lib/old", index)
            self.assertNotIn("usr/lib/keep", index)
            self.assertNotIn("etc/removed", index)
            self.assertEqual(module.image_bytes(index, "usr/lib/new"), b"new")

    def test_absolute_image_symlink_is_resolved_virtually_not_on_host(self):
        with tempfile.TemporaryDirectory() as directory:
            p = pathlib.Path(directory)/"layer.tar"
            layer(p, [("usr/lib/real", b"image bytes"), ("lib/alias", ("/usr/lib/real",))])
            index = module.merge_layers([p])
            self.assertEqual(module.image_bytes(index, "lib/alias"), b"image bytes")

    def test_root_opaque_whiteout_keeps_only_current_layer(self):
        with tempfile.TemporaryDirectory() as directory:
            a, b = pathlib.Path(directory)/"a.tar", pathlib.Path(directory)/"b.tar"
            layer(a, [("old", b"old")])
            layer(b, [("new", b"new"), (".wh..wh..opq", b"")])
            index = module.merge_layers([a, b])
            self.assertNotIn("old", index)
            self.assertEqual(module.image_bytes(index, "new"), b"new")

    def test_symlink_cycles_and_root_escape_are_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            p = pathlib.Path(directory)/"layer.tar"
            layer(p, [("a", ("b",)), ("b", ("a",)), ("escape", ("../../host",))])
            index = module.merge_layers([p])
            for bad in ["a", "escape"]:
                with self.subTest(bad=bad), self.assertRaises(ValueError):
                    module.image_bytes(index, bad)

    def test_layer_digest_mismatch_rejects_before_merge(self):
        with tempfile.TemporaryDirectory() as directory:
            p = pathlib.Path(directory)/"layer.tar"
            layer(p, [("x", b"x")])
            with self.assertRaises(ValueError):
                module.check_digest(p, "sha256:"+"0"*64)
            module.check_digest(p, "sha256:"+hashlib.sha256(p.read_bytes()).hexdigest())

    def test_duplicate_archive_members_and_links_are_not_extracted(self):
        with tempfile.TemporaryDirectory() as directory:
            p = pathlib.Path(directory)/"archive.tar"
            for entries in [[("manifest.json", b"a"), ("./manifest.json", b"b")], [("alias", ("/host",))]]:
                layer(p, entries)
                with tarfile.open(p, "r:") as archive, self.assertRaises(ValueError):
                    module.image_archive_members(archive)

    def test_duplicate_layer_names_are_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            p = pathlib.Path(directory)/"layer.tar"
            layer(p, [("x", b"a"), ("./x", b"b")])
            with self.assertRaises(ValueError):
                module.merge_layers([p])

    def test_dependencies_require_a_real_amd64_elf(self):
        with self.assertRaises(ValueError):
            module.elf_info(b"not an ELF")


if __name__ == "__main__":
    unittest.main()
