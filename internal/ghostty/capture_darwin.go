//go:build darwin && cgo

package ghostty

/*
#cgo LDFLAGS: -framework ApplicationServices -framework CoreFoundation
#include <ApplicationServices/ApplicationServices.h>
#include <stdlib.h>

// AX calls have their own timeout so a frozen GUI cannot indefinitely block
// the daemon. The returned UTF-8 buffer contains at most 16K UTF-16 units.
static int onibi_capture(pid_t pid, char **text, CFIndex *length) {
    if (!AXIsProcessTrusted()) return 1;
    AXUIElementRef app = AXUIElementCreateApplication(pid);
    if (!app) return 3;
    AXUIElementSetMessagingTimeout(app, 1.0);
    CFTypeRef focused = NULL, role = NULL, value = NULL, after = NULL;
    CFStringRef tail = NULL;
    int result = 3;
    AXError error = AXUIElementCopyAttributeValue(app, kAXFocusedUIElementAttribute, &focused);
    if (error != kAXErrorSuccess || !focused || CFGetTypeID(focused) != AXUIElementGetTypeID()) goto done;
    AXUIElementSetMessagingTimeout((AXUIElementRef)focused, 1.0);
    pid_t focused_pid = 0;
    if (AXUIElementGetPid((AXUIElementRef)focused, &focused_pid) != kAXErrorSuccess || focused_pid != pid) goto done;
    if (AXUIElementCopyAttributeValue((AXUIElementRef)focused, kAXRoleAttribute, &role) != kAXErrorSuccess || !role || !CFEqual(role, kAXTextAreaRole)) {
        result = 2;
        goto done;
    }
    if (AXUIElementCopyAttributeValue((AXUIElementRef)focused, kAXValueAttribute, &value) != kAXErrorSuccess || !value || CFGetTypeID(value) != CFStringGetTypeID()) goto done;
    if (AXUIElementCopyAttributeValue(app, kAXFocusedUIElementAttribute, &after) != kAXErrorSuccess || !after || !CFEqual(focused, after)) {
        result = 2;
        goto done;
    }
    CFIndex count = CFStringGetLength((CFStringRef)value);
    CFIndex start = count > 16384 ? count - 16384 : 0;
    if (start < count) {
        UniChar first = CFStringGetCharacterAtIndex((CFStringRef)value, start);
        if (first >= 0xDC00 && first <= 0xDFFF) start++;
    }
    tail = CFStringCreateWithSubstring(NULL, (CFStringRef)value, CFRangeMake(start, count-start));
    if (!tail) goto done;
    CFIndex capacity = CFStringGetMaximumSizeForEncoding(CFStringGetLength(tail), kCFStringEncodingUTF8);
    *text = malloc((size_t)capacity + 1);
    if (!*text) goto done;
    CFIndex used = 0;
    CFIndex converted = CFStringGetBytes(tail, CFRangeMake(0, CFStringGetLength(tail)), kCFStringEncodingUTF8, '?', false, (UInt8 *)*text, capacity, &used);
    if (converted != CFStringGetLength(tail)) {
        free(*text);
        *text = NULL;
        goto done;
    }
    (*text)[used] = 0;
    *length = used;
    result = 0;
done:
    if (tail) CFRelease(tail);
    if (after) CFRelease(after);
    if (value) CFRelease(value);
    if (role) CFRelease(role);
    if (focused) CFRelease(focused);
    CFRelease(app);
    return result;
}
*/
import "C"

import (
	"context"
	"fmt"
	"unsafe"
)

func captureNative(ctx context.Context, pid int) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	var data *C.char
	var length C.CFIndex
	status := C.onibi_capture(C.pid_t(pid), &data, &length)
	if data != nil {
		defer C.free(unsafe.Pointer(data))
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	switch status {
	case 0:
		return C.GoStringN(data, C.int(length)), nil
	case 1:
		return "", ErrCapturePermission
	case 2:
		return "", ErrFocusRequired
	default:
		return "", fmt.Errorf("%w: Ghostty could not provide focused terminal text; focus the terminal and try again", ErrCaptureUnavailable)
	}
}

func captureReadiness() Check {
	if C.AXIsProcessTrusted() != 0 {
		return Check{"capture", "ready", "Accessibility is authorized for this process. Focused-pane text has not been read."}
	}
	return Check{"capture", "capture_permission", "Allow the process launching Onibi in macOS Privacy & Security → Accessibility to read output. No permission prompt was requested."}
}
