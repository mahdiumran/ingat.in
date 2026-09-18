# ---------------------------------------------------------------------------
# Ingat.in — backend image (satu image untuk mode api | worker | migrate)
#
# Pola diadopsi dari mcnvpn: builder golang alpine, runtime alpine dengan
# postgresql-client (untuk pg_dump) dan gzip (untuk backup).
# ---------------------------------------------------------------------------
FROM golang:1.25-alpine AS builder

WORKDIR /src

# Cache dependency lebih dulu agar rebuild cepat.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG APP_VERSION=dev
RUN CGO_ENABLED=0 GOOS=linux go build \
      -trimpath \
      -ldflags "-s -w -X main.buildVersion=${APP_VERSION}" \
      -o /out/ingatin ./cmd/ingatin

# ---------------------------------------------------------------------------
FROM alpine:3.20

# pg_dump: dipakai backup; gzip: kompresi; tzdata: zona waktu WIB; ca-certificates: HTTPS ke provider.
RUN apk add --no-cache postgresql-client gzip ca-certificates tzdata \
    && addgroup -S ingatin && adduser -S ingatin -G ingatin

WORKDIR /app

COPY --from=builder /out/ingatin /app/ingatin

RUN mkdir -p /app/data/backups /app/data/logs \
    && chown -R ingatin:ingatin /app

USER ingatin

# 8081 dipakai oleh mode api. Mode worker tidak membuka port.
EXPOSE 8081

ENTRYPOINT ["/app/ingatin"]
CMD ["-mode=api"]
