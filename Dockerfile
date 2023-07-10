FROM golang:1.20 as build

WORKDIR /src

ENV CGO_ENABLED=0

COPY go.* .

RUN go mod download

COPY . .

RUN --mount=type=cache,target=/root/.cache/go-build go build -o /src/main

FROM quay.io/argoproj/argo-rollouts:v1.5.1

COPY  --from=build /src/main /home/argo-rollouts/plugin

ENTRYPOINT ["/bin/rollouts-controller"]