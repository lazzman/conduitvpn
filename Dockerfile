# ---- build stage ----
FROM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/aimilivpn ./cmd/aimilivpn

# ---- runtime stage ----
FROM alpine:3.20
RUN apk add --no-cache openvpn ca-certificates iproute2 iptables tzdata

COPY --from=build /out/aimilivpn /usr/local/bin/aimilivpn

ENV AIMILI_DATA_DIR=/data/aimilivpn
ENV LOCAL_PROXY_HOST=0.0.0.0
ENV UI_HOST=0.0.0.0

VOLUME ["/data/aimilivpn"]
EXPOSE 8787 7928

HEALTHCHECK --interval=30s --timeout=8s --start-period=25s --retries=3 \
  CMD wget -qO- "http://127.0.0.1:8787/healthz" >/dev/null 2>&1 || exit 1

ENTRYPOINT ["aimilivpn"]
