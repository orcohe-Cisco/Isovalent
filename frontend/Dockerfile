FROM node:22-alpine AS deps
WORKDIR /app
COPY package.json package-lock.json* ./
RUN npm ci || npm install

FROM node:22-alpine AS build
WORKDIR /app
COPY --from=deps /app/node_modules ./node_modules
COPY . .
ENV NEXT_TELEMETRY_DISABLED=1
RUN npm run build

FROM node:22-alpine AS run
WORKDIR /app
ENV NODE_ENV=production NEXT_TELEMETRY_DISABLED=1
# Next.js standalone binds to $HOSTNAME; Kubernetes sets that to the pod name,
# which makes the server listen only on the pod IP and breaks port-forward.
# Force it to all interfaces.
ENV HOSTNAME=0.0.0.0 PORT=3000 NEXT_PORT=3100
COPY --from=build /app/.next/standalone ./
COPY --from=build /app/.next/static ./.next/static
COPY --from=build /app/public ./public
# Single-origin reverse proxy. Next runs privately on $NEXT_PORT; this listens
# on $PORT and forwards /api, /ws and /healthz to the backend Service, so the
# browser never needs a direct route to the backend and any local port works.
COPY --from=build /app/proxy.js ./proxy.js
# Numeric UID (alpine "node" = 1000) so Kubernetes runAsNonRoot can verify it.
USER 1000:1000
EXPOSE 3000
CMD ["node", "proxy.js"]
