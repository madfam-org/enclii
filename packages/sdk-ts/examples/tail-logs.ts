/**
 * Tail live logs for a service. Uses the Node-only subpath for graceful
 * reconnect-with-backoff. Press Ctrl-C to stop.
 *
 * Usage:
 *   ENCLII_TOKEN=... ENCLII_SERVICE_ID=... tsx examples/tail-logs.ts
 *
 * The token goes in the Authorization header, so no Origin is needed.
 * ENCLII_WS_ORIGIN is optional: set it to one of the server's allowed
 * WebSocket origins only for a switchyard-api that predates Bearer upgrades
 * without an Origin (such a server answers 403 without it).
 */

import { EncliiClient, nodeLogsTail } from '@madfam/enclii-sdk/node';

async function main() {
  const client = new EncliiClient({
    baseUrl: process.env.ENCLII_BASE_URL ?? 'https://api.enclii.dev/v1',
    token: process.env.ENCLII_TOKEN,
  });
  const serviceId = process.env.ENCLII_SERVICE_ID;
  if (!serviceId) {
    console.error('set ENCLII_SERVICE_ID');
    process.exit(64);
  }

  const abort = new AbortController();
  process.on('SIGINT', () => {
    console.log('\nstopping...');
    abort.abort();
  });

  for await (const frame of nodeLogsTail(client, serviceId, {
    env: process.env.ENCLII_ENV ?? 'production',
    origin: process.env.ENCLII_WS_ORIGIN,
    signal: abort.signal,
    maxReconnects: 10,
    onReconnect: (attempt, reason) => {
      console.error(`[reconnect attempt ${attempt}] ${reason}`);
    },
  })) {
    if (frame.type === 'log') {
      console.log(`${frame.timestamp} ${frame.pod ?? '-'} ${frame.message}`);
    } else {
      console.error(`[${frame.type}] ${frame.message}`);
    }
  }
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
