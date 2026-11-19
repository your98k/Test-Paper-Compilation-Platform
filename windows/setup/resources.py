#!/usr/bin/env python3
"""Generate an ICO and Go-linkable Windows COFF resources from the web SVG."""
import io
import shutil
import struct
import subprocess
import sys
from pathlib import Path
from PIL import Image

ROOT = Path(__file__).resolve().parent
SVG = ROOT.parent.parent / 'web-bank/icons/icon.svg'
LANG = 0x0804


def pack(fmt, *values):
    return struct.pack('<' + fmt, *values)


def align(data):
    return data + b'\0' * (-len(data) % 4)


def wide(text):
    return (text + '\0').encode('utf-16le')


def version():
    def block(key, value=b'', children=b'', text=False):
        header = align(pack('HHH', 0, len(value) // (2 if text else 1), int(text)) + wide(key))
        body = header + (align(value) if children else value) + children
        return align(pack('H', len(body)) + body[2:])

    strings = {
        'CompanyName': 'your98k', 'FileDescription': '组卷台', 'FileVersion': '1.0.0',
        'InternalName': 'K12QuestionBank', 'OriginalFilename': 'K12QuestionBank.exe',
        'ProductName': '组卷台', 'ProductVersion': '1.0.0',
    }
    table = b''.join(block(k, wide(v), text=True) for k, v in strings.items())
    sfi = block('StringFileInfo', children=block('080404B0', children=table, text=True), text=True)
    vfi = block('VarFileInfo', children=block('Translation', pack('HH', LANG, 1200)), text=True)
    fixed = pack('13I', 0xFEEF04BD, 0x10000, 0x10000, 0, 0x10000, 0,
                 0x3f, 0, 0x40004, 1, 0, 0, 0)
    return block('VS_VERSION_INFO', fixed, sfi + vfi)


def icon(out):
    converter = shutil.which('rsvg-convert')
    if converter:
        png = subprocess.check_output([converter, '-w', '256', '-h', '256', str(SVG)])
    else:
        # Alpine systems with the librsvg library but not the rsvg-convert CLI.
        import ctypes as c
        lib = c.CDLL('librsvg-2.so.2')
        lib.rsvg_handle_new_from_file.argtypes = [c.c_char_p, c.c_void_p]
        lib.rsvg_handle_new_from_file.restype = c.c_void_p
        lib.rsvg_handle_get_pixbuf.argtypes = [c.c_void_p]
        lib.rsvg_handle_get_pixbuf.restype = c.c_void_p
        pix = lib.rsvg_handle_get_pixbuf(lib.rsvg_handle_new_from_file(bytes(SVG), None))
        if not pix:
            raise SystemExit('SVG render failed')
        p = c.CDLL('libgdk_pixbuf-2.0.so.0')
        for name in ('width', 'height', 'rowstride'):
            getattr(p, 'gdk_pixbuf_get_' + name).argtypes = [c.c_void_p]
        p.gdk_pixbuf_get_pixels.argtypes = [c.c_void_p]
        p.gdk_pixbuf_get_pixels.restype = c.c_void_p
        w, h, stride = (getattr(p, 'gdk_pixbuf_get_' + k)(pix) for k in ('width', 'height', 'rowstride'))
        im = Image.frombytes('RGBA', (w, h), c.string_at(p.gdk_pixbuf_get_pixels(pix), stride * (h-1) + w*4), 'raw', 'RGBA', stride)
    if converter:
        im = Image.open(io.BytesIO(png)).convert('RGBA')
    sizes = [16, 24, 32, 48, 64, 128, 256]
    im.save(out, format='ICO', sizes=[(s, s) for s in sizes], bitmap_format='bmp')
    data = out.read_bytes()
    count = struct.unpack_from('<H', data, 4)[0]
    group = data[:6]
    images = {}
    for i in range(count):
        entry = data[6+i*16:22+i*16]
        length, offset = struct.unpack_from('<II', entry, 8)
        group += entry[:12] + pack('H', i+1)
        images[i+1] = {LANG: data[offset:offset+length]}
    return images, group


def coff(resources, machine, reloc_type):
    section = bytearray()
    leaves = []

    def directory(tree, depth):
        start = len(section)
        section.extend(pack('IIHHHH', 0, 0, 0, 0, 0, len(tree)) + bytes(8 * len(tree)))
        for i, (key, value) in enumerate(sorted(tree.items())):
            if depth < 2:
                target = 0x80000000 | directory(value, depth + 1)
            else:
                target = len(section)
                section.extend(bytes(16))
                leaves.append((target, value))
            struct.pack_into('<II', section, start+16+8*i, key, target)
        return start

    directory(resources, 0)
    relocations = b''
    for target, value in leaves:
        struct.pack_into('<II', section, target, len(section), len(value))
        section.extend(align(value))
        relocations += pack('IIH', target, 0, reloc_type)
    raw = 60
    rel = raw + len(section)
    sym = rel + len(relocations)
    header = pack('HHIIIHH', machine, 1, 0, sym, 1, 0, 0)
    header += pack('8sIIIIIIHHI', b'.rsrc', 0, 0, len(section), raw, rel, 0, len(leaves), 0, 0x40300040)
    symbol = pack('8sIhHBB', b'.rsrc', 0, 1, 0, 3, 0)
    return header + section + relocations + symbol + pack('I', 4)


if __name__ == '__main__':
    dest = Path(sys.argv[1]).resolve() if len(sys.argv) > 1 else ROOT.parent
    dest.mkdir(parents=True, exist_ok=True)
    ico = Path(sys.argv[2]).resolve() if len(sys.argv) > 2 else ROOT / 'app.ico'
    icons, group = icon(ico)
    resources = {3: icons, 14: {101: {LANG: group}}, 16: {1: {LANG: version()}},
                 24: {1: {0: (ROOT / 'app.manifest').read_bytes()}}}
    for arch, machine, reloc in [('amd64', 0x8664, 3), ('arm64', 0xaa64, 2)]:
        (dest / f'k12_resources_windows_{arch}.syso').write_bytes(coff(resources, machine, reloc))
