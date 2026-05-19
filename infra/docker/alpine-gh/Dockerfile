FROM docker.io/library/alpine:3.20

RUN apk add --no-cache ca-certificates curl github-cli jq \
  && addgroup -g 1000 app \
  && adduser -D -u 1000 -G app app

USER 1000:1000
ENV HOME=/tmp

ENTRYPOINT ["/bin/sh"]
