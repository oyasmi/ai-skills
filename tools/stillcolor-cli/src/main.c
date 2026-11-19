#include "stillcolor.h"

#include <errno.h>
#include <fcntl.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/file.h>
#include <sys/stat.h>
#include <unistd.h>

enum command {
    STATUS,
    APPLY,
    SET,
};

static void usage(FILE *out)
{
    fputs("Usage: stillcolor-cli status\n"
          "       stillcolor-cli on|off\n"
          "       stillcolor-cli apply\n"
          "       stillcolor-cli watch\n\n"
          "  status     Show the saved policy and each framebuffer's enableDither value.\n"
          "  on | off   Save and immediately apply a temporal dithering policy.\n"
          "  apply      Apply the saved policy once (default: off).\n"
          "  watch      Apply the policy at startup and after display events or wake.\n", out);
}

const char *dither_name(enum dithering value)
{
    switch (value) {
    case DITHER_ON: return "on";
    case DITHER_OFF: return "off";
    default: return "unknown";
    }
}

static enum dithering parse_dithering(const char *value)
{
    if (strcmp(value, "on") == 0) {
        return DITHER_ON;
    }
    if (strcmp(value, "off") == 0) {
        return DITHER_OFF;
    }
    return DITHER_UNKNOWN;
}

static int open_settings(void)
{
    const char *home = getenv("HOME");
    if (home == NULL || home[0] != '/') {
        errno = EINVAL;
        return -1;
    }

    int dir = open(home, O_RDONLY | O_DIRECTORY | O_CLOEXEC);
    const char *components[] = {"Library", "Application Support", "stillcolor-cli"};
    for (size_t index = 0; dir >= 0 && index < sizeof(components) / sizeof(*components); index++) {
        const char *component = components[index];
        int next = -1;
        if (mkdirat(dir, component, 0700) == 0 || errno == EEXIST) {
            next = openat(dir, component, O_RDONLY | O_DIRECTORY | O_CLOEXEC);
        }
        int saved_errno = errno;
        close(dir);
        errno = saved_errno;
        dir = next;
    }
    return dir;
}

static enum dithering load_policy(int dir, enum dithering default_policy)
{
    int fd = openat(dir, "dithering", O_RDONLY | O_CLOEXEC);
    if (fd == -1) {
        return errno == ENOENT ? default_policy : DITHER_UNKNOWN;
    }

    // An extra byte detects trailing data beyond the longest valid record.
    char contents[sizeof("off\n")];
    ssize_t count = read(fd, contents, sizeof(contents));
    int saved_errno = errno;
    close(fd);
    errno = saved_errno;
    if (count == 3 && memcmp(contents, "on\n", 3) == 0) {
        return DITHER_ON;
    }
    if (count == 4 && memcmp(contents, "off\n", 4) == 0) {
        return DITHER_OFF;
    }
    if (count >= 0) {
        errno = EINVAL;
    }
    return DITHER_UNKNOWN;
}

static int save_policy(int dir, enum dithering value)
{
    // The lock covers this temporary file and the subsequent hardware update.
    int fd = openat(dir, "dithering.tmp", O_WRONLY | O_CREAT | O_TRUNC | O_NOFOLLOW | O_CLOEXEC, 0600);
    if (fd == -1) {
        return -1;
    }

    const char *record = value == DITHER_ON ? "on\n" : "off\n";
    size_t length = strlen(record);
    ssize_t written = write(fd, record, length);
    if (written >= 0 && (size_t)written != length) {
        errno = EIO;
    }

    int result = ((size_t)written == length) ? fsync(fd) : -1;
    int saved_errno = errno;
    if (close(fd) == -1 && result == 0) {
        result = -1;
        saved_errno = errno;
    }
    if (result == 0) {
        result = renameat(dir, "dithering.tmp", dir, "dithering");
        saved_errno = errno;
    }
    if (result == -1) {
        unlinkat(dir, "dithering.tmp", 0);
        errno = saved_errno;
    }
    return result;
}

static int run_command(enum command command, enum dithering desired, bool report_errors)
{
    int result = 1;
    int dir = open_settings();
    if (dir == -1) {
        if (report_errors) {
            perror("stillcolor-cli: open settings directory under $HOME/Library/Application Support");
        }
        return result;
    }

    int lock = openat(dir, "lock", O_WRONLY | O_CREAT | O_NOFOLLOW | O_CLOEXEC, 0600);
    if (lock == -1 || flock(lock, LOCK_EX) == -1) {
        if (report_errors) {
            perror("stillcolor-cli: lock settings");
        }
    } else if (command == SET) {
        bool changed = load_policy(dir, DITHER_UNKNOWN) != desired;
        if (changed && save_policy(dir, desired) == -1) {
            perror("stillcolor-cli: save dithering policy");
        } else {
            printf(changed ? "Dithering policy saved: %s\n" : "Dithering policy already set to %s\n",
                   dither_name(desired));
            result = display_dithering(desired, report_errors);
        }
    } else {
        enum dithering policy = load_policy(dir, DITHER_OFF);
        if (policy == DITHER_UNKNOWN) {
            if (report_errors) {
                perror("stillcolor-cli: read dithering policy (use 'on' or 'off' to replace it)");
            }
        } else if (command == STATUS) {
            printf("Dithering policy: %s\n", dither_name(policy));
            result = display_dithering(DITHER_UNKNOWN, report_errors);
        } else {
            result = display_dithering(policy, report_errors);
        }
    }

    if (lock != -1) {
        close(lock);
    }
    close(dir);
    return result;
}

static int apply_policy(bool report_errors)
{
    return run_command(APPLY, DITHER_UNKNOWN, report_errors);
}

int main(int argc, char *argv[])
{
    // Barback redirects stdout to a file. Changes should appear as they happen.
    setvbuf(stdout, NULL, _IOLBF, 0);

    if (argc == 2) {
        if (strcmp(argv[1], "--help") == 0 || strcmp(argv[1], "-h") == 0) {
            usage(stdout);
            return 0;
        }
        if (strcmp(argv[1], "status") == 0) {
            return run_command(STATUS, DITHER_UNKNOWN, true);
        }
        if (strcmp(argv[1], "apply") == 0) {
            return apply_policy(true);
        }
        if (strcmp(argv[1], "watch") == 0) {
            return watch_displays(apply_policy);
        }
        enum dithering desired = parse_dithering(argv[1]);
        if (desired != DITHER_UNKNOWN) {
            return run_command(SET, desired, true);
        }
    }
    usage(stderr);
    return 2;
}
