import { proxyToGo, readJSONBody } from "@/lib/backend-proxy";

export const runtime = "nodejs";

export function GET(request: Request): Promise<Response> {
  return proxyToGo("/api/schemas", request, { method: "GET" });
}

export async function POST(request: Request): Promise<Response> {
  const body = await readJSONBody(request);
  if (body instanceof Response) return body;
  return proxyToGo("/api/schemas", request, { method: "POST", body });
}
