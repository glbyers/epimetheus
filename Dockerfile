# Start by building the application.
FROM golang:1.24 AS build

WORKDIR /go/src/app
COPY . .

RUN go mod download
RUN CGO_ENABLED=0 go build -o /go/bin/app

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /go/bin/app /
ENTRYPOINT ["/app"]
