# syntax=docker/dockerfile:1

# Every FROM names an exact version and its digest, for the reason given at
# the top of api/Dockerfile.
#
# Node 22 is also what ci.yml runs lint and tests on. Change the major in
# both together, and @types/node in package.json with them.
FROM node:22.23.4-alpine@sha256:fe4f6d74e20370acb04ad889077d14d0f64a9c8ea6a42bc60cddf1e844699914 AS builder
WORKDIR /app
COPY package*.json ./
RUN npm ci
COPY . .
RUN npm run build

# nginx is the only application server that faces the internet (behind
# Caddy). 1.30 is the stable line; odd-numbered lines are development
# releases.
FROM nginx:1.30.5-alpine@sha256:0985e772fb9f729e6fa0980da05fca5d9c468e870eed43071545afa9d2e27d94 AS prod
COPY --from=builder /app/dist /usr/share/nginx/html
COPY nginx.conf /etc/nginx/conf.d/default.conf
EXPOSE 80
