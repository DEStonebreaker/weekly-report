export namespace app {
	
	export class Recipient {
	    name: string;
	    address: string;
	
	    static createFrom(source: any = {}) {
	        return new Recipient(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.address = source["address"];
	    }
	}
	export class Config {
	    sender: string;
	    subject: string;
	    taskListId: string;
	    startDate: string;
	    endDate: string;
	    windowDays: number;
	    scheduleEnabled: boolean;
	    scheduleTime: string;
	    recipients: Recipient[];
	
	    static createFrom(source: any = {}) {
	        return new Config(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sender = source["sender"];
	        this.subject = source["subject"];
	        this.taskListId = source["taskListId"];
	        this.startDate = source["startDate"];
	        this.endDate = source["endDate"];
	        this.windowDays = source["windowDays"];
	        this.scheduleEnabled = source["scheduleEnabled"];
	        this.scheduleTime = source["scheduleTime"];
	        this.recipients = this.convertValues(source["recipients"], Recipient);
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
	export class DeliveryResult {
	    recipient: Recipient;
	    status: string;
	    messageId?: string;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new DeliveryResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.recipient = this.convertValues(source["recipient"], Recipient);
	        this.status = source["status"];
	        this.messageId = source["messageId"];
	        this.error = source["error"];
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
	export class MessagePreview {
	    recipient: Recipient;
	    html: string;
	
	    static createFrom(source: any = {}) {
	        return new MessagePreview(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.recipient = this.convertValues(source["recipient"], Recipient);
	        this.html = source["html"];
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
	
	export class ReportPreview {
	    sender: string;
	    subject: string;
	    taskListId: string;
	    startDate: string;
	    endDate: string;
	    messages: MessagePreview[];
	
	    static createFrom(source: any = {}) {
	        return new ReportPreview(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sender = source["sender"];
	        this.subject = source["subject"];
	        this.taskListId = source["taskListId"];
	        this.startDate = source["startDate"];
	        this.endDate = source["endDate"];
	        this.messages = this.convertValues(source["messages"], MessagePreview);
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

export namespace google {
	
	export class TaskList {
	    ID: string;
	    Title: string;
	
	    static createFrom(source: any = {}) {
	        return new TaskList(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ID = source["ID"];
	        this.Title = source["Title"];
	    }
	}

}

export namespace main {
	
	export class DesktopInfo {
	    name: string;
	    platform: string;
	    arch: string;
	
	    static createFrom(source: any = {}) {
	        return new DesktopInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.platform = source["platform"];
	        this.arch = source["arch"];
	    }
	}
	export class PreparedPreview {
	    id: string;
	    preview: app.ReportPreview;
	
	    static createFrom(source: any = {}) {
	        return new PreparedPreview(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.preview = this.convertValues(source["preview"], app.ReportPreview);
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
	export class ScheduledRunStatus {
	    time: string;
	    status: string;
	    source?: string;
	    startDate?: string;
	    endDate?: string;
	    taskCount?: number;
	    results?: app.DeliveryResult[];
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new ScheduledRunStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.time = source["time"];
	        this.status = source["status"];
	        this.source = source["source"];
	        this.startDate = source["startDate"];
	        this.endDate = source["endDate"];
	        this.taskCount = source["taskCount"];
	        this.results = this.convertValues(source["results"], app.DeliveryResult);
	        this.error = source["error"];
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
	export class SendOutcome {
	    results: app.DeliveryResult[];
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new SendOutcome(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.results = this.convertValues(source["results"], app.DeliveryResult);
	        this.error = source["error"];
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
	export class SettingsSaveResult {
	    saved: boolean;
	    schedulerActive: boolean;
	    scheduleError?: string;
	
	    static createFrom(source: any = {}) {
	        return new SettingsSaveResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.saved = source["saved"];
	        this.schedulerActive = source["schedulerActive"];
	        this.scheduleError = source["scheduleError"];
	    }
	}
	export class SetupData {
	    settings: app.Config;
	    settingsSaved: boolean;
	    googleConnected: boolean;
	    credentialsPath: string;
	    schedulerActive: boolean;
	    lastScheduledRun?: ScheduledRunStatus;
	
	    static createFrom(source: any = {}) {
	        return new SetupData(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.settings = this.convertValues(source["settings"], app.Config);
	        this.settingsSaved = source["settingsSaved"];
	        this.googleConnected = source["googleConnected"];
	        this.credentialsPath = source["credentialsPath"];
	        this.schedulerActive = source["schedulerActive"];
	        this.lastScheduledRun = this.convertValues(source["lastScheduledRun"], ScheduledRunStatus);
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

