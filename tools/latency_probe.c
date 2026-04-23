#define _POSIX_C_SOURCE 200809L
#include <errno.h>
#include <fcntl.h>
#include <inttypes.h>
#include <stdbool.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/mman.h>
#include <sys/time.h>
#include <time.h>
#include <unistd.h>
#include <poll.h>
#include <wayland-client.h>
#include "xdg-shell-client-protocol.h"

#define STATE_PATH "/tmp/llrdc-latency-probe.json"
#define MARKER_BITS 16
#define MARKER_REF_DARK_X 40
#define MARKER_REF_BRIGHT_X 72
#define MARKER_START_X 120
#define MARKER_START_Y 40
#define MARKER_CELL_SIZE 20
#define MARKER_CELL_GAP 10

struct probe_app {
    struct wl_display *display;
    struct wl_registry *registry;
    struct wl_compositor *compositor;
    struct wl_shm *shm;
    struct xdg_wm_base *xdg_wm_base;
    struct wl_seat *seat;
    struct wl_pointer *pointer;
    struct wl_surface *surface;
    struct xdg_surface *xdg_surface;
    struct xdg_toplevel *xdg_toplevel;

    int width, height;
    bool running;
    bool is_white;
    int marker;
    int64_t requested_at_ms;
    int64_t drawn_at_ms;

    int mouse_x, mouse_y;
    struct wl_buffer *buffer;
    uint32_t *data;
    int64_t last_trigger_ms;
};

static int64_t get_now_ms(void) {
    struct timespec ts;
    clock_gettime(CLOCK_MONOTONIC, &ts);
    return ((int64_t)ts.tv_sec * 1000LL) + ((int64_t)ts.tv_nsec / 1000000LL);
}

static void write_state(struct probe_app *app) {
    FILE *f = fopen(STATE_PATH, "w");
    if (!f) return;
    fprintf(f, "{\"marker\": %d, \"color\": \"%s\", \"requestedAtMs\": %" PRId64 ", \"drawnAtMs\": %" PRId64 "}\n",
            app->marker, app->is_white ? "white" : "black", app->requested_at_ms, app->drawn_at_ms);
    fclose(f);
}

static void draw_rect(struct probe_app *app, int x, int y, int w, int h, uint32_t color) {
    for (int iy = 0; iy < h; iy++) {
        for (int ix = 0; ix < w; ix++) {
            int px = x + ix; int py = y + iy;
            if (px >= 0 && px < app->width && py >= 0 && py < app->height) app->data[py * app->width + px] = color;
        }
    }
}

static void fill_buffer(struct probe_app *app) {
    uint32_t bg_color = app->is_white ? 0xFFFFFFFF : 0xFF000000;
    uint32_t marker_color = app->is_white ? 0xFFCCCCCC : 0xFF333333; 
    uint32_t cursor_color = app->is_white ? 0xFFFFFFFF : 0xFFFF0000; // White on White (High Brightness) or Red on Black
    uint32_t dark_code_color = 0xFF202020;
    uint32_t bright_code_color = 0xFFE0E0E0;

    for (int i = 0; i < app->width * app->height; ++i) app->data[i] = bg_color;

    // Static markers
    draw_rect(app, 0, 0, 20, 20, marker_color);
    draw_rect(app, app->width - 20, 0, 20, 20, marker_color);
    draw_rect(app, 0, app->height - 20, 20, 20, marker_color);
    draw_rect(app, app->width - 20, app->height - 20, 20, 20, marker_color);

    // Reference cells plus a unary marker stripe survive VP8 compression
    // more reliably than single-pixel binary sampling.
    draw_rect(app, MARKER_REF_DARK_X, MARKER_START_Y, MARKER_CELL_SIZE, MARKER_CELL_SIZE, dark_code_color);
    draw_rect(app, MARKER_REF_BRIGHT_X, MARKER_START_Y, MARKER_CELL_SIZE, MARKER_CELL_SIZE, bright_code_color);
    for (int bit = 0; bit < MARKER_BITS; bit++) {
        int x = MARKER_START_X + bit * (MARKER_CELL_SIZE + MARKER_CELL_GAP);
        uint32_t color = (bit < app->marker) ? dark_code_color : bright_code_color;
        draw_rect(app, x, MARKER_START_Y, MARKER_CELL_SIZE, MARKER_CELL_SIZE, color);
    }

    // Crosshair (Only draw if NOT at center to keep center pixel pure)
    if (!app->is_white) {
        draw_rect(app, app->width/2 - 30, app->height/2, 60, 1, marker_color);
        draw_rect(app, app->width/2, app->height/2 - 30, 1, 60, marker_color);
    }

    // The Mouse Square
    draw_rect(app, app->mouse_x - 32, app->mouse_y - 32, 64, 64, cursor_color);
}

static void frame_handle_done(void *data, struct wl_callback *callback, uint32_t time) {
    struct probe_app *app = data;
    app->drawn_at_ms = get_now_ms();
    write_state(app);
    wl_callback_destroy(callback);
}
static const struct wl_callback_listener frame_listener = { .done = frame_handle_done };

static void commit_frame(struct probe_app *app) {
    fill_buffer(app);
    wl_surface_attach(app->surface, app->buffer, 0, 0);
    wl_surface_damage(app->surface, 0, 0, app->width, app->height);
    struct wl_callback *callback = wl_surface_frame(app->surface);
    wl_callback_add_listener(callback, &frame_listener, app);
    wl_surface_commit(app->surface);
}

static void trigger(struct probe_app *app) {
    int64_t now = get_now_ms();
    if (now - app->last_trigger_ms < 150) return;
    app->marker++;
    app->requested_at_ms = now;
    app->last_trigger_ms = now;
    commit_frame(app);
}

static void xdg_surface_handle_configure(void *data, struct xdg_surface *xdg_surface, uint32_t serial) {
    struct probe_app *app = data;
    xdg_surface_ack_configure(xdg_surface, serial);
    if (app->buffer) { wl_buffer_destroy(app->buffer); munmap(app->data, app->width * app->height * 4); }
    int size = app->width * app->height * 4;
    char name[] = "/tmp/llrdc-shm-XXXXXX";
    int fd = mkstemp(name); unlink(name); ftruncate(fd, size);
    app->data = mmap(NULL, size, PROT_READ | PROT_WRITE, MAP_SHARED, fd, 0);
    struct wl_shm_pool *pool = wl_shm_create_pool(app->shm, fd, size);
    app->buffer = wl_shm_pool_create_buffer(pool, 0, app->width, app->height, app->width * 4, WL_SHM_FORMAT_XRGB8888);
    wl_shm_pool_destroy(pool); close(fd);
    commit_frame(app);
}
static const struct xdg_surface_listener xdg_surface_listener = { .configure = xdg_surface_handle_configure };

static void pointer_handle_motion(void *data, struct wl_pointer *wl_pointer, uint32_t time, wl_fixed_t surface_x, wl_fixed_t surface_y) {
    struct probe_app *app = data;
    app->mouse_x = wl_fixed_to_int(surface_x);
    app->mouse_y = wl_fixed_to_int(surface_y);

    bool near_center = (abs(app->mouse_x - app->width/2) < 50 && abs(app->mouse_y - app->height/2) < 50);
    if (near_center != app->is_white) {
        app->is_white = near_center;
        if (near_center) {
            trigger(app);
        } else {
            commit_frame(app);
        }
    } else {
        commit_frame(app); 
    }
}
static void pointer_handle_enter(void *data, struct wl_pointer *p, uint32_t s, struct wl_surface *sur, wl_fixed_t x, wl_fixed_t y) {}
static void pointer_handle_leave(void *data, struct wl_pointer *p, uint32_t s, struct wl_surface *sur) {}
static void pointer_handle_button(void *data, struct wl_pointer *p, uint32_t s, uint32_t t, uint32_t b, uint32_t st) { if (st == 1) trigger((struct probe_app *)data); }
static void pointer_handle_axis(void *data, struct wl_pointer *p, uint32_t t, uint32_t a, wl_fixed_t v) {}
static void pointer_handle_frame(void *data, struct wl_pointer *p) {}
static void pointer_handle_axis_source(void *data, struct wl_pointer *p, uint32_t s) {}
static void pointer_handle_axis_stop(void *data, struct wl_pointer *p, uint32_t t, uint32_t a) {}
static void pointer_handle_axis_discrete(void *data, struct wl_pointer *p, uint32_t a, int32_t d) {}
static void pointer_handle_axis_value120(void *data, struct wl_pointer *p, uint32_t a, int32_t v) {}
static const struct wl_pointer_listener pointer_listener = { .enter = pointer_handle_enter, .leave = pointer_handle_leave, .motion = pointer_handle_motion, .button = pointer_handle_button, .axis = pointer_handle_axis, .frame = pointer_handle_frame, .axis_source = pointer_handle_axis_source, .axis_stop = pointer_handle_axis_stop, .axis_discrete = pointer_handle_axis_discrete, .axis_value120 = pointer_handle_axis_value120 };

static void seat_handle_capabilities(void *data, struct wl_seat *seat, uint32_t caps) {
    struct probe_app *app = data;
    if (caps & WL_SEAT_CAPABILITY_POINTER) { app->pointer = wl_seat_get_pointer(seat); wl_pointer_add_listener(app->pointer, &pointer_listener, app); }
}
static void seat_handle_name(void *data, struct wl_seat *seat, const char *name) {}
static const struct wl_seat_listener seat_listener = { .capabilities = seat_handle_capabilities, .name = seat_handle_name };

static void xdg_wm_base_handle_ping(void *data, struct xdg_wm_base *xdg_wm_base, uint32_t serial) { xdg_wm_base_pong(xdg_wm_base, serial); }
static const struct xdg_wm_base_listener xdg_wm_base_listener = { .ping = xdg_wm_base_handle_ping };

static void registry_handle_global(void *data, struct wl_registry *registry, uint32_t name, const char *interface, uint32_t version) {
    struct probe_app *app = data;
    if (strcmp(interface, wl_compositor_interface.name) == 0) app->compositor = wl_registry_bind(registry, name, &wl_compositor_interface, 4);
    else if (strcmp(interface, wl_shm_interface.name) == 0) app->shm = wl_registry_bind(registry, name, &wl_shm_interface, 1);
    else if (strcmp(interface, xdg_wm_base_interface.name) == 0) { app->xdg_wm_base = wl_registry_bind(registry, name, &xdg_wm_base_interface, 1); xdg_wm_base_add_listener(app->xdg_wm_base, &xdg_wm_base_listener, app); }
    else if (strcmp(interface, wl_seat_interface.name) == 0) { app->seat = wl_registry_bind(registry, name, &wl_seat_interface, 7); wl_seat_add_listener(app->seat, &seat_listener, app); }
}
static void registry_handle_global_remove(void *data, struct wl_registry *registry, uint32_t name) {}
static const struct wl_registry_listener registry_listener = { .global = registry_handle_global, .global_remove = registry_handle_global_remove };

static void xdg_toplevel_handle_configure(void *data, struct xdg_toplevel *xdg_toplevel, int32_t width, int32_t height, struct wl_array *states) {
    struct probe_app *app = data;
    if (width > 0 && height > 0) { app->width = width; app->height = height; }
}
static void xdg_toplevel_handle_close(void *data, struct xdg_toplevel *xdg_toplevel) { ((struct probe_app *)data)->running = false; }
static const struct xdg_toplevel_listener xdg_toplevel_listener = { .configure = xdg_toplevel_handle_configure, .close = xdg_toplevel_handle_close };

int main(void) {
    struct probe_app app = { .width = 1280, .height = 720, .running = true, .is_white = false, .mouse_x = 0, .mouse_y = 0 };
    app.display = wl_display_connect(NULL);
    if (!app.display) return 1;
    app.registry = wl_display_get_registry(app.display);
    wl_registry_add_listener(app.registry, &registry_listener, &app);
    wl_display_roundtrip(app.display);
    app.surface = wl_compositor_create_surface(app.compositor);
    app.xdg_surface = xdg_wm_base_get_xdg_surface(app.xdg_wm_base, app.surface);
    xdg_surface_add_listener(app.xdg_surface, &xdg_surface_listener, &app);
    app.xdg_toplevel = xdg_surface_get_toplevel(app.xdg_surface);
    xdg_toplevel_add_listener(app.xdg_toplevel, &xdg_toplevel_listener, &app);
    xdg_toplevel_set_title(app.xdg_toplevel, "LLrdc Latency Probe");
    xdg_toplevel_set_fullscreen(app.xdg_toplevel, NULL);
    wl_surface_commit(app.surface);
    write_state(&app);
    struct pollfd pfd = { .fd = wl_display_get_fd(app.display), .events = POLLIN };
    while (app.running) {
        while (wl_display_prepare_read(app.display) != 0) wl_display_dispatch_pending(app.display);
        wl_display_flush(app.display);
        if (poll(&pfd, 1, 1000) > 0) { wl_display_read_events(app.display); wl_display_dispatch_pending(app.display); }
        else wl_display_cancel_read(app.display);
    }
    return 0;
}
