#include <stdbool.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <wayland-client.h>

struct probe_state {
    bool has_screencopy;
    bool has_dmabuf;
};

static void registry_global(void *data, struct wl_registry *registry, uint32_t name, const char *interface, uint32_t version) {
    (void)registry;
    (void)name;
    (void)version;

    struct probe_state *state = data;
    if (strcmp(interface, "zwlr_screencopy_manager_v1") == 0) {
        state->has_screencopy = true;
    } else if (strcmp(interface, "zwp_linux_dmabuf_v1") == 0) {
        state->has_dmabuf = true;
    }
}

static void registry_global_remove(void *data, struct wl_registry *registry, uint32_t name) {
    (void)data;
    (void)registry;
    (void)name;
}

static const struct wl_registry_listener registry_listener = {
    .global = registry_global,
    .global_remove = registry_global_remove,
};

int main(void) {
    struct wl_display *display = wl_display_connect(NULL);
    if (display == NULL) {
        fprintf(stderr, "failed to connect to Wayland display\n");
        return 1;
    }

    struct probe_state state = {0};
    struct wl_registry *registry = wl_display_get_registry(display);
    if (registry == NULL) {
        fprintf(stderr, "failed to get Wayland registry\n");
        wl_display_disconnect(display);
        return 1;
    }

    wl_registry_add_listener(registry, &registry_listener, &state);
    if (wl_display_roundtrip(display) < 0) {
        fprintf(stderr, "failed to read Wayland globals\n");
        wl_registry_destroy(registry);
        wl_display_disconnect(display);
        return 1;
    }

    printf("screencopy=%d\n", state.has_screencopy ? 1 : 0);
    printf("dmabuf=%d\n", state.has_dmabuf ? 1 : 0);

    wl_registry_destroy(registry);
    wl_display_disconnect(display);
    return (state.has_screencopy && state.has_dmabuf) ? 0 : 2;
}
