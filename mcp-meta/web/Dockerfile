FROM node:22-slim AS builder

WORKDIR /app

COPY package.json package-lock.json* ./
RUN npm install

COPY . .

ARG NEXT_PUBLIC_API_URL=http://localhost:9876
ARG NEXT_PUBLIC_MARKETPLACE_URL=http://localhost:5050
ARG NEXT_PUBLIC_STELLAR_NETWORK=TESTNET
ARG NEXT_PUBLIC_HORIZON_URL=https://horizon-testnet.stellar.org

RUN npm run build

FROM node:22-slim

WORKDIR /app

COPY --from=builder /app/.next ./.next
COPY --from=builder /app/node_modules ./node_modules
COPY --from=builder /app/package.json ./
COPY --from=builder /app/next.config.ts ./
COPY --from=builder /app/app ./app
COPY --from=builder /app/components ./components
COPY --from=builder /app/lib ./lib
COPY --from=builder /app/tsconfig.json ./

ENV PORT=3002
ENV HOSTNAME=0.0.0.0
EXPOSE 3002

CMD ["npx", "next", "start", "-p", "3002", "-H", "0.0.0.0"]
