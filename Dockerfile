FROM golang:1.16.2 as build

ENV GOOS=linux
ENV GOARCH=amd64
ENV CGO_ENABLED=0
WORKDIR /workspace
ADD go.mod go.sum ./
RUN go mod download
ADD . .
RUN go build -o gcsuploader -ldflags '-w -s' .

FROM gcr.io/moonrhythm-containers/go-scratch

COPY --from=build /workspace/gcsuploader /gcsuploader

ENTRYPOINT ["/gcsuploader"]
