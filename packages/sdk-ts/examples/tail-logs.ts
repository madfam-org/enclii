/**
 * Tail live logs for a service. Uses the Node-only subpath for graceful
 * reconnect-with-backoff. Press Ctrl-C to stop.
 *
 * Usage:
 *   ENCLII_TOKEN=... ENCLII_SERVICE_ID=... tsx examples/tail-logs.ts
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

  for await (const entry of nodeLogsTail(client, serviceId, {
    level: 'info',
    signal: abort.signal,
    maxReconnects: 10,
    onReconnect: (attempt, reason) => {
      console.error(`[reconnect attempt ${attempt}] ${reason}`);
    },
  })) {
    const ts = entry.timestamp;
    const level = entry.level ?? '-';
    console.log(`${ts} [${level}] ${entry.pod} ${entry.message}`);
  }
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
