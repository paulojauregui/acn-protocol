FROM golang:1.27-alpine AS builder

WORKDIR /src

COPY go.mod ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /out/acn-node ./cmd/acn-node

FROM alpine:3.21

RUN addgroup -g 10001 -S acn && adduser -u 10001 -S -G acn -H -s /sbin/nologin acn && \
    apk --no-cache add ca-certificates

COPY --from=builder /out/acn-node /usr/local/bin/acn-node

USER 10001:10001
EXPOSE 8000

ENV PORT=8000

ENTRYPOINT ["/usr/local/bin/acn-node"]
