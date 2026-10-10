# syntax=docker/dockerfile:1
#
# CARE frontend image for CARE Clinic. Builds unmodified care_fe source and
# wraps it so a clinic's settings are filled in when the container starts.
#
#   docker buildx build images/frontend \
#     --build-context care_fe=<care_fe checkout> \
#     --target builder -t <registry>/care-clinic-frontend-builder:<sha>   # local rebuild fallback
#   docker buildx build images/frontend \
#     --build-context care_fe=<care_fe checkout> \
#     -t <registry>/care-clinic-frontend:<sha>
#
# clinic.env must sit next to this file: images/scripts/frontend-env.mjs
# writes it (compile-time values plus a placeholder per runtime-keys.json key).
# Build each architecture natively or under emulation: unlike care_fe's own
# Dockerfile the builder stage is not pinned to the build platform, so the
# published builder image runs on the clinic's architecture.

ARG NODE_IMAGE=node:22-bookworm-slim
ARG GO_IMAGE=golang:1.26-alpine
ARG NGINX_IMAGE=nginx:stable-alpine

FROM ${NODE_IMAGE} AS builder
WORKDIR /app
ENV NODE_OPTIONS=--max-old-space-size=4096
RUN apt-get update \
 && apt-get install -y --no-install-recommends git ca-certificates \
 && rm -rf /var/lib/apt/lists/*
COPY --from=care_fe package.json package-lock.json ./
RUN npm install --ignore-scripts
COPY --from=care_fe . .
RUN npm run postinstall

FROM builder AS build
COPY clinic.env /app/.env.local
RUN npm run build

FROM --platform=$BUILDPLATFORM ${GO_IMAGE} AS configure
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src
COPY configure/ .
RUN go test ./... \
 && CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /care-clinic-configure .

FROM ${NGINX_IMAGE}
COPY nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=configure /care-clinic-configure /usr/local/bin/care-clinic-configure
COPY --chmod=0755 40-care-clinic-config.sh /docker-entrypoint.d/40-care-clinic-config.sh
COPY runtime-keys.json /care-clinic/runtime-keys.json
COPY --from=build /app/build /care-clinic/html
RUN rm -rf /usr/share/nginx/html/*
EXPOSE 80
