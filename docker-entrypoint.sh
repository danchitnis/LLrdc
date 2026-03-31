#!/usr/bin/env bash
set -e

ensure_group_for_gid() {
    gid="$1"
    preferred_name="$2"

    if [ -z "${gid}" ]; then
        return 0
    fi

    existing_name=$(getent group "${gid}" | cut -d: -f1 || true)
    if [ -n "${existing_name}" ]; then
        usermod -aG "${existing_name}" remote || true
        return 0
    fi

    if getent group "${preferred_name}" >/dev/null 2>&1; then
        groupmod -g "${gid}" "${preferred_name}" || true
        usermod -aG "${preferred_name}" remote || true
        return 0
    fi

    groupadd -g "${gid}" "${preferred_name}" || true
    usermod -aG "${preferred_name}" remote || true
}

if [ -n "${HOST_UID}" ]; then
    CURRENT_UID=$(id -u remote)
    if [ "${HOST_UID}" != "${CURRENT_UID}" ]; then
        echo "Updating 'remote' UID from ${CURRENT_UID} to ${HOST_UID}..."
        usermod -u "${HOST_UID}" remote || true
        chown -R remote:remote /home/remote /app || true
    fi
fi

if [ -e /dev/uinput ]; then
    chmod 666 /dev/uinput || true
fi

if [ -d /dev/dri ]; then
    chmod 666 /dev/dri/renderD* 2>/dev/null || true
    chmod 666 /dev/dri/card* 2>/dev/null || true
fi

ensure_group_for_gid "${RENDER_GID:-}" hostrender
ensure_group_for_gid "${VIDEO_GID:-}" hostvideo

# Execute the main process as the remote user, preserving environment
exec sudo -E -H -u remote "$@"
