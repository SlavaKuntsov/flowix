FROM node:20-alpine AS base
WORKDIR /app
# Build-time public env must be inlined by Next.js; pass via --build-arg
ARG NEXT_PUBLIC_API_URL=http://localhost:8080
ARG GATEWAY_URL=http://gateway:8080
ENV NEXT_PUBLIC_API_URL=$NEXT_PUBLIC_API_URL
ENV GATEWAY_URL=$GATEWAY_URL
COPY package.json package-lock.json* ./
RUN npm ci --ignore-scripts 2>/dev/null || npm install --ignore-scripts
COPY . .
RUN npm run build

FROM node:20-alpine AS runner
WORKDIR /app
ENV NODE_ENV=production
ENV NEXT_TELEMETRY_DISABLED=1
COPY --from=base /app/.next/standalone ./
COPY --from=base /app/.next/static ./.next/static
EXPOSE 3000
CMD ["node", "server.js"]
