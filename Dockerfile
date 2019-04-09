FROM golang:1.12-alpine AS build
ADD . /src
WORKDIR /src
RUN apk add --no-cache git
RUN go mod download
RUN go build

FROM alpine:latest
COPY --from=build /src/easy-novnc /
EXPOSE 8080
ENTRYPOINT ["/easy-novnc"]