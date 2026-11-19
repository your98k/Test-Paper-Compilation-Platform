#!/bin/sh
set -eu
if [ "$#" -ne 2 ] && { [ "$#" -ne 3 ] || [ "$3" != --x64 ]; }; then
    echo 'Usage: sh setup/package.sh PAYLOAD_ROOT OUTPUT_DIR [--x64]' >&2
    exit 2
fi
mode=${3-}
setup=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
payload=$(CDPATH='' cd -- "$1" && pwd)
mkdir -p -- "$2"
out=$(CDPATH='' cd -- "$2" && pwd)
python3 - "$payload" "$mode" <<'PY'
import struct, sys
from pathlib import Path
arches = [('amd64', 0x8664)] if sys.argv[2] == '--x64' else [('amd64', 0x8664), ('arm64', 0xaa64)]
for arch, expected in arches:
    path = Path(sys.argv[1]) / arch / 'K12QuestionBank.exe'
    with path.open('rb') as f:
        mz = f.read(64)
        if mz[:2] != b'MZ' or len(mz) != 64:
            sys.exit(f'{path}: not a PE')
        f.seek(struct.unpack_from('<I', mz, 60)[0])
        header = f.read(96)
        if (len(header) != 96 or header[:4] != b'PE\0\0'
                or struct.unpack_from('<H', header, 4)[0] != expected
                or struct.unpack_from('<H', header, 24)[0] != 0x20b
                or struct.unpack_from('<H', header, 92)[0] != 2):
            sys.exit(f'{path}: not a {arch} PE32+ Windows GUI executable')
PY
set -- "-DAMD64_PAYLOAD=$payload/amd64/K12QuestionBank.exe"
if [ "$mode" != --x64 ]; then
    set -- "$@" "-DARM64_PAYLOAD=$payload/arm64/K12QuestionBank.exe"
fi
"${MAKENSIS:-makensis}" -INPUTCHARSET UTF8 -NOCD -WX \
    "$@" \
    "-DAPP_ICON=$setup/app.ico" \
    "-DOUTPUT=$out/K12QuestionBank-Setup.exe" \
    "$setup/installer.nsi"
