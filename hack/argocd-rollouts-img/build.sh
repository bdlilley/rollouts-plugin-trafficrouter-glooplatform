#!/bin/bash

rm -rf ./dist/*
mkdir -p ./dist

GOOS=linux GOARCH=amd64 go build -o ./dist/plugin-linux-amd64 ../../.

chmod a+rx ./dist/plugin-linux-amd64

docker buildx build --push --platform linux/arm64,linux/amd64 -t kodacd/argo-rollouts . 