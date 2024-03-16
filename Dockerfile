FROM alpine:3.19

RUN apk update
RUN apk add tini xvfb x11vnc

EXPOSE 5900
