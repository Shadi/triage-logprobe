FROM --platform=$BUILDPLATFORM docker.io/library/golang:1.27.1 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY rubric.go rubric.json ./
COPY cmd ./cmd
COPY internal ./internal
ARG TARGETOS TARGETARCH
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /out/triage-logprobe ./cmd/triage-logprobe

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/triage-logprobe /triage-logprobe
EXPOSE 8080
ENTRYPOINT ["/triage-logprobe"]
