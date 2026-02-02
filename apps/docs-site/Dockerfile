# Production Dockerfile for Enclii Documentation Site (Docusaurus)
# Build context should be the enclii root directory, not apps/docs-site
# Usage: docker build -f apps/docs-site/Dockerfile -t enclii-docs .

FROM node:20-alpine AS base
RUN apk add --no-cache libc6-compat
WORKDIR /app

# Install dependencies
FROM base AS deps
WORKDIR /app/docs-site
COPY apps/docs-site/package.json apps/docs-site/package-lock.json* ./
RUN npm install --legacy-peer-deps

# Build the application
FROM base AS builder
WORKDIR /app

# Copy docs-site app
COPY apps/docs-site ./docs-site

# Copy docs content
COPY docs ./docs

# Install dependencies from deps stage
COPY --from=deps /app/docs-site/node_modules ./docs-site/node_modules

# Build the static site
WORKDIR /app/docs-site
ENV DOCKER_BUILD=true
RUN npm run build

# Production image - serve static files with nginx (non-root)
FROM nginxinc/nginx-unprivileged:alpine AS runner

# Copy custom nginx config (listen on unprivileged port 8080)
RUN echo 'server { \
    listen 8080; \
    server_name localhost; \
    root /usr/share/nginx/html; \
    index index.html; \
    gzip on; \
    gzip_types text/plain text/css application/json application/javascript text/xml application/xml application/xml+rss text/javascript; \
    location ~* \.(js|css|png|jpg|jpeg|gif|ico|svg|woff|woff2)$ { \
        expires 1y; \
        add_header Cache-Control "public, immutable"; \
    } \
    location / { \
        try_files $uri $uri/ /index.html; \
    } \
    location /health { \
        access_log off; \
        return 200 "OK"; \
        add_header Content-Type text/plain; \
    } \
}' > /etc/nginx/conf.d/default.conf

# Copy built static files
COPY --from=builder /app/docs-site/build /usr/share/nginx/html

USER nginx
EXPOSE 8080

CMD ["nginx", "-g", "daemon off;"]
