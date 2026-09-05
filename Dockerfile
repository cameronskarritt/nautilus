FROM golang:1.27.0 AS builder

WORKDIR /build

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 go build -ldflags '-s -w' -o /build/bin/app ./cmd/app
RUN CGO_ENABLED=0 go build -ldflags '-s -w' -o /build/bin/agent ./cmd/agent

FROM gcr.io/distroless/static-debian13:nonroot

COPY --from=builder /build/bin /bin

EXPOSE 8081

CMD ["/bin/app", "serve"]
