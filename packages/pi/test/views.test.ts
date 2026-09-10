import test from "node:test";
import assert from "node:assert/strict";
import { readonlySnapshot, loadingView, nativeMemoryView, nativeStatusView } from "../src/views.ts";
test("readonly snapshots preserve observed stale data", async () => { const a=await readonlySnapshot(async()=>({x:1})); const b=await readonlySnapshot(async()=>{throw Error()},a); assert.equal(b.state,"stale");assert.equal(b.observedAt,a.observedAt);assert.deepEqual(loadingView(),{state:"loading"}); });
test("memory view is bounded and untrusted",()=>{const rows=Array.from({length:11},(_,i)=>({ID:String(i),Title:"t".repeat(300),Preview:"p".repeat(1100),CreatedAt:"bad",UpdatedAt:"bad",References:Array(9).fill("r".repeat(300))}));const v=nativeMemoryView(rows);assert.equal(v.entries.length,10);assert.equal(v.entries[0].title.length,256);assert.equal(v.entries[0].references.length,8);assert.throws(()=>nativeMemoryView({}));});
test("status exposes versions and Windows limit",()=>{const v=nativeStatusView({workspace:"w",mode:"full",role:"manager",session:{},packageVersion:"p",backendVersion:"b",piVersion:"pi",platform:"win32"});assert.equal(v.packageVersion,"p");assert.equal(v.workers.state,"unavailable");});
