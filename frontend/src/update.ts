// Update check: asks the Go side (which asks GitHub) whether a newer release
// is out. At start it only runs when turned on in the settings.

import {CheckUpdate} from '../wailsjs/go/main/App';
import {BrowserOpenURL} from '../wailsjs/runtime/runtime';
import {ask} from './dialog';

/**
 * Checks for a newer release and offers to open its page. manual: the
 * "지금 확인" button, which always asks and returns what it found (errors
 * are thrown); at start nothing is said unless there is news.
 */
export async function checkUpdate(manual: boolean): Promise<string> {
    let info;
    try {
        info = await CheckUpdate(manual);
    } catch (e) {
        if (manual) throw e;
        return '';
    }
    if (!info.checked) return '';
    if (!info.newer) return `최신 버전입니다 (V${info.current})`;
    const back = document.activeElement as HTMLElement | null;
    const open = await ask('새 버전', `choboterm V${info.latest}이(가) 나왔습니다. 지금은 V${info.current}입니다.\n다운로드 페이지를 열까요?`, '다운로드 페이지 열기');
    back?.focus();
    if (open && info.url) BrowserOpenURL(info.url);
    return `새 버전 V${info.latest}이(가) 있습니다`;
}
