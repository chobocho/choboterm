"""Renders the choboterm app icon (1024px PNG + multi-size ICO).

The mark is a "CT" monogram: a bold sunset-gradient "C" (Chobo) wrapping a
"T" (Term) whose stem is a terminal block cursor.

Usage: python tools/make_icon.py build/appicon.png build/windows/icon.ico preview.png
Requires Pillow.
"""
import math
import sys
from PIL import Image, ImageDraw, ImageFilter, ImageChops

S = 4                      # supersampling factor
N = 1024 * S

ORANGE = (255, 150, 50)
PINK = (255, 46, 136)
MINT = (94, 234, 212, 255)
WHITE = (250, 247, 255, 255)


def px(v):
    return int(v * S)


def lerp(a, b, t):
    return tuple(int(a[i] + (b[i] - a[i]) * t) for i in range(len(a)))


def rounded_mask(box, radius):
    m = Image.new("L", (N, N), 0)
    ImageDraw.Draw(m).rounded_rectangle([px(v) for v in box], radius=px(radius), fill=255)
    return m


def vertical_gradient(top, bottom):
    g = Image.new("RGBA", (1, N))
    for y in range(N):
        g.putpixel((0, y), lerp(top, bottom, y / (N - 1)))
    return g.resize((N, N))


def diagonal_gradient(a, b):
    """Top-left a -> bottom-right b."""
    small = Image.new("RGBA", (256, 256))
    for y in range(256):
        for x in range(256):
            small.putpixel((x, y), lerp(a, b, (x + y) / 510) + (255,))
    return small.resize((N, N), Image.BILINEAR)


def arc_mask(cx, cy, r_out, r_in, gap_deg):
    """Thick ring with a gap centred on the right side, with round caps."""
    m = Image.new("L", (N, N), 0)
    d = ImageDraw.Draw(m)
    half = gap_deg / 2
    d.pieslice([px(cx - r_out), px(cy - r_out), px(cx + r_out), px(cy + r_out)],
               start=half, end=360 - half, fill=255)
    d.ellipse([px(cx - r_in), px(cy - r_in), px(cx + r_in), px(cy + r_in)], fill=0)
    # Round caps at both ends of the stroke.
    rc = (r_out - r_in) / 2
    rm = (r_out + r_in) / 2
    for a in (half, -half):
        x = cx + rm * math.cos(math.radians(a))
        y = cy + rm * math.sin(math.radians(a))
        d.ellipse([px(x - rc), px(y - rc), px(x + rc), px(y + rc)], fill=255)
    return m


def render(simple=False):
    """simple=True: thicker strokes and a lone cursor, for 16-32px."""
    img = Image.new("RGBA", (N, N), (0, 0, 0, 0))

    # Squircle body: deep ink-violet, so the warm C pops.
    body = rounded_mask((64, 64, 960, 960), 220)
    img.paste(vertical_gradient((44, 22, 78, 255), (14, 8, 30, 255)), (0, 0), body)

    if simple:
        c = arc_mask(512, 512, 380, 200, 84)
    else:
        c = arc_mask(512, 512, 352, 232, 76)
    grad = diagonal_gradient(ORANGE, PINK)

    # Soft warm glow under the C.
    glow = Image.new("RGBA", (N, N), PINK + (0,))
    glow.putalpha(c.filter(ImageFilter.GaussianBlur(px(36))).point(lambda a: int(a * 0.55)))
    img = Image.alpha_composite(img, glow)

    layer = Image.new("RGBA", (N, N), (0, 0, 0, 0))
    layer.paste(grad, (0, 0), c)
    img = Image.alpha_composite(img, layer)

    glyph = Image.new("RGBA", (N, N), (0, 0, 0, 0))
    gd = ImageDraw.Draw(glyph)
    if simple:
        # Just the cursor block inside the C.
        gd.rounded_rectangle([px(440), px(380), px(584), px(644)], radius=px(18), fill=MINT)
    else:
        # "T": white crossbar + mint block-cursor stem.
        gd.rounded_rectangle([px(372), px(372), px(652), px(436)], radius=px(16), fill=WHITE)
        gd.rounded_rectangle([px(470), px(456), px(554), px(652)], radius=px(12), fill=MINT)
        cglow = glyph.filter(ImageFilter.GaussianBlur(px(22)))
        cglow.putalpha(cglow.getchannel("A").point(lambda a: int(a * 0.6)))
        img = Image.alpha_composite(img, cglow)
    img = Image.alpha_composite(img, glyph)

    # Subtle top sheen on the body.
    hl = Image.new("L", (N, N), 0)
    ImageDraw.Draw(hl).ellipse([px(-200), px(-600), px(1224), px(380)], fill=16)
    hl = hl.filter(ImageFilter.GaussianBlur(px(40)))
    img.paste((255, 255, 255, 255), (0, 0), ImageChops.multiply(hl, body))

    return img.resize((1024, 1024), Image.LANCZOS)


if __name__ == "__main__":
    png, ico, preview = sys.argv[1], sys.argv[2], sys.argv[3]
    icon = render()
    icon.save(png)
    small = render(simple=True)
    frames = [small.resize((s, s), Image.LANCZOS) for s in (16, 24, 32)]
    frames += [icon.resize((s, s), Image.LANCZOS) for s in (48, 64, 128, 256)]
    frames[-1].save(ico, format="ICO", sizes=[f.size for f in frames], append_images=frames[:-1])
    pick = {f.size[0]: f for f in frames}

    # Preview sheet: large icon plus small sizes on light and dark backgrounds.
    sheet = Image.new("RGBA", (900, 420), (240, 240, 240, 255))
    sheet.paste((32, 32, 32, 255), (450, 0, 900, 420))
    big = icon.resize((256, 256), Image.LANCZOS)
    for ox in (0, 450):
        sheet.alpha_composite(big, (ox + 20, 20))
        x = ox + 20
        for s in (64, 48, 32, 24, 16):
            sheet.alpha_composite(pick[s] if s in pick else icon.resize((s, s), Image.LANCZOS), (x, 300))
            x += s + 10 if s > 32 else s + 8
        for i, s in enumerate((48, 32, 16)):
            sheet.alpha_composite(pick[s], (ox + 300 + i * 56, 40))
    sheet.save(preview)
