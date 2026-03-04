FROM golang:1.25.3 AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download
COPY . .

ARG SERVICE=gateway
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/service ./backend/cmd/${SERVICE}

FROM gcr.io/distroless/base-debian12
WORKDIR /app
COPY --from=build /out/service /app/service

ENV HUBGAME_ADDR=:8080
EXPOSE 8080
ENTRYPOINT ["/app/service"]
