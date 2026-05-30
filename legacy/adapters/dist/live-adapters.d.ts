import type { DatabaseClient } from "./database-adapter.js";
import type { Adapter } from "./types.js";
export interface LiveAdapterHttpOptions {
    readonly fetch?: typeof fetch | undefined;
    readonly googleApiBaseUrl?: string | undefined;
    readonly googleTokenUrl?: string | undefined;
    readonly whatsappApiBaseUrl?: string | undefined;
    readonly telegramApiBaseUrl?: string | undefined;
}
export interface SmtpTransportOptions {
    readonly host?: string | undefined;
    readonly port?: number | undefined;
    readonly secure?: boolean | undefined;
    readonly username?: string | undefined;
    readonly password?: string | undefined;
    readonly from?: string | undefined;
}
export interface LiveAdapterOptions extends LiveAdapterHttpOptions {
    readonly smtp?: SmtpTransportOptions | undefined;
    readonly database?: DatabaseClient | undefined;
    readonly sqliteBin?: string | undefined;
}
export declare function createDefaultLiveAdapters(options?: LiveAdapterOptions): Map<string, Adapter>;
//# sourceMappingURL=live-adapters.d.ts.map