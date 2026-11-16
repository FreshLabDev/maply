# SPDX-License-Identifier: Apache-2.0
# Both bases are pinned by digest as well as tag, so a tag moved upstream
# never slips into a release; Dependabot moves the pair together.
FROM golang:1.27.0-alpine@sha256:4c9fe60190a2a3350ddc51de80d0224b8a6698d12bdfc999fee45ea9d6c46dbc AS build

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

FROM alpine:3.24@sha256:f59abe656b692d901e184be3445e084786602108599d2a51d229c0f5a48c71bf

# ffmpeg encodes the animated posts; without it they go out as photos.
RUN apk add --no-cache ca-certificates tzdata ffmpeg \
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
