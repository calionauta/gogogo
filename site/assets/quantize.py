#!/usr/bin/env python3
"""Re-encode logo.png to an indexed-colour PNG (palette), which is what a flat
logo with large transparent areas actually needs.

stdlib only (zlib + struct) — no Pillow, no pngquant. Strategy:
  1. Decode the source PNG (handles filter types 0-4).
  2. Collect unique RGBA colours; if <= 256, write a palette PNG
     (colour type 3) with a tRNS chunk for the transparent entry.
     A flat logo quantises to a handful of colours, so this is lossless
     in practice and typically 10-30x smaller.
  3. If more than 256 colours, fall back to a fixed 6x6x6 web-safe palette
     with ordered (Bayer 4x4) dithering, so the output is still bounded.

Usage: python3 site/assets/quantize.py <src.png> <dst.png>
"""
import struct
import sys
import zlib


def decode_png(path):
    """Return (width, height, rgba_bytes) for a non-interlaced 8-bit PNG."""
    data = open(path, "rb").read()
    assert data[:8] == b"\x89PNG\r\n\x1a\n", "not a PNG"
    pos = 8
    idat = b""
    width = height = depth = color = None
    while pos < len(data):
        (length,) = struct.unpack(">I", data[pos : pos + 4])
        ctype = data[pos + 4 : pos + 8]
        body = data[pos + 8 : pos + 8 + length]
        if ctype == b"IHDR":
            width, height, depth, color, comp, filt, interlace = struct.unpack(
                ">IIBBBBB", body
            )
            assert depth == 8, f"only 8-bit supported, got {depth}"
            assert interlace == 0, "interlaced PNG not supported"
            assert color in (2, 6), f"expected RGB/RGBA, got colour type {color}"
        elif ctype == b"IDAT":
            idat += body
        elif ctype == b"IEND":
            break
        pos += 12 + length

    channels = 4 if color == 6 else 3
    bpp = channels
    stride = width * bpp
    raw = zlib.decompress(idat)
    out = bytearray(stride * height)
    prev = bytearray(stride)
    i = 0
    for y in range(height):
        f = raw[i]
        i += 1
        line = bytearray(raw[i : i + stride])
        i += stride
        if f == 1:
            for x in range(bpp, stride):
                line[x] = (line[x] + line[x - bpp]) & 255
        elif f == 2:
            for x in range(stride):
                line[x] = (line[x] + prev[x]) & 255
        elif f == 3:
            for x in range(stride):
                a = line[x - bpp] if x >= bpp else 0
                line[x] = (line[x] + ((a + prev[x]) >> 1)) & 255
        elif f == 4:
            for x in range(stride):
                a = line[x - bpp] if x >= bpp else 0
                b = prev[x]
                c = prev[x - bpp] if x >= bpp else 0
                p = a + b - c
                pa, pb, pc = abs(p - a), abs(p - b), abs(p - c)
                pred = a if (pa <= pb and pa <= pc) else (b if pb <= pc else c)
                line[x] = (line[x] + pred) & 255
        out[y * stride : (y + 1) * stride] = line
        prev = line

    if channels == 3:  # expand RGB -> RGBA
        rgba = bytearray(width * height * 4)
        for p in range(width * height):
            rgba[p * 4 : p * 4 + 3] = out[p * 3 : p * 3 + 3]
            rgba[p * 4 + 3] = 255
        out = rgba
    return width, height, bytes(out)


BAYER = [
    [0, 8, 2, 10],
    [12, 4, 14, 6],
    [3, 11, 1, 9],
    [15, 7, 13, 5],
]


def chunk(ctype, body):
    return (
        struct.pack(">I", len(body))
        + ctype
        + body
        + struct.pack(">I", zlib.crc32(ctype + body) & 0xFFFFFFFF)
    )


def encode_png(path, width, height, indices, palette, trns):
    """Write a colour-type-3 (indexed) PNG."""
    raw = bytearray()
    for y in range(height):
        raw.append(0)  # filter: None
        raw += indices[y * width : (y + 1) * width]
    plte = bytearray()
    for r, g, b in palette:
        plte += bytes((r, g, b))
    body = struct.pack(">IIBBBBB", width, height, 8, 3, 0, 0, 0)
    out = b"\x89PNG\r\n\x1a\n"
    out += chunk(b"IHDR", body)
    out += chunk(b"PLTE", bytes(plte))
    if trns:
        out += chunk(b"tRNS", bytes(trns))
    out += chunk(b"IDAT", zlib.compress(bytes(raw), 9))
    out += chunk(b"IEND", b"")
    open(path, "wb").write(out)


def build_exact_palette(rgba, npix):
    """Exact palette when <= 256 unique colours (the normal case for a logo)."""
    seen = {}
    for p in range(npix):
        px = rgba[p * 4 : p * 4 + 4]
        if px not in seen:
            seen[px] = len(seen)
            if len(seen) > 256:
                return None
    palette = [tuple(k) for k in seen]
    order = {k: v for v, k in enumerate(palette)}
    indices = bytearray(order[rgba[p * 4 : p * 4 + 4]] for p in range(npix))
    return indices, palette


def build_dither_palette(rgba, npix, width):
    """Fallback: 216-colour cube + greys, Bayer-dithered."""
    palette = []
    for r in range(0, 6):
        for g in range(0, 6):
            for b in range(0, 6):
                palette.append((r * 51, g * 51, b * 51))
    for v in range(0, 24):
        c = v * 11
        palette.append((c, c, c))
    palette = palette[:256]

    def nearest(r, g, b):
        best, bd = 0, 1 << 30
        for idx, (pr, pg, pb) in enumerate(palette):
            d = (r - pr) ** 2 + (g - pg) ** 2 + (b - pb) ** 2
            if d < bd:
                bd, best = d, idx
        return best

    indices = bytearray(npix)
    for p in range(npix):
        r, g, b, a = rgba[p * 4 : p * 4 + 4]
        if a < 128:
            indices[p] = 0  # reserve slot 0 as transparent below
            continue
        x, y = p % width, p // width
        d = (BAYER[y % 4][x % 4] - 7.5) / 16 * 24
        indices[p] = nearest(
            max(0, min(255, int(r + d))),
            max(0, min(255, int(g + d))),
            max(0, min(255, int(b + d))),
        )
    return indices, palette


def main():
    src, dst = sys.argv[1], sys.argv[2]
    width, height, rgba = decode_png(src)
    npix = width * height

    built = build_exact_palette(rgba, npix)
    exact = built is not None
    if not exact:
        indices, palette = build_dither_palette(rgba, npix, width)
    else:
        indices, palette = built

    # transparency: find transparent entries and give them alpha 0 via tRNS
    trns = []
    trans_slots = set()
    for idx, (r, g, b) in enumerate(palette):
        # a palette entry is "the transparent one" if some source pixel with
        # alpha<128 mapped to it
        pass
    # rebuild a tRNS covering every alpha seen for each index
    alpha_of = {}
    for p in range(npix):
        idx = indices[p]
        a = rgba[p * 4 + 3]
        prev = alpha_of.get(idx)
        if prev is None or a < prev:
            alpha_of[idx] = a
    for idx in range(len(palette)):
        a = alpha_of.get(idx, 255)
        if a < 255:
            trns.append(a)
        else:
            if trns and trns[-1] != 255:
                trns.append(255)
    # normalise: tRNS must be contiguous from index 0
    final_trns = []
    for idx in range(len(palette)):
        a = alpha_of.get(idx, 255)
        final_trns.append(a)
        if a == 255 and all(alpha_of.get(j, 255) == 255 for j in range(idx + 1)):
            break
    del trans_slots

    encode_png(dst, width, height, indices, palette, final_trns)
    print(
        f"{width}x{height} -> {dst} | "
        f"{len(palette)} colours ({'exact' if exact else 'dithered'}) | "
        f"transparent entries: {sum(1 for t in final_trns if t < 255)}"
    )


if __name__ == "__main__":
    main()