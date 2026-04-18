import { describe, expect, it } from 'vitest';
import { verifyWebhookSignature } from '../../resources/webhooks';
import {
  createStubFetch,
  jsonResponse,
  newClient,
} from '../test-helpers';

describe('WebhooksResource', () => {
  it('rejects non-https URLs before sending', async () => {
    const { fetch } = createStubFetch(() => jsonResponse({}));
    const client = newClient({ fetch });
    await expect(
      client.webhooks.create('proj-1', {
        name: 'x',
        url: 'http://insecure.example.com',
      }),
    ).rejects.toThrow(/https:\/\//);
  });

  it('create() returns the signing secret', async () => {
    const { fetch, calls } = createStubFetch(() =>
      jsonResponse(
        {
          subscription: {
            id: 'sub-1',
            url: 'https://hooks.example.com',
            event_types: [],
            active: true,
            consecutive_failures: 0,
          },
          signing_secret: 'whsec_plain',
          note: 'save this',
        },
        { status: 201 },
      ),
    );
    const client = newClient({ fetch });
    const out = await client.webhooks.create('proj-1', {
      name: 'x',
      url: 'https://hooks.example.com',
    });
    expect(out.signing_secret).toBe('whsec_plain');
    expect(calls[0]!.url).toContain('/projects/proj-1/lifecycle-webhooks');
  });

  it('test() returns the delivery', async () => {
    const { fetch } = createStubFetch(() =>
      jsonResponse(
        {
          delivery: {
            id: 'del-1',
            subscription_id: 'sub-1',
            event_type: 'test.ping',
            attempt_number: 1,
            status: 'pending',
          },
        },
        { status: 202 },
      ),
    );
    const client = newClient({ fetch });
    const out = await client.webhooks.test('sub-1');
    expect(out.id).toBe('del-1');
    expect(out.event_type).toBe('test.ping');
  });

  it('eventTypes() returns the catalogue', async () => {
    const { fetch } = createStubFetch(() =>
      jsonResponse({
        event_types: ['deploy.started', 'deploy.succeeded'],
      }),
    );
    const client = newClient({ fetch });
    const out = await client.webhooks.eventTypes();
    expect(out).toEqual(['deploy.started', 'deploy.succeeded']);
  });

  it('rotateSecret() returns a new secret', async () => {
    const { fetch } = createStubFetch(() =>
      jsonResponse({
        subscription: { id: 'sub-1', event_types: [] },
        signing_secret: 'whsec_rotated',
        note: 'save this',
      }),
    );
    const client = newClient({ fetch });
    const out = await client.webhooks.rotateSecret('sub-1');
    expect(out.signing_secret).toBe('whsec_rotated');
  });
});

describe('verifyWebhookSignature', () => {
  // HMAC-SHA256("whsec_secret", "1700000000.{\"a\":1}") computed offline.
  const SECRET = 'whsec_secret';
  const BODY = '{"a":1}';
  const TS = 1700000000;

  async function computeSig(
    secret: string,
    ts: number,
    body: string,
  ): Promise<string> {
    const enc = new TextEncoder();
    const key = await crypto.subtle.importKey(
      'raw',
      enc.encode(secret),
      { name: 'HMAC', hash: 'SHA-256' },
      false,
      ['sign'],
    );
    const sig = await crypto.subtle.sign(
      'HMAC',
      key,
      enc.encode(`${ts}.${body}`),
    );
    return Array.from(new Uint8Array(sig))
      .map((b) => b.toString(16).padStart(2, '0'))
      .join('');
  }

  it('accepts a valid signature within tolerance', async () => {
    const hex = await computeSig(SECRET, TS, BODY);
    await expect(
      verifyWebhookSignature(BODY, `t=${TS},v1=${hex}`, SECRET, {
        nowSeconds: () => TS,
      }),
    ).resolves.toBeUndefined();
  });

  it('rejects a signature signed with the wrong secret', async () => {
    const hex = await computeSig('other-secret', TS, BODY);
    await expect(
      verifyWebhookSignature(BODY, `t=${TS},v1=${hex}`, SECRET, {
        nowSeconds: () => TS,
      }),
    ).rejects.toThrow(/no matching v1 signature/);
  });

  it('rejects a stale signature outside the tolerance window', async () => {
    const hex = await computeSig(SECRET, TS, BODY);
    await expect(
      verifyWebhookSignature(BODY, `t=${TS},v1=${hex}`, SECRET, {
        nowSeconds: () => TS + 10_000, // >5min drift
      }),
    ).rejects.toThrow(/skew/);
  });

  it('rejects a missing header', async () => {
    await expect(
      verifyWebhookSignature(BODY, null, SECRET),
    ).rejects.toThrow(/missing signature/);
  });

  it('rejects a malformed header (no timestamp)', async () => {
    await expect(
      verifyWebhookSignature(BODY, 'v1=abc', SECRET),
    ).rejects.toThrow(/missing or invalid/);
  });

  it('rejects a header with no v1 signatures', async () => {
    await expect(
      verifyWebhookSignature(BODY, `t=${TS}`, SECRET, {
        nowSeconds: () => TS,
      }),
    ).rejects.toThrow(/no v1 signatures/);
  });

  it('accepts a body passed as Uint8Array', async () => {
    const bodyBytes = new TextEncoder().encode(BODY);
    const hex = await computeSig(SECRET, TS, BODY);
    await expect(
      verifyWebhookSignature(bodyBytes, `t=${TS},v1=${hex}`, SECRET, {
        nowSeconds: () => TS,
      }),
    ).resolves.toBeUndefined();
  });

  it('accepts multiple v1 signatures (rotation window)', async () => {
    const hexA = await computeSig('old-secret', TS, BODY);
    const hexB = await computeSig(SECRET, TS, BODY);
    await expect(
      verifyWebhookSignature(
        BODY,
        `t=${TS},v1=${hexA},v1=${hexB}`,
        SECRET,
        { nowSeconds: () => TS },
      ),
    ).resolves.toBeUndefined();
  });
});
