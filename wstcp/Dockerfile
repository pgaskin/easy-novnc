FROM golang:1.13-alpine AS build
ADD . /src
WORKDIR /src
RUN apk add --no-cache git
RUN go mod download
RUN go build ./wstcp

FROM alpine:latest
COPY --from=build /src/wstcp /
EXPOSE 5900
ENTRYPOINT ["/wstcp"]