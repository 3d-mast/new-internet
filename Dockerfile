FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go test ./... && \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags "-s -w" -o /out/canopy ./cmd/canopy

FROM alpine:3.22
RUN addgroup -S canopy && adduser -S -G canopy canopy
COPY --from=build /out/canopy /usr/local/bin/canopy
COPY LICENSE NOTICE /usr/share/doc/canopy/
RUN mkdir -p /data && chown canopy:canopy /data
USER canopy
VOLUME ["/data"]
EXPOSE 47831/tcp
EXPOSE 47830/udp
ENTRYPOINT ["/usr/local/bin/canopy", "--server", "--no-browser", "--data-dir", "/data"]
