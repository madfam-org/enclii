/**
 * Create an outbound lifecycle webhook subscription and verify a signed
 * delivery. Illustrates the "signing secret returned exactly once" contract.
 *
 * Usage:
 *   ENCLII_TOKEN=... ENCLII_PROJECT=my-project \
 *     ENCLII_WEBHOOK_URL=https://hooks.example.com/enclii \
 *     tsx examples/webhook-subscription.ts
 */

import {
  EncliiClient,
  verifyWebhookSignature,
} from '@madfam/enclii-sdk';

async function main() {
  const client = new EncliiClient({
    baseUrl: process.env.ENCLII_BASE_URL ?? 'https://api.enclii.dev/v1',
    token: process.env.ENCLII_TOKEN,
  });

  const project = req('ENCLII_PROJECT');
  const url = req('ENCLII_WEBHOOK_URL');

  // 1. Create the subscription. This returns the raw signing secret exactly once.
  const { subscription, signing_secret } = await client.webhooks.create(
    project,
    {
      name: 'example-subscription',
      url,
      event_types: ['deploy.succeeded', 'deploy.failed', 'rollback.succeeded'],
    },
  );
  console.log(`Created subscription ${subscription.id}`);
  console.log(`URL:          ${subscription.url}`);
  console.log(`Events:       ${subscription.event_types.join(', ')}`);
  console.log();
  console.log('=== SIGNING SECRET (save now — never shown again) ===');
  console.log(signing_secret);
  console.log();

  // 2. Trigger a synthetic test.ping to verify the endpoint handles our
  //    signed deliveries correctly.
  console.log('Firing test.ping...');
  const delivery = await client.webhooks.test(subscription.id);
  console.log(`Test delivery ${delivery.id} enqueued.`);

  // 3. In a real receiver, you'd verify signatures like this. The raw body is
  //    the exact bytes you received — do NOT re-serialize a parsed JSON object,
  //    whitespace differences break the HMAC.
  const exampleRawBody = '{"example":true}';
  const exampleTimestamp = Math.floor(Date.now() / 1000);
  const exampleSig = await computeSignatureFixture(
    signing_secret,
    exampleTimestamp,
    exampleRawBody,
  );
  await verifyWebhookSignature(
    exampleRawBody,
    `t=${exampleTimestamp},v1=${exampleSig}`,
    signing_secret,
  );
  console.log('Signature verification round-trip works.');
}

async function computeSignatureFixture(
  secret: string,
  ts: number,
  body: string,
): Promise<string> {
  const key = await crypto.subtle.importKey(
    'raw',
    new TextEncoder().encode(secret),
    { name: 'HMAC', hash: 'SHA-256' },
    false,
    ['sign'],
  );
  const sig = await crypto.subtle.sign(
    'HMAC',
    key,
    new TextEncoder().encode(`${ts}.${body}`),
  );
  return Array.from(new Uint8Array(sig))
    .map((b) => b.toString(16).padStart(2, '0'))
    .join('');
}

function req(name: string): string {
  const v = process.env[name];
  if (!v) {
    console.error(`missing ${name}`);
    process.exit(64);
  }
  return v;
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
