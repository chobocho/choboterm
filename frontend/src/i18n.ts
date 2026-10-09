// English screen. The page is written in Korean; with English on, text that
// appears in the page (labels, tooltips, messages, errors from the Go side)
// is looked up in i18n_en.json and replaced as it appears. Terminal output,
// edited file contents and the help window (which has its own English) are
// left alone. Keys may hold {} for a changing part (a file name, a number);
// the translation puts the parts back in order, or by {0} {1} if the order
// differs. A text with no entry is tried line by line, then left as it is.

import EN from './i18n_en.json';

export type Lang = 'ko' | 'en';

let lang: Lang = 'ko';

const HANGUL = /[가-힣]/;
const exact = new Map<string, string>(Object.entries(EN as Record<string, string>));

interface Pattern {
    re: RegExp;
    to: string;
}

// Keys with {}: the more literal text a key has, the earlier it is tried.
const patterns: Pattern[] = [...exact.entries()]
    .filter(([k]) => k.includes('{}'))
    .sort((a, b) => b[0].replace(/\{\}/g, '').length - a[0].replace(/\{\}/g, '').length)
    .map(([k, to]) => ({
        re: new RegExp('^' + k.split('{}').map(s => s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')).join('([\\s\\S]*?)') + '$'),
        to,
    }));

const cache = new Map<string, string>();

function fill(to: string, parts: string[]): string {
    let next = 0;
    return to.replace(/\{(\d*)\}/g, (_, n: string) => parts[n === '' ? next++ : Number(n)] ?? '');
}

function lookup(s: string, depth: number): string | undefined {
    const hit = exact.get(s);
    if (hit !== undefined) return hit;
    if (depth > 3) return undefined;
    for (const p of patterns) {
        const m = p.re.exec(s);
        if (m) return fill(p.to, m.slice(1).map(x => translate(x, depth + 1)));
    }
    return undefined;
}

function translate(s: string, depth = 0): string {
    if (!HANGUL.test(s)) return s;
    const lead = s.match(/^\s*/)![0];
    const tail = s.slice(lead.length).match(/\s*$/)![0];
    const core = s.slice(lead.length, s.length - tail.length);
    let out = lookup(core, depth);
    if (out === undefined && core.includes('\n')) out = core.split('\n').map(l => translate(l, depth)).join('\n');
    return out === undefined ? s : lead + out + tail;
}

/** The text in the current language (as is in Korean). */
export function tr(s: string): string {
    if (lang === 'ko' || !HANGUL.test(s)) return s;
    let out = cache.get(s);
    if (out === undefined) {
        out = translate(s);
        if (cache.size > 5000) cache.clear();
        cache.set(s, out);
    }
    return out;
}

export function currentLang(): Lang {
    return lang;
}

// ---- The page ----

const ATTRS = ['title', 'placeholder'];
// Not translated: terminals, file contents in the viewer, the help window.
const SKIP = '.xterm, .cm-content, #help';

function skipped(n: Node): boolean {
    const el = n instanceof Element ? n : n.parentElement;
    return !el || !!el.closest(SKIP);
}

function doText(n: Text) {
    const v = n.data;
    if (!HANGUL.test(v) || skipped(n)) return;
    const t = tr(v);
    if (t !== v) n.data = t;
}

function doAttrs(el: Element) {
    for (const a of ATTRS) {
        const v = el.getAttribute(a);
        if (v && HANGUL.test(v)) {
            const t = tr(v);
            if (t !== v) el.setAttribute(a, t);
        }
    }
}

function doTree(root: Node) {
    if (skipped(root)) return;
    if (root instanceof Text) return doText(root);
    if (root instanceof Element) doAttrs(root);
    const walk = document.createTreeWalker(root, NodeFilter.SHOW_ELEMENT | NodeFilter.SHOW_TEXT, {
        acceptNode: n => (n instanceof Element && n.matches(SKIP) ? NodeFilter.FILTER_REJECT : NodeFilter.FILTER_ACCEPT),
    });
    for (let n = walk.nextNode(); n; n = walk.nextNode()) {
        if (n instanceof Text) doText(n);
        else doAttrs(n as Element);
    }
}

/** Sets the language; with English, translates the page now and whatever is shown later. */
export function startI18n(l: Lang) {
    lang = l;
    document.documentElement.lang = l;
    if (l === 'ko') return;
    doTree(document.body);
    new MutationObserver(records => {
        for (const r of records) {
            if (r.type === 'characterData') doText(r.target as Text);
            else if (r.type === 'attributes') {
                if (!skipped(r.target)) doAttrs(r.target as Element);
            } else r.addedNodes.forEach(doTree);
        }
    }).observe(document.body, {subtree: true, childList: true, characterData: true, attributes: true, attributeFilter: ATTRS});
}
