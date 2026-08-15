ARG VGXNESS_GO_BUILDER_IMAGE
ARG VGXNESS_RUNTIME_IMAGE
FROM --platform=$BUILDPLATFORM ${VGXNESS_GO_BUILDER_IMAGE} AS build
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -buildvcs=false -mod=readonly -o /out/vgxness-syncd ./cmd/vgxness-syncd

FROM ${VGXNESS_RUNTIME_IMAGE}
COPY --from=build /out/vgxness-syncd /vgxness-syncd
USER 65532:65532
ENTRYPOINT ["/vgxness-syncd"]
