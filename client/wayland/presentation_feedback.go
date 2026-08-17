//go:build linux && native && cgo

package wayland

/*
#cgo pkg-config: sdl2 wayland-client
#include <SDL2/SDL.h>
#include <SDL2/SDL_syswm.h>
#include <wayland-client.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>
#include "/usr/local/include/llrdc-presentation-time-client-protocol.h"
#include "/usr/local/include/llrdc-presentation-time-client-protocol.c"

typedef struct llrdc_feedback_result {
	uint64_t token;
	int64_t presented_ns;
	int discarded;
} llrdc_feedback_result;

typedef struct llrdc_feedback_request {
	struct llrdc_presentation_tracker *tracker;
	uint64_t token;
} llrdc_feedback_request;

typedef struct llrdc_presentation_tracker {
	struct wl_display *display;
	struct wl_registry *registry;
	struct wp_presentation *presentation;
	struct wl_surface *surface;
	uint32_t clock_id;
	uint64_t next_token;
	llrdc_feedback_result results[128];
	int result_count;
} llrdc_presentation_tracker;

static void llrdc_presentation_clock_id_event(void *data, struct wp_presentation *obj, uint32_t clk_id) {
	(void)obj;
	((llrdc_presentation_tracker *)data)->clock_id = clk_id;
}
static const struct wp_presentation_listener llrdc_presentation_listener = {
	.clock_id = llrdc_presentation_clock_id_event,
};

static void llrdc_push_result(llrdc_feedback_request *request, int64_t ns, int discarded) {
	llrdc_presentation_tracker *tracker = request->tracker;
	if (tracker->result_count < (int)(sizeof(tracker->results) / sizeof(tracker->results[0]))) {
		tracker->results[tracker->result_count++] = (llrdc_feedback_result){
			.token = request->token,
			.presented_ns = ns,
			.discarded = discarded,
		};
	}
	free(request);
}

static void llrdc_feedback_sync_output(void *data, struct wp_presentation_feedback *feedback, struct wl_output *output) {
	(void)data; (void)feedback; (void)output;
}
static void llrdc_feedback_presented(void *data, struct wp_presentation_feedback *feedback,
		uint32_t sec_hi, uint32_t sec_lo, uint32_t nsec, uint32_t refresh,
		uint32_t seq_hi, uint32_t seq_lo, uint32_t flags) {
	(void)feedback; (void)refresh; (void)seq_hi; (void)seq_lo; (void)flags;
	llrdc_feedback_request *request = data;
	uint64_t seconds = ((uint64_t)sec_hi << 32) | sec_lo;
	llrdc_push_result(request, (int64_t)(seconds * 1000000000ULL + nsec), 0);
}
static void llrdc_feedback_discarded(void *data, struct wp_presentation_feedback *feedback) {
	(void)feedback;
	llrdc_push_result((llrdc_feedback_request *)data, 0, 1);
}
static const struct wp_presentation_feedback_listener llrdc_feedback_listener = {
	.sync_output = llrdc_feedback_sync_output,
	.presented = llrdc_feedback_presented,
	.discarded = llrdc_feedback_discarded,
};

static void llrdc_registry_global(void *data, struct wl_registry *registry, uint32_t name,
		const char *interface, uint32_t version) {
	llrdc_presentation_tracker *tracker = data;
	if (strcmp(interface, wp_presentation_interface.name) == 0 && !tracker->presentation) {
		uint32_t bind_version = version < 1 ? version : 1;
		tracker->presentation = wl_registry_bind(registry, name, &wp_presentation_interface, bind_version);
		wp_presentation_add_listener(tracker->presentation, &llrdc_presentation_listener, tracker);
	}
}
static void llrdc_registry_remove(void *data, struct wl_registry *registry, uint32_t name) {
	(void)data; (void)registry; (void)name;
}
static const struct wl_registry_listener llrdc_registry_listener = {
	.global = llrdc_registry_global,
	.global_remove = llrdc_registry_remove,
};

static llrdc_presentation_tracker *llrdc_presentation_create(SDL_Window *window, int *error_code) {
	SDL_SysWMinfo info;
	SDL_VERSION(&info.version);
	if (!SDL_GetWindowWMInfo(window, &info)) {
		if (error_code) *error_code = 1;
		return NULL;
	}
	if (info.subsystem != SDL_SYSWM_WAYLAND) {
		if (error_code) *error_code = 2;
		return NULL;
	}
	if (!info.info.wl.display || !info.info.wl.surface) {
		if (error_code) *error_code = 3;
		return NULL;
	}
	llrdc_presentation_tracker *tracker = calloc(1, sizeof(*tracker));
	if (!tracker) return NULL;
	tracker->display = info.info.wl.display;
	tracker->surface = info.info.wl.surface;
	tracker->registry = wl_display_get_registry(tracker->display);
	if (!tracker->registry || wl_registry_add_listener(tracker->registry, &llrdc_registry_listener, tracker) < 0) {
		if (error_code) *error_code = 4;
		free(tracker); return NULL;
	}
	if (wl_display_roundtrip(tracker->display) < 0 || wl_display_roundtrip(tracker->display) < 0) {
		if (error_code) *error_code = 5;
		free(tracker); return NULL;
	}
	if (!tracker->presentation) {
		if (error_code) *error_code = 6;
		free(tracker); return NULL;
	}
	if (tracker->clock_id == 0) {
		if (error_code) *error_code = 7;
		free(tracker); return NULL;
	}
	return tracker;
}

static void llrdc_presentation_destroy(llrdc_presentation_tracker *tracker) {
	if (!tracker) return;
	if (tracker->presentation) wp_presentation_destroy(tracker->presentation);
	if (tracker->registry) wl_registry_destroy(tracker->registry);
	free(tracker);
}

static uint64_t llrdc_presentation_request(llrdc_presentation_tracker *tracker) {
	if (!tracker || !tracker->presentation || !tracker->surface) return 0;
	llrdc_feedback_request *request = calloc(1, sizeof(*request));
	if (!request) return 0;
	request->tracker = tracker;
	request->token = ++tracker->next_token;
	struct wp_presentation_feedback *feedback = wp_presentation_feedback(tracker->presentation, tracker->surface);
	if (!feedback || wp_presentation_feedback_add_listener(feedback, &llrdc_feedback_listener, request) < 0) {
		free(request); return 0;
	}
	return request->token;
}

static int llrdc_presentation_poll(llrdc_presentation_tracker *tracker, uint64_t *token, int64_t *presented_ns, int *discarded) {
	if (!tracker || tracker->result_count <= 0) return 0;
	llrdc_feedback_result result = tracker->results[0];
	memmove(&tracker->results[0], &tracker->results[1], (tracker->result_count - 1) * sizeof(tracker->results[0]));
	tracker->result_count--;
	*token = result.token;
	*presented_ns = result.presented_ns;
	*discarded = result.discarded;
	return 1;
}

static void llrdc_presentation_dispatch(llrdc_presentation_tracker *tracker) {
	if (tracker && tracker->display) {
		// SDL pumps the Wayland socket, but does not guarantee that callbacks
		// registered outside SDL are dispatched before returning to the Go loop.
		// Drain pending presentation-time events explicitly before Poll().
		(void)wl_display_dispatch_pending(tracker->display);
	}
}

static int llrdc_presentation_error(llrdc_presentation_tracker *tracker) {
	return (tracker && tracker->display) ? wl_display_get_error(tracker->display) : 0;
}

static uint32_t llrdc_presentation_clock_id_value(llrdc_presentation_tracker *tracker) {
	return tracker ? tracker->clock_id : 0;
}
*/
import "C"

import (
	"fmt"
	"unsafe"
)

type presentationFeedback struct {
	ptr *C.llrdc_presentation_tracker
}

type presentationResult struct {
	token       uint64
	presentedNs int64
	discarded   bool
}

func newPresentationFeedback(window unsafe.Pointer) (*presentationFeedback, error) {
	var errorCode C.int
	tracker := C.llrdc_presentation_create((*C.SDL_Window)(window), &errorCode)
	if tracker == nil {
		return nil, fmt.Errorf("Wayland presentation-time protocol unavailable (bridge error %d)", int(errorCode))
	}
	return &presentationFeedback{ptr: tracker}, nil
}

func (p *presentationFeedback) Close() {
	if p == nil || p.ptr == nil {
		return
	}
	C.llrdc_presentation_destroy(p.ptr)
	p.ptr = nil
}

func (p *presentationFeedback) Request() uint64 {
	if p == nil || p.ptr == nil {
		return 0
	}
	return uint64(C.llrdc_presentation_request(p.ptr))
}

func (p *presentationFeedback) ClockID() uint32 {
	if p == nil || p.ptr == nil {
		return 0
	}
	return uint32(C.llrdc_presentation_clock_id_value(p.ptr))
}

func (p *presentationFeedback) Poll() []presentationResult {
	if p == nil || p.ptr == nil {
		return nil
	}
	results := make([]presentationResult, 0, 4)
	for {
		var token C.uint64_t
		var presentedNs C.int64_t
		var discarded C.int
		if C.llrdc_presentation_poll(p.ptr, &token, &presentedNs, &discarded) == 0 {
			break
		}
		results = append(results, presentationResult{
			token:       uint64(token),
			presentedNs: int64(presentedNs),
			discarded:   discarded != 0,
		})
	}
	return results
}

func (p *presentationFeedback) Dispatch() {
	if p == nil || p.ptr == nil {
		return
	}
	C.llrdc_presentation_dispatch(p.ptr)
}

func (p *presentationFeedback) Error() int {
	if p == nil || p.ptr == nil {
		return 0
	}
	return int(C.llrdc_presentation_error(p.ptr))
}
