# Production build: Node builds static files, Caddy serves them
# For development with HMR, use Dockerfile.e2e instead

# Build stage
FROM node:20-alpine AS builder
WORKDIR /app
COPY package*.json ./
RUN npm ci
COPY . .
RUN npm run build

# Production stage - Caddy serves static files
FROM caddy:2-alpine

# Install wget for health checks
RUN apk add --no-cache wget

COPY --from=builder /app/build /srv
COPY Caddyfile /etc/caddy/Caddyfile
EXPOSE 8080
