FROM golang:1.21.0 AS builder
WORKDIR /app
COPY go.mod /app
COPY go.sum /app
RUN go mod download
COPY . /app
RUN CGO_ENABLED=0 go build -o xhe-hub

FROM scratch
WORKDIR /app
COPY --from=builder /app/xhe-hub /app/xhe-hub
# HEALTHCHECK --interval=1s --timeout=3s CMD [ "/app/xhe-hub", "check" ]
ENTRYPOINT [ "/app/xhe-hub" ]
