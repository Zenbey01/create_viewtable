import { proxyToGo } from "@/lib/backend-proxy";

export const runtime = "nodejs";

export async function DELETE(
  request: Request,
  context: { params: Promise<{ id: string }> },
): Promise<Response> {
  const { id } = await context.params;
  if (!/^[\w-]+$/.test(id)) {
    return Response.json({ error: "Invalid schema ID" }, { status: 400 });
  }
  return proxyToGo(`/api/schemas/${encodeURIComponent(id)}`, request, {
    method: "DELETE",
  });
}
