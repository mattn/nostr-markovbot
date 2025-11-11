# syntax=docker/dockerfile:1.4

FROM golang:1.25-alpine AS build-dev
WORKDIR /go/src/app
COPY --link go.mod go.sum ./
RUN apk add --no-cache upx || \
    go version && \
    go mod download
COPY --link . .
RUN CGO_ENABLED=0 go install -buildvcs=false -trimpath -ldflags '-w -s'
RUN [ -e /usr/bin/upx ] && upx /go/bin/nostr-markovbot || echo
FROM scratch
COPY --link --from=build-dev /go/bin/nostr-markovbot /go/bin/nostr-markovbot
COPY --link --from=build-dev /go/src/app/ignores.txt /go/bin/ignores.txt
COPY --from=build-dev /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
ENV IGNORES=/go/bin/ignores.txt
ENTRYPOINT ["/go/bin/nostr-markovbot"]

