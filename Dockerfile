FROM golang:1.20 as build

LABEL repo="https://github.com/tonyo/admission-control"
ARG GIT_COMMIT=""
LABEL commit=$GIT_COMMIT
ENV GIT_COMMIT=$GIT_COMMIT

WORKDIR /go/src/app
COPY go.mod .
COPY go.sum .

RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go install -v ./...

FROM gcr.io/distroless/base
COPY --from=build /go/bin/admissiond /
EXPOSE 8443

CMD ["/admissiond", "-cert-path", "certs/cert.crt", "-key-path", "certs/key.key", "-port", "8443"]
