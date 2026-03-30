# Production build: adapter-node SSR server
# Gateway Caddy handles routing — this container just runs Node

FROM node:20-alpine AS builder
WORKDIR /app
COPY package*.json ./
RUN npm ci
COPY . .
RUN npm run build

# Production stage — run the Node SSR server
FROM node:20-alpine
WORKDIR /app

# Install wget for health checks
RUN apk add --no-cache wget

COPY --from=builder /app/build build
COPY --from=builder /app/package.json .

EXPOSE 3000
ENV PORT=3000

CMD ["node", "build"]
