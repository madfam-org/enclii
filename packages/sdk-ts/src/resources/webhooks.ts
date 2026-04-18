import type { EncliiClient } from '../client';
import type {
  CreateWebhookSubscriptionRequest,
  CreateWebhookSubscriptionResponse,
  OutboundWebhookDelivery,
  OutboundWebhookEventType,
  OutboundWebhookSubscription,
  Page,
  UpdateWebhookSubscriptionRequest,
} from '../types';

/**
 * Outbound lifecycle webhook subscriptions (P2.3).
 *
 * Signing secrets are returned **exactly once** at create/rotate time.
 * The server only stores a SHA-256 hash and cannot return the raw value again —
 * persist it immediately from `CreateWebhookSubscriptionResponse.signing_secret`.
 *
 * Subscribers verify deliveries using the `X-Enclii-Signature` header
 * (Stripe-compatible format: `t=<unix>,v1=<hex>`). Use the exported
 * `verifyWebhookSignature()` helper for verification.
 */
export class WebhooksResource {
  constructor(private readonly client: EncliiClient) {}

  /** List subscriptions for a project. */
  async list(
    projectSlug: string,
    options: { limit?: number; cursor?: string } = {},
  ): Promise<Page<OutboundWebhookSubscription>> {
    const resp = await this.client.get<{
      subscriptions: OutboundWebhookSubscription[];
      next_cursor?: string | null;
    }>(
      `/projects/${encodeURIComponent(projectSlug)}/lifecycle-webhooks`,
      options,
    );
    return {
      data: resp.subscriptions ?? [],
      nextCursor: resp.next_cursor ?? null,
    };
  }

  iter(
    projectSlug: string,
    options: { pageSize?: number } = {},
  ): AsyncIterable<OutboundWebhookSubscription> {
    return this.client.paginate<OutboundWebhookSubscription>(
      `/projects/${encodeURIComponent(projectSlug)}/lifecycle-webhooks`,
      { itemsField: 'subscriptions', pageSize: options.pageSize },
    );
  }

  /**
   * Create a new subscription. The returned `signing_secret` is shown **once**
   * and cannot be retrieved later — save it immediately.
   */
  async create(
    projectSlug: string,
    input: CreateWebhookSubscriptionRequest,
  ): Promise<CreateWebhookSubscriptionResponse> {
    if (!input.url.startsWith('https://')) {
      throw new Error(
        'webhooks.create: url must start with https:// (plain http is rejected by the API).',
      );
    }
    return this.client.post<CreateWebhookSubscriptionResponse>(
      `/projects/${encodeURIComponent(projectSlug)}/lifecycle-webhooks`,
      input,
    );
  }

  async get(subscriptionId: string): Promise<OutboundWebhookSubscription> {
    return this.client.get<OutboundWebhookSubscription>(
      `/lifecycle-webhooks/${encodeURIComponent(subscriptionId)}`,
    );
  }

  async update(
    subscriptionId: string,
    input: UpdateWebhookSubscriptionRequest,
  ): Promise<OutboundWebhookSubscription> {
    return this.client.patch<OutboundWebhookSubscription>(
      `/lifecycle-webhooks/${encodeURIComponent(subscriptionId)}`,
      input,
    );
  }

  /** Rotate the signing secret. New secret returned exactly once. */
  async rotateSecret(
    subscriptionId: string,
  ): Promise<CreateWebhookSubscriptionResponse> {
    return this.client.post<CreateWebhookSubscriptionResponse>(
      `/lifecycle-webhooks/${encodeURIComponent(subscriptionId)}/rotate-secret`,
      {},
    );
  }

  /** Soft-delete a subscription. */
  async delete(subscriptionId: string): Promise<void> {
    await this.client.del(
      `/lifecycle-webhooks/${encodeURIComponent(subscriptionId)}`,
    );
  }

  /**
   * Enqueue a synthetic `test.ping` delivery — useful for verifying signature
   * validation is wired correctly. Poll `deliveries()` to see the outcome.
   */
  async test(subscriptionId: string): Promise<OutboundWebhookDelivery> {
    const resp = await this.client.post<{
      delivery: OutboundWebhookDelivery;
    }>(
      `/lifecycle-webhooks/${encodeURIComponent(subscriptionId)}/test`,
      {},
    );
    return resp.delivery;
  }

  /** List recent deliveries for a subscription. */
  async deliveries(
    subscriptionId: string,
    options: { limit?: number; cursor?: string } = {},
  ): Promise<Page<OutboundWebhookDelivery>> {
    const resp = await this.client.get<{
      deliveries: OutboundWebhookDelivery[];
      next_cursor?: string | null;
    }>(
      `/lifecycle-webhooks/${encodeURIComponent(subscriptionId)}/deliveries`,
      options,
    );
    return {
      data: resp.deliveries ?? [],
      nextCursor: resp.next_cursor ?? null,
    };
  }

  /** List all subscribable event types (public — no auth required). */
  async eventTypes(): Promise<OutboundWebhookEventType[]> {
    const resp = await this.client.get<{
      event_types: OutboundWebhookEventType[];
    }>('/webhooks/event-types');
    return resp.event_types ?? [];
  }
}

// -----------------------------------------------------------------------------
// Signature verification helper
// -----------------------------------------------------------------------------

/**
 * Maximum allowable skew between subscriber clock and signature timestamp.
 * Mirrors Stripe's default and matches `OutboundWebhookSignatureTolerance`
 * on the server (5 minutes).
 */
export const DEFAULT_SIGNATURE_TOLERANCE_SECONDS = 5 * 60;

export interface VerifyWebhookSignatureOptions {
  /** Tolerance in seconds; defaults to 300 (5 min). Matches server default. */
  toleranceSeconds?: number;
  /** Override for testing — defaults to Date.now()/1000. */
  nowSeconds?: () => number;
}

/**
 * Verify an outbound webhook signature header.
 *
 * @param rawBody The raw request body — **not** a parsed JSON object. The
 *                server signs the exact bytes it sent, so whitespace matters.
 * @param signatureHeader The value of the `X-Enclii-Signature` header,
 *                         formatted `t=<unix>,v1=<hex>,v1=<hex>,...`.
 * @param secret The raw signing secret you received at subscription-create
 *                time. Never the SHA-256 hash.
 * @throws {Error} When the signature is invalid, expired, or malformed.
 *
 * @example
 *   // In an Express handler:
 *   const raw = req.rawBody; // e.g. from bodyParser.raw({ type: 'application/json' })
 *   await verifyWebhookSignature(
 *     raw,
 *     req.header('x-enclii-signature'),
 *     process.env.ENCLII_WEBHOOK_SECRET!,
 *   );
 */
export async function verifyWebhookSignature(
  rawBody: string | Uint8Array,
  signatureHeader: string | null | undefined,
  secret: string,
  options: VerifyWebhookSignatureOptions = {},
): Promise<void> {
  if (!signatureHeader) {
    throw new Error('verifyWebhookSignature: missing signature header');
  }
  const parsed = parseSignatureHeader(signatureHeader);
  if (parsed.v1Signatures.length === 0) {
    throw new Error(
      'verifyWebhookSignature: header contained no v1 signatures',
    );
  }

  const tolerance =
    options.toleranceSeconds ?? DEFAULT_SIGNATURE_TOLERANCE_SECONDS;
  const now = (options.nowSeconds ?? (() => Math.floor(Date.now() / 1000)))();
  const skew = Math.abs(now - parsed.timestamp);
  if (skew > tolerance) {
    throw new Error(
      `verifyWebhookSignature: timestamp skew ${skew}s exceeds tolerance ${tolerance}s`,
    );
  }

  const bodyBytes =
    typeof rawBody === 'string' ? new TextEncoder().encode(rawBody) : rawBody;
  const signedPayload = concatBytes(
    new TextEncoder().encode(`${parsed.timestamp}.`),
    bodyBytes,
  );
  const expected = await hmacSha256Hex(secret, signedPayload);

  // Constant-time comparison — avoids timing attacks even though we're in TS.
  const match = parsed.v1Signatures.some((sig) => timingSafeEqual(sig, expected));
  if (!match) {
    throw new Error('verifyWebhookSignature: no matching v1 signature');
  }
}

interface ParsedSignature {
  timestamp: number;
  v1Signatures: string[];
}

function parseSignatureHeader(header: string): ParsedSignature {
  const parts = header.split(',').map((p) => p.trim());
  let timestamp: number | null = null;
  const v1Signatures: string[] = [];
  for (const part of parts) {
    const eq = part.indexOf('=');
    if (eq < 0) continue;
    const key = part.slice(0, eq);
    const value = part.slice(eq + 1);
    if (key === 't') {
      timestamp = Number.parseInt(value, 10);
    } else if (key === 'v1') {
      v1Signatures.push(value);
    }
  }
  if (timestamp === null || Number.isNaN(timestamp)) {
    throw new Error(
      'verifyWebhookSignature: header missing or invalid `t=<unix>` segment',
    );
  }
  return { timestamp, v1Signatures };
}

async function hmacSha256Hex(
  secret: string,
  body: Uint8Array,
): Promise<string> {
  const subtle = getSubtleCrypto();
  const key = await subtle.importKey(
    'raw',
    new TextEncoder().encode(secret),
    { name: 'HMAC', hash: 'SHA-256' },
    false,
    ['sign'],
  );
  const sig = await subtle.sign(
    'HMAC',
    key,
    body as BufferSource,
  );
  return bytesToHex(new Uint8Array(sig));
}

function getSubtleCrypto(): SubtleCrypto {
  const fromGlobal = (globalThis as { crypto?: Crypto }).crypto;
  if (fromGlobal?.subtle) return fromGlobal.subtle;
  throw new Error(
    'verifyWebhookSignature: no SubtleCrypto implementation available. ' +
      'Node < 19 requires `globalThis.crypto = require("crypto").webcrypto` at startup.',
  );
}

function concatBytes(a: Uint8Array, b: Uint8Array): Uint8Array {
  const out = new Uint8Array(a.length + b.length);
  out.set(a, 0);
  out.set(b, a.length);
  return out;
}

function bytesToHex(bytes: Uint8Array): string {
  let hex = '';
  for (let i = 0; i < bytes.length; i++) {
    // eslint-disable-next-line @typescript-eslint/no-non-null-assertion
    hex += bytes[i]!.toString(16).padStart(2, '0');
  }
  return hex;
}

function timingSafeEqual(a: string, b: string): boolean {
  if (a.length !== b.length) return false;
  let diff = 0;
  for (let i = 0; i < a.length; i++) {
    diff |= a.charCodeAt(i) ^ b.charCodeAt(i);
  }
  return diff === 0;
}
