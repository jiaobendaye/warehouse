export namespace config {
	
	export class Config {
	    Host: string;
	    Port: number;
	    DBPath: string;
	    Headless: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Config(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Host = source["Host"];
	        this.Port = source["Port"];
	        this.DBPath = source["DBPath"];
	        this.Headless = source["Headless"];
	    }
	}

}

export namespace domain {
	
	export class Accessory {
	    id: number;
	    sku: string;
	    name: string;
	    current_stock: number;
	    low_stock_threshold: number;
	    notes: string;
	    created_at: string;
	    updated_at: string;
	
	    static createFrom(source: any = {}) {
	        return new Accessory(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.sku = source["sku"];
	        this.name = source["name"];
	        this.current_stock = source["current_stock"];
	        this.low_stock_threshold = source["low_stock_threshold"];
	        this.notes = source["notes"];
	        this.created_at = source["created_at"];
	        this.updated_at = source["updated_at"];
	    }
	}
	export class AccessoryUpdate {
	    name?: string;
	    low_stock_threshold?: number;
	    notes?: string;
	
	    static createFrom(source: any = {}) {
	        return new AccessoryUpdate(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.low_stock_threshold = source["low_stock_threshold"];
	        this.notes = source["notes"];
	    }
	}
	export class InventoryFlow {
	    id: number;
	    accessory_id: number;
	    type: string;
	    quantity: number;
	    unit_cost: number;
	    unit_price: number;
	    balance_after: number;
	    client_ref?: string;
	    remark?: string;
	    occurred_at: string;
	    created_at: string;
	
	    static createFrom(source: any = {}) {
	        return new InventoryFlow(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.accessory_id = source["accessory_id"];
	        this.type = source["type"];
	        this.quantity = source["quantity"];
	        this.unit_cost = source["unit_cost"];
	        this.unit_price = source["unit_price"];
	        this.balance_after = source["balance_after"];
	        this.client_ref = source["client_ref"];
	        this.remark = source["remark"];
	        this.occurred_at = source["occurred_at"];
	        this.created_at = source["created_at"];
	    }
	}

}

export namespace service {
	
	export class ReplenishmentItem {
	    accessory_id: number;
	    sku: string;
	    name: string;
	    current_stock: number;
	    threshold: number;
	    shortage: number;
	    suggested_quantity: number;
	
	    static createFrom(source: any = {}) {
	        return new ReplenishmentItem(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.accessory_id = source["accessory_id"];
	        this.sku = source["sku"];
	        this.name = source["name"];
	        this.current_stock = source["current_stock"];
	        this.threshold = source["threshold"];
	        this.shortage = source["shortage"];
	        this.suggested_quantity = source["suggested_quantity"];
	    }
	}
	export class BatchCheckResult {
	    items: ReplenishmentItem[];
	    not_found: string[];
	
	    static createFrom(source: any = {}) {
	        return new BatchCheckResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.items = this.convertValues(source["items"], ReplenishmentItem);
	        this.not_found = source["not_found"];
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
	export class BatchResult {
	    accepted: number;
	    flows: domain.InventoryFlow[];
	    ids: number[];
	
	    static createFrom(source: any = {}) {
	        return new BatchResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.accepted = source["accepted"];
	        this.flows = this.convertValues(source["flows"], domain.InventoryFlow);
	        this.ids = source["ids"];
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
	export class InboundCmd {
	    accessory_id: number;
	    quantity: number;
	    unit_cost?: number;
	    remark?: string;
	    occurred_at?: string;
	    client_ref?: string;
	
	    static createFrom(source: any = {}) {
	        return new InboundCmd(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.accessory_id = source["accessory_id"];
	        this.quantity = source["quantity"];
	        this.unit_cost = source["unit_cost"];
	        this.remark = source["remark"];
	        this.occurred_at = source["occurred_at"];
	        this.client_ref = source["client_ref"];
	    }
	}
	export class OutboundCmd {
	    accessory_id: number;
	    quantity: number;
	    unit_price?: number;
	    remark?: string;
	    occurred_at?: string;
	    client_ref?: string;
	
	    static createFrom(source: any = {}) {
	        return new OutboundCmd(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.accessory_id = source["accessory_id"];
	        this.quantity = source["quantity"];
	        this.unit_price = source["unit_price"];
	        this.remark = source["remark"];
	        this.occurred_at = source["occurred_at"];
	        this.client_ref = source["client_ref"];
	    }
	}

}

