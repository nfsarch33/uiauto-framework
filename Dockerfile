# syntax=docker/dockerfile:1.7
FROM golang:1.24.11-bookworm AS build
WORKDIR /src
COPY go.mod ./
RUN go mod download || true
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/ui-agent ./cmd/ui-agent

FROM gcr.io/distroless/base-debian12:nonroot
COPY --from=build /out/ui-agent /ui-agent
WORKDIR /work
ENTRYPOINT ["/ui-agent"]
