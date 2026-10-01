FROM --platform=$BUILDPLATFORM golang:1.27 AS build

# Build from the CrossLink repository root.
WORKDIR /app/supply
COPY supply/go.mod supply/go.sum ./

RUN --mount=type=cache,sharing=shared,target=/go/pkg/mod \
    go mod download

COPY supply/main.go ./
COPY supply/app/ ./app/

ARG TARGETARCH

RUN --mount=type=cache,sharing=shared,target=/go/pkg/mod \
    --mount=type=cache,sharing=shared,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux GOARCH=$TARGETARCH go build -o /supply .

RUN adduser \
    --disabled-password \
    --gecos "" \
    --home "/nonexistent" \
    --shell "/sbin/nologin" \
    --no-create-home \
    --uid 65532 \
    supply-user

# Multipart uploads larger than the in-memory limit need a writable temp directory.
RUN mkdir -p /runtime/tmp && chmod 1777 /runtime/tmp

FROM scratch

COPY --from=build /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /etc/passwd /etc/passwd
COPY --from=build /etc/group /etc/group
COPY --from=build /runtime/ /
COPY --from=build /supply /supply

ENV HTTP_PORT=8086
EXPOSE ${HTTP_PORT}

USER supply-user:supply-user
CMD ["/supply"]
