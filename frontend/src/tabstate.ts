// The tabs that are open and the one that is shown: what the rest of the page
// shares. A tab bar entry shows one or more panes; each pane is a Tab.

import {Terminal} from '@xterm/xterm';
import {FitAddon} from '@xterm/addon-fit';
import type {WebglAddon} from '@xterm/addon-webgl';
import {DockerMode, DockerSource} from './containers';
import {Highlighter} from './highlight';
import {attachSearch} from './search';
import {filesOpen, focusFiles} from './files';
import {store, terminal} from '../wailsjs/go/models';
import type {ScriptBox} from './luaconsole';

const $ = <T extends HTMLElement>(id: string) => document.getElementById(id) as T;

// idle: not connected, on: terminal session, ftp: FTP (file window only)
export type TabState = 'idle' | 'on' | 'ftp';

// A tab bar entry. It shows one or more panes (split with Ctrl+Shift+\ / -);
// each pane is a Tab with its own connection and its own entry in the chip.
export interface Group {
    box: HTMLDivElement;  // the panes' layout in #terms
    chip: HTMLDivElement; // the panes' tab bar entries
}

export type SplitDir = 'row' | 'col'; // row: side by side, col: one above the other

export interface Tab {
    id: number;
    group: Group;
    term: Terminal;
    fit: FitAddon;
    search: ReturnType<typeof attachSearch>;
    hl: Highlighter;
    pane: HTMLDivElement;
    el: HTMLDivElement;
    label: HTMLSpanElement;
    state: TabState;
    proto: string; // "ssh" / "telnet" / "ftp" / "local" / "serial" as reported by Connect
    encoding: string;
    // Last connection, kept in memory only, for reconnect / duplicate.
    req?: terminal.ConnectRequest;
    // A docker exec / logs tab (no req): what to open again on reconnect.
    docker?: {src: DockerSource; id: string; name: string; mode: DockerMode};
    // The saved session req came from: its name is the tab label.
    session?: store.SavedSession;
    // This tab's Connect dialog, while it is open.
    dialog?: DialogState;
    // Created just for a Connect dialog: cancelling the dialog closes it.
    temp?: boolean;
    // Automatic reconnect after the connection broke: the pending attempt.
    retry?: {attempt: number; timer: number};
    reconnecting?: boolean;
    // Session log file while logging is on.
    log?: string;
    // The Lua script running in the tab, the badge showing its time, and the
    // box with its print() output and how it ended (shown over the pane).
    script?: {name: string; started: number; console: boolean};
    run: HTMLSpanElement;
    scriptBox?: ScriptBox;
    // Input waiting for the Send call in flight (see sendInput).
    outbox: string;
    sending: boolean;
    // The shell's folder as reported by OSC 7, and the window title it set.
    osc7?: string;
    title?: string;
    // The GPU (WebGL) renderer, while it draws this tab instead of the DOM renderer.
    webgl?: WebglAddon;
    webglLoading?: boolean;
    // Connecting with a clear screen: it is cleared when the new session opens (term:open).
    fresh?: boolean;
    // Color theme chosen for this tab's server; undefined: the one in Settings.
    theme?: string;
}

// Connect dialog contents saved per tab, so switching tabs keeps them.
export interface DialogState {
    host: string;
    port: string;
    login: string;
    pass: string;
    encoding: string;
    jump: string;
    error: string;
    connecting: boolean;
    resolved: string; // Host text whose ~/.ssh/config values were already filled in
    session?: store.SavedSession; // the saved session picked from the list
}

export const tabs = new Map<number, Tab>();
export const tabsEl = $<HTMLDivElement>('tabs');
export const termsEl = $<HTMLDivElement>('terms');
export let active: Tab | undefined;

export function setActive(t: Tab | undefined) {
    active = t;
}

/** All panes in tab bar order (the panes of a split tab are next to each other). */
export function orderedTabs(): Tab[] {
    return Array.from(tabsEl.querySelectorAll<HTMLElement>('.tab'))
        .map(el => tabs.get(Number(el.dataset.id)))
        .filter((t): t is Tab => !!t);
}

/** The panes of group g, in tab bar order. */
export function panesOf(g: Group): Tab[] {
    return orderedTabs().filter(t => t.group === g);
}

/** Fits a pane to its size; panes of hidden tabs are fitted when they are shown. */
export function fitPane(t: Tab) {
    if (t.pane.clientWidth > 0 && t.pane.clientHeight > 0) t.fit.fit();
}

// The Connect dialog's Host field.
const host = $<HTMLInputElement>('host');

export function focusActive() {
    const t = active;
    if (!t) return;
    if (t.dialog) host.focus();
    else if (filesOpen()) focusFiles();
    else t.term.focus();
}
