# Build: docker build -f deploy/Dockerfile -t pfm-backend .
FROM golang:1.26.11-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/ ./cmd/...

# Distroless, non-root, tanpa shell.
FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /out/api /out/migrate /out/worker /app/
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/app/api"]
