/**
 * Tail live logs for a service. Uses the Node-only subpath for graceful
 * reconnect-with-backoff. Press Ctrl-C to stop.
 *
 * Usage:
 *   ENCLII_TOKEN=... ENCLII_SERVICE_ID=... ENCLII_WS_ORIGIN=... tsx examples/tail-logs.ts
 *
 * ENCLII_WS_ORIGIN must be one of the server's allowed WebSocket origins;
 * the server refuses an upgrade without a matching Origin header.
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
