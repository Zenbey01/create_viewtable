import { SESSION_COOKIE, SESSION_MAX_AGE_S, authSecrets, createSession, passwordMatches } from "@/lib/session";

export const runtime = "nodejs";

// Best-effort brute-force limit (per server instance; add Vercel Firewall
// rate limiting for a hard guarantee).
const MAX_FAILURES = 5;
const WINDOW_MS = 15 * 60 * 1000;
const failures = new Map<string, { count: number; first: number }>();

function clientKey(request: Request): string {
  return request.headers.get("x-forwarded-for")?.split(",")[0].trim() || "unknown";
}

export async function POST(request: Request): Promise<Response> {
  const secrets = authSecrets();
  if (!secrets) return Response.json({ error: "ยังไม่ได้ตั้งค่า login บนเซิร์ฟเวอร์" }, { status: 503 });

  const key = clientKey(request);
  const now = Date.now();
  const entry = failures.get(key);
  if (entry && now - entry.first > WINDOW_MS) failures.delete(key);
  if ((failures.get(key)?.count ?? 0) >= MAX_FAILURES) {
    return Response.json({ error: "ลองผิดหลายครั้งเกินไป กรุณารอ 15 นาที" }, { status: 429 });
  }

  let password = "";
  try {
    const body = await request.json();
    if (typeof body?.password === "string") password = body.password.slice(0, 256);
  } catch {
    // fall through: empty password fails below
  }

  if (!(await passwordMatches(password, secrets))) {
    const current = failures.get(key) ?? { count: 0, first: now };
    failures.set(key, { count: current.count + 1, first: current.first });
    await new Promise((resolve) => setTimeout(resolve, 600));
    return Response.json({ error: "รหัสผ่านไม่ถูกต้อง" }, { status: 401 });
  }

  failures.delete(key);
  const token = await createSession(secrets.secret);
  const secure = process.env.NODE_ENV === "production" ? "; Secure" : "";
  return new Response(JSON.stringify({ ok: true }), {
    status: 200,
    headers: {
      "Content-Type": "application/json",
      "Cache-Control": "no-store",
      "Set-Cookie": `${SESSION_COOKIE}=${token}; Path=/; HttpOnly; SameSite=Strict; Max-Age=${SESSION_MAX_AGE_S}${secure}`,
    },
  });
}
