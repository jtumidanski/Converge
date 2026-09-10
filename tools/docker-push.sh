#!/usr/bin/env bash
# Pushes the built image tags. The registry is never hard-coded: it comes from
# IMAGE_REPOSITORY, GITHUB_REPOSITORY (GHCR), or CI_REGISTRY_IMAGE (GitLab).
set -euo pipefail

root="$(git rev-parse --show-toplevel)"
version="${VERSION:-$("$root/tools/version.sh")}"
git_sha="${GIT_SHA:-$(git rev-parse HEAD)}"

if [[ -n "${IMAGE_REPOSITORY:-}" ]]; then
  image="$IMAGE_REPOSITORY"
elif [[ -n "${GITHUB_REPOSITORY:-}" ]]; then
  image="ghcr.io/${GITHUB_REPOSITORY,,}"
elif [[ -n "${CI_REGISTRY_IMAGE:-}" ]]; then
  image="$CI_REGISTRY_IMAGE"
else
  echo "docker-push: set IMAGE_REPOSITORY, GITHUB_REPOSITORY or CI_REGISTRY_IMAGE" >&2
  exit 1
fi

# Docker repository names must be lowercase; must match the tags docker-build made.
image="${image,,}"

tags=("$image:$version" "$image:$git_sha")
if [[ "${MAINLINE:-0}" == "1" ]]; then
  tags+=("$image:latest")
fi

for tag in "${tags[@]}"; do
  echo "pushing $tag"
  docker push "$tag"
done
