#!/bin/sh

cd "$(dirname "$0")/.." || exit 1

echo "$GITHUB_TOKEN" | docker login ghcr.io --username "$GITHUB_ACTOR" --password-stdin

# docker buildx build --push --platform linux/arm/v7,linux/arm64/v8,linux/amd64 --tag ghcr.io/vcaesar/zedsearch:test .

docker buildx build --push --platform linux/amd64 --tag ghcr.io/vcaesar/zedsearch:test .

