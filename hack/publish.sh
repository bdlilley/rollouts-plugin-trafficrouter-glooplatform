#!/bin/bash

branch_name="$(git symbolic-ref HEAD 2>/dev/null)"
branch_name=${branch_name##refs/heads/}

if [ "$branch_name" == "main" ]; then
    branch_name="latest"
fi

docker buildx build --platform linux/amd64,linux/arm64 --push -t kodacd/argo-rollouts:${branch_name}  .