// Terminal color themes chosen in the Settings window. Palettes follow the
// well-known schemes (Windows Terminal's versions where it ships one).

import {ITheme} from '@xterm/xterm';

export interface ThemeInfo {
    name: string;
    label: string;
    theme: ITheme;
}

// Order of the 16 ANSI colors in the palette arrays below.
const KEYS = ['black', 'red', 'green', 'yellow', 'blue', 'magenta', 'cyan', 'white',
    'brightBlack', 'brightRed', 'brightGreen', 'brightYellow', 'brightBlue', 'brightMagenta', 'brightCyan', 'brightWhite'] as const;

function scheme(bg: string, fg: string, palette: string[], extra: ITheme = {}): ITheme {
    const t: ITheme = {background: bg, foreground: fg, ...extra};
    KEYS.forEach((k, i) => (t[k] = palette[i]));
    return t;
}

// xterm.js's built-in palette, as choboterm has always looked.
const XTERM = ['#2e3436', '#cc0000', '#4e9a06', '#c4a000', '#3465a4', '#75507b', '#06989a', '#d3d7cf',
    '#555753', '#ef2929', '#8ae234', '#fce94f', '#729fcf', '#ad7fa8', '#34e2e2', '#eeeeec'];

const SOLARIZED = ['#002b36', '#dc322f', '#859900', '#b58900', '#268bd2', '#d33682', '#2aa198', '#eee8d5',
    '#073642', '#cb4b16', '#586e75', '#657b83', '#839496', '#6c71c4', '#93a1a1', '#fdf6e3'];

export const THEMES: ThemeInfo[] = [
    {name: 'choboterm', label: '기본 (검정)', theme: scheme('#000000', '#ffffff', XTERM)},
    {
        name: 'campbell', label: 'Campbell (Windows 터미널)', theme: scheme('#0c0c0c', '#cccccc', [
            '#0c0c0c', '#c50f1f', '#13a10e', '#c19c00', '#0037da', '#881798', '#3a96dd', '#cccccc',
            '#767676', '#e74856', '#16c60c', '#f9f1a5', '#3b78ff', '#b4009e', '#61d6d6', '#f2f2f2']),
    },
    {
        name: 'putty', label: 'PuTTY', theme: scheme('#000000', '#bbbbbb', [
            '#000000', '#bb0000', '#00bb00', '#bbbb00', '#0000bb', '#bb00bb', '#00bbbb', '#bbbbbb',
            '#555555', '#ff5555', '#55ff55', '#ffff55', '#5555ff', '#ff55ff', '#55ffff', '#ffffff']),
    },
    {
        name: 'tango', label: 'Tango Dark', theme: scheme('#000000', '#d3d7cf', [
            '#000000', '#cc0000', '#4e9a06', '#c4a000', '#3465a4', '#75507b', '#06989a', '#d3d7cf',
            '#555753', '#ef2929', '#8ae234', '#fce94f', '#729fcf', '#ad7fa8', '#34e2e2', '#eeeeec']),
    },
    {
        name: 'one-half-dark', label: 'One Half Dark', theme: scheme('#282c34', '#dcdfe4', [
            '#282c34', '#e06c75', '#98c379', '#e5c07b', '#61afef', '#c678dd', '#56b6c2', '#dcdfe4',
            '#5a6374', '#e06c75', '#98c379', '#e5c07b', '#61afef', '#c678dd', '#56b6c2', '#dcdfe4']),
    },
    {
        name: 'dracula', label: 'Dracula', theme: scheme('#282a36', '#f8f8f2', [
            '#21222c', '#ff5555', '#50fa7b', '#f1fa8c', '#bd93f9', '#ff79c6', '#8be9fd', '#f8f8f2',
            '#6272a4', '#ff6e6e', '#69ff94', '#ffffa5', '#d6acff', '#ff92df', '#a4ffff', '#ffffff'],
        {selectionBackground: '#44475a'}),
    },
    {
        name: 'gruvbox-dark', label: 'Gruvbox Dark', theme: scheme('#282828', '#ebdbb2', [
            '#282828', '#cc241d', '#98971a', '#d79921', '#458588', '#b16286', '#689d6a', '#a89984',
            '#928374', '#fb4934', '#b8bb26', '#fabd2f', '#83a598', '#d3869b', '#8ec07c', '#ebdbb2']),
    },
    {name: 'solarized-dark', label: 'Solarized Dark', theme: scheme('#002b36', '#839496', SOLARIZED, {cursor: '#93a1a1'})},
    {
        name: 'solarized-light', label: 'Solarized Light (밝은 배경)', theme: scheme('#fdf6e3', '#657b83', SOLARIZED,
            {cursor: '#002b36', selectionBackground: '#d3cbb780'}),
    },
    {
        name: 'one-half-light', label: 'One Half Light (밝은 배경)', theme: scheme('#fafafa', '#383a42', [
            '#383a42', '#e45649', '#50a14f', '#c18301', '#0184bc', '#a626a4', '#0997b3', '#fafafa',
            '#4f525d', '#df6c75', '#98c379', '#e4c07a', '#61afef', '#c577dd', '#56b5c1', '#ffffff'],
        {cursor: '#4f525d', selectionBackground: '#bfceff80'}),
    },
    // Green-phosphor monochrome like a Hercules card on a green monitor: every
    // color is a shade of green, kept apart enough that colored backgrounds
    // (htop, mc) stay readable.
    {
        name: 'hercules', label: '허큘리스 (검은 배경 · 녹색 글자)', theme: scheme('#000000', '#33ff33', [
            '#002200', '#1f8f1f', '#33cc33', '#66dd44', '#1a7a3a', '#4aa84a', '#3fbf7f', '#8fe68f',
            '#2f5f2f', '#4fd24f', '#55ff55', '#aaff66', '#33aa66', '#77e077', '#66ffaa', '#ccffcc'],
        {cursor: '#33ff33', cursorAccent: '#000000', selectionBackground: '#33ff3355'}),
    },
    {
        name: 'hercules-inverse', label: '허큘리스 반전 (녹색 배경 · 검은 글자)', theme: scheme('#33cc33', '#000000', [
            '#000000', '#0f400f', '#004d00', '#2d4d00', '#00331a', '#1a4d1a', '#003d26', '#c8f5c8',
            '#145214', '#082808', '#002200', '#1f3300', '#001f10', '#0f330f', '#00261a', '#eaffea'],
        {cursor: '#000000', cursorAccent: '#33cc33', selectionBackground: '#00000040'}),
    },
];

export function themeByName(name: string): ThemeInfo {
    return THEMES.find(t => t.name === name) ?? THEMES[0];
}

/** The 16 ANSI colors of a theme, black first. */
export function paletteOf(theme: ITheme): string[] {
    return KEYS.map(k => theme[k] as string);
}
