#include "stillcolor.h"

#include <signal.h>
#include <stdio.h>
#include <stdlib.h>

int display_dithering(enum dithering desired, bool report_errors)
{
    if (getenv("FAKE_DISPLAY_FAIL") != NULL) {
        if (report_errors) {
            fputs("simulated display failure\n", stderr);
        }
        return 1;
    }

    const char *path = getenv("FAKE_DISPLAY");
    FILE *display = fopen(path, desired == DITHER_UNKNOWN ? "r" : "w");
    if (display == NULL) {
        perror("fake display state");
        return 1;
    }
    if (desired == DITHER_UNKNOWN) {
        char value[16];
        if (fscanf(display, "%15s", value) != 1) {
            fclose(display);
            return 1;
        }
        printf("fake enableDither=%s\n", value);
    } else {
        fprintf(display, "%s\n", dither_name(desired));
    }
    return fclose(display) == EOF;
}

int watch_displays(int (*reconcile)(bool report_errors))
{
    sigset_t signals;
    sigemptyset(&signals);
    sigaddset(&signals, SIGUSR1);
    sigaddset(&signals, SIGTERM);
    sigprocmask(SIG_BLOCK, &signals, NULL);

    reconcile(true);
    puts("ready");
    int signum;
    while (sigwait(&signals, &signum) == 0 && signum == SIGUSR1) {
        reconcile(true);
        puts("reconciled");
    }
    return 0;
}
