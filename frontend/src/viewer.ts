// Read-only text viewer for remote files (CodeMirror 6).
// The raw bytes are kept, so the encoding can be switched without downloading again.

import {Compartment, EditorState} from '@codemirror/state';
import {
    drawSelection,
    EditorView,
    highlightActiveLine,
    highlightActiveLineGutter,
    highlightSpecialChars,
    keymap,
    lineNumbers,
} from '@codemirror/view';
import {defaultKeymap} from '@codemirror/commands';
import {highlightSelectionMatches, search, searchKeymap} from '@codemirror/search';
import {TextSaveAs} from '../wailsjs/go/main/App';
import {formatSize} from './files';

const $ = <T extends HTMLElement>(id: string) => document.getElementById(id) as T;

const overlay = $<HTMLDivElement>('viewerOverlay');
const panel = $<HTMLDivElement>('viewer');
const title = $<HTMLSpanElement>('viewerTitle');
const encSelect = $<HTMLSelectElement>('vEnc');
const wrapBox = $<HTMLInputElement>('vWrap');
const info = $<HTMLSpanElement>('vInfo');
const textEl = $<HTMLDivElement>('vText');
const status = $<HTMLDivElement>('vError');
const saveEnc = $<HTMLSelectElement>('vSaveEnc');

export type Encoding = 'UTF-8' | 'EUC-KR';

export interface ViewFile {
    name: string;
    path: string;
    size: number; // size of the whole remote file
    data: Uint8Array; // up to the viewer limit
    truncated: boolean;
    onClose?: () => void;
}

let file: ViewFile | undefined;
let detected: Encoding = 'UTF-8';
let wrapping = false;

const wrap = new Compartment();

// Korean labels for the search panel.
const phrases = {
    'Find': '찾을 내용',
    'next': '다음',
    'previous': '이전',
    'all': '모두',
    'match case': '대소문자 구분',
    'regexp': '정규식',
    'by word': '단어 단위',
    'close': '닫기',
    'Go to line': '줄 번호',
    'go': '이동',
};

const view = new EditorView({
    parent: textEl,
    state: EditorState.create({extensions: extensions()}),
});

function extensions() {
    return [
        lineNumbers(),
        highlightActiveLineGutter(),
        highlightActiveLine(),
        highlightSpecialChars(),
        drawSelection(),
        search({top: true}),
        highlightSelectionMatches(),
        keymap.of([...searchKeymap, ...defaultKeymap]),
        EditorState.readOnly.of(true),
        EditorState.phrases.of(phrases),
        EditorState.tabSize.of(8),
        wrap.of(wrapping ? EditorView.lineWrapping : []),
    ];
}

export function viewerOpen() {
    return !overlay.hidden;
}

/** Drops a UTF-8 sequence cut off at the end (the file was loaded only partly). */
function trimCutUTF8(b: Uint8Array): Uint8Array {
    for (let i = 1; i <= Math.min(3, b.length); i++) {
        const c = b[b.length - i];
        if ((c & 0xc0) !== 0x80) {
            // Lead byte: how long should its sequence be?
            const want = c >= 0xf0 ? 4 : c >= 0xe0 ? 3 : c >= 0xc0 ? 2 : 1;
            return want > i ? b.subarray(0, b.length - i) : b;
        }
    }
    return b;
}

/** UTF-8 if the bytes are valid UTF-8 (ASCII included), otherwise EUC-KR (CP949). */
export function detectEncoding(b: Uint8Array, truncated: boolean): Encoding {
    try {
        new TextDecoder('utf-8', {fatal: true}).decode(truncated ? trimCutUTF8(b) : b);
        return 'UTF-8';
    } catch {
        return 'EUC-KR';
    }
}

/** Decodes with enc; bytes that don't fit become U+FFFD. EUC-KR is read as CP949. */
export function decode(b: Uint8Array, enc: Encoding, truncated: boolean): string {
    if (enc === 'UTF-8') return new TextDecoder('utf-8').decode(truncated ? trimCutUTF8(b) : b);
    return new TextDecoder('euc-kr').decode(b);
}

function looksBinary(b: Uint8Array) {
    return b.subarray(0, 8192).includes(0);
}

function setStatus(msg: string, ok = false) {
    status.textContent = msg;
    status.classList.toggle('info', ok);
}

/** Shows the file decoded with enc. */
function render(enc: Encoding) {
    if (!file) return;
    const text = decode(file.data, enc, file.truncated);
    view.setState(EditorState.create({doc: text, extensions: extensions()}));
    const bad = (text.match(/�/g) ?? []).length;
    const parts = [formatSize(file.size), `${view.state.doc.lines.toLocaleString()}줄`];
    if (file.truncated) parts.push(`앞 ${formatSize(file.data.length)}만 표시`);
    info.textContent = parts.join(' · ');
    if (looksBinary(file.data)) setStatus('바이너리 파일로 보입니다.');
    else if (bad > 0) setStatus(`${enc}로 읽을 수 없는 글자가 ${bad}개 있습니다. 다른 인코딩을 골라 보세요.`);
    else setStatus('');
    // Saving converts by default: to the other encoding.
    saveEnc.value = enc === 'UTF-8' ? 'EUC-KR' : 'UTF-8';
}

/** Opens the viewer with a downloaded file. */
export function openViewer(f: ViewFile) {
    file = f;
    detected = detectEncoding(f.data, f.truncated);
    encSelect.options[0].textContent = `자동 (${detected})`;
    encSelect.value = 'auto';
    title.textContent = `보기 - ${f.path}`;
    overlay.hidden = false;
    render(detected);
    view.focus();
}

export function closeViewer() {
    if (!file) return;
    const done = file.onClose;
    file = undefined;
    overlay.hidden = true;
    view.setState(EditorState.create({extensions: extensions()}));
    done?.();
}

async function saveAs() {
    if (!file) return;
    const target = saveEnc.value as Encoding;
    try {
        const res = await TextSaveAs(file.name, view.state.doc.toString(), target);
        if (!res.path) return;
        const msg = `${target}로 저장했습니다: ${res.path}`;
        setStatus(res.bad > 0 ? `${msg} (${target}에 없는 글자 ${res.bad}개는 ?로 바꿨습니다)` : msg, res.bad === 0);
    } catch (e) {
        setStatus(String(e));
    } finally {
        view.focus();
    }
}

encSelect.addEventListener('change', () => {
    render(encSelect.value === 'auto' ? detected : encSelect.value as Encoding);
    view.focus();
});

wrapBox.addEventListener('change', () => {
    wrapping = wrapBox.checked;
    view.dispatch({effects: wrap.reconfigure(wrapping ? EditorView.lineWrapping : [])});
    view.focus();
});

$<HTMLButtonElement>('vSave').addEventListener('click', saveAs);
$<HTMLButtonElement>('vClose').addEventListener('click', closeViewer);
$<HTMLButtonElement>('viewerX').addEventListener('click', closeViewer);

// Esc closes the viewer, unless the search panel took it.
panel.addEventListener('keydown', ev => {
    if (ev.key === 'Escape' && !ev.defaultPrevented) {
        ev.preventDefault();
        closeViewer();
    }
});
