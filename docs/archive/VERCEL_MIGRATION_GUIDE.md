# Vercel to Enclii Migration Guide

**Version**: 1.0
**Last Updated**: 2025-11-20
**Estimated Migration Time**: 1-3 hours per project

---

## Table of Contents

1. [Pre-Migration Assessment](#pre-migration-assessment)
2. [Migration Decision Tree](#migration-decision-tree)
3. [Next.js Migration](#nextjs-migration)
4. [Static Site Migration](#static-site-migration)
5. [CDN Integration (Cloudflare)](#cdn-integration-cloudflare)
6. [Environment Variables & Secrets](#environment-variables--secrets)
7. [Custom Domains](#custom-domains)
8. [Performance Optimization](#performance-optimization)
9. [Deployment & Verification](#deployment--verification)
10. [Migration Examples](#migration-examples)

---

## Pre-Migration Assessment

### Is Your App a Good Fit for Enclii?

Use this decision tree to assess migration feasibility:

```
┌─────────────────────────────────────────────────────────────┐
│ Does your app use Edge Functions or Edge Middleware?       │
├─────────────────┬───────────────────────────────────────────┤
│ YES             │ NO                                        │
│ ↓               │ ↓                                         │
│ ⚠️  BLOCKER     │ ✅ GOOD FIT                              │
│                 │                                           │
│ Solutions:      │ Continue to next question...              │
│ 1. Cloudflare   │                                           │
│    Workers      │                                           │
│ 2. Stay on      │                                           │
│    Vercel       │                                           │
└─────────────────┴───────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────┐
│ Is your app purely static (no SSR/ISR)?                    │
├─────────────────┬───────────────────────────────────────────┤
│ YES             │ NO                                        │
│ ↓               │ ↓                                         │
│ ✅ EXCELLENT    │ Is it Next.js with SSR/ISR?              │
│    FIT          │                                           │
│                 │ ├─ YES: ✅ GOOD FIT                      │
│ Recommended:    │ └─ NO: ✅ PROBABLY GOOD FIT              │
│ Enclii +        │                                           │
│ Cloudflare CDN  │ Continue to next question...              │
└─────────────────┴───────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────┐
│ Do you need image optimization (next/image)?               │
├─────────────────┬───────────────────────────────────────────┤
│ YES             │ NO                                        │
│ ↓               │ ↓                                         │
│ 🟡 PARTIAL      │ ✅ EXCELLENT FIT                         │
│                 │                                           │
│ Solution:       │ Proceed with migration!                   │
│ Use Cloudflare  │                                           │
│ Polish for      │                                           │
│ optimization    │                                           │
└─────────────────┴───────────────────────────────────────────┘
```

### Assessment Checklist

Run this audit on your Vercel project:

```bash
# Install Vercel CLI
npm i -g vercel

# Login
vercel login

# List projects
vercel list

# Export environment variables
vercel env pull .env.local

# Check build settings
cat vercel.json 2>/dev/null || echo "No vercel.json found"
cat next.config.js 2>/dev/null || echo "No next.config.js found"
```

**Document:**
- [ ] Framework (Next.js, Nuxt, Gatsby, Static HTML, etc.)
- [ ] Rendering mode (Static, SSR, ISR, Edge)
- [ ] Build command
- [ ] Output directory
- [ ] Environment variables (especially secrets)
- [ ] Custom domains
- [ ] Edge Functions/Middleware usage
- [ ] Image optimization requirements
- [ ] API routes (Next.js API routes)
- [ ] Current build time
- [ ] Traffic volume and geographic distribution

---

## Migration Decision Tree

### App Type: Static Site (Gatsby, Docusaurus, Hugo, etc.)

**Migration Risk**: 🟢 **LOW**

**Strategy**: Direct migration + Cloudflare CDN

```
┌──────────────────┐
│  Cloudflare CDN  │ ← Edge caching, DDoS protection
└────────┬─────────┘
         │
         ▼
┌──────────────────┐
│  Enclii (Origin) │ ← Nginx serving static files
└──────────────────┘
```

**Time Estimate**: 1-2 hours

---

### App Type: Next.js SSR/ISR (No Edge Features)

**Migration Risk**: 🟡 **MEDIUM**

**Strategy**: Next.js Standalone + Cloudflare CDN

```
┌──────────────────┐
│  Cloudflare CDN  │ ← Cache static assets, Polish for images
└────────┬─────────┘
         │
         ▼
┌──────────────────┐
│  Enclii (Origin) │ ← Next.js server (standalone mode)
│  - SSR rendering │
│  - API routes    │
│  - ISR cache     │
└──────────────────┘
```

**Time Estimate**: 2-3 hours

---

### App Type: Next.js with Edge Middleware

**Migration Risk**: 🔴 **HIGH**

**Strategy**: Enclii + Cloudflare Workers for edge logic

```
┌──────────────────────┐
│  Cloudflare Workers  │ ← Edge middleware (auth, A/B, redirects)
└────────┬─────────────┘
         │
         ▼
┌──────────────────┐
│  Cloudflare CDN  │ ← Cache static assets
└────────┬─────────┘
         │
         ▼
┌──────────────────┐
│  Enclii (Origin) │ ← Next.js server
└──────────────────┘
```

**Time Estimate**: 4-6 hours (requires Worker development)

---

## Next.js Migration

### Step 1: Configure Next.js for Standalone Output

Update `next.config.js`:

```javascript
/** @type {import('next').NextConfig} */
const nextConfig = {
  // Enable standalone output for Docker deployment
  output: 'standalone',

  // Configure image optimization
  images: {
    // Use Cloudflare Polish as image loader
    loader: 'custom',
    loaderFile: './imageLoader.js',

    // Or disable optimization and use Cloudflare Polish directly
    // unoptimized: true,
  },

  // Configure environment variables
  env: {
    NEXT_PUBLIC_API_URL: process.env.NEXT_PUBLIC_API_URL,
  },

  // Disable Vercel Analytics (use your own)
  experimental: {
    instrumentationHook: true,
  },

  // Configure output file tracing (for smaller Docker images)
  experimental: {
    outputFileTracingRoot: require('path').join(__dirname, '../../'),
  },
}

module.exports = nextConfig
```

Create `imageLoader.js`:

```javascript
// imageLoader.js
// Custom image loader for Cloudflare Polish

export default function cloudflareLoader({ src, width, quality }) {
  const params = []

  if (width) {
    params.push(`width=${width}`)
  }

  if (quality) {
    params.push(`quality=${quality}`)
  }

  const paramsString = params.join(',')

  // Cloudflare Image Resizing
  return `/cdn-cgi/image/${paramsString}/${src}`
}
```

### Step 2: Create Dockerfile

```dockerfile
# Dockerfile
FROM node:18-alpine AS base

# Install dependencies only when needed
FROM base AS deps
RUN apk add --no-cache libc6-compat
WORKDIR /app

# Copy package files
COPY package.json package-lock.json* ./
RUN npm ci

# Rebuild the source code only when needed
FROM base AS builder
WORKDIR /app
COPY --from=deps /app/node_modules ./node_modules
COPY . .

# Set environment variables for build
ENV NEXT_TELEMETRY_DISABLED 1
ENV NODE_ENV production

# Build Next.js
RUN npm run build

# Production image, copy all the files and run next
FROM base AS runner
WORKDIR /app

ENV NODE_ENV production
ENV NEXT_TELEMETRY_DISABLED 1

RUN addgroup --system --gid 1001 nodejs
RUN adduser --system --uid 1001 nextjs

# Copy standalone output
COPY --from=builder /app/public ./public
COPY --from=builder --chown=nextjs:nodejs /app/.next/standalone ./
COPY --from=builder --chown=nextjs:nodejs /app/.next/static ./.next/static

USER nextjs

EXPOSE 3000

ENV PORT 3000
ENV HOSTNAME "0.0.0.0"

CMD ["node", "server.js"]
```

### Step 3: Create Enclii Service

```yaml
# enclii.yaml
apiVersion: enclii.dev/v1
kind: Service
metadata:
  name: web-app
  project: myapp
spec:
  # Build with Dockerfile
  build:
    dockerfile: Dockerfile
    context: .

  # Runtime configuration
  runtime:
    port: 3000
    replicas: 3

    # Health check
    healthCheck:
      type: http
      path: /api/health
      port: 3000
      initialDelaySeconds: 30
      periodSeconds: 10

    # Resources (Next.js typically needs more memory)
    resources:
      requests:
        memory: "512Mi"
        cpu: "500m"
      limits:
        memory: "2Gi"
        cpu: "1000m"

    # Auto-scaling for traffic spikes
    autoscaling:
      enabled: true
      minReplicas: 3
      maxReplicas: 20
      targetCPU: 70

  # Environment variables (public)
  env:
    - name: NODE_ENV
      value: production
    - name: NEXT_PUBLIC_API_URL
      value: https://api.myapp.com

  # Secrets (private)
  secrets:
    - DATABASE_URL
    - NEXTAUTH_SECRET
    - NEXTAUTH_URL
    - STRIPE_SECRET_KEY
    - OPENAI_API_KEY

  # Custom domain
  routes:
    - domain: myapp.com
      path: /
      pathType: Prefix
      tlsEnabled: true
      tlsIssuer: letsencrypt-prod
    - domain: www.myapp.com
      path: /
      pathType: Prefix
      tlsEnabled: true
      tlsIssuer: letsencrypt-prod
```

### Step 4: Create Health Check Endpoint

Create `pages/api/health.js`:

```javascript
// pages/api/health.js
export default function handler(req, res) {
  res.status(200).json({
    status: 'healthy',
    timestamp: new Date().toISOString(),
    version: process.env.NEXT_PUBLIC_VERSION || 'unknown'
  })
}
```

Or for App Router (`app/api/health/route.js`):

```javascript
// app/api/health/route.js
export async function GET() {
  return Response.json({
    status: 'healthy',
    timestamp: new Date().toISOString(),
    version: process.env.NEXT_PUBLIC_VERSION || 'unknown'
  })
}
```

### Step 5: Deploy to Enclii

```bash
# Create project
enclii project create myapp --region us-central1

# Create environment
enclii env create production

# Import secrets (see Environment Variables section)
enclii secret create NEXTAUTH_SECRET "$(openssl rand -base64 32)" --env production
enclii secret create DATABASE_URL "postgresql://..." --env production

# Create service
enclii service create -f enclii.yaml --env production

# Deploy
enclii deploy --env production

# Monitor deployment
enclii status --env production --follow

# Check logs
enclii logs web-app --env production --follow
```

---

## Static Site Migration

### For Gatsby, Docusaurus, Hugo, Jekyll, etc.

**Step 1: Build Locally**

```bash
# Build your static site
npm run build
# Output: public/ or build/ or dist/

# Verify build output
ls -la public/  # or build/ or dist/
```

**Step 2: Create Dockerfile**

```dockerfile
# Dockerfile
FROM nginx:alpine

# Copy static files
COPY public/ /usr/share/nginx/html/

# Copy nginx configuration
COPY nginx.conf /etc/nginx/conf.d/default.conf

EXPOSE 80

CMD ["nginx", "-g", "daemon off;"]
```

**Step 3: Create nginx.conf**

```nginx
# nginx.conf
server {
    listen 80;
    server_name _;
    root /usr/share/nginx/html;
    index index.html;

    # Gzip compression
    gzip on;
    gzip_vary on;
    gzip_min_length 1024;
    gzip_types text/plain text/css text/xml text/javascript application/javascript application/xml+rss application/json;

    # Cache static assets
    location ~* \.(jpg|jpeg|png|gif|ico|css|js|svg|woff|woff2|ttf|eot)$ {
        expires 1y;
        add_header Cache-Control "public, immutable";
    }

    # SPA routing - serve index.html for all routes
    location / {
        try_files $uri $uri/ /index.html;
    }

    # Security headers
    add_header X-Frame-Options "SAMEORIGIN" always;
    add_header X-Content-Type-Options "nosniff" always;
    add_header X-XSS-Protection "1; mode=block" always;

    # Health check endpoint
    location /health {
        access_log off;
        return 200 "healthy\n";
        add_header Content-Type text/plain;
    }
}
```

**Step 4: Create Enclii Service**

```yaml
# enclii.yaml
apiVersion: enclii.dev/v1
kind: Service
metadata:
  name: docs-site
spec:
  build:
    dockerfile: Dockerfile

  runtime:
    port: 80
    replicas: 2

    healthCheck:
      type: http
      path: /health
      port: 80

    resources:
      requests:
        memory: "64Mi"
        cpu: "100m"
      limits:
        memory: "128Mi"
        cpu: "200m"

  routes:
    - domain: docs.myapp.com
      tlsEnabled: true
```

**Step 5: Deploy**

```bash
enclii service create -f enclii.yaml --env production
enclii deploy --env production
```

---

## CDN Integration (Cloudflare)

### Why Use Cloudflare with Enclii?

Cloudflare provides the edge capabilities that Enclii doesn't have:

- **Edge Caching**: Global CDN with 275+ locations
- **Image Optimization**: Automatic WebP conversion, resizing (Polish)
- **DDoS Protection**: Built-in Layer 3/4/7 protection
- **WAF**: Web Application Firewall
- **Workers**: Edge compute for middleware logic
- **Analytics**: Real User Monitoring (RUM)

### Setup: Cloudflare as CDN

**Step 1: Add Site to Cloudflare**

1. Go to https://dash.cloudflare.com
2. Click "Add a Site"
3. Enter your domain: `myapp.com`
4. Choose Free or Pro plan
5. Update nameservers at your registrar

**Step 2: Configure DNS**

```dns
# A record pointing to Enclii Ingress
@        300  IN  A     <enclii-ingress-ip>
www      300  IN  CNAME myapp.com
```

Get Enclii ingress IP:

```bash
kubectl get svc -n ingress-nginx ingress-nginx-controller -o jsonpath='{.status.loadBalancer.ingress[0].ip}'
```

**Step 3: Enable Cloudflare Proxy**

In Cloudflare DNS settings:
- Toggle the orange cloud icon to "Proxied" for all records
- This routes traffic through Cloudflare's CDN

**Step 4: Configure SSL/TLS**

Cloudflare SSL/TLS Settings:
- **Mode**: Full (strict)
- **Always Use HTTPS**: On
- **Automatic HTTPS Rewrites**: On
- **Minimum TLS Version**: 1.2

**Step 5: Configure Caching**

Cloudflare Caching Settings:

```
Caching Level: Standard
Browser Cache TTL: 4 hours
Always Online: On
```

Create Page Rules for aggressive caching:

```
Rule 1: Cache static assets
  URL: myapp.com/*.{jpg,jpeg,png,gif,css,js,woff,woff2,ttf,svg,ico}
  Cache Level: Cache Everything
  Edge Cache TTL: 1 year
  Browser Cache TTL: 1 year

Rule 2: Cache HTML with short TTL
  URL: myapp.com/*
  Cache Level: Cache Everything
  Edge Cache TTL: 2 hours
  Browser Cache TTL: 30 minutes
```

**Step 6: Enable Image Optimization (Polish)**

Cloudflare Speed Settings:
- **Polish**: Lossless
- **WebP**: On

Update `next.config.js` to use Cloudflare Polish:

```javascript
module.exports = {
  images: {
    loader: 'custom',
    loaderFile: './imageLoader.js',
  },
}
```

```javascript
// imageLoader.js
export default function cloudflareLoader({ src, width, quality }) {
  return `/cdn-cgi/image/width=${width},quality=${quality || 75}/${src}`
}
```

**Step 7: Configure Workers (for Edge Middleware)**

If you need edge middleware (auth, A/B testing, etc.):

```javascript
// cloudflare-worker.js
export default {
  async fetch(request, env) {
    const url = new URL(request.url)

    // A/B Testing
    if (url.pathname.startsWith('/new-feature')) {
      const variant = Math.random() < 0.5 ? 'A' : 'B'
      const response = await fetch(request)
      const newResponse = new Response(response.body, response)
      newResponse.headers.set('X-AB-Variant', variant)
      return newResponse
    }

    // Authentication check
    if (url.pathname.startsWith('/dashboard')) {
      const token = request.headers.get('Authorization')
      if (!token) {
        return new Response('Unauthorized', { status: 401 })
      }
      // Validate token...
    }

    // Geo-redirect
    const country = request.cf.country
    if (country === 'CN' && !url.pathname.startsWith('/cn')) {
      return Response.redirect(`https://cn.myapp.com${url.pathname}`, 302)
    }

    // Pass through to origin (Enclii)
    return fetch(request)
  }
}
```

Deploy Worker:

```bash
npm install -g wrangler
wrangler login
wrangler deploy cloudflare-worker.js
```

**Step 8: Test CDN**

```bash
# Test caching
curl -I https://myapp.com
# Look for: cf-cache-status: HIT

# Test image optimization
curl -I https://myapp.com/cdn-cgi/image/width=800/hero.jpg
# Look for: content-type: image/webp

# Test from different regions
curl -H "CF-IPCountry: JP" https://myapp.com
```

---

## Environment Variables & Secrets

### Export from Vercel

```bash
# Download environment variables
vercel env pull .env.production

# Or via Vercel API
curl -X GET "https://api.vercel.com/v9/projects/$PROJECT_ID/env" \
  -H "Authorization: Bearer $VERCEL_TOKEN" \
  | jq -r '.envs[] | "\(.key)=\(.value)"' > vercel.env
```

### Import to Enclii

```bash
# Create secrets
while IFS='=' read -r key value; do
  [[ $key =~ ^#.*$ ]] && continue
  [[ -z $key ]] && continue
  value=$(echo $value | sed 's/^"//; s/"$//')

  # Public variables (NEXT_PUBLIC_*)
  if [[ $key =~ ^NEXT_PUBLIC_ ]]; then
    echo "Skipping public var: $key (add to enclii.yaml env section)"
    continue
  fi

  # Private secrets
  echo "Importing secret: $key"
  enclii secret create "$key" "$value" --env production
done < .env.production
```

Update `enclii.yaml`:

```yaml
spec:
  # Public environment variables
  env:
    - name: NEXT_PUBLIC_API_URL
      value: https://api.myapp.com
    - name: NEXT_PUBLIC_STRIPE_PUBLISHABLE_KEY
      value: pk_live_xxxxx

  # Private secrets
  secrets:
    - DATABASE_URL
    - NEXTAUTH_SECRET
    - STRIPE_SECRET_KEY
    - OPENAI_API_KEY
```

---

## Custom Domains

### Vercel Setup
- `myapp.com` → Automatic HTTPS
- `www.myapp.com` → Automatic redirect

### Enclii + Cloudflare Setup

**Step 1: Add Domains to Enclii**

```bash
# Add primary domain
enclii domain add myapp.com \
  --service web-app \
  --env production \
  --tls-enabled \
  --tls-issuer letsencrypt-prod

# Add www subdomain
enclii domain add www.myapp.com \
  --service web-app \
  --env production \
  --tls-enabled \
  --tls-issuer letsencrypt-prod
```

**Step 2: Configure DNS in Cloudflare**

```dns
@        300  IN  A     <enclii-ingress-ip>  (Proxied)
www      300  IN  CNAME myapp.com             (Proxied)
```

**Step 3: Verify Domains**

```bash
# Verify DNS ownership
enclii domain verify myapp.com --service web-app --env production
enclii domain verify www.myapp.com --service web-app --env production
```

**Step 4: Test HTTPS**

```bash
curl -I https://myapp.com
curl -I https://www.myapp.com

# Check certificate
echo | openssl s_client -connect myapp.com:443 -servername myapp.com 2>/dev/null | openssl x509 -noout -issuer -subject
```

**Step 5: Configure Redirects (Optional)**

To redirect `www` to apex domain:

```yaml
# In enclii.yaml
spec:
  routes:
    - domain: myapp.com
      path: /
      tlsEnabled: true
    - domain: www.myapp.com
      path: /
      tlsEnabled: true
      redirect:
        to: https://myapp.com
        permanent: true
```

---

## Performance Optimization

### 1. Enable HTTP/2 and HTTP/3

Cloudflare automatically enables HTTP/2. Enable HTTP/3:

Cloudflare Dashboard → Network → HTTP/3 (with QUIC): **On**

### 2. Configure Compression

Enclii (nginx) already has gzip enabled. Enable Brotli in Cloudflare:

Cloudflare Dashboard → Speed → Optimization → Brotli: **On**

### 3. Minimize JavaScript

```javascript
// next.config.js
module.exports = {
  swcMinify: true,
  compiler: {
    removeConsole: process.env.NODE_ENV === 'production',
  },
}
```

### 4. Optimize Images

Use Next.js Image component:

```jsx
import Image from 'next/image'

<Image
  src="/hero.jpg"
  alt="Hero"
  width={1200}
  height={630}
  priority
  quality={85}
/>
```

Cloudflare Polish will automatically:
- Convert to WebP
- Resize based on device
- Strip metadata

### 5. Implement ISR (Incremental Static Regeneration)

```jsx
// pages/blog/[slug].js
export async function getStaticProps({ params }) {
  const post = await fetchPost(params.slug)

  return {
    props: { post },
    revalidate: 3600, // Revalidate every hour
  }
}
```

### 6. Configure Cache Headers

```javascript
// next.config.js
module.exports = {
  async headers() {
    return [
      {
        source: '/:path*',
        headers: [
          {
            key: 'Cache-Control',
            value: 'public, max-age=3600, s-maxage=86400, stale-while-revalidate=604800',
          },
        ],
      },
    ]
  },
}
```

---

## Deployment & Verification

### Deploy to Staging First

```bash
# Deploy to staging
enclii deploy --env staging

# Test
curl https://staging.myapp.com/api/health
curl https://staging.myapp.com

# Run E2E tests
npm run test:e2e -- --baseUrl https://staging.myapp.com
```

### Blue-Green Production Deploy

```bash
# Deploy to production (Enclii)
enclii deploy --env production

# Parallel test (before switching DNS)
curl -H "Host: myapp.com" http://<enclii-ingress-ip>

# Update Cloudflare DNS to point to Enclii
# (via Cloudflare Dashboard or API)

# Monitor
enclii logs web-app --env production --follow
enclii metrics --env production
```

### Performance Comparison

```bash
# Before (Vercel)
curl -w "@curl-format.txt" -o /dev/null -s https://myapp.vercel.app

# After (Enclii + Cloudflare)
curl -w "@curl-format.txt" -o /dev/null -s https://myapp.com
```

Create `curl-format.txt`:

```
time_namelookup:  %{time_namelookup}\n
time_connect:  %{time_connect}\n
time_appconnect:  %{time_appconnect}\n
time_pretransfer:  %{time_pretransfer}\n
time_redirect:  %{time_redirect}\n
time_starttransfer:  %{time_starttransfer}\n
----------\n
time_total:  %{time_total}\n
```

---

## Migration Examples

### Example 1: Simple Next.js Blog

**Vercel Setup:**
- Next.js 14 with App Router
- ISR for blog posts
- Custom domain: `blog.myapp.com`

**Migration:**

```bash
# 1. Update next.config.js
cat > next.config.js <<EOF
module.exports = {
  output: 'standalone',
  images: {
    loader: 'custom',
    loaderFile: './imageLoader.js',
  },
}
EOF

# 2. Create imageLoader.js (Cloudflare Polish)
# (see above)

# 3. Create Dockerfile
# (see Next.js migration section)

# 4. Create enclii.yaml
# (see above)

# 5. Deploy
enclii service create -f enclii.yaml --env production
enclii deploy --env production

# 6. Configure Cloudflare
# (add DNS, enable proxy, configure caching)

# 7. Test
curl https://blog.myapp.com
```

**Time:** 2 hours

### Example 2: E-commerce Site (Next.js + Database)

**Vercel Setup:**
- Next.js 14 with App Router
- Vercel Postgres
- Custom domain: `shop.myapp.com`
- Heavy image usage

**Migration:**

```bash
# 1. Migrate database (see Railway guide)
# Export Vercel Postgres → Import to RDS/CloudSQL

# 2. Update Next.js config (standalone + Cloudflare Polish)

# 3. Create enclii.yaml with database secret
spec:
  secrets:
    - DATABASE_URL
    - STRIPE_SECRET_KEY

# 4. Deploy
enclii secret create DATABASE_URL "postgresql://..." --env production
enclii service create -f enclii.yaml --env production
enclii deploy --env production

# 5. Configure Cloudflare
# Enable Polish (Lossy for e-commerce product images)

# 6. Test checkout flow thoroughly
```

**Time:** 4 hours

### Example 3: Docusaurus Documentation Site

**Vercel Setup:**
- Docusaurus 3.0
- Custom domain: `docs.myapp.com`
- Versioned docs

**Migration:**

```bash
# 1. Build locally
npm run build
# Output: build/

# 2. Create Dockerfile (nginx)
# (see Static Site Migration section)

# 3. Create enclii.yaml
cat > enclii.yaml <<EOF
apiVersion: enclii.dev/v1
kind: Service
metadata:
  name: docs
spec:
  build:
    dockerfile: Dockerfile
  runtime:
    port: 80
    replicas: 2
    healthCheck:
      path: /health
  routes:
    - domain: docs.myapp.com
      tlsEnabled: true
EOF

# 4. Deploy
enclii service create -f enclii.yaml --env production
enclii deploy --env production

# 5. Configure Cloudflare
# Aggressive caching for static docs

# 6. Test
curl https://docs.myapp.com
```

**Time:** 1 hour

---

## Post-Migration Checklist

- [ ] All routes work correctly
- [ ] HTTPS works on all domains
- [ ] Images load and are optimized
- [ ] API routes respond correctly
- [ ] Database connections work
- [ ] Environment variables are set
- [ ] SSR/ISR renders correctly
- [ ] Performance is acceptable (compare to Vercel baseline)
- [ ] Error rates are normal
- [ ] Logs are flowing to monitoring
- [ ] Analytics are tracking correctly
- [ ] SEO metadata is correct
- [ ] Sitemap is accessible
- [ ] robots.txt is correct

---

## Decommission Vercel

After 2 weeks of successful operation:

```bash
# 1. Verify zero traffic on Vercel
# Check Vercel Analytics

# 2. Delete Vercel deployments
vercel remove <project-name> --yes

# 3. Cancel Vercel subscription
# (via Vercel Dashboard)

# 4. Document migration
cat > vercel_migration_complete.md <<EOF
# Vercel Migration Completed

- **Date**: $(date)
- **Domain**: myapp.com
- **Framework**: Next.js 14
- **Downtime**: 0 minutes
- **Cost Savings**: $XX/month

## Performance Comparison

### Vercel (Baseline)
- TTFB: XXms
- LCP: XXms
- Build time: XXs

### Enclii + Cloudflare
- TTFB: XXms (±X%)
- LCP: XXms (±X%)
- Build time: XXs (±X%)
EOF
```

---

## FAQ

**Q: Will my Next.js build times be slower on Enclii?**
A: Build times may be 10-20% slower without Vercel's optimized build cache, but you can implement your own build cache using Docker layer caching or CI/CD cache.

**Q: Can I use `next/image` optimization?**
A: Yes, but configure a custom loader to use Cloudflare Polish instead of Vercel's image optimization.

**Q: What about Analytics?**
A: Use Vercel Analytics alternatives like Plausible, Fathom, or Google Analytics. Cloudflare also provides Web Analytics.

**Q: Can I still use preview deployments?**
A: Yes, configure Enclii to create preview environments for each PR. See Railway Migration Guide for preview environment setup.

**Q: Is ISR (Incremental Static Regeneration) supported?**
A: Yes, ISR works normally in Next.js standalone mode. Configure Cloudflare cache TTL to match your ISR revalidation time.

---

## Support

For migration assistance:
- Enclii Docs: https://docs.enclii.dev/migration/vercel
- Discord: https://discord.gg/enclii
- Professional Migration Services: support@enclii.dev

---

**Next Steps:** After completing this migration, consider the [Railway Migration Guide](./RAILWAY_MIGRATION_GUIDE.md) for backend services.
