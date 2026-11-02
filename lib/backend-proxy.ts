import "server-only";

const requestTimeoutMs = 10_000;
const maxRequestBytes = 5 * 1024 * 1024;

type ProxyOptions = {
  method: "GET" | "POST" | "DELETE";
  body?: string;
};

function jsonError(status: number, message: string): Response {
  return Response.json({ error: message }, { status });
}

export async function proxyToGo(
  path: string,
  request: Request,
  options: ProxyOptions,
): Promise<Response> {
  const backendURL = process.env.GO_API_URL?.replace(/\/+$/, "");
  const backendToken = process.env.GO_API_TOKEN;
  const accessClientID = process.env.CLOUDFLARE_ACCESS_CLIENT_ID;
  const accessClientSecret = process.env.CLOUDFLARE_ACCESS_CLIENT_SECRET;

  if (!backendURL || !backendToken) {
    return jsonError(503, "Go API is not configured on the server");
  }
  if (Boolean(accessClientID) !== Boolean(accessClientSecret)) {
    return jsonError(503, "Cloudflare Access credentials are not fully configured");
  }

  const headers = new Headers({
    Accept: "application/json",
    Authorization: `Bearer ${backendToken}`,
  });
  if (options.body !== undefined) {
    headers.set("Content-Type", "application/json");
    if (new TextEncoder().encode(options.body).byteLength > maxRequestBytes) {
      return jsonError(413, "Request body is too large");
    }
  }
  if (accessClientID && accessClientSecret) {
    headers.set("CF-Access-Client-Id", accessClientID);
    headers.set("CF-Access-Client-Secret", accessClientSecret);
  }

  let upstream: Response;
  try {
    upstream = await fetch(new URL(path, `${backendURL}/`), {
      method: options.method,
      headers,
      body: options.body,
      cache: "no-store",
      signal: AbortSignal.timeout(requestTimeoutMs),
    });
  } catch (error) {
    console.error("Go API request failed", error);
    if (error instanceof Error && error.name === "TimeoutError") {
      return jsonError(504, "Go API request timed out");
    }
    return jsonError(502, "Could not reach Go API");
  }

  if (upstream.status === 204) {
    return new Response(null, { status: 204 });
  }
  return new Response(await upstream.arrayBuffer(), {
    status: upstream.status,
    headers: {
      "Content-Type":
        upstream.headers.get("Content-Type") ?? "application/json; charset=utf-8",
      "Cache-Control": "no-store",
    },
  });
}

export async function readJSONBody(request: Request): Promise<string | Response> {
  const contentLength = Number(request.headers.get("Content-Length"));
  if (Number.isFinite(contentLength) && contentLength > maxRequestBytes) {
    return jsonError(413, "Request body is too large");
  }

  const reader = request.body?.getReader();
  if (!reader) return jsonError(400, "Request body is required");

  const chunks: Uint8Array[] = [];
  let totalBytes = 0;
  while (true) {
    const { done, value } = await reader.read();
    if (done) break;
    totalBytes += value.byteLength;
    if (totalBytes > maxRequestBytes) {
      await reader.cancel();
      return jsonError(413, "Request body is too large");
    }
    chunks.push(value);
  }
  const bytes = new Uint8Array(totalBytes);
  let offset = 0;
  for (const chunk of chunks) {
    bytes.set(chunk, offset);
    offset += chunk.byteLength;
  }
  const body = new TextDecoder().decode(bytes);
  try {
    JSON.parse(body);
  } catch {
    return jsonError(400, "Invalid JSON request body");
  }
  return body;
}
