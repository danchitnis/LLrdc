#define _POSIX_C_SOURCE 200809L
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>
#include <sys/mman.h>
#include <time.h>
#include <wayland-client.h>
#include <xkbcommon/xkbcommon.h>
#include "wlr-virtual-pointer-unstable-v1-client-protocol.h"
#include "virtual-keyboard-unstable-v1-client-protocol.h"

struct input_client {
	struct wl_display *display;
	struct wl_registry *registry;
	struct zwlr_virtual_pointer_manager_v1 *pointer_manager;
	struct zwlr_virtual_pointer_v1 *pointer;
	struct zwp_virtual_keyboard_manager_v1 *keyboard_manager;
	struct zwp_virtual_keyboard_v1 *keyboard;
	struct wl_seat *seat;
	struct xkb_keymap *keymap;
	struct xkb_state *xkb_state;
};

static uint32_t last_time = 0;

static uint32_t get_time_ms(void) {
	struct timespec ts;
	clock_gettime(CLOCK_MONOTONIC, &ts);
	uint32_t t = (uint32_t)(ts.tv_sec * 1000 + ts.tv_nsec / 1000000);
	if (t <= last_time) {
		t = last_time + 1;
	}
	last_time = t;
	return t;
}

static void registry_handle_global(void *data, struct wl_registry *registry,
		uint32_t name, const char *interface, uint32_t version) {
	struct input_client *client = data;
	if (strcmp(interface, zwlr_virtual_pointer_manager_v1_interface.name) == 0) {
		client->pointer_manager = wl_registry_bind(registry, name,
			&zwlr_virtual_pointer_manager_v1_interface, 1);
	} else if (strcmp(interface, zwp_virtual_keyboard_manager_v1_interface.name) == 0) {
		client->keyboard_manager = wl_registry_bind(registry, name,
			&zwp_virtual_keyboard_manager_v1_interface, 1);
	} else if (strcmp(interface, wl_seat_interface.name) == 0) {
		client->seat = wl_registry_bind(registry, name, &wl_seat_interface, 1);
	}
}

static void registry_handle_global_remove(void *data, struct wl_registry *registry,
		uint32_t name) {
}

static const struct wl_registry_listener registry_listener = {
	.global = registry_handle_global,
	.global_remove = registry_handle_global_remove,
};

static int create_anonymous_file(size_t size) {
	char template[] = "/tmp/llrdc-keymap-XXXXXX";
	int fd = mkstemp(template);
	if (fd < 0) return -1;
	unlink(template);
	if (ftruncate(fd, size) < 0) {
		close(fd);
		return -1;
	}
	return fd;
}

static void setup_keyboard(struct input_client *client) {
	client->keyboard = zwp_virtual_keyboard_manager_v1_create_virtual_keyboard(
		client->keyboard_manager, client->seat);

	struct xkb_context *context = xkb_context_new(XKB_CONTEXT_NO_FLAGS);
	struct xkb_rule_names names = { .layout = "us" };
	client->keymap = xkb_keymap_new_from_names(context, &names, XKB_KEYMAP_COMPILE_NO_FLAGS);
	client->xkb_state = xkb_state_new(client->keymap);
	char *keymap_str = xkb_keymap_get_as_string(client->keymap, XKB_KEYMAP_FORMAT_TEXT_V1);
	size_t keymap_size = strlen(keymap_str) + 1;

	int fd = create_anonymous_file(keymap_size);
	void *ptr = mmap(NULL, keymap_size, PROT_READ | PROT_WRITE, MAP_SHARED, fd, 0);
	strcpy(ptr, keymap_str);
	munmap(ptr, keymap_size);

	zwp_virtual_keyboard_v1_keymap(client->keyboard, WL_KEYBOARD_KEYMAP_FORMAT_XKB_V1, fd, keymap_size);
	close(fd);
	free(keymap_str);
	xkb_context_unref(context);
}

int main(int argc, char *argv[]) {
	setvbuf(stdin, NULL, _IONBF, 0);
	struct input_client client = {0};
	client.display = wl_display_connect(NULL);
	if (!client.display) {
		fprintf(stderr, "Failed to connect to wayland display\n");
		return 1;
	}

	client.registry = wl_display_get_registry(client.display);
	wl_registry_add_listener(client.registry, &registry_listener, &client);
	wl_display_roundtrip(client.display);

	if (!client.pointer_manager || !client.keyboard_manager) {
		fprintf(stderr, "Compositor missing required protocols\n");
		return 1;
	}

	client.pointer = zwlr_virtual_pointer_manager_v1_create_virtual_pointer(client.pointer_manager, NULL);
	setup_keyboard(&client);
	printf("READY\n");
	fflush(stdout);

	char line[256];
	while (fgets(line, sizeof(line), stdin)) {
		char type[16];
		if (sscanf(line, "%s", type) != 1) continue;

		uint32_t t = get_time_ms();
		if (strcmp(type, "move") == 0) {
			int x, y, w, h;
			if (sscanf(line, "move %d %d %d %d", &x, &y, &w, &h) == 4) {
				zwlr_virtual_pointer_v1_motion_absolute(client.pointer, t, x, y, w, h);
				zwlr_virtual_pointer_v1_frame(client.pointer);
			}
		} else if (strcmp(type, "button") == 0) {
			int btn, state;
			if (sscanf(line, "button %d %d", &btn, &state) == 2) {
				zwlr_virtual_pointer_v1_button(client.pointer, t, btn, state);
				zwlr_virtual_pointer_v1_frame(client.pointer);
			}
		} else if (strcmp(type, "key") == 0) {
			int key, state;
			if (sscanf(line, "key %d %d", &key, &state) == 2) {
				zwp_virtual_keyboard_v1_key(client.keyboard, t, key, state);
				
				xkb_state_update_key(client.xkb_state, key + 8, state ? XKB_KEY_DOWN : XKB_KEY_UP);
				uint32_t depressed = xkb_state_serialize_mods(client.xkb_state, XKB_STATE_MODS_DEPRESSED);
				uint32_t latched = xkb_state_serialize_mods(client.xkb_state, XKB_STATE_MODS_LATCHED);
				uint32_t locked = xkb_state_serialize_mods(client.xkb_state, XKB_STATE_MODS_LOCKED);
				uint32_t group = xkb_state_serialize_layout(client.xkb_state, XKB_STATE_LAYOUT_EFFECTIVE);
				zwp_virtual_keyboard_v1_modifiers(client.keyboard, depressed, latched, locked, group);
				wl_display_roundtrip(client.display);
			}
		} else if (strcmp(type, "axis") == 0) {
			int axis;
			float value;
			if (sscanf(line, "axis %d %f", &axis, &value) == 2) {
				zwlr_virtual_pointer_v1_axis(client.pointer, t, axis, wl_fixed_from_double(value));
				zwlr_virtual_pointer_v1_frame(client.pointer);
			}
		} else if (strcmp(type, "ping") == 0) {
			// Trigger a tiny 1-pixel jitter to force a damage update.
			// This is invisible to the user but forces a frame out during damage tracking.
			zwlr_virtual_pointer_v1_motion(client.pointer, t, wl_fixed_from_double(1.0), wl_fixed_from_double(1.0));
			zwlr_virtual_pointer_v1_frame(client.pointer);
			
			zwlr_virtual_pointer_v1_motion(client.pointer, t + 1, wl_fixed_from_double(-1.0), wl_fixed_from_double(-1.0));
			zwlr_virtual_pointer_v1_frame(client.pointer);
		}
		wl_display_flush(client.display);
	}

	zwp_virtual_keyboard_v1_destroy(client.keyboard);
	zwp_virtual_keyboard_manager_v1_destroy(client.keyboard_manager);
	zwlr_virtual_pointer_v1_destroy(client.pointer);
	zwlr_virtual_pointer_manager_v1_destroy(client.pointer_manager);
	wl_registry_destroy(client.registry);
	wl_display_disconnect(client.display);

	return 0;
}
