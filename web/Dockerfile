# syntax=docker/dockerfile:1

# Every FROM names an exact version and its digest, for the reason given at
# the top of api/Dockerfile.
#
# Node 22 is also what ci.yml runs lint and tests on. Change the major in
# both together, and @types/node in package.json with them.
FROM node:22.23.5-alpine@sha256:1314fb30ec37592d5845e5a160d522c0e680972aa534549940b8d73bde88481a AS builder
WORKDIR /app
COPY package*.json ./
RUN npm ci
COPY . .
RUN npm run build

# nginx is the only application server that faces the internet (behind
# Caddy). 1.30 is the stable line; odd-numbered lines are development
# releases.
FROM nginx:1.30.6-alpine@sha256:901e593c95dc85f5a729f5765516321366ef4ac401821431d408897e95287d0a AS prod
COPY --from=builder /app/dist /usr/share/nginx/html
COPY nginx.conf /etc/nginx/conf.d/default.conf
EXPOSE 80
