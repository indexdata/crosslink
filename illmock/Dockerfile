FROM golang:1.26 AS build

WORKDIR /app

# Copy manifests first so source changes do not invalidate the dependency cache.
COPY directory-mock/go.mod directory-mock/go.sum ./directory-mock/
COPY httpclient/go.mod httpclient/go.sum ./httpclient/
COPY iso18626/go.mod iso18626/go.sum ./iso18626/
COPY marcxml/go.mod marcxml/go.sum ./marcxml/
COPY ncip/go.mod ncip/go.sum ./ncip/
COPY sru/go.mod sru/go.sum ./sru/
COPY testutil/go.mod testutil/go.sum ./testutil/
COPY illmock/go.mod illmock/go.sum ./illmock/

RUN go work init \
    ./illmock \
    ./directory-mock \
    ./httpclient \
    ./iso18626 \
    ./marcxml \
    ./ncip \
    ./sru \
    ./testutil

WORKDIR /app/illmock

RUN --mount=type=cache,sharing=shared,target=/go/pkg/mod \
    go mod download

# Copy sources after dependencies have been downloaded.
COPY directory-mock/ /app/directory-mock/
COPY httpclient/ /app/httpclient/
COPY iso18626/ /app/iso18626/
COPY marcxml/ /app/marcxml/
COPY ncip/ /app/ncip/
COPY sru/ /app/sru/
COPY testutil/ /app/testutil/
COPY illmock/ ./

# Build
RUN --mount=type=cache,sharing=shared,target=/go/pkg/mod \
    --mount=type=cache,sharing=shared,target=/root/.cache/go-build \
    CGO_ENABLED=0 \
    GOOS=linux \
    go build -o /illmock ./cmd/illmock

# create runtime user
RUN adduser \
  --disabled-password \
  --gecos "" \
  --home "/nonexistent" \
  --shell "/sbin/nologin" \
  --no-create-home \
  --uid 65532 \
  illmock-user

# create small runtime image
FROM scratch

# need to copy SSL certs and runtime use
COPY --from=build /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /etc/passwd /etc/passwd
COPY --from=build /etc/group /etc/group

# copy the binary
COPY --from=build /illmock .

ENV HTTP_PORT=8081
EXPOSE ${HTTP_PORT}

# Run
USER illmock-user:illmock-user
CMD ["/illmock"]
