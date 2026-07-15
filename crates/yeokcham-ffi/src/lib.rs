#![deny(unsafe_op_in_unsafe_fn)]

#[allow(unsafe_code)]
mod c_abi;

pub const YEOKCHAM_ABI_VERSION_MAJOR: u32 = 1;
pub const YEOKCHAM_ABI_VERSION_MINOR: u32 = 0;
pub const YEOKCHAM_ABI_VERSION: u32 = 1;

#[repr(C)]
pub struct YeokchamHandle {
    _private: u8,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
#[repr(i32)]
pub enum YeokchamStatus {
    Ok = 0,
    InvalidInput = 1,
    UnsupportedVersion = 2,
    ResourceLimit = 3,
    State = 4,
}

pub use c_abi::{MAX_C_ABI_HANDLES, yeokcham_handle_create, yeokcham_handle_release};

#[cfg(test)]
mod tests {
    use super::{
        MAX_C_ABI_HANDLES, YEOKCHAM_ABI_VERSION, YEOKCHAM_ABI_VERSION_MAJOR,
        YEOKCHAM_ABI_VERSION_MINOR, YeokchamStatus, yeokcham_handle_create,
        yeokcham_handle_release,
    };

    const HEADER: &str = include_str!("../include/yeokcham.h");

    #[test]
    fn published_header_has_the_stable_abi_version() {
        assert_eq!(YEOKCHAM_ABI_VERSION_MAJOR, 1);
        assert_eq!(YEOKCHAM_ABI_VERSION_MINOR, 0);
        assert_eq!(YEOKCHAM_ABI_VERSION, 1);
        assert!(HEADER.contains("#ifndef YEOKCHAM_V1_H\n#define YEOKCHAM_V1_H"));
        assert!(HEADER.contains("#include <stdint.h>"));
        assert!(HEADER.contains("#define YEOKCHAM_ABI_VERSION_MAJOR UINT32_C(1)"));
        assert!(HEADER.contains("#define YEOKCHAM_ABI_VERSION_MINOR UINT32_C(0)"));
        assert!(HEADER.contains("#define YEOKCHAM_ABI_VERSION UINT32_C(1)"));
        assert!(HEADER.contains("typedef struct yeokcham_handle yeokcham_handle_t;"));
        assert!(HEADER.ends_with("#endif\n"));
    }

    #[test]
    fn published_handle_remains_opaque() {
        assert!(HEADER.contains("typedef struct yeokcham_handle yeokcham_handle_t;"));
        assert!(!HEADER.contains("struct yeokcham_handle {"));
    }

    #[test]
    fn published_status_codes_match_rust() {
        assert_eq!(YeokchamStatus::Ok as i32, 0);
        assert_eq!(YeokchamStatus::InvalidInput as i32, 1);
        assert_eq!(YeokchamStatus::UnsupportedVersion as i32, 2);
        assert_eq!(YeokchamStatus::ResourceLimit as i32, 3);
        assert_eq!(YeokchamStatus::State as i32, 4);
        assert!(HEADER.contains("typedef int32_t yeokcham_status_t;"));
        assert!(HEADER.contains("#define YEOKCHAM_STATUS_OK INT32_C(0)"));
        assert!(HEADER.contains("#define YEOKCHAM_STATUS_INVALID_INPUT INT32_C(1)"));
        assert!(HEADER.contains("#define YEOKCHAM_STATUS_UNSUPPORTED_VERSION INT32_C(2)"));
        assert!(HEADER.contains("#define YEOKCHAM_STATUS_RESOURCE_LIMIT INT32_C(3)"));
        assert!(HEADER.contains("#define YEOKCHAM_STATUS_STATE INT32_C(4)"));
        assert!(HEADER.contains("yeokcham_handle_t *yeokcham_handle_create(void);"));
        assert!(HEADER.contains("yeokcham_handle_release(yeokcham_handle_t *handle);"));
    }

    #[test]
    fn created_handles_release_once_and_reject_invalid_inputs() {
        let handle = yeokcham_handle_create();
        assert!(!handle.is_null());
        assert_eq!(
            yeokcham_handle_release(std::ptr::null_mut()),
            YeokchamStatus::InvalidInput
        );
        assert_eq!(yeokcham_handle_release(handle), YeokchamStatus::Ok);
        assert_eq!(
            yeokcham_handle_release(handle),
            YeokchamStatus::InvalidInput
        );
        assert!(MAX_C_ABI_HANDLES > 0);
    }
}
