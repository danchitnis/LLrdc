#!/usr/bin/env bash

detect_image_variant() {
  local image_ref="$1"
  local variant
  variant=$(docker image inspect --format '{{ index .Config.Labels "com.danchitnis.llrdc.build-variant" }}' "$image_ref" 2>/dev/null || true)
  if [ "$variant" = "<no value>" ]; then
    variant=""
  fi
  printf '%s' "$variant"
}

ensure_image_exists() {
  local image_ref="$1"
  local intel_requested="$2"
  if docker image inspect "$image_ref" >/dev/null 2>&1; then
    return 0
  fi

  echo "ERROR: Docker image ${image_ref} is not available locally."
  if [ "$intel_requested" = "true" ]; then
    echo "Build it with: ./docker-build.sh --intel"
  else
    echo "Build it with: ./docker-build.sh"
  fi
  exit 1
}

append_words() {
  local var_name="$1"
  local words="${2:-}"
  if [ -z "$words" ]; then
    return 0
  fi

  local word
  # Intentional word splitting: callers pass Docker arg fragments assembled by this repo.
  for word in $words; do
    eval "$var_name+=(\"\$word\")"
  done
}

print_command() {
  local arg
  printf '%q' "$1"
  shift
  for arg in "$@"; do
    printf ' %q' "$arg"
  done
  printf '\n'
}
