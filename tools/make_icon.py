"""Renders the choboterm app icon (1024px PNG + multi-size ICO).

Usage: python tools/make_icon.py build/appicon.png build/windows/icon.ico preview.png
Requires Pillow.
"""
import sys
from PIL import Image, ImageDraw, ImageFilter, ImageChops

S = 4                      # supersampling factor
N = 1024 * S


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


def stroke_poly(draw, pts, width, fill):
    w = px(width)
    p = [(px(x), px(y)) for x, y in pts]
    draw.line(p, fill=fill, width=w, joint="curve")
    r = w // 2
    for x, y in (p[0], p[-1]):
        draw.ellipse([x - r, y - r, x + r, y + r], fill=fill)


def render(simple=False):
    """simple=True: no inner screen and a bigger glyph, for 16-32px."""
    img = Image.new("RGBA", (N, N), (0, 0, 0, 0))

    # Squircle body: deep blue gradient, like the old PC-communication blue screens.
    body_box = (64, 64, 960, 960)
    body = rounded_mask(body_box, 210)
    top, bottom = ((30, 64, 175, 255), (10, 15, 45, 255)) if simple else ((37, 99, 235, 255), (17, 24, 72, 255))
    img.paste(vertical_gradient(top, bottom), (0, 0), body)

    if simple:
        glyph = Image.new("RGBA", (N, N), (0, 0, 0, 0))
        gd = ImageDraw.Draw(glyph)
        mint = (94, 234, 212, 255)
        stroke_poly(gd, [(250, 318), (500, 512), (250, 706)], 140, mint)
        gd.rounded_rectangle([px(570), px(636), px(820), px(746)], radius=px(30), fill=mint)
        img = Image.alpha_composite(img, glyph)
        return img.resize((1024, 1024), Image.LANCZOS)

    # Inner screen with a soft border.
    screen_box = (150, 190, 874, 834)
    border = rounded_mask((140, 180, 884, 844), 96)
    img.paste((96, 165, 250, 90), (0, 0), ImageChops.multiply(border, body))
    screen = rounded_mask(screen_box, 86)
    img.paste(vertical_gradient((10, 18, 48, 255), (4, 8, 26, 255)), (0, 0), screen)

    # Faint scanlines on the screen.
    lines = Image.new("L", (N, N), 0)
    ld = ImageDraw.Draw(lines)
    for y in range(190, 834, 14):
        ld.rectangle([0, px(y), N, px(y + 5)], fill=22)
    img.paste((120, 180, 255, 255), (0, 0), ImageChops.multiply(lines, screen))

    # Prompt glyph ">_" in mint, drawn as strokes so it stays crisp at 16px.
    glyph = Image.new("RGBA", (N, N), (0, 0, 0, 0))
    gd = ImageDraw.Draw(glyph)
    mint = (94, 234, 212, 255)
    stroke_poly(gd, [(300, 372), (468, 512), (300, 652)], 92, mint)
    gd.rounded_rectangle([px(540), px(616), px(736), px(696)], radius=px(26), fill=mint)

    glow = glyph.filter(ImageFilter.GaussianBlur(px(30)))
    glow.putalpha(glow.getchannel("A").point(lambda a: int(a * 0.85)))
    img = Image.alpha_composite(img, glow)
    img = Image.alpha_composite(img, glyph)

    # Glossy highlight on the upper half of the body.
    hl = Image.new("L", (N, N), 0)
    ImageDraw.Draw(hl).ellipse([px(-200), px(-560), px(1224), px(420)], fill=22)
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
