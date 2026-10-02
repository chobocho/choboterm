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
	export class HostEntry {
	    host: string;
	    port: number;
	    login: string;
	    encoding: string;
	
	    static createFrom(source: any = {}) {
	        return new HostEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.host = source["host"];
	        this.port = source["port"];
	        this.login = source["login"];
	        this.encoding = source["encoding"];
	    }
	}

}

