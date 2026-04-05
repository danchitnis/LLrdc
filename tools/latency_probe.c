#define _POSIX_C_SOURCE 200809L
#include <errno.h>
#include <fcntl.h>
#include <stdbool.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/mman.h>
#include <sys/time.h>
#include <time.h>
#include <unistd.h>
#include <wayland-client.h>
#include <wayland-cursor.h>
#include "xdg-shell-client-protocol.h"

#define STATE_PATH "/tmp/llrdc-latency-probe.json"

struct probe_app {
    struct wl_display *display;
    struct wl_registry *registry;
    struct wl_compositor *compositor;
    struct wl_shm *shm;
    struct xdg_wm_base *xdg_wm_base;
    struct wl_seat *seat;
    struct wl_pointer *pointer;
    struct wl_keyboard *keyboard;

    struct wl_cursor_theme *cursor_theme;
    struct wl_cursor *cursor;
    struct wl_surface *cursor_surface;

    struct wl_surface *surface;
    struct xdg_surface *xdg_surface;
    struct xdg_toplevel *xdg_toplevel;

    int width, height;
    bool running;
    bool color_white;
    int marker;
    double requested_at_ms;
    double drawn_at_ms;
    double first_move_at_ms;
    int last_mouse_x;
    bool is_moving;

    struct wl_buffer *buffer;
    uint32_t *data;
};

static double get_now_ms(void) {
    struct timespec ts;
    clock_gettime(CLOCK_REALTIME, &ts);
    return (double)ts.tv_sec * 1000.0 + (double)ts.tv_nsec / 1000000.0;
}

static void write_state(struct probe_app *app) {
    FILE *f = fopen(STATE_PATH, "w");
    if (!f) return;
    fprintf(f, "{\"marker\": %d, \"color\": \"%s\", \"requestedAtMs\": %.3f, \"drawnAtMs\": %.3f, \"firstMoveAtMs\": %.3f, \"isMoving\": %s, \"pid\": %d}\n",
            app->marker, app->color_white ? "white" : "black", app->requested_at_ms, app->drawn_at_ms, app->first_move_at_ms, app->is_moving ? "true" : "false", getpid());
    fclose(f);
}

static void fill_buffer(struct probe_app *app) {
    uint32_t color = app->color_white ? 0xFFFFFFFF : 0xFF000000;
    for (int i = 0; i < app->width * app->height; ++i) {
        app->data[i] = color;
    }
}

static void frame_handle_done(void *data, struct wl_callback *callback, uint32_t time) {
    struct probe_app *app = data;
    app->drawn_at_ms = get_now_ms();
    write_state(app);
    wl_callback_destroy(callback);
}

static const struct wl_callback_listener frame_listener = {
    .done = frame_handle_done,
};

static void toggle(struct probe_app *app) {
    fprintf(stderr, "Toggling color: marker=%d, color=%s\n", app->marker + 1, app->color_white ? "black" : "white");
    app->marker++;
    app->requested_at_ms = get_now_ms();
    app->color_white = !app->color_white;
    fill_buffer(app);

    wl_surface_attach(app->surface, app->buffer, 0, 0);
    wl_surface_damage(app->surface, 0, 0, app->width, app->height);

    struct wl_callback *callback = wl_surface_frame(app->surface);
    wl_callback_add_listener(callback, &frame_listener, app);

    wl_surface_commit(app->surface);
}

static void xdg_toplevel_handle_configure(void *data, struct xdg_toplevel *xdg_toplevel,
                                          int32_t width, int32_t height, struct wl_array *states) {
    struct probe_app *app = data;
    if (width > 0 && height > 0) {
        app->width = width;
        app->height = height;
    }
}

static void xdg_toplevel_handle_close(void *data, struct xdg_toplevel *xdg_toplevel) {
    struct probe_app *app = data;
    app->running = false;
}

static const struct xdg_toplevel_listener xdg_toplevel_listener = {
    .configure = xdg_toplevel_handle_configure,
    .close = xdg_toplevel_handle_close,
};

static void xdg_surface_handle_configure(void *data, struct xdg_surface *xdg_surface, uint32_t serial) {
    struct probe_app *app = data;
    xdg_surface_ack_configure(xdg_surface, serial);

    if (!app->buffer) {
        int size = app->width * app->height * 4;
        char name[] = "/tmp/llrdc-shm-XXXXXX";
        int fd = mkstemp(name);
        unlink(name);
        ftruncate(fd, size);
        app->data = mmap(NULL, size, PROT_READ | PROT_WRITE, MAP_SHARED, fd, 0);
        struct wl_shm_pool *pool = wl_shm_create_pool(app->shm, fd, size);
        app->buffer = wl_shm_pool_create_buffer(pool, 0, app->width, app->height, app->width * 4, WL_SHM_FORMAT_XRGB8888);
        wl_shm_pool_destroy(pool);
        close(fd);
    }

    fill_buffer(app);
    wl_surface_attach(app->surface, app->buffer, 0, 0);
    wl_surface_commit(app->surface);
}

static const struct xdg_surface_listener xdg_surface_listener = {
    .configure = xdg_surface_handle_configure,
};

static void pointer_handle_button(void *data, struct wl_pointer *pointer, uint32_t serial,
                                  uint32_t time, uint32_t button, uint32_t state) {
    struct probe_app *app = data;
    fprintf(stderr, "Pointer button: button=%d, state=%d\n", button, state);
    if (state == WL_POINTER_BUTTON_STATE_PRESSED) {
        toggle(app);
    }
}

static void pointer_handle_enter(void *data, struct wl_pointer *wl_pointer, uint32_t serial,
                                 struct wl_surface *surface, wl_fixed_t surface_x, wl_fixed_t surface_y) {
    struct probe_app *app = data;
    fprintf(stderr, "Pointer entered surface, setting cursor...\n");
    if (app->cursor && app->cursor->image_count > 0 && app->cursor_surface) {
        struct wl_cursor_image *image = app->cursor->images[0];
        struct wl_buffer *buffer = wl_cursor_image_get_buffer(image);
        wl_pointer_set_cursor(wl_pointer, serial, app->cursor_surface, image->hotspot_x, image->hotspot_y);
        wl_surface_attach(app->cursor_surface, buffer, 0, 0);
        wl_surface_damage(app->cursor_surface, 0, 0, image->width, image->height);
        wl_surface_commit(app->cursor_surface);
    }
}
static void pointer_handle_leave(void *data, struct wl_pointer *wl_pointer, uint32_t serial, struct wl_surface *surface) {}
static void pointer_handle_motion(void *data, struct wl_pointer *wl_pointer, uint32_t time, wl_fixed_t surface_x, wl_fixed_t surface_y) {
    struct probe_app *app = data;
    int x = surface_x / 256;
    
    if (!app->is_moving) {
        app->is_moving = true;
        app->first_move_at_ms = get_now_ms();
        write_state(app);
    }

    if (app->last_mouse_x >= 0) {
        int mid = app->width / 2;
        if (mid <= 0) mid = 960; // Fallback to 1920/2
        if ((app->last_mouse_x < mid && x >= mid) || (app->last_mouse_x >= mid && x < mid)) {
            fprintf(stderr, "Crossing midpoint: x=%d, last_x=%d, mid=%d\n", x, app->last_mouse_x, mid);
            toggle(app);
            app->is_moving = false; // Reset to catch the next sweep's start
        }
    }
    app->last_mouse_x = x;
}
static void pointer_handle_axis(void *data, struct wl_pointer *wl_pointer, uint32_t time, uint32_t axis, wl_fixed_t value) {}

static void pointer_handle_frame(void *data, struct wl_pointer *wl_pointer) {}
static void pointer_handle_axis_source(void *data, struct wl_pointer *wl_pointer, uint32_t axis_source) {}
static void pointer_handle_axis_stop(void *data, struct wl_pointer *wl_pointer, uint32_t time, uint32_t axis) {}
static void pointer_handle_axis_discrete(void *data, struct wl_pointer *wl_pointer, uint32_t axis, int32_t discrete) {}
static void pointer_handle_axis_value120(void *data, struct wl_pointer *wl_pointer, uint32_t axis, int32_t value120) {}

static const struct wl_pointer_listener pointer_listener = {
    .enter = pointer_handle_enter,
    .leave = pointer_handle_leave,
    .motion = pointer_handle_motion,
    .button = pointer_handle_button,
    .axis = pointer_handle_axis,
    .frame = pointer_handle_frame,
    .axis_source = pointer_handle_axis_source,
    .axis_stop = pointer_handle_axis_stop,
    .axis_discrete = pointer_handle_axis_discrete,
    .axis_value120 = pointer_handle_axis_value120,
};

static void keyboard_handle_key(void *data, struct wl_keyboard *keyboard, uint32_t serial,
                                uint32_t time, uint32_t key, uint32_t state) {
    struct probe_app *app = data;
    fprintf(stderr, "Keyboard key: key=%d, state=%d\n", key, state);
    if (state == WL_KEYBOARD_KEY_STATE_PRESSED) {
        if (key == 1) { // Esc
            app->running = false;
        } else {
            toggle(app);
        }
    }
}

static void keyboard_handle_keymap(void *data, struct wl_keyboard *keyboard, uint32_t format, int fd, uint32_t size) { close(fd); }
static void keyboard_handle_enter(void *data, struct wl_keyboard *keyboard, uint32_t serial, struct wl_surface *surface, struct wl_array *keys) {}
static void keyboard_handle_leave(void *data, struct wl_keyboard *keyboard, uint32_t serial, struct wl_surface *surface) {}
static void keyboard_handle_modifiers(void *data, struct wl_keyboard *keyboard, uint32_t serial, uint32_t mods_depressed, uint32_t mods_latched, uint32_t mods_locked, uint32_t group) {}
static void keyboard_handle_repeat_info(void *data, struct wl_keyboard *keyboard, int32_t rate, int32_t delay) {}

static const struct wl_keyboard_listener keyboard_listener = {
    .keymap = keyboard_handle_keymap,
    .enter = keyboard_handle_enter,
    .leave = keyboard_handle_leave,
    .key = keyboard_handle_key,
    .modifiers = keyboard_handle_modifiers,
    .repeat_info = keyboard_handle_repeat_info,
};

static void seat_handle_capabilities(void *data, struct wl_seat *seat, uint32_t capabilities) {
    struct probe_app *app = data;
    if (capabilities & WL_SEAT_CAPABILITY_POINTER) {
        app->pointer = wl_seat_get_pointer(seat);
        wl_pointer_add_listener(app->pointer, &pointer_listener, app);
    }
    if (capabilities & WL_SEAT_CAPABILITY_KEYBOARD) {
        app->keyboard = wl_seat_get_keyboard(seat);
        wl_keyboard_add_listener(app->keyboard, &keyboard_listener, app);
    }
}

static void seat_handle_name(void *data, struct wl_seat *seat, const char *name) {}

static const struct wl_seat_listener seat_listener = {
    .capabilities = seat_handle_capabilities,
    .name = seat_handle_name,
};

static void xdg_wm_base_handle_ping(void *data, struct xdg_wm_base *xdg_wm_base, uint32_t serial) {
    xdg_wm_base_pong(xdg_wm_base, serial);
}

static const struct xdg_wm_base_listener xdg_wm_base_listener = {
    .ping = xdg_wm_base_handle_ping,
};

static void registry_handle_global(void *data, struct wl_registry *registry,
                                   uint32_t name, const char *interface, uint32_t version) {
    struct probe_app *app = data;
    if (strcmp(interface, wl_compositor_interface.name) == 0) {
        app->compositor = wl_registry_bind(registry, name, &wl_compositor_interface, 4);
    } else if (strcmp(interface, wl_shm_interface.name) == 0) {
        app->shm = wl_registry_bind(registry, name, &wl_shm_interface, 1);
        app->cursor_theme = wl_cursor_theme_load(NULL, 24, app->shm);
        if (app->cursor_theme) {
            app->cursor = wl_cursor_theme_get_cursor(app->cursor_theme, "left_ptr");
            if (!app->cursor) fprintf(stderr, "Failed to get left_ptr cursor\n");
        } else {
            fprintf(stderr, "Failed to load cursor theme\n");
        }
    } else if (strcmp(interface, xdg_wm_base_interface.name) == 0) {
        app->xdg_wm_base = wl_registry_bind(registry, name, &xdg_wm_base_interface, 1);
        xdg_wm_base_add_listener(app->xdg_wm_base, &xdg_wm_base_listener, app);
    } else if (strcmp(interface, wl_seat_interface.name) == 0) {
        app->seat = wl_registry_bind(registry, name, &wl_seat_interface, 7);
        wl_seat_add_listener(app->seat, &seat_listener, app);
    }
}

static const struct wl_registry_listener registry_listener = {
    .global = registry_handle_global,
    .global_remove = (void *)xdg_toplevel_handle_close,
};

int main(void) {
    struct probe_app app = {
        .width = 1920,
        .height = 1080,
        .running = true,
        .color_white = false,
    };

    app.display = wl_display_connect(NULL);
    if (!app.display) return 1;

    app.registry = wl_display_get_registry(app.display);
    wl_registry_add_listener(app.registry, &registry_listener, &app);
    wl_display_roundtrip(app.display);

    if (app.compositor) {
        app.cursor_surface = wl_compositor_create_surface(app.compositor);
    }

    app.surface = wl_compositor_create_surface(app.compositor);
    app.xdg_surface = xdg_wm_base_get_xdg_surface(app.xdg_wm_base, app.surface);
    xdg_surface_add_listener(app.xdg_surface, &xdg_surface_listener, &app);
    app.xdg_toplevel = xdg_surface_get_toplevel(app.xdg_surface);
    xdg_toplevel_add_listener(app.xdg_toplevel, &xdg_toplevel_listener, &app);
    xdg_toplevel_set_title(app.xdg_toplevel, "LLrdc Latency Probe");
    xdg_toplevel_set_fullscreen(app.xdg_toplevel, NULL);
    wl_surface_commit(app.surface);

    write_state(&app);

    while (app.running && wl_display_dispatch(app.display) != -1);

    return 0;
}
