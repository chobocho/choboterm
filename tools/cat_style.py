"""Retarget the existing raster artwork to the stretch row's character.

No generated drawings are used: the walking and legacy heads are composited
from the artist's sheet, while the video's body and contact timing are kept.
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
        return Image.fromarray(a, 'RGBA')


def normalize_sheet(rows, palette, scale):
    out = []
    for row_index, row in enumerate(rows[:5]):
        normal = []
        for i, frame in enumerate(row):
            frame = palette.match(trim(frame))
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
    """Use one reference head, tracking the filmed neck through the whole cycle.

    Body transforms and palette are shared by all frames. No per-frame crop or
    per-frame scale can make the cat grow/shrink or move its planted paws.
    Composition at 4x atlas resolution preserves subpixel head motion.
    """
    reference = np.asarray(frames[0].convert('RGB'))
    template = cv2.cvtColor(reference[60:245, 500:690], cv2.COLOR_RGB2GRAY)
    src_fur, src_cream = fur_centres(frames[0]), cream_centre(frames[0])
    head = head_from_stand(palette.match(stand))
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
        replacement = scaled(head, scale * ss)
        # Neck anchors: (187, 109) on the sheet; (590, 274) in video frame 78.
        hx = origin[0] + round(((590 + dx) * sx - 187 * scale) * ss)
        hy = origin[1] + round(((274 + dy) * sy - 109 * scale) * ss)
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
