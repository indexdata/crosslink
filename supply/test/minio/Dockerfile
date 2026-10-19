FROM alpine:3.22

# Preserve the imported test fixture's release without relying on its registry image.
# Checksums are from the corresponding official GitHub release assets.
RUN set -eu; \
    apk add --no-cache ca-certificates curl; \
    case "$(uname -m)" in \
        x86_64) arch=amd64; checksum=8ec42f70f5db7bfe584790b06af3f7e2b16f48488ea4c96e0ebd8cea40990dfa ;; \
        aarch64) arch=arm64; checksum=27c89f905e06aa1706bddb3c55d53dbbc550df8a44043cd77b36bc7ea66e156b ;; \
        *) echo "Unsupported MinIO test architecture" >&2; exit 1 ;; \
    esac; \
    curl --fail --location --max-time 120 \
        "https://github.com/minio/minio/releases/download/RELEASE.2025-03-12T18-04-18Z/minio.linux-${arch}.RELEASE.2025-03-12T18-04-18Z" \
        --output /usr/local/bin/minio; \
    printf '%s  /usr/local/bin/minio\n' "$checksum" | sha256sum -c -; \
    chmod 755 /usr/local/bin/minio

ENTRYPOINT ["/usr/local/bin/minio"]
