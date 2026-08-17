#define _GNU_SOURCE

#include <dlfcn.h>
#include <errno.h>
#include <time.h>

typedef int (*clock_gettime_fn)(clockid_t, struct timespec *);

int clock_gettime(clockid_t clock_id, struct timespec *tp) {
    // Weston chooses CLOCK_MONOTONIC_RAW first, then CLOCK_MONOTONIC_COARSE.
    // Make the nested regression compositor select CLOCK_MONOTONIC instead;
    // this is a failure injection for clock selection, not timestamp fakery.
    if (clock_id == CLOCK_MONOTONIC_RAW || clock_id == CLOCK_MONOTONIC_COARSE) {
        errno = EINVAL;
        return -1;
    }
    static clock_gettime_fn real_clock_gettime;
    if (!real_clock_gettime) {
        real_clock_gettime = (clock_gettime_fn)dlsym(RTLD_NEXT, "clock_gettime");
    }
    return real_clock_gettime ? real_clock_gettime(clock_id, tp) : -1;
}
