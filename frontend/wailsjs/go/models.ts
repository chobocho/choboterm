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
	    forwards: Forward[];
	
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
	        this.forwards = this.convertValues(source["forwards"], Forward);
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
	export class Macro {
	    name: string;
	    key: string;
	    text: string;
	
	    static createFrom(source: any = {}) {
	        return new Macro(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.key = source["key"];
	        this.text = source["text"];
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
	    pasteNoConfirm: boolean;
	    keepAlive: number;
	    autoReconnect: boolean;
	    logAuto: boolean;
	    logDir: string;
	    logRaw: boolean;
	    macros: Macro[];
	    window?: WindowState;
	
	    static createFrom(source: any = {}) {
	        return new Settings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.fontSize = source["fontSize"];
	        this.pasteNoConfirm = source["pasteNoConfirm"];
	        this.keepAlive = source["keepAlive"];
	        this.autoReconnect = source["autoReconnect"];
	        this.logAuto = source["logAuto"];
	        this.logDir = source["logDir"];
	        this.logRaw = source["logRaw"];
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

}

