# Build stage
FROM node:20-alpine AS builder

WORKDIR /app

# Install dependencies
# Support both repo root context (apps/landing/) and local context (.)
COPY apps/landing/package*.json ./
RUN npm ci

# Copy source files
COPY apps/landing/ .

# Build static site
RUN npm run build

# Production stage — unprivileged nginx (runs as uid 101, no root required)
FROM nginxinc/nginx-unprivileged:alpine

# Copy nginx config
COPY apps/landing/nginx.conf /etc/nginx/conf.d/default.conf

# Copy built static files
COPY --from=builder /app/out /usr/share/nginx/html

EXPOSE 4204

CMD ["nginx", "-g", "daemon off;"]
