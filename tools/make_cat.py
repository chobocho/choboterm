"""Builds the cat's sprites from the artist's sheets.

Two sheets on a dark background are the cat now:
  ACTS: five rows: sit and yawn (7 frames); stretch from standing, ending
        sat down (7); sit, nod off and curl up (7); wake from curled up to
        standing (7); crouch, leap, land (6).
  WALK: one row of 8 frames, walking in place.
The old checker sheet (OLD) still gives the actions the new ones don't
have: grooming, looking back, the back view and rolling over; it is drawn
in the same cute style.

New sheets: the cats are cut out of the near-black background (flood-filled
from the border; the edge fades by brightness from the background's to the
outline's), cats joined only by a whisker are kept apart, and each frame
keeps its place in its cell, so an action plays as drawn (a stretching
cat's nose goes forward). Everything is scaled so the walking cat is 96 px
tall (twice its 48 px on screen) and packed into cat.png, with the frame
positions, each action's anchor (ax: the first frame's nose, from the
left) and the walk's steps in catframes.json.

Usage: python tools/make_cat.py OLD.png ACTS.png WALK.png [PREVIEW_DIR]
Writes frontend/src/assets/cat.png and frontend/src/catframes.json.
Requires Pillow and numpy.
"""
import json
import os
import sys
from collections import deque

import numpy as np
from PIL import Image, ImageDraw, ImageFilter

HEIGHT = 96  # a walking cat, in atlas px; shown at half size, so sharp on HiDPI too
ROOT = os.path.join(os.path.dirname(os.path.abspath(__file__)), '..')
OUT_PNG = os.path.join(ROOT, 'frontend', 'src', 'assets', 'cat.png')
OUT_JSON = os.path.join(ROOT, 'frontend', 'src', 'catframes.json')
N4 = ((1, 0), (-1, 0), (0, 1), (0, -1))

# ---- The old checker sheet: grooming, looking back, the back view, rolling ----

# Where each action's frames are on the old sheet: the block, the top of its
# row (frames whose top is within 95 px below it), and which frames of the
# row (left to right) it takes.
LAYOUT = [
    ('roll',     'left',  275, (0, 1, 2)),
    ('groom',    'left',  375, (0, 1, 2)),
    ('back',     'left',  510, (2,)),
    ('sitdown',  'right', 5,   (0, 1, 2)),   # only to size the old frames: its sitting cat
    ('lookback', 'right', 170, (0, 1, 2)),
]
SPLIT_X = 600  # left block | right block


def lum(c):
    return 0.299 * c[0] + 0.587 * c[1] + 0.114 * c[2]


def is_checker(c):
    return max(c[:3]) - min(c[:3]) <= 12 and lum(c) >= 180


def cut_out(sheet):
    """The sheet with the background transparent and the edges un-matted."""
    W, H = sheet.size
    src = sheet.load()
    im = sheet.copy()
    px = im.load()
    bgmask = bytearray(W * H)
    q = deque()
    for x in range(W):
        q.extend(((x, 0), (x, H - 1)))
    for y in range(H):
        q.extend(((0, y), (W - 1, y)))
    while q:
        x, y = q.popleft()
        i = y * W + x
        if bgmask[i] or not is_checker(src[x, y]):
            continue
        bgmask[i] = 1
        for dx, dy in N4:
            nx, ny = x + dx, y + dy
            if 0 <= nx < W and 0 <= ny < H and not bgmask[ny * W + nx]:
                q.append((nx, ny))
    for y in range(H):
        for x in range(W):
            if bgmask[y * W + x]:
                px[x, y] = (0, 0, 0, 0)
    # Un-matte: the outline was anti-aliased against the checker square under
    # it, so an edge pixel is the outline o and that square's colour b mixed,
    # c = a*o + (1-a)*b. Take b from the background pixels next to it, o as
    # the outline's dark brown, a from the brightness, and keep o at alpha a:
    # no light fringe on a dark terminal, and smooth edges on any background.
    outline = (62, 30, 16)
    Lo = lum(outline)
    for _ in range(2):
        ring = []
        for y in range(H):
            for x in range(W):
                c = px[x, y]
                if c[3] != 255:
                    continue
                bs = [src[x + dx, y + dy] for dx, dy in N4
                      if 0 <= x + dx < W and 0 <= y + dy < H and bgmask[(y + dy) * W + x + dx]]
                if not bs:
                    continue
                Lb = sum(lum(b) for b in bs) / len(bs)
                L = lum(c)
                if L <= Lo + 12:
                    continue
                ring.append((x, y, max(0.0, min(1.0, (Lb - L) / (Lb - Lo)))))
        if not ring:
            break
        for x, y, a in ring:
            px[x, y] = (0, 0, 0, 0) if a < 0.1 else outline + (round(255 * a),)
            bgmask[y * W + x] = 1  # the next ring compares with the true background
    return im


def blobs(im, min_area=1500):
    """(box, image) of each opaque blob of at least min_area pixels."""
    W, H = im.size
    px = im.load()
    lab = [-1] * (W * H)
    res = []
    for y in range(H):
        for x in range(W):
            if lab[y * W + x] != -1 or px[x, y][3] == 0:
                continue
            n = len(res)
            pts, q = [], deque([(x, y)])
            lab[y * W + x] = n
            while q:
                cx, cy = q.popleft()
                pts.append((cx, cy))
                for dx, dy in N4:
                    nx, ny = cx + dx, cy + dy
                    if 0 <= nx < W and 0 <= ny < H and lab[ny * W + nx] == -1 and px[nx, ny][3]:
                        lab[ny * W + nx] = n
                        q.append((nx, ny))
            res.append(pts)
    out = []
    for pts in res:
        if len(pts) < min_area:
            continue
        xs, ys = [p[0] for p in pts], [p[1] for p in pts]
        box = (min(xs), min(ys), max(xs) + 1, max(ys) + 1)
        sp = Image.new('RGBA', (box[2] - box[0], box[3] - box[1]), (0, 0, 0, 0))
        sp_px = sp.load()
        for x, y in pts:
            sp_px[x - box[0], y - box[1]] = px[x, y]
        out.append((box, sp))
    return out


def actions_of(sheet):
    """{action: [frame images]} following LAYOUT."""
    found = blobs(cut_out(sheet))
    acts = {}
    for name, block, top, take in LAYOUT:
        row = [(box, sp) for box, sp in found
               if (box[0] < SPLIT_X) == (block == 'left') and top <= box[1] < top + 95]
        row.sort(key=lambda b: b[0][0])
        if len(row) <= max(take):
            sys.exit(f'{name}: found {len(row)} frames on the row at y={top} ({block})')
        acts[name] = [row[i][1] for i in take]
    return acts


def ground_runs(f):
    """x ranges of the paws on the ground (opaque in the bottom 3 rows)."""
    al = f.getchannel('A').load()
    w, h = f.size
    cols = [any(al[x, y] > 128 for y in range(h - 3, h)) for x in range(w)]
    out, x = [], 0
    while x < w:
        if cols[x]:
            s = x
            while x < w and cols[x]:
                x += 1
            out.append((s, x - 1))
        else:
            x += 1
    return out


def locked_step(a, b):
    """How far the body moves from frame a to frame b (same size, nose on the
    right edge) so the paws on the ground in both stay on the same spot: the
    shift that best overlaps their ground runs. Moving by it at the change of
    frame keeps the paws from sliding."""
    ra, rb = ground_runs(a), ground_runs(b)
    best = None
    for s in range(-2, 71):
        sc = sum(max(0, min(a1, b1 + s) - max(a0, b0 + s) + 1) for a0, a1 in ra for b0, b1 in rb)
        if best is None or sc > best[0] or (sc == best[0] and abs(s) < abs(best[1])):
            best = (sc, s)
    return best[1]


# ---- The new sheets, on a dark background ----

def cut_dark(sheet):
    """The sheet with its near-black background transparent."""
    a = np.asarray(sheet.convert('RGB')).astype(np.int32)
    H, W = a.shape[:2]
    mx, mn = a.max(2), a.min(2)
    # the background: near-black, grey or slightly navy (the outline is a
    # reddish brown, so it isn't taken for it)
    bgc = (mx <= 50) & (mx - mn <= 20)
    seed = Image.fromarray(np.where(bgc, 255, 0).astype(np.uint8)).copy()  # (fromarray's buffer is read-only)
    for x in range(0, W, 5):
        for y in (0, H - 1):
            if seed.getpixel((x, y)) == 255:
                ImageDraw.floodfill(seed, (x, y), 128)
    for y in range(0, H, 5):
        for x in (0, W - 1):
            if seed.getpixel((x, y)) == 255:
                ImageDraw.floodfill(seed, (x, y), 128)
    bg = np.asarray(seed) == 128
    alpha = np.where(bg, 0, 255).astype(np.float64)
    # the edge fades from the background's brightness to the outline's, as drawn
    near = np.zeros_like(bg)
    for dy, dx in N4:
        near |= np.roll(np.roll(bg, dy, 0), dx, 1)
    edge = ~bg & near
    L = a.mean(2)
    alpha[edge] = 255 * np.clip((L[edge] - 22) / (70 - 22), 0, 1)
    return Image.fromarray(np.dstack([a, alpha]).astype(np.uint8), 'RGBA').copy()


def split_blobs(im, min_area):
    """(box, image) of each cat. Cats joined only by thin lines (a whisker
    touching the next cat's tail) are kept apart: blobs are found with thin
    lines opened away, then every opaque pixel goes to the blob it is
    nearest to."""
    a = np.asarray(im)
    solid = Image.fromarray(np.where(a[..., 3] > 60, 255, 0).astype(np.uint8))
    core = np.asarray(solid.filter(ImageFilter.MinFilter(5)).filter(ImageFilter.MaxFilter(5))) > 0
    H, W = core.shape
    lab = np.full((H, W), -1, np.int32)
    seeds = Image.fromarray(np.where(core, 255, 0).astype(np.uint8)).copy()
    q = deque()
    n = 0
    for y, x in zip(*np.nonzero(core)):
        if seeds.getpixel((int(x), int(y))) != 255:
            continue
        ImageDraw.floodfill(seeds, (int(x), int(y)), 77)
        m = np.asarray(seeds) == 77
        if m.sum() >= min_area // 2:
            lab[m] = n
            q.extend(zip(*np.nonzero(m)))
            n += 1
        seeds.paste(Image.fromarray(np.where(m, 1, np.asarray(seeds)).astype(np.uint8)))
    opaque = a[..., 3] > 0
    while q:
        y, x = q.popleft()
        for dy, dx in N4:
            yy, xx = y + dy, x + dx
            if 0 <= yy < H and 0 <= xx < W and opaque[yy, xx] and lab[yy, xx] < 0:
                lab[yy, xx] = lab[y, x]
                q.append((yy, xx))
    out = []
    for k in range(n):
        ys, xs = np.nonzero(lab == k)
        if len(xs) < min_area:
            continue
        box = (int(xs.min()), int(ys.min()), int(xs.max()) + 1, int(ys.max()) + 1)
        sub = a[box[1]:box[3], box[0]:box[2]].copy()
        sub[lab[box[1]:box[3], box[0]:box[2]] != k] = 0
        out.append((box, Image.fromarray(sub, 'RGBA')))
    return out


def rows_of(sheet, nrows):
    """The sheet's cats, row by row, left to right, each with its place in its
    cell: [(image, x, y)], x and y from the cell's top-left."""
    im = cut_dark(sheet)
    W, H = im.size
    rows = [[] for _ in range(nrows)]
    for box, sp in split_blobs(im, 1500):
        rows[min(nrows - 1, int((box[1] + box[3]) / 2 / (H / nrows)))].append((box, sp))
    out = []
    for r, row in enumerate(rows):
        row.sort(key=lambda b: b[0][0])
        # The cells: evenly spaced from the first cat to the last (a row
        # needn't span the sheet's width), by the cats' centres.
        cs = [(box[0] + box[2]) / 2 for box, _ in row]
        cw = (cs[-1] - cs[0]) / max(1, len(row) - 1)
        ch = H / nrows
        out.append([(sp, box[0] - (cs[0] + i * cw), box[1] - r * ch) for i, (box, sp) in enumerate(row)])
    return out


def body_x(f):
    """x of the middle of the body: the centre of the opaque pixels in the
    band from 30% to 60% of the height, right of the tail (the left quarter)."""
    al = np.asarray(f.getchannel('A')) > 128
    h, w = al.shape
    band = al[int(h * 0.3):int(h * 0.6), w // 4:]
    xs = np.nonzero(band)[1]
    return float(xs.mean()) + w // 4 if len(xs) else w / 2


def nose(f):
    """x of the rightmost opaque column: where the cat's nose (or front) is."""
    al = np.asarray(f.getchannel('A')) > 128
    cols = np.nonzero(al.any(0))[0]
    return int(cols[-1]) + 1 if len(cols) else f.size[0]


def save_png(im, path):
    """A 128-colour palette PNG (with alpha): a fifth of the size of full colour."""
    im.quantize(colors=128, method=Image.Quantize.FASTOCTREE, dither=Image.Dither.NONE).save(path, optimize=True)


def scaled(f, s):
    """f scaled by s, with premultiplied alpha so no dark fringe bleeds in."""
    return f.convert('RGBa').resize((max(1, round(f.size[0] * s)), max(1, round(f.size[1] * s))), Image.LANCZOS).convert('RGBA')


def main(old_path, acts_path, walk_path, preview_dir=None):
    acts = rows_of(Image.open(acts_path), 5)
    walk = rows_of(Image.open(walk_path), 1)[0]
    old = actions_of(Image.open(old_path).convert('RGBA'))
    if [len(r) for r in acts] != [7, 7, 7, 7, 6] or len(walk) != 8:
        sys.exit(f'unexpected frames: acts {[len(r) for r in acts]}, walk {len(walk)}')
    # one size for all: the walking cat HEIGHT tall; the acts sheet's standing
    # cat (stretch's first frame, the same pose) as tall as it; the old
    # sheet's sitting cat as tall as the new one
    sw = HEIGHT / sorted(f.size[1] for f, _, _ in walk)[4]
    sa = walk[0][0].size[1] * sw / acts[1][0][0].size[1]
    so = acts[0][0][0].size[1] * sa / old['sitdown'][2].size[1]
    print(f'scales: walk {sw:.3f}, acts {sa:.3f}, old {so:.3f}')

    def placed(frames, s):
        return [(scaled(f, s), x * s, y * s) for f, x, y in frames]

    def nose_right(frames, s):
        # old frames: feet on the bottom, nose on the right, as they were drawn
        fs = [scaled(f, s) for f in frames]
        w = max(f.size[0] for f in fs)
        h = max(f.size[1] for f in fs)
        return [(f, w - f.size[0], h - f.size[1]) for f in fs]

    # The walk's cats aren't drawn quite alike: a few are smaller, and where
    # each sits in its cell drifts. Every frame is scaled to the same height,
    # stands on one line (a walking cat always has paws on the ground) and is
    # placed by its body (the middle band, without legs and tail), so only
    # the legs move.
    wf = []
    for f, _, _ in walk:
        g = scaled(f, HEIGHT / f.size[1])
        wf.append((g, -body_x(g), 0))
    ACTIONS = {
        'walk': wf,
        'yawn': placed(acts[0], sa),      # sit, look about, yawn, eyes shut, sit
        'stretch': placed(acts[1], sa),   # stand, stretch, stand, sit down
        'asleep': placed(acts[2], sa),    # sit, nod off, curl up (3-6: asleep)
        'wake': placed(acts[3], sa),      # curled up, look up, stretch a paw, sit, stand
        'jump': placed(acts[4], sa),      # crouch, leap, fly, land, stand
        'groom': nose_right(old['groom'], so),
        'lookback': nose_right(old['lookback'], so),
        'back': nose_right(old['back'], so),
        'roll': nose_right(old['roll'], so),
    }
    strips = []
    for name, frames in ACTIONS.items():
        x0 = min(int(x) for _, x, _ in frames)
        y0 = min(int(y) for _, _, y in frames)
        fw = max(int(round(x)) - x0 + f.size[0] for f, x, _ in frames)
        fh = max(int(round(y)) - y0 + f.size[1] for f, _, y in frames)
        padded = []
        for f, x, y in frames:
            g = Image.new('RGBA', (fw, fh), (0, 0, 0, 0))
            g.alpha_composite(f, (int(round(x)) - x0, int(round(y)) - y0))
            padded.append(g)
        strips.append((name, fw, fh, padded))
    atlas_w = max(fw * len(fr) for _, fw, _, fr in strips)
    atlas_h = sum(fh for _, _, fh, _ in strips)
    atlas = Image.new('RGBA', (atlas_w, atlas_h), (0, 0, 0, 0))
    meta = {'height': HEIGHT // 2, 'w': atlas_w, 'h': atlas_h, 'actions': {}}
    y = 0
    for name, fw, fh, frames in strips:
        pos = []
        for i, f in enumerate(frames):
            atlas.alpha_composite(f, (i * fw, y))
            pos.append([i * fw, y])
        # ax: where the first frame's nose is, from the frame's left edge; the
        # app keeps it put when an action starts, so the cat doesn't jump
        meta['actions'][name] = {'w': fw, 'h': fh, 'ax': nose(frames[0]), 'frames': pos}
        if name == 'walk':
            # steps[i]: atlas px the body moves when frame i follows frame i-1,
            # keeping the paws on the ground put. The cat walks in place at an
            # even pace, so every step is the measured ones' mean.
            steps = [locked_step(frames[i - 1], frames[i]) for i in range(len(frames))]
            print('walk steps (atlas px):', steps)
            mean = sum(steps) / len(steps)
            meta['actions'][name]['steps'] = [round(mean, 2)] * len(steps)
        y += fh
    os.makedirs(os.path.dirname(OUT_PNG), exist_ok=True)
    save_png(atlas, OUT_PNG)
    with open(OUT_JSON, 'w') as f:
        json.dump(meta, f, separators=(',', ':'))
        f.write('\n')
    print(f'cat.png {atlas_w}x{atlas_h}, {os.path.getsize(OUT_PNG) // 1024} KB; '
          + ', '.join(f'{n} {len(a["frames"])}' for n, a in meta['actions'].items()))
    if preview_dir:
        bg = Image.new('RGBA', atlas.size, (12, 12, 12, 255))
        bg.alpha_composite(atlas)
        bg.convert('RGB').save(os.path.join(preview_dir, 'atlas_dark.png'))


if __name__ == '__main__':
    if len(sys.argv) not in (4, 5):
        sys.exit(__doc__)
    main(*sys.argv[1:])
