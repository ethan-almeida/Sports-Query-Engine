FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /bin/gateway .

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /bin/gateway /bin/gateway
ENTRYPOINT ["/bin/gateway"]
