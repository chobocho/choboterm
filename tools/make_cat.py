"""Builds the cat's sprites from the artist's sheet and a video of it walking.

SHEET: the cute cheese cat on a dark background, in seven rows:
  0 sit: looking about, up, yawning, eyes shut (7 frames)
  1 stretch: stand, stretch down, stand, sit down (7)
  2 doze: sit, nod off, curl up; 3-6 asleep, breathing (7)
  3 wake: curled up, look up, lift the head, sit, stand (7)
  4 jump: crouch staring, leap, fly, land, stand (6)
  5, 6 a walk of 8 frames: not used. Its paws on the ground don't stay put
     from frame to frame in any order of the 8 (the steps that would keep
     them put range from 1 to 18 px), and the tail is up in three frames
     and down in five, so it flicks.
VIDEO: the same cat walking in place on a grey background; the walk is
  COUNT frames from frame FIRST (0-based), every EVERY frames: one stride.
  In the video of 2026-10 (24 fps) frames 78-101 are one stride (1 s; frame
  102 looks like frame 78), with the paws on the ground moving back evenly.
tools/cat_keep.png: what the new sheet has no frames for, kept from the cat
  of v0.2.31 (its atlas, drawn from an older sheet in the same style):
  looking back (sat turned away, the back, sat facing) and rolling over
  showing the belly, then a sitting cat to size them by.

Sheet frames are cut out of the near-black background (flood-filled from
the border, the edge fading by brightness), cats joined by a whisker kept
apart. The video's cat is cut out of the grey by its colour; holes in it
are filled only when small (eye glints), not the grey between the legs.
The stretch row defines the character's colour and proportions. Its standing
cat is 96 px tall (twice its 48 px on screen). tools/cat_style.py matches every
pose to it. The walking head retains the video's viewing angle with balanced
eyes; legacy heads use the sheet's artwork. The filmed legs and their contact
timing are retained. Every frame
stands on the bottom of its action's box with its nose on the right edge:
the app keeps the nose put from frame to frame (the sheet's frames sit
loosely in their cells, so where they were drawn says nothing).

Usage: python tools/make_cat.py SHEET.png VIDEO FIRST EVERY COUNT [PREVIEW_DIR]
  e.g. python tools/make_cat.py sheet.png walk.mp4 78 1 24 preview
Writes frontend/src/assets/cat.png and frontend/src/catframes.json, and in
PREVIEW_DIR the atlas on dark, walk.gif (the walk as the app moves it, next
to the video's frames as filmed) and acts.gif (every action), to look at
before trying it in the app. Requires Pillow, numpy, OpenCV and ffmpeg.
"""
import json
import os
import subprocess
import sys
import tempfile
from collections import deque

import numpy as np
from PIL import Image, ImageDraw, ImageFilter

HEIGHT = 96  # the walking cat, in atlas px; shown at half size, so sharp on HiDPI too
ATLAS_MAX_W = 2048  # an action's frames wrap onto more lines past this
ROOT = os.path.join(os.path.dirname(os.path.abspath(__file__)), '..')
OUT_PNG = os.path.join(ROOT, 'frontend', 'src', 'assets', 'cat.png')
OUT_JSON = os.path.join(ROOT, 'frontend', 'src', 'catframes.json')
N4 = ((1, 0), (-1, 0), (0, 1), (0, -1))
ACTS = ['sit', 'stretch', 'doze', 'wake', 'jump']  # the sheet's rows 0-4
ROW_SIZES = [7, 7, 7, 7, 6, 4, 4]
KEEP_PNG = os.path.join(ROOT, 'tools', 'cat_keep.png')
# cat_keep.png, left to right: (action, frame width, height, count), each
# frame on the image's bottom, its nose on its right edge
KEEP = [('lookback', 127, 130, 3), ('roll', 173, 70, 3), ('sitref', 114, 108, 1)]


# ---- The sheet ----

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


def sheet_rows(sheet):
    """The sheet's cats, row by row (a new row where the vertical centres
    jump by more than 100 px), each left to right."""
    found = split_blobs(cut_dark(sheet), 1500)
    found.sort(key=lambda b: (b[0][1] + b[0][3]) / 2)
    rows, last = [], None
    for box, sp in found:
        c = (box[1] + box[3]) / 2
        if last is None or c - last > 100:
            rows.append([])
        rows[-1].append((box, sp))
        last = c
    if [len(r) for r in rows] != ROW_SIZES:
        sys.exit(f'unexpected frames per row: {[len(r) for r in rows]}, want {ROW_SIZES}')
    return [[sp for _, sp in sorted(r, key=lambda b: b[0][0])] for r in rows]


# ---- The video ----

def video_frames(video, first, every, count):
    """count frames of video from frame first (0-based), every every frames, through ffmpeg."""
    want = [first + i * every for i in range(count)]
    sel = '+'.join(f'eq(n\\,{n})' for n in want)
    with tempfile.TemporaryDirectory() as d:
        subprocess.run(['ffmpeg', '-v', 'error', '-i', video, '-vf', f"select='{sel}'",
                        '-fps_mode', 'passthrough', os.path.join(d, 'f%03d.png')], check=True)
        return [Image.open(os.path.join(d, f'f{i + 1:03d}.png')).convert('RGB') for i in range(count)]


def video_mask(frame, max_hole=0.004):
    """The cat in a video frame: saturated (orange, brown, cream, green, pink)
    or dark (the outline) pixels. The background, its shadow and the
    watermark are grey and drop out; so do the outline's light anti-aliased
    edge pixels, which would leave a grey fringe. Holes inside the cat are
    filled only when small (eye glints, under max_hole of its area): grey
    closed in between the legs stays background."""
    a = np.asarray(frame, dtype=np.float32) / 255
    mx, mn = a.max(2), a.min(2)
    cat = ((mx - mn) / (mx + 1e-6) > 0.18) | (mx < 0.22)
    # (copy: an image made by fromarray shares the array's read-only buffer,
    # and floodfill silently does nothing on it)
    work = Image.fromarray(np.where(cat, 0, 255).astype(np.uint8)).copy()
    ImageDraw.floodfill(work, (0, 0), 128)
    holes = np.asarray(work) == 255
    area = cat.sum()
    seen = np.zeros_like(holes)
    for y, x in zip(*np.nonzero(holes)):
        if seen[y, x]:
            continue
        ImageDraw.floodfill(work, (int(x), int(y)), 77)
        m = np.asarray(work) == 77
        seen |= m
        if m.sum() <= max_hole * area:
            cat |= m
        work.paste(Image.fromarray(np.where(m, 1, np.asarray(work)).astype(np.uint8)))
    return cat


def walk_from_video(video, first, every, count):
    """The walk's frames, all cropped to one box (the cat walks in place: its
    nose stays put, the legs move), and the frames as filmed, for the preview."""
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
    return out, [f.crop((box[0] - 20, box[1] - 10, box[2] + 20, box[3] + 30)) for f in frames]


# ---- Measuring ----

def kept():
    """{action: [frames]} from cat_keep.png."""
    im = Image.open(KEEP_PNG).convert('RGBA')
    out, x = {}, 0
    for name, w, h, n in KEEP:
        out[name] = [im.crop((x + i * w, im.size[1] - h, x + (i + 1) * w, im.size[1])) for i in range(n)]
        x += w * n
    return out


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
    shift that best overlaps their ground runs."""
    ra, rb = ground_runs(a), ground_runs(b)
    best = None
    for s in range(-2, 71):
        sc = sum(max(0, min(a1, b1 + s) - max(a0, b0 + s) + 1) for a0, a1 in ra for b0, b1 in rb)
        if best is None or sc > best[0] or (sc == best[0] and abs(s) < abs(best[1])):
            best = (sc, s)
    return best[1]


def to_ears(f):
    """Height from the feet to the ear tips: the topmost opaque pixel in the
    right 40% (the head; the tail is on the left)."""
    al = np.asarray(f.getchannel('A'))[:, int(f.size[0] * 0.6):] > 128
    rows = np.nonzero(al.any(1))[0]
    return f.size[1] - int(rows[0])


def trim(f):
    """f cropped to its opaque pixels (alpha over 8)."""
    box = Image.fromarray((np.asarray(f.getchannel('A')) > 8).astype(np.uint8) * 255).getbbox()
    return f.crop(box) if box else f


def scaled(f, s):
    """f scaled by s, with premultiplied alpha so no dark fringe bleeds in."""
    return f.convert('RGBa').resize((max(1, round(f.size[0] * s)), max(1, round(f.size[1] * s))),
                                    Image.LANCZOS).convert('RGBA')


def save_png(im, path):
    """Keep shared RGB colours and full edge alpha; avoid palette banding."""
    im.save(path, optimize=True)


# ---- Previews ----

def save_gif(frames, path, ms):
    frames = [f.convert('RGB') for f in frames]
    frames[0].save(path, save_all=True, append_images=frames[1:], duration=ms, loop=0, optimize=True)


def walk_preview(frames, step, filmed, ms, path):
    """The walk as the app moves it (step atlas px a frame, at twice the
    screen size) on a dark lane with ticks every 40 px, so sliding paws
    show, below the video's frames as filmed. Three strides."""
    w, h = frames[0].size
    fw = round(filmed[0].size[0] * h / filmed[0].size[1])
    width = max(3 * len(frames) * step + w + 20, fw + 20)
    out = []
    for k in range(3 * len(frames)):
        i = k % len(frames)
        bg = Image.new('RGBA', (int(width), 2 * h + 30), (24, 24, 28, 255))
        bg.paste(filmed[i].resize((fw, h), Image.LANCZOS).convert('RGBA'), (10, 4))
        for t in range(0, int(width), 40):
            bg.paste((70, 70, 80, 255), (t, 2 * h + 13, t + 2, 2 * h + 19))
        bg.alpha_composite(frames[i], (int(round(10 + (k + 1) * step)), h + 13))
        out.append(bg)
    save_gif(out, path, ms)


def acts_preview(strips, path):
    """Every action of the sheet, one after the other, nose put, 220 ms a frame."""
    w = max(fw for _, fw, _, _ in strips) + 20
    h = max(fh for _, _, fh, _ in strips) + 20
    out = []
    for name, fw, fh, frames in strips:
        if name == 'walk':
            continue
        for f in frames:
            bg = Image.new('RGBA', (w, h), (24, 24, 28, 255))
            bg.alpha_composite(f, (w - 10 - fw, h - 10 - fh))
            ImageDraw.Draw(bg).text((4, 2), name, fill=(200, 200, 90, 255))
            out.append(bg)
    save_gif(out, path, 220)


def main(sheet_path, video, first, every, count, preview_dir=None):
    from cat_style import Palette, normalize_kept, normalize_sheet, retarget_walk

    rows = sheet_rows(Image.open(sheet_path))
    vframes, filmed = walk_from_video(video, first, every, count)
    stand = trim(rows[1][0])
    sa = HEIGHT / stand.height
    palette = Palette(stand)
    walk, tracks = retarget_walk(vframes, stand, palette, sa)
    normalized = normalize_sheet(rows, palette, sa)
    keep = normalize_kept(kept(), rows, palette, sa)
    sources = list(zip(ACTS, normalized))
    sources += [(name, keep[name]) for name in ('lookback', 'roll')]
    print(f'stretch reference scale {sa:.3f}; walking head tracking {tracks}')

    strips = [('walk', walk[0].size[0], walk[0].size[1], walk)]
    for name, fs in sources:
        fw, fh = max(f.size[0] for f in fs), max(f.size[1] for f in fs)
        padded = []
        for f in fs:
            g = Image.new('RGBA', (fw, fh), (0, 0, 0, 0))
            g.alpha_composite(f, (fw - f.size[0], fh - f.size[1]))  # feet down, nose right
            padded.append(g)
        strips.append((name, fw, fh, padded))

    # Each action on its own lines, wrapped at ATLAS_MAX_W.
    layout, y, atlas_w = [], 0, 0
    for name, fw, fh, frames in strips:
        per = max(1, ATLAS_MAX_W // fw)
        pos = [[(i % per) * fw, y + (i // per) * fh] for i in range(len(frames))]
        layout.append(pos)
        atlas_w = max(atlas_w, min(per, len(frames)) * fw)
        y += -(-len(frames) // per) * fh
    atlas = Image.new('RGBA', (atlas_w, y), (0, 0, 0, 0))
    meta = {'height': HEIGHT // 2, 'w': atlas_w, 'h': y, 'actions': {}}
    for (name, fw, fh, frames), pos in zip(strips, layout):
        for f, (px, py) in zip(frames, pos):
            atlas.alpha_composite(f, (px, py))
        meta['actions'][name] = {'w': fw, 'h': fh, 'frames': pos}

    # The walk: steps[i] is how far the body moves (atlas px) as frame i comes
    # up, keeping the paws on the ground put. The cat walks in place on a
    # treadmill at an even pace, so the paws on the ground move back evenly:
    # the same step every frame, the measured ones' mean, keeps them put
    # without the jitter of whole-pixel measurements.
    steps = [locked_step(walk[i - 1], walk[i]) for i in range(len(walk))]
    mean = round(sum(steps) / len(steps), 2)
    ms = round(1000 * every / 24)  # the video's frames are 1/24 s apart
    meta['actions']['walk'].update(steps=[mean] * len(walk), ms=ms)
    print(f'walk steps (atlas px): {steps}; mean {mean}, sd {np.std(steps):.2f}; {ms} ms a frame')

    os.makedirs(os.path.dirname(OUT_PNG), exist_ok=True)
    save_png(atlas, OUT_PNG)
    with open(OUT_JSON, 'w') as f:
        json.dump(meta, f, separators=(',', ':'))
        f.write('\n')
    print(f'cat.png {atlas_w}x{y}, {os.path.getsize(OUT_PNG) // 1024} KB; '
          + ', '.join(f'{n} {len(a["frames"])}' for n, a in meta['actions'].items()))
    if preview_dir:
        os.makedirs(preview_dir, exist_ok=True)
        bg = Image.new('RGBA', atlas.size, (12, 12, 12, 255))
        bg.alpha_composite(atlas)
        bg.convert('RGB').save(os.path.join(preview_dir, 'atlas_dark.png'))
        walk_preview(walk, mean, filmed, ms, os.path.join(preview_dir, 'walk.gif'))
        acts_preview(strips, os.path.join(preview_dir, 'acts.gif'))
        for name, _, _, frames in strips:
            path = os.path.join(preview_dir, 'frames', name)
            os.makedirs(path, exist_ok=True)
            for i, frame in enumerate(frames):
                frame.save(os.path.join(path, f'{i:02}.png'))
        with open(os.path.join(preview_dir, 'head-tracking.json'), 'w') as f:
            json.dump(tracks, f, indent=2)


if __name__ == '__main__':
    if len(sys.argv) not in (6, 7):
        sys.exit(__doc__)
    a = sys.argv[1:]
    main(a[0], a[1], int(a[2]), int(a[3]), int(a[4]), *a[5:])
