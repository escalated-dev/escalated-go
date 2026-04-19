# syntax=docker/dockerfile:1.7

FROM golang:1.22-alpine AS builder

RUN apk add --no-cache git build-base

WORKDIR /package
COPY go.mod go.sum ./
RUN go mod download
COPY . .

# Host app lives here. Replace the module path to use the local copy.
WORKDIR /host
COPY docker/host-app/go.mod docker/host-app/go.sum* ./
COPY docker/host-app/*.go ./
RUN go mod edit -replace=github.com/escalated-dev/escalated-go=/package && \
    go mod tidy && \
    go build -o /out/app ./...

FROM alpine:3.20 AS runtime
RUN apk add --no-cache postgresql-client ca-certificates
COPY --from=builder /out/app /usr/local/bin/app
COPY docker/entrypoint.sh /usr/local/bin/entrypoint.sh
RUN chmod +x /usr/local/bin/entrypoint.sh
EXPOSE 8000
ENTRYPOINT ["entrypoint.sh"]
CMD ["app"]
