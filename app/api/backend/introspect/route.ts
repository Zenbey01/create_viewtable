import { proxyToGo, readJSONBody } from "@/lib/backend-proxy";

export const runtime = "nodejs";

export async function POST(request: Request): Promise<Response> {
  const body = await readJSONBody(request);
  if (body instanceof Response) return body;
  return proxyToGo("/api/introspect", request, { method: "POST", body });
}
