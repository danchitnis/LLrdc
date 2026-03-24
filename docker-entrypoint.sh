#!/usr/bin/env bash
set -e

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

# Execute the main process as the remote user, preserving environment
exec sudo -E -H -u remote "$@"