"""Builds frontend/dist/fonts/quarters-symbols.woff2: plain, one-cell
versions of the symbols Claude Code prints that phones (iOS above all) draw
as colour emoji, two cells wide, over the next letter: ⏺ ⏸ ⏹ ⏩ ⏵ ▶ ✔ ✓ ✳ ⚡.

The terminal lists it right after JetBrains Mono, whose cell it matches
(1000 units per em, 600 wide).

    python3 -m venv .venv && .venv/bin/pip install fonttools brotli
    .venv/bin/python tools/symbols/build.py
"""

import math
import os

from fontTools.fontBuilder import FontBuilder
from fontTools.pens.ttGlyphPen import TTGlyphPen

UPEM, ADVANCE = 1000, 600
CX, CY = 300, 330  # middle of the cell, at the height of a bullet
ASCENT, DESCENT = 1020, -300  # JetBrains Mono's

OUT = os.path.join(os.path.dirname(__file__), "..", "..", "frontend", "dist", "fonts", "quarters-symbols.woff2")


def polygon(pen, pts):
    pen.moveTo(pts[0])
    for p in pts[1:]:
        pen.lineTo(p)
    pen.closePath()


def circle(pen, cx, cy, r, n=8):
    """A circle from n quadratic curves (on-curve points between controls)."""
    k = r / math.cos(math.pi / n)  # control points sit outside the circle
    on = lambda i: (cx + r * math.cos(2 * math.pi * i / n), cy + r * math.sin(2 * math.pi * i / n))
    off = lambda i: (cx + k * math.cos(2 * math.pi * (i + 0.5) / n), cy + k * math.sin(2 * math.pi * (i + 0.5) / n))
    pen.moveTo(on(0))
    for i in range(n):
        pen.qCurveTo(off(i), on(i + 1))
    pen.closePath()


def bar(pen, cx, cy, length, width, angle):
    """A rectangle centred on (cx, cy), turned by angle degrees."""
    a = math.radians(angle)
    dx, dy = math.cos(a), math.sin(a)
    px, py = -dy, dx
    h, w = length / 2, width / 2
    polygon(pen, [(cx - dx * h - px * w, cy - dy * h - py * w), (cx + dx * h - px * w, cy + dy * h - py * w),
                  (cx + dx * h + px * w, cy + dy * h + py * w), (cx - dx * h + px * w, cy - dy * h + py * w)])


def rect(pen, x0, y0, x1, y1):
    polygon(pen, [(x0, y0), (x0, y1), (x1, y1), (x1, y0)])


def record(pen):
    return {
        0x23FA: lambda: circle(pen, CX, CY, 190),  # ⏺ record: Claude's bullet
        0x23F8: lambda: (rect(pen, 150, 130, 250, 530), rect(pen, 350, 130, 450, 530)),  # ⏸ pause
        0x23F9: lambda: rect(pen, 140, 160, 460, 500),  # ⏹ stop
        0x23E9: lambda: (polygon(pen, [(60, 180), (60, 480), (300, 330)]), polygon(pen, [(300, 180), (300, 480), (540, 330)])),  # ⏩
        0x23F5: lambda: polygon(pen, [(170, 150), (170, 510), (460, 330)]),  # ⏵ (Claude's ⏵⏵ mode marker)
        0x25B6: lambda: polygon(pen, [(150, 120), (150, 540), (490, 330)]),  # ▶
        0x2714: lambda: polygon(pen, [(50, 330), (140, 420), (235, 300), (480, 600), (560, 515), (235, 130)]),  # ✔ heavy check
        0x2713: lambda: polygon(pen, [(70, 330), (125, 385), (235, 255), (490, 580), (540, 530), (235, 150)]),  # ✓ check
        0x2733: lambda: [bar(pen, CX, CY, 460, 80, a) for a in (0, 45, 90, 135)],  # ✳ eight spokes
        0x26A1: lambda: polygon(pen, [(360, 760), (120, 300), (290, 300), (220, -60), (490, 420), (320, 420)]),  # ⚡
    }


def main():
    glyphs, cmap, metrics = {}, {}, {}
    empty = TTGlyphPen(None)
    glyphs[".notdef"] = empty.glyph()
    metrics[".notdef"] = (ADVANCE, 0)
    for cp in record(None):
        pen = TTGlyphPen(None)
        record(pen)[cp]()
        name = f"uni{cp:04X}"
        glyphs[name] = pen.glyph()
        cmap[cp] = name
        metrics[name] = (ADVANCE, 0)
    order = [".notdef"] + sorted(n for n in glyphs if n != ".notdef")
    fb = FontBuilder(UPEM, isTTF=True)
    fb.setupGlyphOrder(order)
    fb.setupCharacterMap(cmap)
    fb.setupGlyf(glyphs)
    for name in order:  # left side bearing = the glyph's xMin
        g = fb.font["glyf"][name]
        metrics[name] = (ADVANCE, getattr(g, "xMin", 0))
    fb.setupHorizontalMetrics(metrics)
    fb.setupHorizontalHeader(ascent=ASCENT, descent=DESCENT)
    fb.setupNameTable({"familyName": "Quarters Symbols", "styleName": "Regular"})
    fb.setupOS2(sTypoAscender=ASCENT, sTypoDescender=DESCENT, usWinAscent=ASCENT, usWinDescent=-DESCENT, xAvgCharWidth=ADVANCE)
    fb.setupPost(isFixedPitch=1)
    fb.font.flavor = "woff2"
    os.makedirs(os.path.dirname(OUT), exist_ok=True)
    fb.save(OUT)
    print(f"{os.path.normpath(OUT)}: {len(cmap)} symbols, {os.path.getsize(OUT)} bytes")


if __name__ == "__main__":
    main()
