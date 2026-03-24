#define _POSIX_C_SOURCE 200809L
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>
#include <wayland-client.h>
#include "wlr-virtual-pointer-unstable-v1-client-protocol.h"

struct mouse_client {
	struct wl_display *display;
	struct wl_registry *registry;
	struct zwlr_virtual_pointer_manager_v1 *manager;
	struct zwlr_virtual_pointer_v1 *pointer;
};

static void registry_handle_global(void *data, struct wl_registry *registry,
		uint32_t name, const char *interface, uint32_t version) {
	struct mouse_client *client = data;
	if (strcmp(interface, zwlr_virtual_pointer_manager_v1_interface.name) == 0) {
		client->manager = wl_registry_bind(registry, name,
			&zwlr_virtual_pointer_manager_v1_interface, 1);
	}
}

static void registry_handle_global_remove(void *data, struct wl_registry *registry,
		uint32_t name) {
}

static const struct wl_registry_listener registry_listener = {
	.global = registry_handle_global,
	.global_remove = registry_handle_global_remove,
};

int main(int argc, char *argv[]) {
	struct mouse_client client = {0};
	client.display = wl_display_connect(NULL);
	if (!client.display) {
		fprintf(stderr, "Failed to connect to wayland display\n");
		return 1;
	}

	client.registry = wl_display_get_registry(client.display);
	wl_registry_add_listener(client.registry, &registry_listener, &client);
	wl_display_roundtrip(client.display);

	if (!client.manager) {
		fprintf(stderr, "Compositor does not support wlr-virtual-pointer-v1\n");
		return 1;
	}

	client.pointer = zwlr_virtual_pointer_manager_v1_create_virtual_pointer(client.manager, NULL);

	char line[256];
	while (fgets(line, sizeof(line), stdin)) {
		char type[16];
		if (sscanf(line, "%s", type) != 1) continue;

		if (strcmp(type, "move") == 0) {
			int x, y, w, h;
			if (sscanf(line, "move %d %d %d %d", &x, &y, &w, &h) == 4) {
				zwlr_virtual_pointer_v1_motion_absolute(client.pointer, 0, x, y, w, h);
				zwlr_virtual_pointer_v1_frame(client.pointer);
			}
		} else if (strcmp(type, "button") == 0) {
			int btn, state;
			if (sscanf(line, "button %d %d", &btn, &state) == 2) {
				zwlr_virtual_pointer_v1_button(client.pointer, 0, btn, state);
				zwlr_virtual_pointer_v1_frame(client.pointer);
			}
		}
		wl_display_flush(client.display);
	}

	zwlr_virtual_pointer_v1_destroy(client.pointer);
	zwlr_virtual_pointer_manager_v1_destroy(client.manager);
	wl_registry_destroy(client.registry);
	wl_display_disconnect(client.display);

	return 0;
}
