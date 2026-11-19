#pragma once

#include <stdbool.h>

enum dithering {
    DITHER_UNKNOWN,
    DITHER_OFF,
    DITHER_ON,
};

const char *dither_name(enum dithering value);

// DITHER_UNKNOWN reports registry values. A known value applies that setting.
int display_dithering(enum dithering desired, bool report_errors);
int watch_displays(int (*reconcile)(bool report_errors));
