# Build stage
FROM node:22-alpine AS builder

WORKDIR /build
COPY package*.json ./
RUN npm ci

COPY . .
RUN npm run build
RUN npm ci --omit=dev

# Run stage
FROM node:22-alpine

RUN apk add --no-cache tzdata
WORKDIR /app
COPY --from=builder /build/node_modules ./node_modules
COPY --from=builder /build/dist ./dist
COPY --from=builder /build/package.json ./

EXPOSE 3000
USER node
ENTRYPOINT ["node", "dist/main.js"]
