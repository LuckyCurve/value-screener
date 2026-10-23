"""Draws the app icon, app/icon.png.

The mark is the one in the window's header: a ledger page with a magnifier.
It is drawn rather than shipped as a binary so it can be changed like any
other source file:

    python scripts/make-icon.py

Windows picks the PNG up as the executable's icon through mygo.json, and the
builds of the other platforms use it the same way.
"""

from __future__ import annotations

import os

from PIL import Image, ImageDraw

SIZE = 1024
SS = 4  # supersampling, so the curves and the small glyph stay smooth
W = SIZE * SS

BLUE_TOP = (59, 130, 246)
BLUE_BOTTOM = (29, 78, 216)
WHITE = (255, 255, 255)


def gradient(w: int, h: int) -> Image.Image:
    """A vertical blue gradient, the window's accent from light to deep."""
    img = Image.new("RGB", (1, h))
    px = img.load()
    for y in range(h):
        t = y / max(1, h - 1)
        px[0, y] = tuple(
            round(a + (b - a) * t) for a, b in zip(BLUE_TOP, BLUE_BOTTOM)
        )
    return img.resize((w, h), Image.BILINEAR)


def rounded_mask(w: int, h: int, radius: int) -> Image.Image:
    mask = Image.new("L", (w, h), 0)
    ImageDraw.Draw(mask).rounded_rectangle(
        (0, 0, w - 1, h - 1), radius=radius, fill=255
    )
    return mask


def draw() -> Image.Image:
    body = gradient(W, W)
    body.putalpha(rounded_mask(W, W, int(W * 0.225)))

    glyph = Image.new("RGBA", (W, W), (0, 0, 0, 0))
    d = ImageDraw.Draw(glyph)
    u = W / 24.0  # the 24-unit grid the window's icons are drawn on
    stroke = max(1, round(u * 1.9))

    def line(points, width=stroke):
        d.line([(x * u, y * u) for x, y in points], fill=WHITE, width=width,
               joint="curve")
        # Round the ends, as the window's icons are.
        for x, y in (points[0], points[-1]):
            d.ellipse((x * u - width / 2, y * u - width / 2,
                       x * u + width / 2, y * u + width / 2), fill=WHITE)

    # The ledger page: a sheet with two ruled lines.
    d.rounded_rectangle(
        (4.6 * u, 3.2 * u, 15.4 * u, 20.8 * u),
        radius=1.8 * u, outline=WHITE, width=stroke,
    )
    line([(7.8, 8.0), (11.4, 8.0)], stroke)
    line([(7.8, 12.0), (11.4, 12.0)], stroke)

    # The magnifier at its lower right, clear of the page.
    cx, cy, r = 17.2, 15.4, 3.6
    d.ellipse((cx * u - r * u, cy * u - r * u,
               cx * u + r * u, cy * u + r * u),
              outline=WHITE, width=stroke)
    line([(19.6, 17.8), (21.4, 19.6)], round(stroke * 1.15))

    body.alpha_composite(glyph)
    return body.resize((SIZE, SIZE), Image.LANCZOS)


def main() -> None:
    root = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
    out = os.path.join(root, "app", "icon.png")
    draw().save(out, optimize=True)
    print("wrote", out)


if __name__ == "__main__":
    main()
