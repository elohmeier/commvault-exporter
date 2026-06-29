FROM alpine:3.24.1

RUN apk add --no-cache ca-certificates netcat-openbsd \
    && addgroup -S commvault-exporter \
    && adduser -S -D -H -h /nonexistent -s /sbin/nologin -G commvault-exporter commvault-exporter

ARG TARGETPLATFORM
COPY ${TARGETPLATFORM}/commvault-exporter /commvault-exporter
USER commvault-exporter:commvault-exporter
EXPOSE 9720
ENTRYPOINT ["/commvault-exporter"]
