// End-to-end: real Go API + real Next.js server, driven over HTTP the way the
// page does. Uses a fake database (pasted DDL), so no real server is touched.
// Run: npm run test:e2e   (starts both servers on free ports, ~20s)
import { test, before, after } from "node:test";
import assert from "node:assert/strict";
import { spawn, execFileSync } from "node:child_process";
import { mkdtempSync, rmSync } from "node:fs";
import { createServer } from "node:net";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";

const root = fileURLToPath(new URL("..", import.meta.url));
const win = process.platform === "win32";
// ponytail: restores tsconfig.json/next-env.d.ts with git checkout, so run it on a tree where those are committed.
const work = mkdtempSync(join(tmpdir(), "e2e-"));
const distDir = ".next-e2e";
const env = { PASSWORD: "e2e-pass", SECRET: "e".repeat(40), TOKEN: "e2e-token-0123456789" };
const procs = [];
let goPort, webPort, goBin, web;
let go;

const freePort = () => new Promise(res => {
  const s = createServer().listen(0, "127.0.0.1", () => { const { port } = s.address(); s.close(() => res(port)); });
});
const kill = p => {
  if (!p || p.exitCode !== null) return;
  if (win) { try { execFileSync("taskkill", ["/PID", String(p.pid), "/T", "/F"], { stdio: "ignore" }); } catch {} }
  else { try { process.kill(-p.pid, "SIGKILL"); } catch { p.kill("SIGKILL"); } } // whole group: next dev forks workers
};
const waitFor = async (url, ms) => {
  const end = Date.now() + ms;
  while (Date.now() < end) {
    try { if ((await fetch(url, { redirect: "manual" })).status < 500) return; } catch {}
    await new Promise(r => setTimeout(r, 400));
  }
  throw new Error(`timeout waiting for ${url}`);
};
const startGo = async () => {
  go = spawn(goBin, [], { env: { ...process.env, API_ADDR: `127.0.0.1:${goPort}`, API_TOKEN: env.TOKEN, DATA_DIR: work, ALLOWED_DB_HOSTS: "" }, stdio: "ignore", detached: !win });
  procs.push(go);
  await waitFor(`http://127.0.0.1:${goPort}/healthz`, 15000);
};

before(async () => {
  goBin = join(work, win ? "api.exe" : "api");
  execFileSync("go", ["build", "-o", goBin, "."], { cwd: join(root, "backend") });
  [goPort, webPort] = [await freePort(), await freePort()];
  await startGo();
  web = spawn(process.execPath, [join(root, "node_modules/next/dist/bin/next"), "dev", "-p", String(webPort)], {
    cwd: root, stdio: "ignore", detached: !win,
    env: { ...process.env, NEXT_DIST_DIR: distDir, APP_PASSWORD: env.PASSWORD, SESSION_SECRET: env.SECRET,
      GO_API_URL: `http://127.0.0.1:${goPort}`, GO_API_TOKEN: env.TOKEN },
  });
  procs.push(web);
  await waitFor(`http://127.0.0.1:${webPort}/login`, 120000);
});

after(async () => {
  procs.forEach(kill);
  await new Promise(r => setTimeout(r, 1500)); // let Windows release file handles
  for (const d of [work, join(root, distDir)]) try { rmSync(d, { recursive: true, force: true }); } catch {}
  // next dev rewrites these to point at the throwaway dist dir; put them back.
  try { execFileSync("git", ["checkout", "--", "tsconfig.json", "next-env.d.ts"], { cwd: root, stdio: "ignore" }); } catch {}
});

const url = p => `http://127.0.0.1:${webPort}${p}`;
const json = (cookie, method, path, body) => fetch(url(path), {
  method, redirect: "manual", headers: { "Content-Type": "application/json", ...(cookie && { Cookie: cookie }) },
  body: body === undefined ? undefined : JSON.stringify(body),
});

const FAKE_DDL = `
CREATE TABLE [dbo].[customers] ([id] INT PRIMARY KEY, [name] NVARCHAR(100));
CREATE TABLE [dbo].[orders] ([id] INT PRIMARY KEY, [customer_id] INT, [total] DECIMAL(10,2),
  CONSTRAINT fk_c FOREIGN KEY ([customer_id]) REFERENCES [dbo].[customers]([id]));`;

let cookie, dbId;

test("logged-out visitors are turned away from pages and APIs", async () => {
  const page = await json(null, "GET", "/");
  assert.equal(page.status, 307);
  assert.match(page.headers.get("location"), /\/login$/);
  assert.equal((await json(null, "GET", "/view-builder.html")).status, 307);
  assert.equal((await json(null, "GET", "/api/backend/schemas")).status, 401);
  assert.equal((await json(null, "POST", "/api/backend/introspect", {})).status, 401);
});

test("wrong password is rejected, right password gives a session cookie", async () => {
  assert.equal((await json(null, "POST", "/api/login", { password: "nope" })).status, 401);
  const ok = await json(null, "POST", "/api/login", { password: env.PASSWORD });
  assert.equal(ok.status, 200);
  cookie = ok.headers.get("set-cookie").split(";")[0];
  assert.match(ok.headers.get("set-cookie"), /HttpOnly/);
  assert.equal((await json(cookie, "GET", "/")).status, 200);
  const builder = await json(cookie, "GET", "/view-builder.html");
  assert.equal(builder.status, 200);
  assert.match(await builder.text(), /SQL View/);
});

test("starts empty (no sample databases)", async () => {
  const res = await json(cookie, "GET", "/api/backend/schemas");
  assert.equal(res.status, 200);
  assert.deepEqual(await res.json(), []);
});

test("adding a fake database parses tables, keys and foreign keys", async () => {
  const bad = await json(cookie, "POST", "/api/backend/schemas", { name: "x", engine: "sqlserver", ddl: "not sql" });
  assert.equal(bad.status, 400);

  const res = await json(cookie, "POST", "/api/backend/schemas", { name: "fake_shop", engine: "sqlserver", ddl: FAKE_DDL });
  assert.equal(res.status, 201);
  const db = await res.json();
  dbId = db.id;
  assert.deepEqual(Object.keys(db.tables).sort(), ["customers", "orders"]);
  assert.equal(db.tables.orders.pk, "id");
  assert.deepEqual(db.tables.orders.columns.find(c => c.name === "customer_id").fk, { table: "customers", column: "id" });

  const list = await (await json(cookie, "GET", "/api/backend/schemas")).json();
  assert.deepEqual(list.map(d => d.name), ["fake_shop"]);
});

test("the fake database survives a Go API restart", async () => {
  kill(go);
  await new Promise(r => setTimeout(r, 500));
  await startGo();
  const list = await (await json(cookie, "GET", "/api/backend/schemas")).json();
  assert.deepEqual(list.map(d => d.id), [dbId]);
});

test("connection import refuses hosts that are not allow-listed and leaks nothing", async () => {
  const res = await json(cookie, "POST", "/api/backend/introspect", {
    name: "live", engine: "sqlserver", connectionString: "sqlserver://sa:TopSecret1@db.example/app", save: true,
  });
  assert.equal(res.status, 400);
  assert.doesNotMatch(await res.text(), /TopSecret1/);
  const list = await (await json(cookie, "GET", "/api/backend/schemas")).json();
  assert.equal(list.length, 1, "a refused import must not be saved");
});

test("deleting removes it, and logging out locks the app again", async () => {
  assert.equal((await json(cookie, "DELETE", `/api/backend/schemas/${dbId}`)).status, 204);
  assert.deepEqual(await (await json(cookie, "GET", "/api/backend/schemas")).json(), []);
  assert.equal((await json(cookie, "DELETE", `/api/backend/schemas/${dbId}`)).status, 404);

  const out = await json(cookie, "POST", "/api/logout");
  assert.equal(out.status, 204);
  const cleared = out.headers.get("set-cookie").split(";")[0];
  assert.equal((await json(cleared, "GET", "/api/backend/schemas")).status, 401);
});
