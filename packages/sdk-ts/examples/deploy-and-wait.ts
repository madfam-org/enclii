/**
 * Deploy a release and wait until the pods are running.
 *
 * Usage:
 *   ENCLII_TOKEN=... ENCLII_SERVICE_ID=... ENCLII_RELEASE_ID=... \
 *     tsx examples/deploy-and-wait.ts
 */

import { EncliiClient, NotFoundError } from '@madfam/enclii-sdk';

async function main() {
  const client = new EncliiClient({
    baseUrl: process.env.ENCLII_BASE_URL ?? 'https://api.enclii.dev/v1',
    token: process.env.ENCLII_TOKEN,
  });

  const serviceId = required('ENCLII_SERVICE_ID');
  const releaseId = required('ENCLII_RELEASE_ID');
  const environment = process.env.ENCLII_ENV ?? 'dev';

  console.log(`Deploying release ${releaseId} to ${serviceId} in ${environment}...`);
  const dep = await client.deployments.deploy(serviceId, {
    release_id: releaseId,
    environment_name: environment,
  });
  console.log(`Deployment ${dep.id} created (v${dep.version_number ?? '?'})`);

  try {
    const final = await client.deployments.wait(dep.id, {
      intervalMs: 5_000,
      timeoutMs: 10 * 60_000,
    });
    if (final.status === 'running') {
      console.log(`Success. Health=${final.health}`);
      process.exit(0);
    }
    console.error(`Deployment ended in ${final.status}: ${final.error_message}`);
    process.exit(1);
  } catch (err) {
    if (err instanceof NotFoundError) {
      console.error(`Deployment not found — may have been deleted: ${err.message}`);
    } else {
      console.error(`Deploy failed: ${(err as Error).message}`);
    }
    process.exit(2);
  }
}

function required(name: string): string {
  const v = process.env[name];
  if (!v) {
    console.error(`missing required env var: ${name}`);
    process.exit(64);
  }
  return v;
}

main();
