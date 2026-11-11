// The builder is one HTML file with inline script, so pull the pure pieces
// (adapters, SQL renderer, DDL parser) out of it and run them in a sandbox.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import vm from "node:vm";

const html = readFileSync(new URL("../public/view-builder.html", import.meta.url), "utf8");
const slice = (from, to) => {
  const i = html.indexOf(from);
  const j = html.indexOf(to, i);
  assert.ok(i >= 0 && j > i, `markers not found: ${from}`);
  return html.slice(i, j);
};
const sandbox = {};
vm.runInNewContext(
  slice("/* Engines open", "/* ---------- State") +
    slice("function renderSql", "/* ---------- Rendering") +
    ";globalThis.api = { renderSql, parseDDL, ENABLED_ENGINES };",
  sandbox,
);
const { renderSql, ENABLED_ENGINES } = sandbox.api;
// Objects built inside the vm have a different prototype, so compare plain JSON copies.
const parseDDL = text => JSON.parse(JSON.stringify(sandbox.api.parseDDL(text)));

const model = (over = {}) => ({
  database: "HRDB", schema: "dbo", view: "v_leave",
  from: { table: "employees", alias: "e" },
  columns: [{ alias: "e", column: "full_name", as: "full_name" }, { alias: "d", column: "name", as: "name" }],
  joins: [{ type: "LEFT", table: "departments", alias: "d",
    left: { alias: "e", column: "department_id" }, right: { alias: "d", column: "id" },
    notDeleted: [{ alias: "d", column: "deleted_at" }] }],
  notDeleted: [{ alias: "e", column: "deleted_at" }],
  ...over,
});

test("only SQL Server is enabled for now", () => {
  assert.deepEqual([...ENABLED_ENGINES], ["sqlserver"]);
});

test("SQL Server output has USE/GO, schema-qualified names and a terminating semicolon", () => {
  assert.equal(renderSql(model(), "sqlserver"), [
    "USE [HRDB];", "GO", "",
    "CREATE OR ALTER VIEW [dbo].[v_leave] AS",
    "SELECT",
    "  [e].[full_name] AS [full_name],",
    "  [d].[name] AS [name]",
    "FROM [dbo].[employees] AS [e]",
    "LEFT JOIN [dbo].[departments] AS [d]",
    "  ON [d].[id] = [e].[department_id]",
    "  AND [d].[deleted_at] IS NULL",
    "WHERE [e].[deleted_at] IS NULL;",
    "GO",
  ].join("\n"));
});

test("without database/schema (pasted DDL) it stays unqualified and has no USE", () => {
  const sql = renderSql(model({ database: "", schema: "", joins: [], notDeleted: [] }), "sqlserver");
  assert.ok(!sql.includes("USE"));
  assert.ok(sql.includes("FROM [employees] AS [e];\nGO"));
});

test("identifiers are escaped per dialect", () => {
  const m = model({ database: "", schema: "", joins: [], notDeleted: [], view: "v]x" });
  assert.ok(renderSql(m, "sqlserver").includes("[v]]x]"));
  assert.ok(renderSql(m, "mysql").includes("`v]x`"));
  assert.ok(renderSql({ ...m, view: 'v"x' }, "postgres").includes('"v""x"'));
});

test("MySQL/PostgreSQL output is unchanged (no USE, no GO)", () => {
  for (const key of ["mysql", "postgres"]) {
    const sql = renderSql(model(), key);
    assert.ok(!/\bUSE\b|\bGO\b/.test(sql), key);
    assert.ok(sql.endsWith(";"), key);
  }
});

test("parseDDL reads bracketed SQL Server DDL with primary and foreign keys", () => {
  const t = parseDDL(`
    CREATE TABLE [dbo].[departments] ([id] INT NOT NULL PRIMARY KEY, [name] NVARCHAR(50));
    CREATE TABLE [dbo].[employees] (
      [id] INT IDENTITY(1,1) NOT NULL,
      [department_id] INT,
      CONSTRAINT pk_e PRIMARY KEY ([id]),
      CONSTRAINT fk_d FOREIGN KEY ([department_id]) REFERENCES [dbo].[departments]([id])
    );`);
  assert.deepEqual(Object.keys(t).sort(), ["departments", "employees"]);
  assert.equal(t.employees.pk, "id");
  assert.deepEqual(t.employees.columns.find(c => c.name === "department_id").fk, { table: "departments", column: "id" });
  assert.equal(t.employees.columns[0].type, "INT");
});

test("parseDDL returns nothing for text with no CREATE TABLE", () => {
  assert.deepEqual(parseDDL("SELECT 1; -- nope"), {});
});
