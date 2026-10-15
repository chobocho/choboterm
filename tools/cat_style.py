"""Retarget the existing raster artwork to the stretch row's character.

No generated drawings are used. The filmed walking head keeps its viewing
angle, while legacy heads come from the artist's sheet. Open eyes are balanced
as a pair; the video's body and contact timing are kept.
Coordinates below are landmarks in the supplied, unscaled source artwork.
Requires numpy, Pillow and OpenCV in addition to make_cat.py's dependencies.
"""
import cv2
import numpy as np
from PIL import Image, ImageDraw, ImageFilter

from make_cat import scaled, trim


def polygon_mask(size, points):
    mask = Image.new('L', size)
    ImageDraw.Draw(mask).polygon(points, fill=255)
    return mask


def head_from_stand(stand, neck=False):
    # Keep the chin and whiskers; the cut follows the neck below the jaw.
    points = [(140, 0), (247, 0), (247, 96), (218, 111)]
    points += [(205, 150), (174, 140)] if neck else [(186, 111)]
    points += [(161, 99), (135, 84), (135, 36)]
    mask = polygon_mask(stand.size, points)
    result = stand.copy()
    result.putalpha(Image.fromarray(np.minimum(np.asarray(mask),
                                              np.asarray(stand.getchannel('A')))))
    return result


def closed_head(sit):
    mask = polygon_mask(sit.size, [(42, 0), (167, 0), (167, 100),
                                    (126, 112), (85, 112), (48, 89)])
    result = sit.copy()
    result.putalpha(Image.fromarray(np.minimum(np.asarray(mask),
                                              np.asarray(sit.getchannel('A')))))
    return trim(result)


def fur_centres(im):
    a = np.asarray(im.convert('RGBA'))
    hsv = cv2.cvtColor(a[..., :3], cv2.COLOR_RGB2HSV)
    mask = ((a[..., 3] > 240) & (hsv[..., 0] > 5) & (hsv[..., 0] < 29)
            & (hsv[..., 1] > 130) & (hsv[..., 2] > 95))
    samples = a[..., :3][mask].astype(np.float32)
    # A deterministic subset and seed keep rebuilds reproducible.
    samples = samples[::max(1, len(samples) // 12000)]
    cv2.setRNGSeed(0)
    _, _, centres = cv2.kmeans(samples, 3, None,
                              (cv2.TERM_CRITERIA_EPS | cv2.TERM_CRITERIA_MAX_ITER, 60, .05),
                              5, cv2.KMEANS_PP_CENTERS)
    return centres[np.argsort(centres @ np.array([.299, .587, .114]))]


def cream_centre(im):
    a = np.asarray(im.convert('RGBA'))
    hsv = cv2.cvtColor(a[..., :3], cv2.COLOR_RGB2HSV)
    mask = ((a[..., 3] > 240) & (hsv[..., 0] >= 10) & (hsv[..., 0] < 36)
            & (hsv[..., 1] > 35) & (hsv[..., 1] < 115) & (hsv[..., 2] > 155))
    return np.median(a[..., :3][mask], axis=0) if mask.sum() > 10 else np.array([248, 211, 153])


def eye_features(im):
    """Find the two whole eyes, including pupils, whites and dark outlines."""
    a = np.asarray(im.convert('RGBA'))
    hsv = cv2.cvtColor(a[..., :3], cv2.COLOR_RGB2HSV)
    iris = ((hsv[..., 0] >= 37) & (hsv[..., 0] < 100)
            & (hsv[..., 1] > 65) & (a[..., 3] > 180)).astype(np.uint8)
    _, labels, stats, _ = cv2.connectedComponentsWithStats(iris, 8)
    candidates = [i for i in range(1, len(stats)) if stats[i, 4] >= 8]
    candidates = sorted(candidates, key=lambda i: stats[i, 4], reverse=True)[:2]
    if len(candidates) != 2:
        return []  # closed eyes and a face turned fully away are intentional
    candidates.sort(key=lambda i: stats[i, 0])
    if stats[candidates[1], 0] - stats[candidates[0], 0] < 8:
        return []
    result = []
    for i in candidates:
        x, y, w, h, _ = stats[i]
        box = (max(0, int(x - .8 * w)), max(0, int(y - 1.6 * h)),
               min(im.width, int(x + 1.8 * w)), min(im.height, int(y + 1.3 * h)))
        hh = hsv[box[1]:box[3], box[0]:box[2]]
        seed = labels[box[1]:box[3], box[0]:box[2]] == i
        feature = (seed | (hh[..., 2] < 95)
                   | ((hh[..., 1] < 65) & (hh[..., 2] > 150))).astype(np.uint8)
        _, components, _, _ = cv2.connectedComponentsWithStats(feature, 8)
        overlap = np.bincount(components[seed], minlength=components.max() + 1)
        overlap[0] = 0
        mask = (components == overlap.argmax()).astype(np.uint8) * 255
        mask = cv2.dilate(mask, np.ones((3, 3), np.uint8))
        bounds = Image.fromarray(mask).getbbox()
        x0, y0, x1, y1 = bounds
        whole = (box[0] + x0, box[1] + y0, box[0] + x1, box[1] + y1)
        patch = im.crop(whole).copy()
        alpha = Image.fromarray(mask[y0:y1, x0:x1]).filter(ImageFilter.GaussianBlur(.5))
        patch.putalpha(alpha)
        result.append((whole, patch))
    return result


def balance_eyes(im):
    """Give both visible eyes one apparent size without altering closed poses.

    Reuse the larger eye's actual painted feature for both sides, at their
    average size. Inpaint the previous features before compositing so shrinking
    the near eye cannot leave a second outline or pupil behind it.
    """
    features = eye_features(im)
    if len(features) != 2:
        return im
    a = np.asarray(im.convert('RGBA')).copy()
    source = max(features, key=lambda f: f[1].width * f[1].height)[1]
    target = tuple(round(sum(f[1].size[k] for f in features) / 2) for k in range(2))
    mask = np.zeros(a.shape[:2], np.uint8)
    for (x0, y0, x1, y1), patch in features:
        mask[y0:y1, x0:x1] |= np.asarray(patch.getchannel('A'))
    mask = cv2.dilate((mask > 8).astype(np.uint8) * 255, np.ones((3, 3), np.uint8))
    a[..., :3] = cv2.inpaint(a[..., :3], mask, 3, cv2.INPAINT_TELEA)
    out = Image.fromarray(a, 'RGBA')
    eye = source.convert('RGBa').resize(target, Image.Resampling.LANCZOS).convert('RGBA')
    for (x0, y0, x1, y1), _ in features:
        x = round((x0 + x1 - target[0]) / 2)
        y = round((y0 + y1 - target[1]) / 2)
        # Keep the far eye inside the cheek contour instead of cropping it.
        for shift in range(max(1, round(target[0] * .35))):
            xx = x - shift
            if xx < 0 or y < 0 or xx + target[0] > im.width or y + target[1] > im.height:
                continue
            background = a[y:y + target[1], xx:xx + target[0], 3]
            solid = np.asarray(eye.getchannel('A')) > 180
            if np.mean(background[solid] > 128) > .995:
                x = xx
                break
        out.alpha_composite(eye, (x, y))
    out.putalpha(im.getchannel('A'))
    return out


class Palette:
    def __init__(self, reference):
        self.fur = fur_centres(reference)
        self.cream = cream_centre(reference)

    def match(self, im, source_fur=None, source_cream=None):
        """Match fur/stripes/cream separately; preserve eyes, mouth and pink ears.

        Smooth colour transforms retain the original shading and antialiasing,
        unlike independently quantizing each sprite to a new palette.
        """
        a = np.asarray(im.convert('RGBA')).copy()
        rgb = a[..., :3].astype(np.float32)
        hsv = cv2.cvtColor(a[..., :3], cv2.COLOR_RGB2HSV).astype(np.float32)
        src = fur_centres(im) if source_fur is None else source_fur
        cream = cream_centre(im) if source_cream is None else source_cream
        lum = rgb @ np.array([.299, .587, .114])
        knots = np.r_[0, src @ np.array([.299, .587, .114]), 255]
        delta = np.vstack([np.zeros(3), self.fur - src, self.fur[-1] - src[-1]])
        fur_delta = np.stack([np.interp(lum, knots, delta[:, c]) for c in range(3)], axis=-1)
        warm = ((hsv[..., 0] < 37) & (hsv[..., 2] > 20)).astype(np.float32)
        fur_weight = np.clip((hsv[..., 1] - 85) / 45, 0, 1) * warm
        cream_weight = np.clip((130 - hsv[..., 1]) / 45, 0, 1) * warm
        cream_weight *= np.clip((hsv[..., 2] - 95) / 75, 0, 1)
        rgb += fur_delta * fur_weight[..., None]
        rgb += (self.cream - cream) * cream_weight[..., None] * (1 - fur_weight[..., None])
        # The older keep sheet has yellow-green eyes. Bring them to the same teal.
        eye = (hsv[..., 0] >= 37) & (hsv[..., 0] < 100) & (hsv[..., 1] > 65)
        recoloured = cv2.cvtColor(np.clip(rgb, 0, 255).astype(np.uint8), cv2.COLOR_RGB2HSV)
        recoloured[..., 0][eye] = 81
        rgb[eye] = cv2.cvtColor(recoloured, cv2.COLOR_HSV2RGB)[eye]
        a[..., :3] = np.clip(rgb, 0, 255).astype(np.uint8)
        a[a[..., 3] == 0] = 0
        return balance_eyes(Image.fromarray(a, 'RGBA'))


def normalize_sheet(rows, palette, scale):
    paired_head = head_from_stand(palette.match(rows[1][0]))
    out = []
    for row_index, row in enumerate(rows[:5]):
        normal = []
        for i, frame in enumerate(row):
            frame = palette.match(trim(frame))
            if row_index == 4 and i < 3:
                # These three drawings are almost in profile and have only one
                # painted iris. Use the balanced reference face so the second
                # eye is actually visible instead of enlarging a nonexistent eye.
                left, anchor = [(135, 184), (183, 236), (190, 242)][i]
                a = np.asarray(frame).copy()
                # The larger reference face covers the old face completely.
                # Keep the shoulder/neck underneath; only old ear tips need removal.
                a[:34, left:] = 0
                canvas = Image.new('RGBA', (frame.width + 64, frame.height + 64))
                canvas.alpha_composite(Image.fromarray(a), (32, 32))
                canvas.alpha_composite(paired_head, (32 + anchor - 187, 34))
                frame = trim(canvas)
            if row_index == 3 and i == 6:
                # The drawn waking stand has an unusually long torso. Compress
                # its body alone so the face keeps the reference proportions.
                a = np.asarray(frame)
                yy = np.indices(a.shape[:2], dtype=np.float32)[0]
                seam, ratio = 204, .80
                width = round(frame.width - seam * (1 - ratio))
                map_x = np.arange(width, dtype=np.float32)
                map_x = np.where(map_x < seam * ratio, map_x / ratio,
                                 map_x + seam * (1 - ratio))
                frame = Image.fromarray(cv2.remap(a, np.broadcast_to(map_x, (frame.height, width)).copy(),
                                                  yy[:, :width], cv2.INTER_CUBIC))
            s = scale
            # Ear-to-floor height is stable for sitting and standing poses.
            sitting = (row_index == 0 or (row_index == 1 and i == 6)
                       or (row_index == 2 and i == 0)
                       or (row_index == 3 and i in (4, 5)))
            if sitting:
                s = 96 / frame.height
            elif row_index == 1 and i in (0, 1, 5):
                s = 96 / frame.height
            elif row_index == 3 and i == 6:
                s = 96 / frame.height
            elif row_index == 4 and i == 5:
                s = 96 / frame.height
            # These are the same curled pose, drawn at slightly different sizes.
            # Preserve a small breathing motion rather than 10% scale jumps.
            if (row_index == 2 and i >= 3) or (row_index == 3 and i == 0):
                target_w = 88 + ([0, .5, 1, .5][i - 3] if row_index == 2 else 0)
                s = target_w / frame.width
            normal.append(scaled(frame, s))
        out.append(normal)
    return out


def retarget_walk(frames, stand, palette, scale):
    """Use the filmed face angle, with balanced eyes and a reference-sized head.

    Body transforms and palette are shared by all frames. No per-frame crop or
    per-frame scale can make the cat grow/shrink or move its planted paws.
    Composition at 4x atlas resolution preserves subpixel head motion.
    """
    reference = np.asarray(frames[0].convert('RGB'))
    template = cv2.cvtColor(reference[60:245, 500:690], cv2.COLOR_RGB2GRAY)
    src_fur, src_cream = fur_centres(frames[0]), cream_centre(frames[0])
    # The video has the requested, more frontal viewing angle. Keep its muzzle,
    # ears and eye placement, but match the stretch cat's palette and head width.
    head = palette.match(frames[0], src_fur, src_cream)
    mask = polygon_mask(head.size, [
        (480, 45), (head.width, 45), (head.width, 245),
        (675, 267), (620, 275), (575, 272), (535, 255), (510, 239), (480, 212),
    ])
    alpha = np.minimum(np.asarray(head.getchannel('A')), np.asarray(mask))
    alpha = cv2.erode(alpha, np.ones((3, 3), np.uint8))
    head.putalpha(Image.fromarray(alpha).filter(ImageFilter.GaussianBlur(.45)))
    head = head.crop((480, 45, 705, 275))
    reference_width = head_from_stand(stand).getchannel('A').getbbox()
    head_scale = (reference_width[2] - reference_width[0]) * scale / head.width
    # Body length relative to the sheet's shorter, kitten-like torso.
    sy, sx = 96 / frames[0].height, 96 / frames[0].height * .9
    ss = 4
    rendered, tracks = [], []
    for frame in frames:
        gray = cv2.cvtColor(np.asarray(frame.convert('RGB')), cv2.COLOR_RGB2GRAY)
        search = gray[25:300, 465:frame.width]
        score = cv2.matchTemplate(search, template, cv2.TM_CCOEFF_NORMED)
        _, confidence, _, location = cv2.minMaxLoc(score)
        dx, dy = location[0] + 465 - 500, location[1] + 25 - 60
        tracks.append([dx, dy, round(confidence, 4)])
        body = palette.match(frame, src_fur, src_cream)
        remove = polygon_mask(body.size, [
            (480 + dx, 0), (body.width, 0), (body.width, 295 + dy),
            (640 + dx, 285 + dy), (580 + dx, 275 + dy),
            (530 + dx, 252 + dy), (495 + dx, 230 + dy), (480 + dx, 210 + dy),
        ])
        alpha = np.asarray(body.getchannel('A')).copy()
        alpha[np.asarray(remove) > 0] = 0
        # Remove the video's pale matte edge before reducing to application size.
        alpha = cv2.erode(alpha, np.ones((3, 3), np.uint8))
        body.putalpha(Image.fromarray(alpha).filter(ImageFilter.GaussianBlur(.45)))
        body = body.convert('RGBa').resize((round(body.width * sx * ss), round(body.height * sy * ss)),
                                          Image.Resampling.LANCZOS).convert('RGBA')
        canvas = Image.new('RGBA', (560, 460))
        origin = (24, 32)
        canvas.alpha_composite(body, origin)
        replacement = scaled(head, head_scale * ss)
        # The cropped video head's neck is (110, 229). A slightly lower body
        # anchor accommodates its taller face while retaining the existing scale.
        hx = origin[0] + round(((590 + dx) * sx - 110 * head_scale) * ss)
        hy = origin[1] + round(((285 + dy) * sy - 229 * head_scale) * ss)
        canvas.alpha_composite(replacement, (hx, hy))
        rendered.append(canvas)
    union = np.maximum.reduce([np.asarray(f.getchannel('A')) for f in rendered])
    box = Image.fromarray((union > 8).astype(np.uint8) * 255).getbbox()
    # Keep a common canvas for all 24 frames, including at the loop boundary.
    size = (round((box[2] - box[0]) / ss), round((box[3] - box[1]) / ss))
    return [f.crop(box).convert('RGBa').resize(size, Image.Resampling.LANCZOS).convert('RGBA')
            for f in rendered], tracks


def normalize_kept(keep, rows, palette, scale):
    """Keep the missing poses but replace their old, small heads with sheet art."""
    head = trim(head_from_stand(palette.match(rows[1][0]), neck=True))
    sleepy = closed_head(palette.match(rows[0][5]))
    result = {'lookback': [], 'roll': []}
    for i, frame in enumerate(keep['lookback']):
        if i == 2:
            # This is the facing-camera endpoint of the turn: use the same sitting cat.
            result['lookback'].append(scaled(palette.match(trim(rows[0][1])), 96 / trim(rows[0][1]).height))
            continue
        frame = palette.match(trim(frame))
        if i == 0:
            # The larger head completely covers the old face. Retain its neck
            # underneath so the turned shoulder never gains a rectangular hole.
            a = np.asarray(frame).copy()
            a[:34, 45:] = 0  # hide the old ears above the replacement forehead
            body = scaled(Image.fromarray(a), .83)
            canvas = Image.new('RGBA', (100, 110))
            canvas.alpha_composite(body, (0, 110 - body.height))
            face = scaled(head, scale)
            canvas.alpha_composite(face, (round(72 * .83 - face.width * .5), 110 - 96))
            frame = trim(canvas)
        else:
            frame = scaled(frame, 96 / frame.height)
        result['lookback'].append(frame)
    for i, frame in enumerate(keep['roll']):
        frame = palette.match(trim(frame))
        a = np.asarray(frame).copy()
        if i < 2:
            a[:, :43] = 0
        else:
            a[:, 129:] = 0
        body = scaled(Image.fromarray(a), .66)
        face = sleepy.transpose(Image.Transpose.ROTATE_90 if i < 2 else Image.Transpose.ROTATE_270)
        face = scaled(face, 47 / face.height)
        canvas = Image.new('RGBA', (150, 64))
        canvas.alpha_composite(body, (18, 64 - body.height))
        x = 4 if i < 2 else 18 + round(129 * .66) - 7
        canvas.alpha_composite(face, (x, 64 - face.height))
        result['roll'].append(trim(canvas))
    return result
