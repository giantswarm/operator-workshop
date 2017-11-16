FROM alpine:3.5

RUN apk add --no-cache ca-certificates

ADD ./operator-workshop /operator-workshop

ENTRYPOINT ["/operator-workshop"]
