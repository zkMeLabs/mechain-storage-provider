FROM golang:1.22.4-alpine as builder

# ENV CGO_CFLAGS="-O -D__BLST_PORTABLE__"
# ENV CGO_CFLAGS_ALLOW="-O -D__BLST_PORTABLE__"
# ENV CGO_ENABLED=1
# ENV GO111MODULE=on

RUN apk add --no-cache make git bash protoc build-base libc-dev

WORKDIR /mechain-storage-provider

COPY . .

RUN  make build

# Pull greenfield into a second stage deploy alpine container
FROM alpine:3.17
COPY --from=builder /mechain-storage-provider/build/mechain-sp /usr/bin/mechain-sp
CMD ["mechain-sp"]