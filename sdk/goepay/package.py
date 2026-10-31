"""Build the portable Go SDK ZIP deterministically; no credentials or network."""
import hashlib
import json
import pathlib
import zipfile

source = pathlib.Path(__file__).resolve().parent
root = source.parents[1]
output = root / 'internal/devdoc/sdk/GoEPay_Go_SDK.zip'
receipt = root / '.tools/sdk-go/package.json'
names = ['README.md', 'client.go', 'signature.go', 'response.go',
         'client_test.go', 'example_test.go']
contents = {name: (source / name).read_bytes() for name in names}
contents['go.mod'] = b'module example.com/goepay-sdk\n\ngo 1.22\n'
manifest = [{'path': name, 'bytes': len(body),
             'sha256': hashlib.sha256(body).hexdigest()}
            for name, body in sorted(contents.items())]
contents['MANIFEST.json'] = (json.dumps(manifest, ensure_ascii=False, indent=2) + '\n').encode()
with zipfile.ZipFile(output, 'w', compression=zipfile.ZIP_DEFLATED, compresslevel=9) as archive:
    for name, body in sorted(contents.items()):
        info = zipfile.ZipInfo('GoEPay_Go_SDK/' + name, date_time=(1980, 1, 1, 0, 0, 0))
        info.create_system = 3
        info.external_attr = 0o100644 << 16
        info.compress_type = zipfile.ZIP_DEFLATED
        archive.writestr(info, body, compress_type=zipfile.ZIP_DEFLATED, compresslevel=9)
result = {'archive': str(output), 'sha256': hashlib.sha256(output.read_bytes()).hexdigest(),
          'bytes': output.stat().st_size, 'entries': len(contents), 'module': 'example.com/goepay-sdk',
          'go_version': '1.22', 'source_members': manifest,
          'excluded': ['server_compat_test.go', 'package.py'], 'network_calls': 0}
receipt.parent.mkdir(parents=True, exist_ok=True)
receipt.write_text(json.dumps(result, indent=2), encoding='utf-8')
print(json.dumps({key: result[key] for key in ('sha256', 'bytes', 'entries', 'module')}))
