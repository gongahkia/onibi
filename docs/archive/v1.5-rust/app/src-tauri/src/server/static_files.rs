use std::{
    env,
    path::{Path, PathBuf},
};
use tower_http::{
    services::{ServeDir, ServeFile},
    set_status::SetStatus,
};

pub fn mobile_service() -> ServeDir<SetStatus<ServeFile>> {
    let dist = mobile_dist_dir();
    ServeDir::new(&dist)
        .append_index_html_on_directories(true)
        .not_found_service(ServeFile::new(dist.join("index.html")))
}

fn mobile_dist_dir() -> PathBuf {
    if let Ok(path) = env::var("ONIBI_MOBILE_DIST") {
        return PathBuf::from(path);
    }

    let candidates = [
        PathBuf::from("mobile/dist"),
        PathBuf::from("../mobile/dist"),
        PathBuf::from("../../mobile/dist"),
        Path::new(env!("CARGO_MANIFEST_DIR")).join("../../mobile/dist"),
    ];

    candidates
        .into_iter()
        .find(|path| path.join("index.html").exists())
        .unwrap_or_else(|| Path::new(env!("CARGO_MANIFEST_DIR")).join("../../mobile/dist"))
}
