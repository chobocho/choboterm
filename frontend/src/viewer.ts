// Text viewer / editor for remote files (CodeMirror 6).
// The raw bytes are kept, so the encoding can be switched without downloading again.
// Saving writes back in the shown encoding, keeping the file's line ending and BOM.

import {Compartment, EditorState, Text} from '@codemirror/state';
import {
    drawSelection,
    EditorView,
    highlightActiveLine,
    highlightActiveLineGutter,
    highlightSpecialChars,
    keymap,
    lineNumbers,
} from '@codemirror/view';
import {defaultKeymap, history, historyKeymap, indentLess, insertTab} from '@codemirror/commands';
import {highlightSelectionMatches, search, searchKeymap} from '@codemirror/search';
import {FileSaveText, TextSaveAs} from '../wailsjs/go/main/App';
import {ask} from './dialog';
import {formatSize} from './files';

const $ = <T extends HTMLElement>(id: string) => document.getElementById(id) as T;

const overlay = $<HTMLDivElement>('viewerOverlay');
const panel = $<HTMLDivElement>('viewer');
const title = $<HTMLSpanElement>('viewerTitle');
const encSelect = $<HTMLSelectElement>('vEnc');
const wrapBox = $<HTMLInputElement>('vWrap');
const editBtn = $<HTMLButtonElement>('vEdit');
const info = $<HTMLSpanElement>('vInfo');
const textEl = $<HTMLDivElement>('vText');
const status = $<HTMLDivElement>('vError');
const saveEnc = $<HTMLSelectElement>('vSaveEnc');
const saveBtn = $<HTMLButtonElement>('vSaveRemote');

export type Encoding = 'UTF-8' | 'EUC-KR';

export interface ViewFile {
    tabId: number;
    name: string;
    path: string;
    data: Uint8Array; // up to the viewer limit
    truncated: boolean;
    // The remote file when it was read (or last saved): checked before saving.
    size: number;
    modTime: string;
    onSaved?: () => void;
    onClose?: () => void;
}

let file: ViewFile | undefined;
let detected: Encoding = 'UTF-8';
let shown: Encoding = 'UTF-8';
let eol = '\n';
let bom = false;
let editing = false;
let saving = false;
let saved: Text = Text.empty; // the document as last read or saved
let wrapping = false;

const wrap = new Compartment();
const readOnly = new Compartment();

// Korean labels for the search panel.
const phrases = {
    'Find': '찾을 내용',
    'next': '다음',
    'previous': '이전',
    'all': '모두',
    'match case': '대소문자 구분',
    'regexp': '정규식',
    'by word': '단어 단위',
    'replace': '바꿀 내용',
    'replace all': '모두 바꾸기',
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
        history(),
        search({top: true}),
        highlightSelectionMatches(),
        keymap.of([
            {key: 'Mod-s', run: () => (save(), true), preventDefault: true},
            {key: 'Tab', run: insertTab},
            {key: 'Shift-Tab', run: indentLess},
            ...searchKeymap,
            ...historyKeymap,
            ...defaultKeymap,
        ]),
        readOnly.of(EditorState.readOnly.of(!editing)),
        EditorState.phrases.of(phrases),
        EditorState.tabSize.of(8),
        wrap.of(wrapping ? EditorView.lineWrapping : []),
        EditorView.updateListener.of(u => u.docChanged && paintDirty()),
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

/** The line ending most lines use: CRLF or LF. */
function lineEnding(text: string) {
    const crlf = (text.match(/\r\n/g) ?? []).length;
    const lf = (text.match(/\n/g) ?? []).length - crlf;
    return crlf > lf ? '\r\n' : '\n';
}

function hasBOM(b: Uint8Array) {
    return b.length >= 3 && b[0] === 0xef && b[1] === 0xbb && b[2] === 0xbf;
}

function looksBinary(b: Uint8Array) {
    return b.subarray(0, 8192).includes(0);
}

function countBad(s: string) {
    return (s.match(/�/g) ?? []).length;
}

function setStatus(msg: string, ok = false) {
    status.textContent = msg;
    status.classList.toggle('info', ok);
}

function dirty() {
    return editing && !view.state.doc.eq(saved);
}

function paintDirty() {
    if (!file) return;
    const d = dirty();
    title.textContent = `${editing ? '편집' : '보기'} - ${file.path}${d ? ' *' : ''}`;
    saveBtn.disabled = !editing || saving || !d;
}

function paintInfo() {
    if (!file) return;
    const parts = [
        bom ? `${shown} BOM` : shown,
        eol === '\r\n' ? 'CRLF' : 'LF',
        formatSize(file.size),
        `${view.state.doc.lines.toLocaleString()}줄`,
    ];
    if (file.truncated) parts.push(`앞 ${formatSize(file.data.length)}만 표시`);
    info.textContent = parts.join(' · ');
}

/** Shows the file decoded with enc. */
function render(enc: Encoding) {
    if (!file) return;
    shown = enc;
    const text = decode(file.data, enc, file.truncated);
    eol = lineEnding(text);
    bom = enc === 'UTF-8' && hasBOM(file.data);
    view.setState(EditorState.create({doc: text, extensions: extensions()}));
    saved = view.state.doc;
    const bad = countBad(text);
    if (looksBinary(file.data)) setStatus('바이너리 파일로 보입니다.');
    else if (bad > 0) setStatus(`${enc}로 읽을 수 없는 글자가 ${bad}개 있습니다. 다른 인코딩을 골라 보세요.`);
    else setStatus('');
    // Saving to the PC converts by default: to the other encoding.
    saveEnc.value = enc === 'UTF-8' ? 'EUC-KR' : 'UTF-8';
    paintInfo();
    paintDirty();
}

function setEditing(on: boolean) {
    editing = on;
    view.dispatch({effects: readOnly.reconfigure(EditorState.readOnly.of(!on))});
    editBtn.disabled = on || !file || file.truncated;
    panel.classList.toggle('editing', on);
    paintDirty();
}

function startEditing() {
    if (!file || editing) return;
    if (file.truncated) {
        setStatus(`파일이 ${formatSize(file.data.length)}보다 커서 편집할 수 없습니다.`);
        return;
    }
    setEditing(true);
    view.focus();
}

/** Opens a downloaded file; edit starts in edit mode (if the file isn't too big). */
export function openViewer(f: ViewFile, edit = false) {
    file = f;
    editing = false;
    saving = false;
    detected = detectEncoding(f.data, f.truncated);
    encSelect.options[0].textContent = `자동 (${detected})`;
    encSelect.value = 'auto';
    overlay.hidden = false;
    render(detected);
    setEditing(false);
    if (edit) startEditing();
    view.focus();
}

/** Closes the window; asks first if there are unsaved changes. */
export async function closeViewer() {
    if (!file) return;
    if (saving) return;
    if (dirty() && !await ask('편집', '저장하지 않은 변경 내용이 있습니다. 저장하지 않고 닫을까요?', '닫기')) {
        view.focus();
        return;
    }
    const done = file.onClose;
    file = undefined;
    editing = false;
    overlay.hidden = true;
    panel.classList.remove('editing');
    view.setState(EditorState.create({extensions: extensions()}));
    done?.();
}

function base64Bytes(s: string) {
    return Uint8Array.from(atob(s), c => c.charCodeAt(0));
}

/** Writes the text back to the server in the shown encoding. */
async function save() {
    const f = file;
    if (!f || !editing || saving || !dirty()) return;
    const doc = view.state.doc;
    const bad = countBad(doc.toString());
    if (bad > 0 && !await ask('저장',
        `읽을 수 없었던 글자(�)가 ${bad}개 있습니다. 저장하면 그 자리의 원래 바이트는 사라집니다.\n` +
        '인코딩이 맞는지 먼저 확인하세요. 그래도 저장할까요?', '저장')) {
        view.focus();
        return;
    }
    saving = true;
    paintDirty();
    setStatus('저장하는 중...', true);
    try {
        const req = {
            path: f.path,
            text: (bom ? '﻿' : '') + doc.toJSON().join(eol),
            encoding: shown,
            size: f.size,
            modTime: f.modTime,
            ignoreConflict: false,
            allowLossy: false,
        };
        for (;;) {
            const r = await FileSaveText(f.tabId, req);
            if (r.saved) {
                f.data = base64Bytes(r.data);
                f.size = r.size;
                f.modTime = r.modTime;
                saved = doc;
                paintInfo();
                const at = new Date().toLocaleTimeString();
                setStatus(r.bad > 0 ? `저장했습니다 (${at}). ${shown}에 없는 글자 ${r.bad}개는 ?로 바꿨습니다.` : `저장했습니다 (${at})`, r.bad === 0);
                f.onSaved?.();
                return;
            }
            if (r.bad > 0 && !req.allowLossy) {
                if (!await ask('저장', `${shown}에 없는 글자가 ${r.bad}개 있습니다. ?로 바꿔서 저장할까요?\n` +
                    '바꾸지 않으려면 취소하고 인코딩을 UTF-8로 고르세요.', '?로 바꿔 저장')) break;
                req.allowLossy = true;
                continue;
            }
            if (r.conflict) {
                if (!await ask('저장', '파일을 연 뒤에 서버에서 파일이 바뀌었습니다(크기 또는 수정한 날짜).\n' +
                    '덮어쓰면 그 변경 내용은 사라집니다. 덮어쓸까요?', '덮어쓰기')) break;
                req.ignoreConflict = true;
                continue;
            }
            break;
        }
        setStatus('저장하지 않았습니다.');
    } catch (e) {
        setStatus(`저장하지 못했습니다: ${e}`);
    } finally {
        saving = false;
        paintDirty();
        if (file === f) view.focus();
    }
}

/** Saves the shown text to the PC, converted to the chosen encoding. */
async function saveAs() {
    if (!file) return;
    const target = saveEnc.value as Encoding;
    try {
        const text = (bom && target === 'UTF-8' ? '﻿' : '') + view.state.doc.toJSON().join(eol);
        const res = await TextSaveAs(file.name, text, target);
        if (!res.path) return;
        const msg = `${target}로 저장했습니다: ${res.path}`;
        setStatus(res.bad > 0 ? `${msg} (${target}에 없는 글자 ${res.bad}개는 ?로 바꿨습니다)` : msg, res.bad === 0);
    } catch (e) {
        setStatus(String(e));
    } finally {
        view.focus();
    }
}

encSelect.addEventListener('change', async () => {
    const enc = encSelect.value === 'auto' ? detected : encSelect.value as Encoding;
    if (dirty() && !await ask('인코딩',
        '인코딩을 바꾸면 파일을 다시 읽으므로 저장하지 않은 변경 내용이 사라집니다. 바꿀까요?', '바꾸기')) {
        encSelect.value = shown === detected ? 'auto' : shown;
        view.focus();
        return;
    }
    render(enc);
    view.focus();
});

wrapBox.addEventListener('change', () => {
    wrapping = wrapBox.checked;
    view.dispatch({effects: wrap.reconfigure(wrapping ? EditorView.lineWrapping : [])});
    view.focus();
});

editBtn.addEventListener('click', startEditing);
saveBtn.addEventListener('click', save);
$<HTMLButtonElement>('vSave').addEventListener('click', saveAs);
$<HTMLButtonElement>('vClose').addEventListener('click', closeViewer);
$<HTMLButtonElement>('viewerX').addEventListener('click', closeViewer);

// Esc closes the window, unless the search panel took it.
panel.addEventListener('keydown', ev => {
    if (ev.key === 'Escape' && !ev.defaultPrevented) {
        ev.preventDefault();
        closeViewer();
    }
});
