# syntax=docker/dockerfile:1

# Every FROM names an exact version and its digest, for the reason given at
# the top of api/Dockerfile.
#
# Node 22 is also what ci.yml runs lint and tests on. Change the major in
# both together, and @types/node in package.json with them.
FROM node:22.23.3-alpine@sha256:0a7108bf6c7bf5de370ffb1a3ed6be93d405b43ff159f681a8d18c0e2bc2e402 AS builder
WORKDIR /app
COPY package*.json ./
RUN npm ci
COPY . .
RUN npm run build

# nginx is the only application server that faces the internet (behind
# Caddy). 1.30 is the stable line; odd-numbered lines are development
# releases.
FROM nginx:1.31.0-alpine@sha256:2f07d83bf561b506400dc183b1b2003803e39efbd22451f848adaba14d28c7c7 AS prod
COPY --from=builder /app/dist /usr/share/nginx/html
COPY nginx.conf /etc/nginx/conf.d/default.conf
EXPOSE 80
