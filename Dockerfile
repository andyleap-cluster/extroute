FROM golang:alpine as builder
RUN mkdir /build 
WORKDIR /build 
ADD go.mod go.sum /build/
RUN go mod download
ADD . /build/
RUN go build -o main .
FROM alpine
COPY --from=builder /build/main /app/
WORKDIR /app
CMD ["./main"]