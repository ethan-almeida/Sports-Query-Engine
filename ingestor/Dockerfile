FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /bin/ingestor .

FROM alpine:3.21
RUN adduser -D ingestor
USER ingestor
WORKDIR /app
COPY --from=build /bin/ingestor /bin/ingestor
COPY --from=build /src/configs /app/configs
ENTRYPOINT ["/bin/ingestor"]
