FROM scratch
ARG TARGETPLATFORM
COPY docker/$TARGETPLATFORM/bmc_exporter /
USER nobody
EXPOSE 9622
ENTRYPOINT ["/bmc_exporter"]
