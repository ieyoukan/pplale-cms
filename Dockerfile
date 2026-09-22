# --platform=$BUILDPLATFORM: run node/go toolchains natively on the builder
# host instead of under QEMU emulation for every target platform. Without
# this, a multi-arch build (linux/amd64,linux/arm64) emulates the whole Go
# toolchain per platform, which is slow and can silently produce a binary the
# target kernel refuses to exec ("exec: no such file or directory"). Go
# cross-compiles natively via GOOS/GOARCH, so emulation buys nothing here.
FROM --platform=$BUILDPLATFORM node:22-alpine AS web
WORKDIR /app/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ cmd/
COPY internal/ internal/
# The nodynamic tag selects the WebP encoder's embedded WebAssembly backend,
# keeping the server fully static for the distroless/static runtime image.
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -tags=nodynamic -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /out/server /app/server
COPY --from=web /app/web/dist /app/web/dist
ENV STATIC_DIR=/app/web/dist
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/app/server"]
