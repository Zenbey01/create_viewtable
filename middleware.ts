import { NextRequest, NextResponse } from "next/server";
import { SESSION_COOKIE, authSecrets, verifySession } from "@/lib/session";

export async function middleware(request: NextRequest) {
  const secrets = authSecrets();
  const isApi = request.nextUrl.pathname.startsWith("/api/");
  if (!secrets) {
    // Fail closed: never serve the app without login configured.
    return isApi
      ? NextResponse.json({ error: "Login is not configured on the server" }, { status: 503 })
      : new NextResponse("Login is not configured (set APP_PASSWORD and SESSION_SECRET)", { status: 503 });
  }
  if (await verifySession(request.cookies.get(SESSION_COOKIE)?.value, secrets.secret)) {
    return NextResponse.next();
  }
  if (isApi) return NextResponse.json({ error: "กรุณาเข้าสู่ระบบ" }, { status: 401 });
  return NextResponse.redirect(new URL("/login", request.url));
}

export const config = {
  matcher: ["/((?!login$|api/login$|_next/static|_next/image|favicon.ico).*)"],
};
