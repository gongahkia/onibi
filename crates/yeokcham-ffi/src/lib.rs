#![forbid(unsafe_code)]

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

#[cfg(test)]
mod tests {
    use super::{YEOKCHAM_ABI_VERSION, YEOKCHAM_ABI_VERSION_MAJOR, YEOKCHAM_ABI_VERSION_MINOR};

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
}
