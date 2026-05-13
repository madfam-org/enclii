# Build stage
FROM node:22-alpine AS builder

WORKDIR /app

# Install pnpm and dependencies
RUN corepack enable && corepack prepare pnpm@latest --activate
COPY apps/landing/package.json ./
RUN pnpm install --frozen-lockfile=false --config.dangerouslyAllowAllBuilds=true

# Copy source files
COPY apps/landing/ .

# Build static site
RUN pnpm run build

# Production stage — unprivileged nginx (runs as uid 101, no root required)
FROM nginxinc/nginx-unprivileged:alpine

# Copy nginx config
COPY apps/landing/nginx.conf /etc/nginx/conf.d/default.conf

# Copy built static files
COPY --from=builder /app/out /usr/share/nginx/html

EXPOSE 4204

CMD ["nginx", "-g", "daemon off;"]
