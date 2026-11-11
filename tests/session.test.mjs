import { test } from "node:test";
import assert from "node:assert/strict";
import { authSecrets, createSession, passwordMatches, verifySession } from "../lib/session.ts";

const secret = "s".repeat(32);

test("authSecrets fails closed unless both values are set and the secret is long enough", () => {
  const saved = { ...process.env };
  try {
    delete process.env.APP_PASSWORD;
    delete process.env.SESSION_SECRET;
    assert.equal(authSecrets(), null);
    process.env.APP_PASSWORD = "pw";
    process.env.SESSION_SECRET = "short";
    assert.equal(authSecrets(), null);
    process.env.SESSION_SECRET = secret;
    assert.deepEqual(authSecrets(), { password: "pw", secret });
  } finally {
    process.env = saved;
  }
});

test("passwordMatches accepts only the exact password", async () => {
  const secrets = { password: "admin", secret };
  assert.equal(await passwordMatches("admin", secrets), true);
  for (const bad of ["", "Admin", "admin ", "admin\0", "x".repeat(300)]) {
    assert.equal(await passwordMatches(bad, secrets), false, JSON.stringify(bad));
  }
});

test("a fresh session verifies; wrong secret, tampering and junk do not", async () => {
  const token = await createSession(secret);
  assert.equal(await verifySession(token, secret), true);
  assert.equal(await verifySession(token, "t".repeat(32)), false);

  const [expires, signature] = token.split(".");
  assert.equal(await verifySession(`${Number(expires) + 1}.${signature}`, secret), false);
  assert.equal(await verifySession(`${expires}.${signature.replace(/.$/, signature.endsWith("0") ? "1" : "0")}`, secret), false);
  for (const junk of [undefined, "", "abc", "123.", ".abc", "x.y", "9999999999.deadbeef"]) {
    assert.equal(await verifySession(junk, secret), false, String(junk));
  }
});

test("a session expires after its max age", async () => {
  const token = await createSession(secret);
  const realNow = Date.now;
  Date.now = () => realNow() + 8 * 60 * 60 * 1000 + 2000;
  try {
    assert.equal(await verifySession(token, secret), false);
  } finally {
    Date.now = realNow;
  }
});
