# --- build: compile every command into /out ---------------------------------
FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/ ./cmd/...

# --- test: used by `make test`, runs unit + integration tests ---------------
FROM golang:1.24-alpine AS test
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
CMD ["go", "test", "-count=1", "./..."]

# --- runtime: small image with the binaries only -----------------------------
FROM alpine:3.20 AS runtime
RUN apk add --no-cache ca-certificates tzdata wget && adduser -D -u 10001 app
COPY --from=build /out/ /usr/local/bin/
USER app
EXPOSE 8080
CMD ["api"]
