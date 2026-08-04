FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go test ./... && \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags "-s -w" -o /out/reef ./cmd/reef

FROM alpine:3.22
RUN addgroup -S reef && adduser -S -G reef reef
COPY --from=build /out/reef /usr/local/bin/reef
COPY LICENSE NOTICE /usr/share/doc/reef/
RUN mkdir -p /data && chown reef:reef /data
USER reef
VOLUME ["/data"]
EXPOSE 47831/tcp
EXPOSE 47830/udp
ENTRYPOINT ["/usr/local/bin/reef", "--server", "--no-browser", "--data-dir", "/data"]
