import { SESSION_COOKIE } from "@/lib/session";

export const runtime = "nodejs";

export function POST(): Response {
  return new Response(null, {
    status: 204,
    headers: { "Set-Cookie": `${SESSION_COOKIE}=; Path=/; HttpOnly; SameSite=Strict; Max-Age=0` },
  });
}
