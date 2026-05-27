FROM scratch
COPY marlin /usr/local/bin/marlin
ENTRYPOINT ["/usr/local/bin/marlin"]
