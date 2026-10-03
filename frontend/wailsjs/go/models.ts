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
	export class Settings {
	    fontSize: number;
	    pasteNoConfirm: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Settings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.fontSize = source["fontSize"];
	        this.pasteNoConfirm = source["pasteNoConfirm"];
	    }
	}

}

