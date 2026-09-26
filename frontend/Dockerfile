# ---------------------------------------------------------------------------
# Ingat.in — frontend image (build Vite lalu serve dengan nginx)
# ---------------------------------------------------------------------------
FROM node:20-alpine AS builder

WORKDIR /app

# Cache dependency lebih dulu.
COPY package.json package-lock.json* ./
RUN npm install --no-audit --no-fund

COPY . .
RUN npm run build

# ---------------------------------------------------------------------------
FROM nginx:1.27-alpine

# Konfigurasi kita menggantikan default.
# Template diproses oleh entrypoint resmi nginx (envsubst) sehingga port
# listen dapat diatur lewat INGATIN_WEB_PORT.
RUN rm -f /etc/nginx/conf.d/default.conf
COPY nginx/default.conf.template /etc/nginx/templates/default.conf.template

# Nilai default bila tidak diset dari compose.
# - INGATIN_WEB_PORT: port listen nginx.
# - INGATIN_API_UPSTREAM: alamat backend API (service `api` di network Compose).
ENV INGATIN_WEB_PORT=8091 \
    INGATIN_API_UPSTREAM=http://api:8081

COPY --from=builder /app/dist /usr/share/nginx/html

EXPOSE 8091

# Healthcheck sederhana untuk compose.
HEALTHCHECK --interval=15s --timeout=3s --start-period=10s --retries=5 \
    CMD wget -q -O /dev/null "http://127.0.0.1:${INGATIN_WEB_PORT}/" || exit 1

CMD ["nginx", "-g", "daemon off;"]
