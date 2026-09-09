import { createHash } from "node:crypto";
import { readFile, writeFile } from "node:fs/promises";
import { canonicalContract, loadManagerContract } from "../src/orchestration/contract.ts";
import { renderPiManagerPrompt } from "../src/orchestration/adapter.ts";
const source=JSON.parse(await readFile(new URL("../../../internal/orchestration/manager_contract.json",import.meta.url)));
const digest=createHash("sha256").update(canonicalContract(source)).digest("hex");
const output=JSON.stringify({...source,sourceDigest:digest})+"\n";
const target=new URL("../resources/orchestration/contract.json",import.meta.url);
const prompt=renderPiManagerPrompt({...source,sourceDigest:digest});
const promptTarget=new URL("../resources/prompts/manager.md",import.meta.url);
if (process.argv.includes("--check")) { if (await readFile(target,"utf8") !== output || await readFile(promptTarget,"utf8") !== prompt) throw new Error("generated contract drift"); loadManagerContract(target); }
else { await writeFile(target,output); loadManagerContract(target); await writeFile(promptTarget,prompt); }
