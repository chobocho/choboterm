export namespace main {
	
	export class ConnectRequest {
	    host: string;
	    port: number;
	    login: string;
	    pass: string;
	    encoding: string;
	    cols: number;
	    rows: number;
	
	    static createFrom(source: any = {}) {
	        return new ConnectRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.host = source["host"];
	        this.port = source["port"];
	        this.login = source["login"];
	        this.pass = source["pass"];
	        this.encoding = source["encoding"];
	        this.cols = source["cols"];
	        this.rows = source["rows"];
	    }
	}
	export class ConsoleFile {
	    name: string;
	    text: string;
	
	    static createFrom(source: any = {}) {
	        return new ConsoleFile(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.text = source["text"];
	    }
	}
	export class DownloadResult {
	    dir: string;
	    count: number;
	
	    static createFrom(source: any = {}) {
	        return new DownloadResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.dir = source["dir"];
	        this.count = source["count"];
	    }
	}
	export class FileEntry {
	    name: string;
	    size: number;
	    isDir: boolean;
	    modTime: string;
	
	    static createFrom(source: any = {}) {
	        return new FileEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.size = source["size"];
	        this.isDir = source["isDir"];
	        this.modTime = source["modTime"];
	    }
	}
	export class FileOpenResult {
	    home: string;
	    protocol: string;
	
	    static createFrom(source: any = {}) {
	        return new FileOpenResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.home = source["home"];
	        this.protocol = source["protocol"];
	    }
	}
	export class Forward {
	    type: string;
	    bindAddr: string;
	    bindPort: number;
	    host: string;
	    port: number;
	
	    static createFrom(source: any = {}) {
	        return new Forward(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.type = source["type"];
	        this.bindAddr = source["bindAddr"];
	        this.bindPort = source["bindPort"];
	        this.host = source["host"];
	        this.port = source["port"];
	    }
	}
	export class ForwardStatus {
	    type: string;
	    bindAddr: string;
	    bindPort: number;
	    host: string;
	    port: number;
	    id: number;
	    error: string;
	    conns: number;
	
	    static createFrom(source: any = {}) {
	        return new ForwardStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.type = source["type"];
	        this.bindAddr = source["bindAddr"];
	        this.bindPort = source["bindPort"];
	        this.host = source["host"];
	        this.port = source["port"];
	        this.id = source["id"];
	        this.error = source["error"];
	        this.conns = source["conns"];
	    }
	}
	export class HostEntry {
	    host: string;
	    port: number;
	    login: string;
	    encoding: string;
	    pass: string;
	
	    static createFrom(source: any = {}) {
	        return new HostEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.host = source["host"];
	        this.port = source["port"];
	        this.login = source["login"];
	        this.encoding = source["encoding"];
	        this.pass = source["pass"];
	    }
	}
	export class ImportResult {
	    added: number;
	    skipped: number;
	
	    static createFrom(source: any = {}) {
	        return new ImportResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.added = source["added"];
	        this.skipped = source["skipped"];
	    }
	}
	export class LocalShell {
	    name: string;
	    label: string;
	
	    static createFrom(source: any = {}) {
	        return new LocalShell(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.label = source["label"];
	    }
	}
	export class Macro {
	    name: string;
	    key: string;
	    text: string;
	    kind: string;
	
	    static createFrom(source: any = {}) {
	        return new Macro(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.key = source["key"];
	        this.text = source["text"];
	        this.kind = source["kind"];
	    }
	}
	export class SSHConfigHost {
	    host: string;
	    hostName: string;
	    port: number;
	    login: string;
	
	    static createFrom(source: any = {}) {
	        return new SSHConfigHost(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.host = source["host"];
	        this.hostName = source["hostName"];
	        this.port = source["port"];
	        this.login = source["login"];
	    }
	}
	export class SSHTarget {
	    host: string;
	    port: number;
	    login: string;
	    hostName: string;
	    fromConfig: boolean;
	
	    static createFrom(source: any = {}) {
	        return new SSHTarget(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.host = source["host"];
	        this.port = source["port"];
	        this.login = source["login"];
	        this.hostName = source["hostName"];
	        this.fromConfig = source["fromConfig"];
	    }
	}
	export class SaveResult {
	    path: string;
	    bad: number;
	
	    static createFrom(source: any = {}) {
	        return new SaveResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.bad = source["bad"];
	    }
	}
	export class SavedSession {
	    id: string;
	    name: string;
	    group: string;
	    host: string;
	    port: number;
	    login: string;
	    encoding: string;
	    pass: string;
	
	    static createFrom(source: any = {}) {
	        return new SavedSession(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.group = source["group"];
	        this.host = source["host"];
	        this.port = source["port"];
	        this.login = source["login"];
	        this.encoding = source["encoding"];
	        this.pass = source["pass"];
	    }
	}
	export class WindowState {
	    left: number;
	    top: number;
	    right: number;
	    bottom: number;
	    maximized: boolean;
	
	    static createFrom(source: any = {}) {
	        return new WindowState(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.left = source["left"];
	        this.top = source["top"];
	        this.right = source["right"];
	        this.bottom = source["bottom"];
	        this.maximized = source["maximized"];
	    }
	}
	export class Settings {
	    fontSize: number;
	    fontUtf8: string;
	    fontEucKr: string;
	    theme: string;
	    cursorStyle: string;
	    cursorBlink: boolean;
	    scrollback: number;
	    translucency: string;
	    opacity: number;
	    glassGpu: boolean;
	    pasteNoConfirm: boolean;
	    keepAlive: number;
	    autoReconnect: boolean;
	    logAuto: boolean;
	    logDir: string;
	    logRaw: boolean;
	    consoleHeight: number;
	    macros: Macro[];
	    window?: WindowState;
	
	    static createFrom(source: any = {}) {
	        return new Settings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.fontSize = source["fontSize"];
	        this.fontUtf8 = source["fontUtf8"];
	        this.fontEucKr = source["fontEucKr"];
	        this.theme = source["theme"];
	        this.cursorStyle = source["cursorStyle"];
	        this.cursorBlink = source["cursorBlink"];
	        this.scrollback = source["scrollback"];
	        this.translucency = source["translucency"];
	        this.opacity = source["opacity"];
	        this.glassGpu = source["glassGpu"];
	        this.pasteNoConfirm = source["pasteNoConfirm"];
	        this.keepAlive = source["keepAlive"];
	        this.autoReconnect = source["autoReconnect"];
	        this.logAuto = source["logAuto"];
	        this.logDir = source["logDir"];
	        this.logRaw = source["logRaw"];
	        this.consoleHeight = source["consoleHeight"];
	        this.macros = this.convertValues(source["macros"], Macro);
	        this.window = this.convertValues(source["window"], WindowState);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class TextSave {
	    path: string;
	    text: string;
	    encoding: string;
	    size: number;
	    modTime: string;
	    ignoreConflict: boolean;
	    allowLossy: boolean;
	
	    static createFrom(source: any = {}) {
	        return new TextSave(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.text = source["text"];
	        this.encoding = source["encoding"];
	        this.size = source["size"];
	        this.modTime = source["modTime"];
	        this.ignoreConflict = source["ignoreConflict"];
	        this.allowLossy = source["allowLossy"];
	    }
	}
	export class TextSaveResult {
	    saved: boolean;
	    conflict: boolean;
	    bad: number;
	    size: number;
	    modTime: string;
	    data: string;
	
	    static createFrom(source: any = {}) {
	        return new TextSaveResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.saved = source["saved"];
	        this.conflict = source["conflict"];
	        this.bad = source["bad"];
	        this.size = source["size"];
	        this.modTime = source["modTime"];
	        this.data = source["data"];
	    }
	}
	export class ViewResult {
	    data: string;
	    truncated: boolean;
	    size: number;
	    modTime: string;
	
	    static createFrom(source: any = {}) {
	        return new ViewResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.data = source["data"];
	        this.truncated = source["truncated"];
	        this.size = source["size"];
	        this.modTime = source["modTime"];
	    }
	}

}

