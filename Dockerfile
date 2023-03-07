FROM golang:1.20.1

ENV CGO_ENABLED=0
WORKDIR /workspace
ADD go.mod go.sum ./
RUN go mod download
ADD . .
RUN go build -o .build/gcsuploader -ldflags "-w -s" .

FROM gcr.io/distroless/static

WORKDIR /app

COPY --from=0 /workspace/.build/* ./

ENTRYPOINT ["/app/gcsuploader"]