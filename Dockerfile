FROM golang:1.26.1 AS build
WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /ownbot ./cmd/ownbot

FROM scratch
COPY --from=build /ownbot /ownbot
USER 65534:65534
ENTRYPOINT ["/ownbot"]
