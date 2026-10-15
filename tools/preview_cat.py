"""Render the actual atlas into review animations, a gallery and a local player.

python tools/preview_cat.py BEFORE.png BEFORE.json VIDEO.mp4 OUTPUT_DIR
Run make_cat.py first. Requires Pillow, numpy, OpenCV and ffmpeg.
The video is compared at frames 78..101, matching make_cat.py's selected stride.
"""
import json
import shutil
import subprocess
import sys
from pathlib import Path

import cv2
import numpy as np
from PIL import Image, ImageDraw, ImageFont

from make_cat import locked_step, scaled


ROOT = Path(__file__).resolve().parent.parent
LABELS = dict(walk='걷기', sit='앉기·하품', stretch='기지개', doze='잠들기',
              wake='일어나기', jump='점프', lookback='뒤돌아보기', roll='구르기')
FONT_PATH = next((str(p) for p in map(Path, [
    'C:/Windows/Fonts/malgun.ttf',
    '/usr/share/fonts/truetype/nanum/NanumGothic.ttf',
    '/usr/share/fonts/opentype/noto/NotoSansCJK-Regular.ttc',
    '/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf',
]) if p.exists()), None)
FONT = ImageFont.truetype(FONT_PATH, 22) if FONT_PATH else ImageFont.load_default()
SMALL = ImageFont.truetype(FONT_PATH, 16) if FONT_PATH else ImageFont.load_default()
BG = (27, 29, 34, 255)


def sprites(image, meta):
    atlas = Image.open(image).convert('RGBA')
    assert atlas.size == (meta['w'], meta['h'])
    out = {}
    for name, action in meta['actions'].items():
        fs = []
        for x, y in action['frames']:
            assert 0 <= x <= atlas.width - action['w']
            assert 0 <= y <= atlas.height - action['h']
            f = atlas.crop((x, y, x + action['w'], y + action['h']))
            assert f.getchannel('A').getbbox(), (name, x, y)
            fs.append(f)
        out[name] = fs
    assert len(out['walk']) == 24
    return out


def text(im, xy, value, small=False):
    ImageDraw.Draw(im).text(xy, value, fill=(225, 228, 234), font=SMALL if small else FONT)


def at_floor(canvas, frame, x, floor, factor=2):
    frame = scaled(frame, factor)
    canvas.alpha_composite(frame, (round(x), round(floor - frame.height)))


def video_samples(video):
    cap = cv2.VideoCapture(str(video))
    cap.set(cv2.CAP_PROP_POS_FRAMES, 78)
    frames = []
    for _ in range(24):
        ok, frame = cap.read()
        assert ok, 'Could not read reference walking cycle'
        frames.append(Image.fromarray(cv2.cvtColor(frame, cv2.COLOR_BGR2RGB)))
    cap.release()
    return frames


def encode_mp4(frames, path, fps):
    w, h = frames[0].size
    args = ['ffmpeg', '-y', '-v', 'error', '-f', 'rawvideo', '-pix_fmt', 'rgb24',
            '-s', f'{w}x{h}', '-r', str(fps), '-i', '-', '-an', '-c:v', 'libx264',
            '-crf', '18', '-pix_fmt', 'yuv420p', '-movflags', '+faststart', str(path)]
    process = subprocess.Popen(args, stdin=subprocess.PIPE)
    try:
        for frame in frames:
            process.stdin.write(frame.convert('RGB').tobytes())
    finally:
        process.stdin.close()
    if process.wait():
        raise RuntimeError(f'ffmpeg could not encode {path}')


def gif(frames, path, ms):
    # One shared palette prevents palette changes from introducing colour flicker.
    sample = Image.new('RGB', (frames[0].width, frames[0].height * 8))
    for i, frame in enumerate(frames[::max(1, len(frames) // 8)][:8]):
        sample.paste(frame.convert('RGB'), (0, i * frame.height))
    palette = sample.quantize(colors=256)
    fs = [f.convert('RGB').quantize(palette=palette, dither=Image.Dither.NONE) for f in frames]
    fs[0].save(path, save_all=True, append_images=fs[1:], loop=0,
               duration=ms, optimize=False, disposal=2)


def main(before_png, before_json, video, output):
    output = Path(output)
    output.mkdir(parents=True, exist_ok=True)
    meta = json.loads((ROOT / 'frontend/src/catframes.json').read_text())
    oldmeta = json.loads(Path(before_json).read_text())
    frames = sprites(ROOT / 'frontend/src/assets/cat.png', meta)
    before = sprites(before_png, oldmeta)
    filmed = video_samples(video)
    shutil.copyfile(ROOT / 'frontend/src/assets/cat.png', output / 'cat.png')
    if Path(before_png).resolve() != (output / 'before.png').resolve():
        shutil.copyfile(before_png, output / 'before.png')

    # All poses, on a common baseline and scale, without overlapping cells.
    gallery = Image.new('RGBA', (1680, 8 * 270), BG)
    for r, (name, fs) in enumerate(frames.items()):
        indices = range(len(fs)) if name != 'walk' else [0, 3, 6, 9, 12, 18, 23]
        for col, i in enumerate(indices):
            x, y = col * 240, r * 270
            text(gallery, (x + 8, y + 6), f'{LABELS[name]} {i + 1}', True)
            at_floor(gallery, fs[i], x + 234 - fs[i].width * 1.8, y + 244, 1.8)
    gallery.convert('RGB').save(output / 'all-poses.png')

    contact = Image.new('RGBA', (1440, 4 * 260), BG)
    for i, frame in enumerate(frames['walk']):
        x, y = (i % 6) * 240, (i // 6) * 260
        text(contact, (x + 8, y + 5), f'{i + 1:02} / 24', True)
        at_floor(contact, frame, x + 235 - frame.width * 1.8, y + 240, 1.8)
    contact.convert('RGB').save(output / 'walk-24-frames.png')

    # Original video and reference character above; actual travelling atlases below.
    comparisons = []
    oldstep = oldmeta['actions']['walk']['steps'][0]
    step = meta['actions']['walk']['steps'][0]
    for k in range(72):
        i = k % 24
        im = Image.new('RGBA', (1280, 800), BG)
        text(im, (22, 14), '원본 영상 · 발 움직임 기준')
        text(im, (660, 14), '기지개 줄 · 외형 기준')
        ref = filmed[i].crop((196, 77, 941, 669)).resize((377, 300), Image.Resampling.LANCZOS)
        im.paste(ref, (132, 50))
        at_floor(im, frames['stretch'][0], 840, 350, 3)
        text(im, (22, 385), '수정 전')
        text(im, (660, 385), '수정 후 · 실제 24프레임')
        draw = ImageDraw.Draw(im)
        for x in range(20, 1260, 40):
            draw.line((x, 740, x, 750), fill=(92, 99, 110), width=2)
        draw.line((20, 740, 620, 740), fill=(92, 99, 110))
        draw.line((660, 740, 1260, 740), fill=(92, 99, 110))
        # Three strides fit in each lane. Coordinates and step come from the app metadata.
        at_floor(im, before['walk'][i], 24 + k * oldstep * 1.7, 738, 1.7)
        at_floor(im, frames['walk'][i], 664 + k * step * 1.7, 738, 1.7)
        text(im, (22, 770), f'프레임 {i + 1:02}/24 · 원본과 동일한 24fps', True)
        comparisons.append(im)
    encode_mp4(comparisons, output / 'walk-comparison.mp4', 24)
    gif(comparisons, output / 'walk-comparison.gif', [40, 40, 40, 40, 40, 50] * 12)
    comparisons[0].convert('RGB').save(output / 'before-after.png')

    # A moving preview and transitions assembled from the same atlas as the app.
    routines = [
        ('stretch', [0, 1, 2, 3, 3, 3, 4, 5, 6]),
        ('doze', [0, 1, 2, 3, 4, 5, 6, 5, 4]),
        ('wake', list(range(7))),
        ('jump', list(range(6))),
        ('lookback', [2, 1, 0, 1, 2]),
        ('roll', [0, 1, 2, 1, 0]),
        ('sit', list(range(7))),
    ]
    action_frames = []
    for name, indices in routines:
        for i in indices:
            im = Image.new('RGBA', (640, 400), BG)
            text(im, (20, 15), LABELS[name])
            at_floor(im, frames[name][i], 390 - frames[name][i].width * 3, 365, 3)
            ImageDraw.Draw(im).line((20, 366, 620, 366), fill=(92, 99, 110))
            action_frames.append(im)
    gif(action_frames, output / 'all-actions.gif', 220)
    encode_mp4([f for f in action_frames for _ in range(5)], output / 'all-actions.mp4', 24)

    # Extract the unchanged filmed stride for the synchronised local player.
    subprocess.run(['ffmpeg', '-y', '-v', 'error', '-i', str(video), '-vf',
                    'trim=start_frame=78:end_frame=102,setpts=N/(24*TB)', '-an',
                    '-r', '24', '-fps_mode', 'cfr', '-frames:v', '24',
                    '-c:v', 'libx264', '-crf', '18', '-pix_fmt', 'yuv420p',
                    '-movflags', '+faststart', str(output / 'reference.mp4')], check=True)
    html = '''<!doctype html><html lang="ko"><meta charset="utf-8">
<title>고양이 애니메이션 비교</title><style>
body{background:#1b1d22;color:#e1e4ea;font:16px system-ui;margin:24px;max-width:1280px}
label,button{margin-right:16px}select,button{font:inherit;padding:5px}canvas{width:100%;border:1px solid #505762}
video{width:420px;max-width:100%}a{color:#8bc7ff}p{line-height:1.6}</style>
<h1>고양이 애니메이션 비교</h1><p>걷기는 원본 영상과 같은 프레임을 재생합니다. 다른 동작도 선택해 크기와 색감을 비교할 수 있습니다.</p>
<p><label>동작 <select id="act"></select></label><label>속도 <select id="speed"><option value="1">정상</option><option value="0.25">느리게</option><option value="2">빠르게</option></select></label><button id="pause">일시정지</button></p>
<canvas id="screen" width="1280" height="380"></canvas><p>원본 영상</p><video id="ref" src="reference.mp4" muted loop autoplay controls></video>
<p><a href="walk-comparison.mp4">걷기 비교 영상</a> · <a href="all-actions.mp4">전체 동작 영상</a> · <a href="all-poses.png">전체 스프라이트</a> · <a href="walk-24-frames.png">걷기 24프레임</a></p>
<script>
const meta=__META__,old=__OLD__,names=__NAMES__;
const png=new Image(),before=new Image();png.src='cat.png';before.src='before.png';
const act=document.querySelector('#act'),speed=document.querySelector('#speed'),ref=document.querySelector('#ref'),screen=document.querySelector('#screen'),ctx=screen.getContext('2d');
for(const name of Object.keys(meta.actions)){const o=document.createElement('option');o.value=name;o.textContent=names[name];act.append(o)}
let paused=false,elapsed=0,last=performance.now();
document.querySelector('#pause').onclick=e=>{paused=!paused;e.target.textContent=paused?'재생':'일시정지';if(paused)ref.pause();else ref.play()};
speed.onchange=()=>ref.playbackRate=Number(speed.value);
act.onchange=()=>{elapsed=0};
function draw(atlas,m,label,x,i){const a=m.actions[act.value],p=a.frames[i%a.frames.length],s=2.5;
ctx.fillStyle='#e1e4ea';ctx.font='20px system-ui';ctx.fillText(label,x+18,32);
ctx.drawImage(atlas,p[0],p[1],a.w,a.h,x+600-a.w*s,340-a.h*s,a.w*s,a.h*s)}
function tick(now){if(!paused)elapsed+=(now-last)*Number(speed.value);last=now;
const i=act.value==='walk'?Math.min(23,Math.floor(ref.currentTime*24)):Math.floor(elapsed/300);
ctx.fillStyle='#1b1d22';ctx.fillRect(0,0,1280,380);ctx.strokeStyle='#505762';ctx.beginPath();ctx.moveTo(0,341);ctx.lineTo(1280,341);ctx.stroke();
if(png.complete&&before.complete){draw(before,old,'수정 전',0,i);draw(png,meta,'수정 후',640,i);screen.dataset.frame=String(i)}requestAnimationFrame(tick)}requestAnimationFrame(tick);
</script></html>'''
    html = html.replace('__META__', json.dumps(meta)).replace('__OLD__', json.dumps(oldmeta))
    html = html.replace('__NAMES__', json.dumps(LABELS, ensure_ascii=False))
    (output / 'index.html').write_text(html, encoding='utf-8')

    measurements = [locked_step(frames['walk'][i - 1], f) for i, f in enumerate(frames['walk'])]
    report = {
        'frames': {name: len(fs) for name, fs in frames.items()},
        'reference': 'stretch:0', 'walking_fps': 24,
        'walk_step_atlas_px': step,
        'contact_step_std_screen_px': round(float(np.std(measurements)) / 2, 3),
        'contact_step_max_residual_screen_px': round(max(abs(v - step) for v in measurements) / 2, 3),
        'walk_loop_step_screen_px': measurements[0] / 2,
        'sitting_heights_atlas_px': [f.height for f in frames['sit']],
        'standing_height_atlas_px': frames['stretch'][0].getchannel('A').getbbox()[3]
                                   - frames['stretch'][0].getchannel('A').getbbox()[1],
        'art_method': 'existing raster composites and colour/geometry correction; no AI image generation',
    }
    (output / 'validation.json').write_text(json.dumps(report, ensure_ascii=False, indent=2), encoding='utf-8')
    print(json.dumps(report, ensure_ascii=False, indent=2))


if __name__ == '__main__':
    if len(sys.argv) != 5:
        sys.exit(__doc__)
    main(*sys.argv[1:])
