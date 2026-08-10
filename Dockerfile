# ---- build stage ----
FROM golang:1.22-alpine@sha256:1699c10032ca2582ec89a24a1312d986a3f094aed3d5c1147b19880afe40e052 AS build
WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/conduitvpn ./cmd/conduitvpn

# ---- sing-box download stage ----
FROM alpine:3.20@sha256:d9e853e87e55526f6b2917df91a2115c36dd7c696a35be12163d44e6e2a4b6bc AS singbox
ARG TARGETARCH
ARG SINGBOX_SHA256_AMD64=30420c7e1a0e4b9c7ee2ff3992c53257be85dec2bdc93074594c8b92d19d4d71
ARG SINGBOX_SHA256_ARM64=d309eec0b2251223227be9bef09c07c348df31d55677e2a785415aa542911951
RUN apk add --no-cache wget ca-certificates && \
	ARCH=${TARGETARCH:-amd64} && \
	case "$ARCH" in amd64) CHECKSUM="$SINGBOX_SHA256_AMD64" ;; arm64) CHECKSUM="$SINGBOX_SHA256_ARM64" ;; *) echo "unsupported architecture: $ARCH" >&2; exit 1 ;; esac && \
	wget -qO /tmp/sb.tgz "https://github.com/SagerNet/sing-box/releases/download/v1.11.7/sing-box-1.11.7-linux-${ARCH}.tar.gz" && \
	echo "$CHECKSUM  /tmp/sb.tgz" | sha256sum -c - && \
	tar -xzf /tmp/sb.tgz -C /opt && \
    mv /opt/sing-box-*/sing-box /usr/local/bin/sing-box && \
    rm -rf /opt/sing-box-* /tmp/sb.tgz && \
    sing-box version | head -1

# ---- runtime stage ----
FROM alpine:3.20@sha256:d9e853e87e55526f6b2917df91a2115c36dd7c696a35be12163d44e6e2a4b6bc
RUN apk add --no-cache openvpn ca-certificates iproute2 iptables tzdata

COPY --from=build /out/conduitvpn /usr/local/bin/conduitvpn
COPY --from=singbox /usr/local/bin/sing-box /usr/local/bin/sing-box

ENV CONDUIT_DATA_DIR=/data/conduitvpn

VOLUME ["/data/conduitvpn"]
EXPOSE 8787 7928

HEALTHCHECK --interval=30s --timeout=8s --start-period=25s --retries=3 \
  CMD if [ -n "$UI_TLS_CERT" ]; then wget --no-check-certificate -qO- "https://127.0.0.1:8787/healthz"; else wget -qO- "http://127.0.0.1:8787/healthz"; fi >/dev/null 2>&1 || exit 1

ENTRYPOINT ["conduitvpn"]
