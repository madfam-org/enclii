/**
 * Start a canary rollout and block until it reaches a terminal state
 * (succeeded, auto_rolled_back, manual_rolled_back, failed).
 *
 * Usage:
 *   ENCLII_TOKEN=... ENCLII_SERVICE_ID=... ENCLII_DIGEST=sha256:... \
 *     tsx examples/canary-rollout.ts
 */

import { EncliiClient, isCanaryTerminal } from '@madfam/enclii-sdk';

async function main() {
  const client = new EncliiClient({
    baseUrl: process.env.ENCLII_BASE_URL ?? 'https://api.enclii.dev/v1',
    token: process.env.ENCLII_TOKEN,
  });

  const serviceId = req('ENCLII_SERVICE_ID');
  const digest = req('ENCLII_DIGEST');

  console.log(`Starting 20% canary for ${serviceId} @ ${digest}...`);
  const rollout = await client.canary.start(serviceId, {
    digest,
    percentage: 20,
    validation_window_minutes: 10,
    error_rate_threshold: 0.01,
    change_ticket_url: process.env.ENCLII_CHANGE_TICKET,
  });
  console.log(`Canary ${rollout.id} in state ${rollout.state}.`);

  // Poll until terminal.
  for (;;) {
    await sleep(10_000);
    const r = await client.canary.get(serviceId, rollout.id);
    console.log(
      `  [${r.state}] canary=${r.canary_replicas}/${r.total_replicas} ` +
        `(~${r.actual_percentage?.toFixed(1) ?? '?'}%)`,
    );
    if (isCanaryTerminal(r.state)) {
      if (r.state === 'succeeded') {
        console.log('Canary promoted to stable.');
        process.exit(0);
      }
      console.error(
        `Canary ended in ${r.state}${r.last_error ? `: ${r.last_error}` : ''}`,
      );
      process.exit(1);
    }
  }
}

function req(name: string): string {
  const v = process.env[name];
  if (!v) {
    console.error(`missing ${name}`);
    process.exit(64);
  }
  return v;
}

function sleep(ms: number) {
  return new Promise((r) => setTimeout(r, ms));
}

main();
