export namespace main {

	export class ConvertRequest {
	    input: string;
	    recursive: boolean;
	    overwrite: boolean;
	    deleteOriginal: boolean;
	    level: number;
	    workers: number;

	    static createFrom(source: any = {}) {
	        return new ConvertRequest(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.input = source["input"];
	        this.recursive = source["recursive"];
	        this.overwrite = source["overwrite"];
	        this.deleteOriginal = source["deleteOriginal"];
	        this.level = source["level"];
	        this.workers = source["workers"];
	    }
	}
	export class ConvertResult {
	    converter: string;
	    found: number;
	    success: number;
	    skipped: number;
	    failed: number;
	    failures: string[];
	    backupDir: string;
	    moved: number;
	    moveFailures: string[];
	    duration: string;

	    static createFrom(source: any = {}) {
	        return new ConvertResult(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.converter = source["converter"];
	        this.found = source["found"];
	        this.success = source["success"];
	        this.skipped = source["skipped"];
	        this.failed = source["failed"];
	        this.failures = source["failures"];
	        this.backupDir = source["backupDir"];
	        this.moved = source["moved"];
	        this.moveFailures = source["moveFailures"];
	        this.duration = source["duration"];
	    }
	}
	export class ConverterStatus {
	    available: boolean;
	    name: string;
	    path: string;
	    message: string;
	    help: string;

	    static createFrom(source: any = {}) {
	        return new ConverterStatus(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.available = source["available"];
	        this.name = source["name"];
	        this.path = source["path"];
	        this.message = source["message"];
	        this.help = source["help"];
	    }
	}

}

