FROM --platform=$BUILDPLATFORM golang:1.27.1-bookworm AS build

ARG TARGETOS
ARG TARGETARCH

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY *.go ./
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /godaddy-domain-exporter .

FROM scratch

COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build --chown=65532:65532 /godaddy-domain-exporter /godaddy-domain-exporter

USER 65532:65532
EXPOSE 8080

ENTRYPOINT ["/godaddy-domain-exporter"]
