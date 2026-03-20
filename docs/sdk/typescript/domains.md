---
title: Domains
description: Configure custom domains with the Enclii TypeScript SDK
sidebar_position: 6
tags: [sdk, typescript, domains, ssl, dns]
---

# Domains

Configure custom domains using the TypeScript SDK.

## Overview

Custom domains allow you to serve your services on your own domain names with automatic SSL certificates.

```typescript
import { EncliiClient } from '@enclii/sdk';

const enclii = new EncliiClient();

// Domains module
enclii.domains.list(serviceId);
enclii.domains.add(serviceId, domain);
enclii.domains.verify(domainId);
enclii.domains.remove(domainId);
```

## List Domains

```typescript
// List all domains for a service
const domains = await enclii.services.listDomains('svc_xyz789');

for (const domain of domains) {
  console.log(`${domain.name}:`);
  console.log(`  Status: ${domain.status}`);
  console.log(`  SSL: ${domain.ssl.status}`);
  console.log(`  DNS: ${domain.dns.verified ? 'Verified' : 'Pending'}`);
}
```

### Filter Domains

```typescript
// Get only verified domains
const verifiedDomains = await enclii.services.listDomains('svc_xyz789', {
  verified: true,
});

// Get domains with SSL issues
const sslPending = await enclii.services.listDomains('svc_xyz789', {
  sslStatus: 'pending',
});
```

## Add Domain

### Basic Domain

```typescript
const domain = await enclii.domains.add('svc_xyz789', 'api.example.com');

console.log(`Domain added: ${domain.name}`);
console.log(`DNS Target: ${domain.dns.target}`);
console.log(`Instructions: Add a CNAME record pointing to ${domain.dns.target}`);
```

### With Options

```typescript
const domain = await enclii.domains.add('svc_xyz789', 'api.example.com', {
  // Enable automatic redirects
  redirectWww: true,  // Redirect www.api.example.com → api.example.com

  // Custom paths
  pathPrefix: '/v1',  // Serve this domain at /v1/* paths only

  // Force HTTPS
  forceHttps: true,
});
```

### Apex (Root) Domain

```typescript
// Add root domain (example.com without subdomain)
const domain = await enclii.domains.add('svc_xyz789', 'example.com', {
  type: 'apex',  // Special handling for root domains
});

// For apex domains, you may need an A record instead of CNAME
console.log(`DNS Type: ${domain.dns.recordType}`);  // 'A' or 'CNAME'
console.log(`DNS Value: ${domain.dns.target}`);
```

### Wildcard Domain

```typescript
// Add wildcard domain
const domain = await enclii.domains.add('svc_xyz789', '*.api.example.com', {
  type: 'wildcard',
});

// Matches: foo.api.example.com, bar.api.example.com, etc.
```

## Verify Domain

```typescript
// Check verification status
const domain = await enclii.domains.verify('domain_abc123');

if (domain.dns.verified) {
  console.log('Domain verified!');
} else {
  console.log('DNS not configured correctly');
  console.log(`Expected: ${domain.dns.recordType} record pointing to ${domain.dns.target}`);
  console.log(`Found: ${domain.dns.current || 'No record found'}`);
}
```

### Wait for Verification

```typescript
const domain = await enclii.domains.add('svc_xyz789', 'api.example.com');

console.log('Please configure DNS:');
console.log(`Type: ${domain.dns.recordType}`);
console.log(`Name: ${domain.name}`);
console.log(`Value: ${domain.dns.target}`);

// Wait for DNS to propagate (with timeout)
const verified = await enclii.domains.waitForVerification('domain_abc123', {
  timeout: 300000,  // 5 minutes
  pollInterval: 10000,  // Check every 10 seconds
});

if (verified) {
  console.log('Domain verified and SSL provisioned!');
} else {
  console.log('Verification timed out. Check your DNS settings.');
}
```

## SSL Certificates

### Check SSL Status

```typescript
const domain = await enclii.domains.get('domain_abc123');

console.log(`SSL Status: ${domain.ssl.status}`);
console.log(`Issuer: ${domain.ssl.issuer}`);
console.log(`Valid Until: ${domain.ssl.validTo}`);
```

### Force SSL Renewal

```typescript
// Renew certificate before expiration
await enclii.domains.renewSsl('domain_abc123');
```

### Custom SSL Certificate

```typescript
// Upload your own certificate (enterprise feature)
await enclii.domains.uploadSsl('domain_abc123', {
  certificate: fs.readFileSync('cert.pem', 'utf8'),
  privateKey: fs.readFileSync('key.pem', 'utf8'),
  chain: fs.readFileSync('chain.pem', 'utf8'),
});
```

## Remove Domain

```typescript
// Remove domain from service
await enclii.domains.remove('domain_abc123', {
  confirm: true,
});

console.log('Domain removed. You can now delete the DNS record.');
```

## Domain Settings

### Update Settings

```typescript
await enclii.domains.update('domain_abc123', {
  forceHttps: true,
  redirectWww: true,
  headers: {
    'X-Frame-Options': 'DENY',
    'X-Content-Type-Options': 'nosniff',
  },
});
```

### CORS Configuration

```typescript
await enclii.domains.update('domain_abc123', {
  cors: {
    enabled: true,
    origins: ['https://app.example.com', 'https://admin.example.com'],
    methods: ['GET', 'POST', 'PUT', 'DELETE'],
    headers: ['Content-Type', 'Authorization'],
    credentials: true,
    maxAge: 86400,
  },
});
```

### Rate Limiting

```typescript
await enclii.domains.update('domain_abc123', {
  rateLimit: {
    enabled: true,
    requestsPerMinute: 100,
    burstSize: 20,
  },
});
```

## DNS Configuration Helpers

### Get DNS Instructions

```typescript
const domain = await enclii.domains.add('svc_xyz789', 'api.example.com');

// Get formatted DNS instructions
const instructions = await enclii.domains.getDnsInstructions('domain_abc123');

console.log('DNS Configuration:');
console.log('==================');
console.log(`Provider: ${instructions.provider || 'Any DNS provider'}`);
console.log('');
console.log('Add the following record:');
console.log(`  Type: ${instructions.recordType}`);
console.log(`  Name: ${instructions.name}`);
console.log(`  Value: ${instructions.value}`);
console.log(`  TTL: ${instructions.ttl} (recommended)`);
```

### Check DNS Propagation

```typescript
// Check if DNS has propagated globally
const propagation = await enclii.domains.checkPropagation('domain_abc123');

console.log(`Propagation: ${propagation.percentage}%`);
console.log('Status by region:');
for (const [region, status] of Object.entries(propagation.regions)) {
  console.log(`  ${region}: ${status ? 'OK' : 'Pending'}`);
}
```

## Multi-Domain Setup

### Add Multiple Domains

```typescript
// Add both www and non-www
const domains = await Promise.all([
  enclii.domains.add('svc_xyz789', 'example.com'),
  enclii.domains.add('svc_xyz789', 'www.example.com', {
    redirectTo: 'example.com',  // Redirect www to apex
  }),
]);

console.log('Domains configured:');
for (const d of domains) {
  console.log(`  ${d.name}: ${d.status}`);
}
```

### Domain Aliases

```typescript
// Primary domain with aliases
const primary = await enclii.domains.add('svc_xyz789', 'api.example.com', {
  primary: true,
});

// Add aliases that redirect to primary
await enclii.domains.add('svc_xyz789', 'api-v2.example.com', {
  aliasOf: primary.id,
  redirectType: 301,  // Permanent redirect
});
```

## Types

```typescript
interface Domain {
  id: string;
  serviceId: string;
  name: string;
  status: 'pending' | 'active' | 'error';
  type: 'subdomain' | 'apex' | 'wildcard';
  dns: {
    recordType: 'CNAME' | 'A';
    target: string;
    verified: boolean;
    current?: string;
    lastChecked: string;
  };
  ssl: {
    status: 'pending' | 'issued' | 'error' | 'expiring';
    issuer: string;
    validFrom: string;
    validTo: string;
  };
  settings: DomainSettings;
  createdAt: string;
  updatedAt: string;
}

interface AddDomainOptions {
  type?: 'subdomain' | 'apex' | 'wildcard';
  redirectWww?: boolean;
  pathPrefix?: string;
  forceHttps?: boolean;
  primary?: boolean;
  aliasOf?: string;
  redirectTo?: string;
  redirectType?: 301 | 302;
}

interface DomainSettings {
  forceHttps: boolean;
  redirectWww: boolean;
  headers: Record<string, string>;
  cors?: CorsConfig;
  rateLimit?: RateLimitConfig;
}
```

## Error Handling

```typescript
import {
  DomainError,
  DnsVerificationError,
  SslError,
  DuplicateDomainError
} from '@enclii/sdk';

try {
  const domain = await enclii.domains.add('svc_xyz789', 'api.example.com');
} catch (error) {
  if (error instanceof DuplicateDomainError) {
    console.error('Domain already exists on another service');
  } else if (error instanceof DnsVerificationError) {
    console.error('DNS not configured:', error.expected);
  } else if (error instanceof SslError) {
    console.error('SSL provisioning failed:', error.reason);
  } else if (error instanceof DomainError) {
    console.error('Domain error:', error.message);
  }
}
```

## Related Documentation

- **SDK Overview**: [TypeScript SDK](/sdk/typescript/)
- **Services**: [Service Management](./services)
- **Networking**: [Networking Troubleshooting](/troubleshooting/networking)
- **DNS Setup**: [DNS Configuration](/infrastructure/dns-setup-porkbun)
- **API Reference**: [Domains API](/api-reference/#tag/domains)
