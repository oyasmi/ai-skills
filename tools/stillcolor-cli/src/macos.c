#include "stillcolor.h"

#include <CoreFoundation/CoreFoundation.h>
#include <CoreGraphics/CoreGraphics.h>
#include <IOKit/IOKitLib.h>
#include <IOKit/IOMessage.h>
#include <IOKit/pwr_mgt/IOPMLib.h>
#include <dispatch/dispatch.h>
#include <inttypes.h>
#include <mach/mach_error.h>
#include <signal.h>
#include <stdio.h>

static enum dithering read_dithering(io_service_t service)
{
    CFTypeRef property = IORegistryEntryCreateCFProperty(
        service, CFSTR("enableDither"), kCFAllocatorDefault, 0);
    if (property == NULL) {
        return DITHER_UNKNOWN;
    }

    enum dithering value = DITHER_UNKNOWN;
    if (CFGetTypeID(property) == CFBooleanGetTypeID()) {
        value = CFBooleanGetValue(property) ? DITHER_ON : DITHER_OFF;
    } else if (CFGetTypeID(property) == CFNumberGetTypeID()) {
        int number;
        if (CFNumberGetValue(property, kCFNumberIntType, &number) && (number == 0 || number == 1)) {
            value = number == 1 ? DITHER_ON : DITHER_OFF;
        }
    }
    CFRelease(property);
    return value;
}

static int update_framebuffer(io_service_t service, enum dithering desired, bool report_errors)
{
    enum dithering current = read_dithering(service);
    if (current == desired && desired != DITHER_UNKNOWN) {
        return 0;
    }

    io_name_t name = "framebuffer";
    IORegistryEntryGetName(service, name);
    uint64_t id = 0;
    IORegistryEntryGetRegistryEntryID(service, &id);
    if (desired == DITHER_UNKNOWN) {
        printf("%s [0x%" PRIx64 "] enableDither=%s\n", name, id, dither_name(current));
        return 0;
    }

    CFBooleanRef property = desired == DITHER_ON ? kCFBooleanTrue : kCFBooleanFalse;
    kern_return_t result = IORegistryEntrySetCFProperty(service, CFSTR("enableDither"), property);
    if (result != KERN_SUCCESS) {
        if (report_errors) {
            fprintf(stderr, "stillcolor-cli: %s [0x%" PRIx64 "] could not set enableDither=%s: %s (0x%x)\n",
                    name, id, dither_name(desired), mach_error_string(result), (unsigned int)result);
        }
        return 1;
    }

    current = read_dithering(service);
    if (current != desired) {
        if (report_errors) {
            fprintf(stderr, "stillcolor-cli: %s [0x%" PRIx64 "] expected enableDither=%s, read %s\n",
                    name, id, dither_name(desired), dither_name(current));
        }
        return 1;
    }
    printf("%s [0x%" PRIx64 "] set enableDither=%s\n", name, id, dither_name(desired));
    return 0;
}

int display_dithering(enum dithering desired, bool report_errors)
{
    // Original Stillcolor matches the common ancestor of AppleCLCD2 and IOMobileFramebufferShim.
    CFMutableDictionaryRef matching = IOServiceMatching("IOMobileFramebufferAP");
    if (matching == NULL) {
        if (report_errors) {
            fputs("stillcolor-cli: could not create a framebuffer matching dictionary\n", stderr);
        }
        return 1;
    }

    io_iterator_t entries;
    kern_return_t result = IOServiceGetMatchingServices(kIOMainPortDefault, matching, &entries);
    if (result != KERN_SUCCESS) {
        if (report_errors) {
            fprintf(stderr, "stillcolor-cli: could not enumerate framebuffers: %s (0x%x)\n",
                    mach_error_string(result), (unsigned int)result);
        }
        return 1;
    }

    bool found = false;
    int failed = 0;
    io_service_t service;
    while ((service = IOIteratorNext(entries)) != IO_OBJECT_NULL) {
        found = true;
        failed |= update_framebuffer(service, desired, report_errors);
        IOObjectRelease(service);
    }
    bool valid = IOIteratorIsValid(entries);
    IOObjectRelease(entries);
    if (!valid && report_errors) {
        fputs("stillcolor-cli: framebuffer list changed during enumeration\n", stderr);
    } else if (!found && report_errors) {
        fputs("stillcolor-cli: no IOMobileFramebufferAP services found\n", stderr);
    }
    return failed || !valid || !found;
}

static const CFTimeInterval retry_interval = 30.0;

struct watcher {
    CFRunLoopTimerRef timer;
    io_connect_t power;
    int (*reconcile)(bool report_errors);
    bool last_failed;
};

static void reconcile_timer(CFRunLoopTimerRef timer, void *context)
{
    (void)timer;
    struct watcher *watcher = context;
    bool failed = watcher->reconcile(!watcher->last_failed) != 0;
    if (watcher->last_failed && !failed) {
        puts("Dithering policy reapplied successfully.");
    }
    watcher->last_failed = failed;
}

static void schedule_reconcile(void *context)
{
    struct watcher *watcher = context;
    if (watcher->timer != NULL) {
        // Give the driver time to finish waking or configuring the framebuffer.
        CFRunLoopTimerSetNextFireDate(watcher->timer, CFAbsoluteTimeGetCurrent() + 1.0);
    }
}

static void display_changed(CGDirectDisplayID display, CGDisplayChangeSummaryFlags flags, void *context)
{
    (void)display;
    CGDisplayChangeSummaryFlags changes = kCGDisplayAddFlag | kCGDisplayRemoveFlag |
        kCGDisplayEnabledFlag | kCGDisplayDisabledFlag | kCGDisplaySetModeFlag |
        kCGDisplayMirrorFlag | kCGDisplayUnMirrorFlag;
    if ((flags & kCGDisplayBeginConfigurationFlag) == 0 && (flags & changes) != 0) {
        dispatch_async_f(dispatch_get_main_queue(), context, schedule_reconcile);
    }
}

static void power_changed(void *context, io_service_t service, natural_t message, void *argument)
{
    (void)service;
    struct watcher *watcher = context;
    switch (message) {
    case kIOMessageCanSystemSleep:
    case kIOMessageSystemWillSleep:
        IOAllowPowerChange(watcher->power, (long)(intptr_t)argument);
        break;
    case kIOMessageSystemHasPoweredOn:
        schedule_reconcile(watcher);
        break;
    }
}

static void stop_watch(void *context)
{
    CFRunLoopStop(context);
}

static dispatch_source_t stop_on_signal(int signum, CFRunLoopRef loop)
{
    dispatch_source_t source = dispatch_source_create(
        DISPATCH_SOURCE_TYPE_SIGNAL, (uintptr_t)signum, 0, dispatch_get_main_queue());
    if (source != NULL) {
        if (signal(signum, SIG_IGN) == SIG_ERR) {
            dispatch_release(source);
            return NULL;
        }
        dispatch_set_context(source, loop);
        dispatch_source_set_event_handler_f(source, stop_watch);
        dispatch_activate(source);
    }
    return source;
}

int watch_displays(int (*reconcile)(bool report_errors))
{
    CFRunLoopRef loop = CFRunLoopGetCurrent();
    struct watcher watcher = {.reconcile = reconcile};
    CFRunLoopTimerContext context = {.info = &watcher};
    watcher.timer = CFRunLoopTimerCreate(kCFAllocatorDefault,
        CFAbsoluteTimeGetCurrent() + retry_interval, retry_interval, 0, 0, reconcile_timer, &context);
    if (watcher.timer == NULL) {
        fputs("stillcolor-cli: could not create a display reconciliation timer\n", stderr);
        return 1;
    }
    CFRunLoopTimerSetTolerance(watcher.timer, 0.2);
    CFRunLoopAddTimer(loop, watcher.timer, kCFRunLoopDefaultMode);

    dispatch_source_t term = stop_on_signal(SIGTERM, loop);
    dispatch_source_t interrupt = stop_on_signal(SIGINT, loop);
    if (term == NULL || interrupt == NULL) {
        fputs("stillcolor-cli: could not install stop signal handlers\n", stderr);
        return 1;
    }

    CGError display_error = CGDisplayRegisterReconfigurationCallback(display_changed, &watcher);
    if (display_error != kCGErrorSuccess) {
        fprintf(stderr, "stillcolor-cli: could not register display notifications (%d); periodic checks remain active\n",
                (int)display_error);
    }

    IONotificationPortRef port = NULL;
    io_object_t notifier = IO_OBJECT_NULL;
    watcher.power = IORegisterForSystemPower(&watcher, &port, power_changed, &notifier);
    if (watcher.power != IO_OBJECT_NULL) {
        CFRunLoopAddSource(loop, IONotificationPortGetRunLoopSource(port), kCFRunLoopDefaultMode);
    } else {
        fputs("stillcolor-cli: could not register wake notifications; periodic checks remain active\n", stderr);
    }

    puts("Watching for display changes.");
    reconcile_timer(watcher.timer, &watcher);
    CFRunLoopRun();

    if (display_error == kCGErrorSuccess) {
        CGDisplayRemoveReconfigurationCallback(display_changed, &watcher);
    }
    if (watcher.power != IO_OBJECT_NULL) {
        CFRunLoopRemoveSource(loop, IONotificationPortGetRunLoopSource(port), kCFRunLoopDefaultMode);
        IODeregisterForSystemPower(&notifier);
        IOServiceClose(watcher.power);
    }
    if (port != NULL) {
        IONotificationPortDestroy(port);
    }
    CFRunLoopTimerInvalidate(watcher.timer);
    CFRelease(watcher.timer);
    watcher.timer = NULL;
    dispatch_source_cancel(term);
    dispatch_source_cancel(interrupt);
    dispatch_release(term);
    dispatch_release(interrupt);
    puts("Stopped watching. The current dithering setting remains in effect.");
    return 0;
}
