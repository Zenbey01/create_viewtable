// Web Crypto only, so it runs in both middleware (edge) and route handlers.
export const SESSION_COOKIE = "session";
export const SESSION_MAX_AGE_S = 8 * 60 * 60;

const encoder = new TextEncoder();

async function hmac(secret: string, message: string): Promise<Uint8Array> {
  const key = await crypto.subtle.importKey(
    "raw",
    encoder.encode(secret),
    { name: "HMAC", hash: "SHA-256" },
    false,
    ["sign"],
  );
  return new Uint8Array(await crypto.subtle.sign("HMAC", key, encoder.encode(message)));
}

function toHex(bytes: Uint8Array): string {
  return Array.from(bytes, (b) => b.toString(16).padStart(2, "0")).join("");
}

function equal(a: Uint8Array, b: Uint8Array): boolean {
  if (a.length !== b.length) return false;
  let diff = 0;
  for (let i = 0; i < a.length; i++) diff |= a[i] ^ b[i];
  return diff === 0;
}

type Secrets = { password: string; secret: string };

export function authSecrets(): Secrets | null {
  const password = process.env.APP_PASSWORD;
  const secret = process.env.SESSION_SECRET;
  if (!password || !secret || secret.length < 32) return null;
  return { password, secret };
}

/** Constant-time password check (compares HMACs, so length does not leak). */
export async function passwordMatches(input: string, secrets: Secrets): Promise<boolean> {
  return equal(await hmac(secrets.secret, input), await hmac(secrets.secret, secrets.password));
}

export async function createSession(secret: string): Promise<string> {
  const expires = String(Math.floor(Date.now() / 1000) + SESSION_MAX_AGE_S);
  return `${expires}.${toHex(await hmac(secret, expires))}`;
}

export async function verifySession(token: string | undefined, secret: string): Promise<boolean> {
  if (!token) return false;
  const [expires, signature] = token.split(".");
  if (!expires || !signature || !/^\d+$/.test(expires)) return false;
  if (Number(expires) < Date.now() / 1000) return false;
  return equal(encoder.encode(signature), encoder.encode(toHex(await hmac(secret, expires))));
}
