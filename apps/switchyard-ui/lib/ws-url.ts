import { API_BASE_URL } from "@/lib/constants";
import { getAuthHeadersRecord } from "@/lib/api";

function appendWsAuth(params: URLSearchParams): void {
  const auth = getAuthHeadersRecord(false);
  if (auth.Authorization?.startsWith("Bearer ")) {
    params.set("token", auth.Authorization.slice("Bearer ".length));
  }
}

/**
 * Build a Switchyard WebSocket URL. Token is passed as a query param because
 * the browser WebSocket API cannot set Authorization headers.
 */
export function buildSwitchyardWsUrl(
  path: string,
  query: Record<string, string | string[] | undefined> = {},
): string {
  const wsProtocol = API_BASE_URL.startsWith("https") ? "wss" : "ws";
  const host = API_BASE_URL.replace(/^https?:\/\//, "");
  const params = new URLSearchParams();
  for (const [key, value] of Object.entries(query)) {
    if (value === undefined) continue;
    if (Array.isArray(value)) {
      for (const v of value) params.append(key, v);
    } else {
      params.set(key, value);
    }
  }
  appendWsAuth(params);
  const qs = params.toString();
  return `${wsProtocol}://${host}${path}${qs ? `?${qs}` : ""}`;
}

/** Loki tail stream for a running service. */
export function buildLogTailWsUrl(
  serviceId: string,
  query: Record<string, string | string[] | undefined>,
): string {
  return buildSwitchyardWsUrl(`/v1/services/${serviceId}/logs/tail`, query);
}

/** Build log stream for a specific release. */
export function buildBuildLogStreamWsUrl(
  serviceId: string,
  releaseId: string,
  query: Record<string, string | string[] | undefined> = {},
): string {
  return buildSwitchyardWsUrl(
    `/v1/services/${serviceId}/builds/${releaseId}/logs/stream`,
    { timestamps: "true", ...query },
  );
}
