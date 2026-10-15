"""Builds the cat's sprites from the artist's sheet.

The sheet is one orange tabby on a light checker background, in two blocks:
on the left, rows of three (walk, walk, roll, groom, stretch and a back view,
walk with the tail up, lie down to sleep); on the right, rows of three too
(stand, crouch and sit; sit turned away and look back; walk; jump; sleep
curled up). Frames are cut out (background flood-filled from the border,
the anti-aliased edge un-matted against the checker square it was drawn
on), scaled so a walking cat is 96 px tall (twice its 48 px on screen),
aligned on their feet and noses, and packed into cat.png with the frame
positions in catframes.json.

The walk can come from a video of the cat walking in place instead (the
sheet's walking rows are separate poses with a hind leg in the air, so the
cat looked three-legged): --walk VIDEO FIRST EVERY COUNT takes COUNT frames
of VIDEO, from frame FIRST (0-based), every EVERY frames: one whole cycle.
The cat is cut out of the plain gray background by its colour, all frames
are cropped to one box (the body stays put, the legs move), and the step
per frame comes from the paws on the ground, as for the sheet.

Usage: python tools/make_cat.py SHEET.png [PREVIEW_DIR] [--walk VIDEO FIRST EVERY COUNT]
  e.g. python tools/make_cat.py sheet.png --walk walk.mp4 0 2 13
Writes frontend/src/assets/cat.png and frontend/src/catframes.json.
Requires Pillow; --walk also numpy and ffmpeg.
"""
import json
import os
import subprocess
import sys
import tempfile
from collections import deque

from PIL import Image, ImageDraw

HEIGHT = 96  # a walking cat, in atlas px; shown at half size, so sharp on HiDPI too
ROOT = os.path.join(os.path.dirname(os.path.abspath(__file__)), '..')
OUT_PNG = os.path.join(ROOT, 'frontend', 'src', 'assets', 'cat.png')
OUT_JSON = os.path.join(ROOT, 'frontend', 'src', 'catframes.json')
N4 = ((1, 0), (-1, 0), (0, 1), (0, -1))

# Where each action's frames are on the sheet: the block, the top of its row
# (frames whose top is within 95 px below it), and which frames of the row
# (left to right) it takes.
LAYOUT = [
    ('walkA',    'left',  10,  (0, 1, 2)),
    ('walkB',    'left',  140, (0, 1, 2)),
    ('roll',     'left',  275, (0, 1, 2)),
    ('groom',    'left',  375, (0, 1, 2)),
    ('stretch',  'left',  510, (0, 1)),
    ('back',     'left',  510, (2,)),
    ('walkC',    'left',  650, (0, 1, 2)),
    ('liedown',  'left',  785, (0, 1, 2)),
    ('sitdown',  'right', 5,   (0, 1, 2)),
    ('lookback', 'right', 170, (0, 1, 2)),
    ('walkD',    'right', 470, (0, 1, 2)),
    ('jump',     'right', 610, (0, 1, 2)),
    ('sleep',    'right', 790, (0, 1, 2)),
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


# What goes into the atlas: action -> [(sheet row, frame index)]. Only walkB
# is a real walk cycle (its paws on the ground move back evenly from frame
# to frame, in the order 0, 2, 1); the other walking rows are separate poses
# and would make the paws slide, so they are left out.
ATLAS = {
    'walk':     [('walkB', 0), ('walkB', 2), ('walkB', 1)],
    'sitdown':  [('sitdown', i) for i in range(3)],   # stand, crouch, sit
    'lookback': [('lookback', i) for i in range(3)],  # sit turned away, turning, sit
    'groom':    [('groom', i) for i in range(3)],
    'stretch':  [('stretch', i) for i in range(2)],
    'back':     [('back', 0)],
    'roll':     [('roll', i) for i in range(3)],
    'liedown':  [('liedown', i) for i in range(3)],   # lying awake, eyes closed, curled up
    'sleep':    [('sleep', i) for i in range(3)],
    'jump':     [('jump', i) for i in range(3)],
}


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


def video_frames(video, first, every, count):
    """count frames of video from frame first (0-based), every every frames, through ffmpeg."""
    want = [first + i * every for i in range(count)]
    sel = '+'.join(f'eq(n\\,{n})' for n in want)
    with tempfile.TemporaryDirectory() as d:
        subprocess.run(['ffmpeg', '-v', 'error', '-i', video, '-vf', f"select='{sel}'",
                        '-fps_mode', 'passthrough', os.path.join(d, 'f%03d.png')], check=True)
        return [Image.open(os.path.join(d, f'f{i + 1:03d}.png')).convert('RGB') for i in range(count)]


def video_mask(frame):
    """The cat in a video frame: saturated (orange, brown, cream, green, pink)
    or dark (the outline) pixels, with the holes inside filled (eye glints).
    The background, its shadow and the watermark are gray and drop out; so do
    the outline's light anti-aliased edge pixels, which would leave a gray
    fringe."""
    import numpy as np
    a = np.asarray(frame, dtype=np.float32) / 255
    mx, mn = a.max(2), a.min(2)
    cat = ((mx - mn) / (mx + 1e-6) > 0.18) | (mx < 0.22)
    # Everything not reachable from the border without crossing the cat is cat.
    # (copy: an image made by fromarray shares the array's read-only buffer,
    # and floodfill silently does nothing on it)
    outside = Image.fromarray(np.where(cat, 0, 255).astype(np.uint8)).copy()
    ImageDraw.floodfill(outside, (0, 0), 128)
    return np.asarray(outside) != 128


def walk_from_video(video, first, every, count):
    """The walk's frames from a video of the cat walking in place, all of one size."""
    import numpy as np
    frames = video_frames(video, first, every, count)
    masks = [video_mask(f) for f in frames]
    # One box for all: the union of each frame's cat, outliers (compression
    # specks) left out by taking the 0.1th..99.9th percentile of its pixels.
    boxes = []
    for m in masks:
        ys, xs = np.nonzero(m)
        x0, x1 = np.percentile(xs, [0.1, 99.9])
        y0, y1 = np.percentile(ys, [0.1, 99.9])
        boxes.append((int(x0), int(y0), int(x1) + 1, int(y1) + 1))
    box = (min(b[0] for b in boxes), min(b[1] for b in boxes), max(b[2] for b in boxes), max(b[3] for b in boxes))
    out = []
    for f, m in zip(frames, masks):
        rgba = f.convert('RGBA')
        rgba.putalpha(Image.fromarray((m * 255).astype(np.uint8)))
        out.append(rgba.crop(box))
    print(f'walk from video: {count} frames, box {box}')
    return out


def shrink(im, size):
    """im resized with premultiplied alpha, so transparent pixels' colour
    doesn't bleed into the edges."""
    return im.convert('RGBa').resize(size, Image.LANCZOS).convert('RGBA')


def save_png(im, path):
    """A 128-colour palette PNG (with alpha): a fifth of the size of full colour."""
    im.quantize(colors=128, method=Image.Quantize.FASTOCTREE, dither=Image.Dither.NONE).save(path, optimize=True)


def main(sheet_path, preview_dir=None, walk=None):
    acts = actions_of(Image.open(sheet_path).convert('RGBA'))
    walk_h = sorted(f.size[1] for f in acts['walkB'])[1]
    scale = HEIGHT / walk_h
    strips = []
    for name, parts in ATLAS.items():
        if name == 'walk' and walk:
            vframes = walk_from_video(*walk)
            # as tall as the sheet's walking cat
            vs = HEIGHT / vframes[0].size[1]
            frames = [shrink(f, (round(f.size[0] * vs), HEIGHT)) for f in vframes]
        else:
            frames = [acts[row][i] for row, i in parts]
            frames = [f.resize((max(1, round(f.size[0] * scale)), max(1, round(f.size[1] * scale))), Image.LANCZOS)
                      for f in frames]
        # one size per action: feet on the bottom, nose (right edge) on the right
        fw, fh = max(f.size[0] for f in frames), max(f.size[1] for f in frames)
        strips.append((name, fw, fh, frames))
    atlas_w = max(fw * len(fr) for _, fw, _, fr in strips)
    atlas_h = sum(fh for _, _, fh, _ in strips)
    atlas = Image.new('RGBA', (atlas_w, atlas_h), (0, 0, 0, 0))
    meta = {'height': HEIGHT // 2, 'w': atlas_w, 'h': atlas_h, 'actions': {}}
    y = 0
    for name, fw, fh, frames in strips:
        pos = []
        for i, f in enumerate(frames):
            atlas.alpha_composite(f, (i * fw + fw - f.size[0], y + fh - f.size[1]))
            pos.append([i * fw, y])
        meta['actions'][name] = {'w': fw, 'h': fh, 'frames': pos}
        if name == 'walk':
            padded = []
            for f in frames:
                g = Image.new('RGBA', (fw, fh), (0, 0, 0, 0))
                g.alpha_composite(f, (fw - f.size[0], fh - f.size[1]))
                padded.append(g)
            # steps[i]: atlas px the body moves when frame i follows frame i-1
            steps = [locked_step(padded[i - 1], padded[i]) for i in range(len(padded))]
            print('walk steps (atlas px):', steps)
            if walk:
                # In the video the cat walks on a treadmill at an even pace, so
                # the paws on the ground move back evenly: the same step every
                # frame, the measured ones' mean, keeps them put without the
                # jitter of whole-pixel measurements. Frames are 1/24 s apart
                # in the video: ms is how long each is shown (before TEMPO).
                mean = sum(steps) / len(steps)
                steps = [round(mean, 2)] * len(steps)
                meta['actions'][name]['ms'] = round(1000 * walk[2] / 24)
                print(f'walk step {steps[0]} atlas px, {meta["actions"][name]["ms"]} ms a frame')
            meta['actions'][name]['steps'] = steps
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
    argv = sys.argv[1:]
    walk = None
    if '--walk' in argv:
        i = argv.index('--walk')
        if len(argv) < i + 5:
            sys.exit(__doc__)
        video, first, every, count = argv[i + 1:i + 5]
        walk = (video, int(first), int(every), int(count))
        del argv[i:i + 5]
    if len(argv) not in (1, 2):
        sys.exit(__doc__)
    main(*argv, walk=walk)
