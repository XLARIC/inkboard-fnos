FROM golang:1.27.1-alpine AS build
WORKDIR /src
COPY . .
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} go build -trimpath -ldflags="-s -w" -o /inkboard .
FROM alpine:3.23
RUN apk add --no-cache ca-certificates && addgroup -g 10001 inkboard && adduser -D -H -u 10001 -G inkboard inkboard && mkdir -p /config /data && chown inkboard:inkboard /config /data
COPY --from=build /inkboard /usr/local/bin/inkboard
COPY LICENSE THIRD_PARTY_NOTICES.md /usr/share/inkboard/
COPY licenses /usr/share/inkboard/licenses
USER 10001:10001
ENV INKBOARD_CONFIG_DIR=/config INKBOARD_DATA_DIR=/data INKBOARD_LISTEN=0.0.0.0:18888
EXPOSE 18888
VOLUME ["/config","/data"]
ENTRYPOINT ["/usr/local/bin/inkboard"]
CMD ["serve"]
