FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/jenkins-mcp-server ./cmd/jenkins-mcp-server

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/jenkins-mcp-server /jenkins-mcp-server
USER nonroot:nonroot
ENTRYPOINT ["/jenkins-mcp-server"]
