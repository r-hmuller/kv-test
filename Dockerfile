FROM golang:alpine
RUN mkdir /app
ADD . /app/
WORKDIR /app
RUN mkdir /app/logs
COPY go.mod .
COPY go.sum .
RUN go mod download
RUN go build -o main .
RUN adduser -S -D -H -h /app appuser
USER appuser
EXPOSE 3000
CMD ["./main"]