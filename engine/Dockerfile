# syntax=docker/dockerfile:1

# ---------------------------------------------------------------------------
# libpst / readpst builder
#
# Alpine does not ship libpst, so readpst is compiled from a pinned source
# release. 0.6.76 needs a one-line prototype fix for modern GCC.
# ---------------------------------------------------------------------------
FROM alpine:3.20 AS libpst

RUN apk add --no-cache build-base curl libgsf-dev libxml2-dev zlib-dev

ARG LIBPST_VERSION=0.6.76
ARG LIBPST_SHA256=3d291beebbdb48d2b934608bc06195b641da63d2a8f5e0d386f2e9d6d05a0b42

WORKDIR /tmp/libpst
RUN curl -fsSL -o libpst.tar.gz \
      "https://www.five-ten-sg.com/libpst/packages/libpst-${LIBPST_VERSION}.tar.gz" \
 && echo "${LIBPST_SHA256}  libpst.tar.gz" | sha256sum -c - \
 && tar -xzf libpst.tar.gz \
 && cd "libpst-${LIBPST_VERSION}" \
 && sed -i 's/^int       grim_reaper();/int       grim_reaper(int waitall);/' src/readpst.c \
 && ./configure --prefix=/opt/libpst --disable-python \
 && make -j"$(nproc)" \
 && make install

# ---------------------------------------------------------------------------
# Go build
# ---------------------------------------------------------------------------
FROM golang:1.26-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build \
      -trimpath -ldflags="-s -w" \
      -o /out/engine ./cmd/engine

# ---------------------------------------------------------------------------
# Runtime
# ---------------------------------------------------------------------------
FROM alpine:3.20

RUN apk add --no-cache \
      ca-certificates \
      libgsf \
      libstdc++ \
      libxml2 \
      tzdata \
      zlib \
 && addgroup -S -g 10001 archive \
 && adduser -S -u 10001 -G archive -h /app archive \
 && mkdir -p /data/blobs /data/spool \
 && chown -R archive:archive /data

COPY --from=libpst /opt/libpst/bin/readpst /usr/local/bin/readpst
COPY --from=build /out/engine /usr/local/bin/engine

USER archive
WORKDIR /app

ENV ARCHIVE_ENV=production \
    BLOB_STORAGE_PATH=/data/blobs \
    SPOOL_PATH=/data/spool \
    API_ADDR=:8080 \
    SMTP_ADDR=:2525

EXPOSE 8080 2525

HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
  CMD wget -qO- http://127.0.0.1:8080/healthz >/dev/null || exit 1

ENTRYPOINT ["/usr/local/bin/engine"]
CMD ["serve"]
