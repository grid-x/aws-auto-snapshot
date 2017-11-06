FROM alpine:3.6

RUN apk --no-cache add ca-certificates

COPY bin/snapshotter.linux /usr/local/bin/snapshotter
RUN chmod +x /usr/local/bin/snapshotter

ENTRYPOINT ["/usr/local/bin/snapshotter"]
