/**
 * @jest-environment jsdom
 */

import { API_BASE_URL } from "./constants";

jest.mock("./api", () => ({
  getAuthHeadersRecord: () => ({ Authorization: "Bearer test-token" }),
}));

import { buildBuildLogStreamWsUrl, buildLogTailWsUrl } from "./ws-url";

describe("buildLogTailWsUrl", () => {
  it("includes service path and token query param", () => {
    const url = buildLogTailWsUrl("svc-1", { env: "production", level: ["info"] });
    expect(url).toContain("/v1/services/svc-1/logs/tail");
    expect(url).toContain("token=test-token");
    expect(url).toContain("env=production");
    const expectedProto = API_BASE_URL.startsWith("https") ? "wss://" : "ws://";
    expect(url.startsWith(expectedProto)).toBe(true);
  });
});

describe("buildBuildLogStreamWsUrl", () => {
  it("targets build log stream path", () => {
    const url = buildBuildLogStreamWsUrl("svc-1", "rel-1");
    expect(url).toContain("/v1/services/svc-1/builds/rel-1/logs/stream");
    expect(url).toContain("timestamps=true");
  });
});
