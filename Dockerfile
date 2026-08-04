FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go test ./... && \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags "-s -w" -o /out/rhizome ./cmd/rhizome

FROM alpine:3.22
RUN addgroup -S rhizome && adduser -S -G rhizome rhizome
COPY --from=build /out/rhizome /usr/local/bin/rhizome
COPY LICENSE NOTICE /usr/share/doc/rhizome/
RUN mkdir -p /data && chown rhizome:rhizome /data
USER rhizome
VOLUME ["/data"]
EXPOSE 47831/tcp
EXPOSE 47830/udp
ENTRYPOINT ["/usr/local/bin/rhizome", "--server", "--no-browser", "--data-dir", "/data"]
