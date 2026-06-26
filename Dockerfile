FROM gcr.io/distroless/static-debian12:nonroot

ARG TARGETPLATFORM
COPY ${TARGETPLATFORM}/commvault-exporter /commvault-exporter
USER nonroot:nonroot
EXPOSE 9720
ENTRYPOINT ["/commvault-exporter"]
