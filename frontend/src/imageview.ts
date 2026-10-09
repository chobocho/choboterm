// Image viewer for remote files. The browser engine (WebView2) decodes the
// formats itself, so nothing is added for JPG / PNG / GIF / BMP / WebP / SVG.

import {formatSize} from './files';

const $ = <T extends HTMLElement>(id: string) => document.getElementById(id) as T;

const overlay = $<HTMLDivElement>('imageOverlay');
const panel = $<HTMLDivElement>('imageView');
const title = $<HTMLSpanElement>('imgTitle');
const info = $<HTMLSpanElement>('imgInfo');
const stage = $<HTMLDivElement>('imgStage');
const img = $<HTMLImageElement>('imgEl');
const status = $<HTMLDivElement>('imgError');
const prevBtn = $<HTMLButtonElement>('imgPrev');
const nextBtn = $<HTMLButtonElement>('imgNext');

const TYPES: Record<string, string> = {
    jpg: 'image/jpeg', jpeg: 'image/jpeg', jfif: 'image/jpeg', png: 'image/png', gif: 'image/gif',
    bmp: 'image/bmp', webp: 'image/webp', svg: 'image/svg+xml', ico: 'image/x-icon', avif: 'image/avif',
};

function ext(name: string) {
    const i = name.lastIndexOf('.');
    return i < 0 ? '' : name.slice(i + 1).toLowerCase();
}

/** True if the image viewer can show a file with this name. */
export function isImage(name: string) {
    return ext(name) in TYPES;
}

export interface ImageSet {
    host: string;
    names: string[]; // the images in the folder, in list order
    index: number; // the one to show first
    sizes: number[];
    load: (name: string, size: number) => Promise<Uint8Array>;
    onClose?: () => void;
}

let set: ImageSet | undefined;
let index = 0;
let url = '';
let seq = 0; // the latest load; older results are dropped
let scale: number | 'fit' = 'fit';

export function imageOpen() {
    return !overlay.hidden;
}

export function openImages(s: ImageSet) {
    set = s;
    overlay.hidden = false;
    panel.focus();
    show(s.index);
}

export function closeImages() {
    if (!set) return;
    const done = set.onClose;
    set = undefined;
    seq++;
    overlay.hidden = true;
    img.removeAttribute('src');
    if (url) URL.revokeObjectURL(url);
    url = '';
    done?.();
}

async function show(i: number) {
    const s = set;
    if (!s || i < 0 || i >= s.names.length) return;
    index = i;
    const name = s.names[i];
    const my = ++seq;
    title.textContent = `${name} - ${s.host}`;
    info.textContent = `불러오는 중... · ${i + 1} / ${s.names.length}`;
    status.textContent = '';
    prevBtn.disabled = i === 0;
    nextBtn.disabled = i === s.names.length - 1;
    try {
        const data = await s.load(name, s.sizes[i]);
        if (my !== seq) return;
        const next = URL.createObjectURL(new Blob([data], {type: TYPES[ext(name)]}));
        img.onload = () => {
            if (my !== seq) return;
            scale = 'fit';
            paint();
        };
        img.onerror = () => {
            if (my !== seq) return;
            status.textContent = '이미지를 읽을 수 없습니다 (손상되었거나 지원하지 않는 형식)';
            info.textContent = `${formatSize(data.length)} · ${i + 1} / ${s.names.length}`;
        };
        img.src = next;
        if (url) URL.revokeObjectURL(url);
        url = next;
    } catch (e) {
        if (my !== seq) return;
        img.removeAttribute('src');
        status.textContent = String(e);
        info.textContent = `${i + 1} / ${s.names.length}`;
    }
}

function fitScale() {
    const w = img.naturalWidth || 1;
    const h = img.naturalHeight || 1;
    return Math.min(1, (stage.clientWidth - 2) / w, (stage.clientHeight - 2) / h);
}

function paint() {
    if (!set || !img.naturalWidth) return;
    const k = scale === 'fit' ? fitScale() : scale;
    img.style.width = `${Math.round(img.naturalWidth * k)}px`;
    img.style.height = `${Math.round(img.naturalHeight * k)}px`;
    stage.classList.toggle('pannable', stage.scrollWidth > stage.clientWidth || stage.scrollHeight > stage.clientHeight);
    const size = set.sizes[index];
    info.textContent = `${img.naturalWidth}×${img.naturalHeight} · ${formatSize(size)} · ${Math.round(k * 100)}%` +
        `${scale === 'fit' ? ' (맞춤)' : ''} · ${index + 1} / ${set.names.length}`;
}

/** Zooms by factor, keeping the point under (x, y) of the stage (default: its middle) in place. */
function zoom(factor: number, x = stage.clientWidth / 2, y = stage.clientHeight / 2) {
    if (!img.naturalWidth) return;
    const old = scale === 'fit' ? fitScale() : scale;
    const k = Math.max(0.02, Math.min(16, old * factor));
    const r = img.getBoundingClientRect();
    const s = stage.getBoundingClientRect();
    // The image point under the cursor, as a fraction of the image.
    const fx = (s.left + x - r.left) / r.width;
    const fy = (s.top + y - r.top) / r.height;
    scale = k;
    paint();
    const r2 = img.getBoundingClientRect();
    stage.scrollLeft += r2.left + fx * r2.width - (s.left + x);
    stage.scrollTop += r2.top + fy * r2.height - (s.top + y);
}

function setScale(k: number | 'fit') {
    scale = k;
    paint();
}

panel.addEventListener('keydown', ev => {
    if (!set) return;
    switch (ev.key) {
        case 'Escape':
        case 'F3':
            closeImages();
            break;
        case 'ArrowLeft':
        case 'PageUp':
        case 'Backspace':
            show(index - 1);
            break;
        case 'ArrowRight':
        case 'PageDown':
        case ' ':
            show(index + 1);
            break;
        case 'Home':
            show(0);
            break;
        case 'End':
            show(set.names.length - 1);
            break;
        case '+':
        case '=':
            zoom(1.25);
            break;
        case '-':
            zoom(0.8);
            break;
        case '0':
            setScale('fit');
            break;
        case '1':
            setScale(1);
            break;
        default:
            return;
    }
    ev.preventDefault();
    ev.stopPropagation();
});

// Ctrl+wheel zooms at the cursor; the plain wheel scrolls a big image.
stage.addEventListener('wheel', ev => {
    if (!ev.ctrlKey) return;
    ev.preventDefault();
    const s = stage.getBoundingClientRect();
    zoom(ev.deltaY < 0 ? 1.25 : 0.8, ev.clientX - s.left, ev.clientY - s.top);
}, {passive: false});

// Dragging moves a picture bigger than the window; a double-click switches fit ↔ 100%.
stage.addEventListener('mousedown', ev => {
    if (ev.button !== 0) return;
    ev.preventDefault();
    panel.focus();
    let x = ev.clientX;
    let y = ev.clientY;
    const move = (e: MouseEvent) => {
        stage.scrollLeft -= e.clientX - x;
        stage.scrollTop -= e.clientY - y;
        x = e.clientX;
        y = e.clientY;
    };
    const up = () => {
        window.removeEventListener('mousemove', move);
        window.removeEventListener('mouseup', up);
    };
    window.addEventListener('mousemove', move);
    window.addEventListener('mouseup', up);
});
stage.addEventListener('dblclick', () => setScale(scale === 'fit' ? 1 : 'fit'));

window.addEventListener('resize', () => scale === 'fit' && paint());

prevBtn.addEventListener('click', () => show(index - 1));
nextBtn.addEventListener('click', () => show(index + 1));
$<HTMLButtonElement>('imgFit').addEventListener('click', () => setScale('fit'));
$<HTMLButtonElement>('imgOrig').addEventListener('click', () => setScale(1));
$<HTMLButtonElement>('imgZoomIn').addEventListener('click', () => zoom(1.25));
$<HTMLButtonElement>('imgZoomOut').addEventListener('click', () => zoom(0.8));
$<HTMLButtonElement>('imgX').addEventListener('click', closeImages);
panel.querySelectorAll('button').forEach(b => b.addEventListener('mouseup', () => panel.focus()));
