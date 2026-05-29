FROM golang:1.23-alpine AS build

RUN apk add --no-cache git curl

# Install templ CLI for generating template Go code
RUN go install github.com/a-h/templ/cmd/templ@v0.2.793

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN templ generate ./templates/...
RUN go build -o /quinoa .

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
COPY --from=build /quinoa /quinoa
EXPOSE 8080
CMD ["/quinoa"]
