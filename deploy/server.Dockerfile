FROM golang:1.24-alpine AS build

WORKDIR /src
RUN apk add --no-cache ca-certificates
COPY server/go.mod server/go.sum ./
RUN go mod download
COPY server/ ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/api ./cmd/api \
    && CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/migrate ./cmd/migrate

FROM alpine:3.21
RUN apk add --no-cache ca-certificates \
    && addgroup -S app \
    && adduser -S -G app app
COPY --from=build /out/api /api
COPY --from=build /out/migrate /migrate
USER app
EXPOSE 8080
ENTRYPOINT ["/api"]
