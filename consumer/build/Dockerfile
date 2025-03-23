FROM golang:1.24-alpine

RUN apk add --no-cache git

RUN go install github.com/air-verse/air@latest

WORKDIR /app

COPY go.mod go.sum ./

RUN go mod download

COPY . .

EXPOSE 5000

CMD ["air", "-c", ".air.toml"]