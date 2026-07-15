#ifndef YEOKCHAM_V1_H
#define YEOKCHAM_V1_H

#include <stdint.h>

#define YEOKCHAM_ABI_VERSION_MAJOR UINT32_C(1)
#define YEOKCHAM_ABI_VERSION_MINOR UINT32_C(0)
#define YEOKCHAM_ABI_VERSION UINT32_C(1)

typedef struct yeokcham_handle yeokcham_handle_t; // library-owned opaque handle; release with yeokcham_handle_release

typedef int32_t yeokcham_status_t;

#define YEOKCHAM_STATUS_OK INT32_C(0)
#define YEOKCHAM_STATUS_INVALID_INPUT INT32_C(1)
#define YEOKCHAM_STATUS_UNSUPPORTED_VERSION INT32_C(2)
#define YEOKCHAM_STATUS_RESOURCE_LIMIT INT32_C(3)
#define YEOKCHAM_STATUS_STATE INT32_C(4)

yeokcham_handle_t *yeokcham_handle_create(void); // null when handle capacity is exhausted
yeokcham_status_t yeokcham_handle_release(yeokcham_handle_t *handle); // invalid input unless handle is active

#endif
