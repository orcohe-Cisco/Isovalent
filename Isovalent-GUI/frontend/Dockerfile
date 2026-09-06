FROM node:22-alpine AS deps
WORKDIR /app
# --no-fund/--no-audit keep the build log readable; disabling the update
# notifier stops npm printing a "new major version available" banner that
# reads like a build error in CI output.
ENV NPM_CONFIG_FUND=false NPM_CONFIG_AUDIT=false NPM_CONFIG_UPDATE_NOTIFIER=false
COPY package.json package-lock.json* ./
RUN npm ci --prefer-offline --no-fund --no-audit

FROM node:22-alpine AS build
WORKDIR /app
ENV NEXT_TELEMETRY_DISABLED=1 NPM_CONFIG_UPDATE_NOTIFIER=false
COPY --from=deps /app/node_modules ./node_modules
COPY . .
ARG NEXT_PUBLIC_API_URL=http://localhost:8081
ENV NEXT_PUBLIC_API_URL=${NEXT_PUBLIC_API_URL}
RUN npm run build

FROM node:22-alpine AS run
WORKDIR /app
ENV NODE_ENV=production NEXT_TELEMETRY_DISABLED=1
# Next.js standalone binds to $HOSTNAME; Kubernetes sets that to the pod name,
# which makes the server listen only on the pod IP and breaks port-forward.
# Force it to all interfaces.
ENV HOSTNAME=0.0.0.0 PORT=3000
COPY --from=build /app/.next/standalone ./
COPY --from=build /app/.next/static ./.next/static
COPY --from=build /app/public ./public
# Numeric UID (alpine "node" = 1000) so Kubernetes runAsNonRoot can verify it.
USER 1000:1000
EXPOSE 3000
CMD ["node", "server.js"]
