import { chromium } from "playwright-core";
// Browser test of the real UI with a fake database (pasted DDL).
// Needs the app running: Go API (with API_TOKEN) + `npm run dev`, then `npm run test:ui`.
// Env: BASE_URL (default http://localhost:3000), APP_PASSWORD (default admin).
import { mkdirSync, readFileSync } from "node:fs";
import assert from "node:assert/strict";
import { fileURLToPath } from "node:url";
import { join } from "node:path";

const BASE = process.env.BASE_URL || "http://localhost:3000";
const PASSWORD = process.env.APP_PASSWORD || "admin";
const OUT = join(fileURLToPath(new URL(".", import.meta.url)), "screenshots") + "/";
mkdirSync(OUT, { recursive: true });
const steps = [];
const ok = (name) => { steps.push(name); console.log("✔", name); };

const browser = await chromium.launch({ channel: "chrome", headless: true });
const ctx = await browser.newContext({ viewport: { width: 1440, height: 860 }, acceptDownloads: true });
await ctx.grantPermissions(["clipboard-read", "clipboard-write"], { origin: BASE });
const page = await ctx.newPage();
page.on("pageerror", e => { throw new Error("page error: " + e.message); });
page.on("dialog", d => d.accept());

// Remove any fake_shop left behind by an earlier failed run.
async function cleanup() {
  await ctx.request.post(BASE + "/api/login", { data: { password: PASSWORD } });
  const list = await (await ctx.request.get(BASE + "/api/backend/schemas")).json();
  for (const d of list.filter(d => d.name === "fake_shop")) await ctx.request.delete(`${BASE}/api/backend/schemas/${d.id}`);
  await ctx.request.post(BASE + "/api/logout");
}
await cleanup();

try {
  // 1. logged out -> login page
  await page.goto(BASE + "/view-builder.html");
  await page.waitForURL("**/login");
  ok("logged-out visit is redirected to /login");

  // 2. wrong then right password
  await page.fill("#password", "wrong");
  await page.click("button[type=submit]");
  await page.locator(".loginError").waitFor();
  assert.match(await page.locator(".loginError").innerText(), /ไม่ถูกต้อง/);
  await page.fill("#password", PASSWORD);
  await Promise.all([page.waitForURL(BASE + "/"), page.click("button[type=submit]")]);
  ok("wrong password shows error, admin logs in");

  // 3. builder, empty state, MySQL/PostgreSQL locked
  await page.goto(BASE + "/view-builder.html");
  await page.waitForSelector("#db-select");
  await page.waitForFunction(() => /เพิ่มฐานกลาง/.test(document.getElementById("table-list").innerText));
  await page.screenshot({ path: OUT + "1-empty.png" });
  ok("empty state shown (no sample databases)");

  // 4. add a fake database from DDL
  await page.click("#add-db");
  await page.fill("#add-name", "fake_shop");
  assert.equal(await page.locator("#add-engine").inputValue(), "sqlserver");
  assert.ok(await page.$eval('#add-engine option[value="mysql"]', o => o.disabled));
  await page.fill("#add-ddl", `CREATE TABLE [dbo].[customers] ([id] INT PRIMARY KEY, [name] NVARCHAR(100), [email] NVARCHAR(100));
CREATE TABLE [dbo].[orders] ([id] INT PRIMARY KEY, [customer_id] INT, [total] DECIMAL(10,2),
  CONSTRAINT fk_c FOREIGN KEY ([customer_id]) REFERENCES [dbo].[customers]([id]));`);
  await page.screenshot({ path: OUT + "2-add-dialog.png" });
  await page.click('#add-form [type="submit"]');
  await page.waitForSelector("#tbl-customers");
  assert.match(await page.locator("#db-select").innerText(), /fake_shop · SQL Server · 2 ตาราง/);
  assert.ok(await page.locator("#target-mysql").isDisabled());
  assert.ok(await page.locator("#target-postgres").isDisabled());
  assert.ok(await page.locator("#target-sqlserver").isEnabled());
  ok("fake DB added from DDL (SQL Server only), MySQL/PostgreSQL buttons disabled");

  // 5. pick tables + columns -> SQL
  await page.check("#tbl-orders");
  await page.check("#tbl-customers");
  await page.check("#col-orders_total");
  await page.check("#col-customers_name");
  await page.fill("#view-name", "v_orders");
  const sql = await page.locator("#sql").innerText();
  assert.equal(sql, [
    "CREATE OR ALTER VIEW [v_orders] AS", "SELECT",
    "  [o].[total] AS [total],", "  [c].[name] AS [name]",
    "FROM [orders] AS [o]", "INNER JOIN [customers] AS [c]",
    "  ON [c].[id] = [o].[customer_id]", ";".length ? "GO" : "",
  ].join("\n").replace("[o].[customer_id]\nGO", "[o].[customer_id];\nGO"));
  assert.match(await page.locator(".join .chip.ok").first().innerText(), /foreign key/);
  await page.screenshot({ path: OUT + "3-sql.png" });
  ok("FK join detected, SQL Server view generated (CREATE OR ALTER ... GO)");

  // PII warning for email
  await page.check("#col-customers_email");
  assert.match(await page.locator("#notes").innerText(), /ข้อมูลส่วนบุคคล/);
  await page.uncheck("#col-customers_email");
  ok("personal-data warning appears for email");

  // 6. download + copy
  const [download] = await Promise.all([page.waitForEvent("download"), page.click("#download")]);
  assert.equal(download.suggestedFilename(), "v_orders.sql");
  const file = readFileSync(await download.path(), "utf8");
  assert.ok(file.startsWith("\ufeffCREATE OR ALTER VIEW [v_orders]"));
  assert.ok(file.trimEnd().endsWith("GO"));
  await page.click("#copy");
  await page.waitForFunction(() => document.getElementById("copy").innerText === "คัดลอกแล้ว");
  const nl = t => t.split(String.fromCharCode(13)).join("");
  assert.equal(nl(await page.evaluate(() => navigator.clipboard.readText())), nl(sql));
  ok("download gives v_orders.sql (UTF-8 BOM), clipboard holds the exact SQL");

  // 7. persistence across reload
  await page.reload();
  await page.waitForSelector("#tbl-customers");
  assert.match(await page.locator("#db-select").innerText(), /fake_shop/);
  ok("saved database is still there after reload");

  // 8. delete -> empty again; logout
  await page.click("#remove-db");
  await page.waitForFunction(() => /เพิ่มฐานกลาง/.test(document.getElementById("table-list").innerText));
  ok("delete removes the database");
  await Promise.all([page.waitForURL("**/login"), page.click("#logout")]);
  await page.goto(BASE + "/view-builder.html");
  await page.waitForURL("**/login");
  ok("logout locks the app again");
} catch (e) {
  await page.screenshot({ path: OUT + "FAIL.png" }).catch(() => {});
  console.error("✖ FAILED:", e.message);
  process.exitCode = 1;
} finally {
  await cleanup().catch(() => {});
  await browser.close();
  console.log(`${steps.length} steps passed`);
}
