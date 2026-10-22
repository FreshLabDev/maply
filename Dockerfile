# SPDX-License-Identifier: Apache-2.0
FROM golang:1.26.10-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG VERSION=dev
ARG COMMIT=none
ARG DATE=unknown

RUN CGO_ENABLED=0 go build -trimpath \
    -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.date=${DATE}" \
    -o /out/maply ./cmd/maply

FROM alpine:3.24

RUN apk add --no-cache ca-certificates tzdata \
    && adduser -D -H -u 10001 maply

WORKDIR /app

# Migrations are embedded in the binary; there is nothing else to copy.
COPY --from=build /out/maply /app/maply

# docker inspect answers which build is running without starting it.
ARG VERSION=dev
ARG COMMIT=none
LABEL org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${COMMIT}" \
      org.opencontainers.image.source="https://github.com/FreshLabDev/maply"
USER maply

EXPOSE 8080

ENTRYPOINT ["/app/maply"]
