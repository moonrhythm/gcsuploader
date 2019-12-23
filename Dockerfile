FROM gcr.io/moonrhythm-containers/golang:1.13-alpine as build

ENV CGO_ENABLED=0
WORKDIR /workspace
ADD go.mod go.sum ./
RUN go mod download
ADD . .
RUN go build -o gcsuploader -ldflags '-w -s' .

FROM gcr.io/moonrhythm-containers/go-scratch

COPY --from=build /workspace/gcsuploader /gcsuploader

ENTRYPOINT ["/gcsuploader"]
