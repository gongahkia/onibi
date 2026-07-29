use arachne_sdk::{SDK_API_VERSION, SDK_API_VERSION_MAJOR, SDK_API_VERSION_MINOR, SdkApiVersion};

#[test]
fn published_sdk_api_version_matches_its_package_release_line() {
    let mut components = env!("CARGO_PKG_VERSION").split('.');
    let major = components
        .next()
        .expect("package version must have a major component")
        .parse::<u16>()
        .expect("package major version must fit SDK API version");
    let minor = components
        .next()
        .expect("package version must have a minor component")
        .parse::<u16>()
        .expect("package minor version must fit SDK API version");
    assert_eq!(components.next(), Some("0"));
    assert_eq!(SDK_API_VERSION_MAJOR, major);
    assert_eq!(SDK_API_VERSION_MINOR, minor);
    assert_eq!(SDK_API_VERSION, SdkApiVersion::new(major, minor));
}

#[test]
fn public_api_rejects_an_incompatible_pre_release_requirement() {
    assert!(SDK_API_VERSION.supports(SdkApiVersion::new(0, 1)));
    assert!(!SDK_API_VERSION.supports(SdkApiVersion::new(0, 2)));
}

#[test]
fn public_api_accepts_stable_prior_minor_but_not_future_or_major_requirements() {
    let version = SdkApiVersion::new(1, 2);
    assert!(version.supports(SdkApiVersion::new(1, 1)));
    assert!(!version.supports(SdkApiVersion::new(1, 3)));
    assert!(!version.supports(SdkApiVersion::new(2, 0)));
}
