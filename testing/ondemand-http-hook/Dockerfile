# Builds MediaMTX from the repo working tree (not a release tarball), so this
# always reflects whatever is currently on disk. Build context is the repo
# root — see docker-compose.yml.
FROM golang:1.27-alpine AS build
RUN apk add --no-cache git
WORKDIR /s
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go generate ./...
RUN CGO_ENABLED=0 go build -o /mediamtx .

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
COPY --from=build /mediamtx /mediamtx
ENTRYPOINT ["/mediamtx"]
