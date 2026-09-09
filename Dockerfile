FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /connector ./cmd/connector

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /connector /connector
EXPOSE 8092
ENTRYPOINT ["/connector"]
