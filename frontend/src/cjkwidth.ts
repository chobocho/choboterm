import type {IUnicodeVersionProvider, Terminal} from '@xterm/xterm';
import {Unicode11Addon} from '@xterm/addon-unicode11';

// East Asian "Ambiguous" width characters that legacy Korean terminals (and EUC-KR
// BBS screens) draw as 2 columns: box drawing, geometric shapes, Greek/Cyrillic,
// circled numbers, arrows, math symbols, etc. (subset of EastAsianWidth.txt "A").
const AMBIGUOUS: ReadonlyArray<readonly [number, number]> = [
    [0x00A1, 0x00A1], [0x00A4, 0x00A4], [0x00A7, 0x00A8], [0x00AA, 0x00AA],
    [0x00AD, 0x00AE], [0x00B0, 0x00B4], [0x00B6, 0x00BA], [0x00BC, 0x00BF],
    [0x00C6, 0x00C6], [0x00D0, 0x00D0], [0x00D7, 0x00D8], [0x00DE, 0x00E1],
    [0x00E6, 0x00E6], [0x00E8, 0x00EA], [0x00EC, 0x00ED], [0x00F0, 0x00F0],
    [0x00F2, 0x00F3], [0x00F7, 0x00FA], [0x00FC, 0x00FC], [0x00FE, 0x00FE],
    [0x0111, 0x0111], [0x0126, 0x0127], [0x0131, 0x0133], [0x0138, 0x0138],
    [0x013F, 0x0142], [0x0149, 0x014B], [0x0152, 0x0153], [0x0166, 0x0167],
    [0x02C7, 0x02C7], [0x02C9, 0x02CB], [0x02CD, 0x02CD], [0x02D0, 0x02D0],
    [0x02D8, 0x02DB], [0x02DD, 0x02DD], [0x0391, 0x03A9], [0x03B1, 0x03C9],
    [0x0401, 0x0401], [0x0410, 0x044F], [0x0451, 0x0451], [0x2010, 0x2010],
    [0x2013, 0x2016], [0x2018, 0x2019], [0x201C, 0x201D], [0x2020, 0x2022],
    [0x2024, 0x2027], [0x2030, 0x2030], [0x2032, 0x2033], [0x2035, 0x2035],
    [0x203B, 0x203B], [0x203E, 0x203E], [0x2074, 0x2074], [0x207F, 0x207F],
    [0x2081, 0x2084], [0x20AC, 0x20AC], [0x2103, 0x2103], [0x2105, 0x2105],
    [0x2109, 0x2109], [0x2113, 0x2113], [0x2116, 0x2116], [0x2121, 0x2122],
    [0x2126, 0x2126], [0x212B, 0x212B], [0x2153, 0x2154], [0x215B, 0x215E],
    [0x2160, 0x216B], [0x2170, 0x2179], [0x2189, 0x2189], [0x2190, 0x2199],
    [0x21B8, 0x21B9], [0x21D2, 0x21D2], [0x21D4, 0x21D4], [0x21E7, 0x21E7],
    [0x2200, 0x2200], [0x2202, 0x2203], [0x2207, 0x2208], [0x220B, 0x220B],
    [0x220F, 0x220F], [0x2211, 0x2211], [0x2215, 0x2215], [0x221A, 0x221A],
    [0x221D, 0x2220], [0x2223, 0x2223], [0x2225, 0x2225], [0x2227, 0x222C],
    [0x222E, 0x222E], [0x2234, 0x2237], [0x223C, 0x223D], [0x2248, 0x2248],
    [0x224C, 0x224C], [0x2252, 0x2252], [0x2260, 0x2261], [0x2264, 0x2267],
    [0x226A, 0x226B], [0x226E, 0x226F], [0x2282, 0x2283], [0x2286, 0x2287],
    [0x2295, 0x2295], [0x2299, 0x2299], [0x22A5, 0x22A5], [0x22BF, 0x22BF],
    [0x2312, 0x2312], [0x2460, 0x24E9], [0x24EB, 0x254B], [0x2550, 0x2573],
    [0x2580, 0x258F], [0x2592, 0x2595], [0x25A0, 0x25A1], [0x25A3, 0x25A9],
    [0x25B2, 0x25B3], [0x25B6, 0x25B7], [0x25BC, 0x25BD], [0x25C0, 0x25C1],
    [0x25C6, 0x25C8], [0x25CB, 0x25CB], [0x25CE, 0x25D1], [0x25E2, 0x25E5],
    [0x25EF, 0x25EF], [0x2605, 0x2606], [0x2609, 0x2609], [0x260E, 0x260F],
    [0x261C, 0x261C], [0x261E, 0x261E], [0x2640, 0x2640], [0x2642, 0x2642],
    [0x2660, 0x2661], [0x2663, 0x2665], [0x2667, 0x266A], [0x266C, 0x266D],
    [0x266F, 0x266F], [0x2776, 0x277F], [0x3248, 0x324F],
];

function isAmbiguous(cp: number): boolean {
    if (cp < 0xA1 || cp > 0x324F) return false;
    let lo = 0;
    let hi = AMBIGUOUS.length - 1;
    while (lo <= hi) {
        const mid = (lo + hi) >> 1;
        const [start, end] = AMBIGUOUS[mid];
        if (cp < start) hi = mid - 1;
        else if (cp > end) lo = mid + 1;
        else return true;
    }
    return false;
}

export const NARROW = '11';
export const WIDE = '11-cjk';

/**
 * Loads Unicode 11 widths plus a "11-cjk" variant that draws ambiguous-width
 * characters as 2 columns. Switch with `term.unicode.activeVersion`.
 */
export function loadUnicodeWidths(term: Terminal) {
    // The Unicode 11 provider isn't exported, so capture it while the addon registers it.
    // `term.unicode` returns a new object on every access, so hook the shared prototype.
    const proto = Object.getPrototypeOf(term.unicode);
    const register = proto.register;
    let v11: IUnicodeVersionProvider | undefined;
    proto.register = function (this: unknown, p: IUnicodeVersionProvider) {
        if (p.version === NARROW) v11 = p;
        return register.call(this, p);
    };
    try {
        term.loadAddon(new Unicode11Addon());
    } finally {
        proto.register = register;
    }
    if (!v11) {
        console.warn('Unicode 11 provider was not captured; ambiguous-width mode disabled');
        return;
    }
    const base = v11;

    term.unicode.register({
        version: WIDE,
        wcwidth: cp => (isAmbiguous(cp) ? 2 : base.wcwidth(cp)),
        charProperties(cp, preceding) {
            const props = base.charProperties(cp, preceding);
            if (!isAmbiguous(cp)) return props;
            // props layout: state << 3 | width << 1 | shouldJoin. Force width 2, no join.
            return (props & ~0b111) | (2 << 1);
        },
    });
}

/** Terminal fonts offered in the settings. legacy fonts draw ambiguous-width symbols full-width. */
export const FONTS: ReadonlyArray<{name: string; label: string; legacy?: boolean}> = [
    {name: 'D2Coding', label: 'D2Coding'},
    {name: 'GulimChe', label: '굴림체', legacy: true},
    {name: 'DotumChe', label: '돋움체', legacy: true},
    {name: 'BatangChe', label: '바탕체', legacy: true},
    {name: 'GungsuhChe', label: '궁서체', legacy: true},
    {name: 'Consolas', label: 'Consolas'},
];

const FALLBACK = '"D2Coding", "Consolas", "Malgun Gothic", monospace';

/** The CSS font-family for a chosen font, in wide (EUC-KR) or narrow mode. */
export function fontFamily(name: string, wide: boolean): string {
    const chosen = FONTS.find(f => f.name === name) ?? FONTS[0];
    // A modern font draws ambiguous symbols half-width; borrow them from a legacy font.
    const amb = wide && !chosen.legacy ? `"${AMBIGUOUS_FONT}", ` : '';
    return `${amb}"${chosen.name}", ${FALLBACK}`;
}

// Font for ambiguous-width characters in EUC-KR mode. D2Coding draws ▒─■ etc. as
// half-width glyphs, so a 2-column cell shows a gap; legacy Korean fixed fonts
// (GulimChe and friends, which Zterm uses) draw them full-width.
const AMBIGUOUS_FONT = 'ChoboAmbiguous';

const hex = (cp: number) => cp.toString(16).toUpperCase();
const ambiguousFace = new FontFace(
    AMBIGUOUS_FONT,
    ['GulimChe', '굴림체', 'DotumChe', '돋움체', 'BatangChe', '바탕체'].map(n => `local("${n}")`).join(', '),
    {unicodeRange: AMBIGUOUS.map(([s, e]) => (s === e ? `U+${hex(s)}` : `U+${hex(s)}-${hex(e)}`)).join(', ')},
);
(document.fonts as unknown as Set<FontFace>).add(ambiguousFace);
/** Resolves (never rejects) once the ambiguous-width face has loaded or failed. */
export const ambiguousFontReady: Promise<void> = ambiguousFace.load().then(
    () => undefined,
    () => console.warn('No legacy Korean font found for ambiguous-width symbols'),
);
