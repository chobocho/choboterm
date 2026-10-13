// A lightweight memo pad floating over a terminal pane. Its contents are
// shared across tabs and kept in local storage as the user types.

import {MemoSave} from '../wailsjs/go/app/App';
import {active, focusActive, Tab} from './tabstate';
import {toast} from './notify';

const storageKey = 'choboterm.memo';

interface MemoBox {
    el: HTMLDivElement;
    input: HTMLTextAreaElement;
}

const boxes = new Map<Tab, MemoBox>();

function loadMemo(): string {
    try {
        return localStorage.getItem(storageKey) ?? '';
    } catch {
        return '';
    }
}

function autoSave(text: string) {
    try {
        localStorage.setItem(storageKey, text);
    } catch (e) {
        toast(`메모 자동 저장 실패: ${e}`);
    }
}

/** Removes a tab's memo pad when that tab closes. */
export function disposeMemo(t: Tab) {
    const box = boxes.get(t);
    box?.el.remove();
    boxes.delete(t);
}

/** Opens or closes the memo pad in the selected terminal pane. */
export function showMemo(t: Tab = active!) {
    if (!t) return;
    const existing = boxes.get(t);
    if (existing) {
        existing.el.remove();
        boxes.delete(t);
        focusActive();
        return;
    }

    const el = document.createElement('div');
    el.className = 'memo-box';
    const top = document.createElement('div');
    top.className = 'top';
    const title = document.createElement('span');
    title.textContent = '메모장';
    const save = document.createElement('button');
    save.type = 'button';
    save.textContent = '다른 이름으로 저장';
    save.title = '메모를 파일로 저장';
    const close = document.createElement('button');
    close.type = 'button';
    close.textContent = '✕';
    close.title = '닫기 (Esc)';
    top.append(title, save, close);

    const input = document.createElement('textarea');
    input.className = 'memo-input';
    input.spellcheck = false;
    input.placeholder = '여기에 메모를 입력하세요. 내용은 자동으로 저장됩니다.';
    input.value = loadMemo();
    input.addEventListener('input', () => {
        autoSave(input.value);
        for (const [other, box] of boxes) {
            if (other !== t && box.input.value !== input.value) box.input.value = input.value;
        }
    });
    input.addEventListener('keydown', ev => {
        if (ev.key === 'Escape') {
            ev.preventDefault();
            ev.stopPropagation();
            el.remove();
            boxes.delete(t);
            focusActive();
        }
    });
    el.addEventListener('mousedown', ev => ev.stopPropagation());
    save.addEventListener('click', async () => {
        try {
            const path = await MemoSave(input.value);
            if (path) toast(`메모를 저장했습니다: ${path}`);
        } catch (e) {
            toast(`메모를 저장하지 못했습니다: ${e}`);
        }
        if (el.isConnected) input.focus();
    });
    close.addEventListener('click', () => {
        el.remove();
        boxes.delete(t);
        focusActive();
    });
    el.append(top, input);
    t.pane.appendChild(el);
    boxes.set(t, {el, input});
    input.focus();
}
