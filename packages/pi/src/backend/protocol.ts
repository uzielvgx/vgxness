export const PROTOCOL = "vgxness-pi/v1";
export const MAX_RECORD_BYTES = 1 << 20;
export const MAX_ACTIVE_REQUESTS = 32;
export const MAX_CORRELATION_IDS = 4096;

export type Mode = "read-only" | "full";
export type Hello = { type: "hello"; protocol: typeof PROTOCOL; implementation: { name: string; version: string; sha256: string }; workspace: string; mode: Mode; role: string; capabilities: string[]; limits: { maxRecordBytes: number; maxActiveRequests: number; maxCorrelationIds: number } };
export type Request = { type: "request"; id: string; operation: string; workspace: string; mode: Mode; role: string; payload: unknown };
export type Result = { type: "result"; id: string; result: unknown };
export type ErrorRecord = { type: "error"; id?: string; code: string; retrySafe: boolean; message: string; recoveryState?: string };
export type Record = Hello | Request | Result | ErrorRecord | { type: "cancel"; id: string };

function hasDuplicateKeys(text: string): boolean {
  let i=0;
  const ws=()=>{while(/[\t\n\r ]/.test(text[i]??""))i++};
  const str=()=>{const start=i++;for(;i<text.length;){const c=text[i++];if(c==='"')return JSON.parse(text.slice(start,i));if(c==='\\')i+=text[i]==='u'?5:1;else if(c<' ')throw Error("string") }throw Error("string")};
  const value=():void=>{ws();if(text[i]==='"'){str();return}if(text[i]==='{'){i++;const keys=new Set<string>();ws();if(text[i]==='}'){i++;return}for(;;){ws();const key=str();if(keys.has(key))throw Error("duplicate");keys.add(key);ws();if(text[i++]!==':')throw Error("colon");value();ws();if(text[i]==='}'){i++;return}if(text[i++]!==',')throw Error("comma")}}if(text[i]==='['){i++;ws();if(text[i]===']'){i++;return}for(;;){value();ws();if(text[i]===']'){i++;return}if(text[i++]!==',')throw Error("comma")}}const m=/^(true|false|null|-?(0|[1-9]\d*)(\.\d+)?([eE][+-]?\d+)?)/.exec(text.slice(i));if(!m)throw Error("value");i+=m[0].length};
  try{value();ws();return i!==text.length}catch{return true}
}
export function decodeRecord(bytes: Uint8Array): Record {
  if (!bytes.length || bytes.length > MAX_RECORD_BYTES || bytes.at(-1) !== 10 || bytes.includes(13)) throw new Error("invalid record framing");
  const text = new TextDecoder("utf-8", { fatal: true }).decode(bytes);
  if (text.slice(0, -1).includes("\n")) throw new Error("invalid record framing");
  const json=text.slice(0,-1); if(hasDuplicateKeys(json)) throw new Error("invalid record"); let value: unknown; try { value = JSON.parse(json); } catch { throw new Error("invalid record"); }
  if (!value || Array.isArray(value) || typeof value !== "object") throw new Error("invalid record");
  const r = value as Record & { [key: string]: unknown };
  const allowed: { [key: string]: string[] } = { hello:["type","protocol","implementation","workspace","mode","role","capabilities","limits"], request:["type","id","operation","workspace","mode","role","payload"], result:["type","id","result"], error:["type","id","code","retrySafe","message","recoveryState"], cancel:["type","id"] };
  if (typeof r.type !== "string" || !allowed[r.type] || Object.keys(r).some(k => !allowed[r.type].includes(k))) throw new Error("invalid record");
  if (r.type === "hello" && !validHello(r)) throw new Error("invalid record");
  if (r.type === "request" && (!validId(r.id) || typeof r.operation !== "string" || !r.operation || typeof r.workspace !== "string" || !r.workspace || !validMode(r.mode) || typeof r.role !== "string" || !r.role || !r.payload || typeof r.payload !== "object" || Array.isArray(r.payload))) throw new Error("invalid record");
  if (r.type === "result" && (!validId(r.id) || !("result" in r))) throw new Error("invalid record");
  if (r.type === "cancel" && !validId(r.id)) throw new Error("invalid record");
  if (r.type === "error" && (!validError(r))) throw new Error("invalid record");
  return r;
}
const errorCodes = new Set(["invalid_request","handshake_rejected","unsupported","workspace_mismatch","mode_denied","authorization_denied","conflict","stale","cancelled","limit_exceeded","integrity_failed","unavailable","recovery_pending","internal"]);
function validMode(value: unknown): value is Mode { return value === "read-only" || value === "full"; }
function validHello(value: Record & { [key: string]: unknown }): boolean {
  const implementation = value.implementation, limits = value.limits;
  return value.protocol === PROTOCOL && typeof value.workspace === "string" && !!value.workspace && validMode(value.mode) && typeof value.role === "string" && !!value.role && Array.isArray(value.capabilities) && value.capabilities.every((item) => typeof item === "string") && !!implementation && typeof implementation === "object" && !Array.isArray(implementation) && Object.keys(implementation).length === 3 && typeof (implementation as any).name === "string" && !!(implementation as any).name && typeof (implementation as any).version === "string" && !!(implementation as any).version && typeof (implementation as any).sha256 === "string" && /^[a-f0-9]{64}$/.test((implementation as any).sha256) && !!limits && typeof limits === "object" && !Array.isArray(limits) && Object.keys(limits).length === 3 && ["maxRecordBytes","maxActiveRequests","maxCorrelationIds"].every((key) => Number.isSafeInteger((limits as any)[key]) && (limits as any)[key] > 0);
}
function validError(value: Record & { [key: string]: unknown }): boolean { return (value.id === undefined || validId(value.id)) && typeof value.code === "string" && errorCodes.has(value.code) && typeof value.retrySafe === "boolean" && typeof value.message === "string" && (value.recoveryState === undefined || typeof value.recoveryState === "string"); }
export function validId(id: unknown): id is string { return typeof id === "string" && /^[!-~]{1,128}$/.test(id); }
export function encodeRecord(record: Record): Uint8Array { const text = JSON.stringify(record); const b = new TextEncoder().encode(text + "\n"); if (b.length > MAX_RECORD_BYTES) throw new Error("record too large"); return b; }
export function sameHello(actual: Hello, expected: Hello): boolean { return actual.type === expected.type && actual.protocol === expected.protocol && actual.implementation.name === expected.implementation.name && actual.implementation.version === expected.implementation.version && actual.implementation.sha256 === expected.implementation.sha256 && actual.workspace === expected.workspace && actual.mode === expected.mode && actual.role === expected.role && actual.limits.maxRecordBytes === expected.limits.maxRecordBytes && actual.limits.maxActiveRequests === expected.limits.maxActiveRequests && actual.limits.maxCorrelationIds === expected.limits.maxCorrelationIds && actual.capabilities.length === expected.capabilities.length && actual.capabilities.every((capability, index) => capability === expected.capabilities[index]); }
