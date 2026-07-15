fn main() {
    println!("cargo:rerun-if-changed=include/yeokcham.h");
    println!("cargo:rerun-if-changed=tests/c_consumer.c");
    cc::Build::new()
        .file("tests/c_consumer.c")
        .include("include")
        .flag_if_supported("-std=c11")
        .compile("yeokcham_c_consumer");
}
