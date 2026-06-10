use std::{
    collections::HashSet,
    env,
    path::{Path, PathBuf},
};

pub fn resolve_binary(command: &str) -> Option<PathBuf> {
    let command = command.trim();
    if command.is_empty() {
        return None;
    }
    let candidate = PathBuf::from(command);
    if candidate.components().count() > 1 {
        return candidate.is_file().then_some(candidate);
    }
    resolve_binary_in_paths(command, binary_search_dirs())
}

fn resolve_binary_in_paths<I, P>(command: &str, paths: I) -> Option<PathBuf>
where
    I: IntoIterator<Item = P>,
    P: AsRef<Path>,
{
    let command = command.trim();
    if command.is_empty() {
        return None;
    }
    for dir in paths {
        let candidate = dir.as_ref().join(command);
        if candidate.is_file() {
            return Some(candidate);
        }
    }
    None
}

fn binary_search_dirs() -> Vec<PathBuf> {
    let mut dirs = Vec::new();
    let mut seen = HashSet::new();
    if let Some(paths) = env::var_os("PATH") {
        for dir in env::split_paths(&paths) {
            push_dir(&mut dirs, &mut seen, dir);
        }
    }
    if let Some(home) = env::var_os("HOME") {
        let home = PathBuf::from(home);
        for suffix in [".local/bin", "bin", ".cargo/bin", ".bun/bin"] {
            push_dir(&mut dirs, &mut seen, home.join(suffix));
        }
    }
    for dir in [
        "/opt/homebrew/bin",
        "/opt/homebrew/sbin",
        "/usr/local/bin",
        "/usr/local/sbin",
        "/usr/bin",
        "/bin",
        "/usr/sbin",
        "/sbin",
    ] {
        push_dir(&mut dirs, &mut seen, PathBuf::from(dir));
    }
    dirs
}

fn push_dir(dirs: &mut Vec<PathBuf>, seen: &mut HashSet<PathBuf>, dir: PathBuf) {
    if dir.as_os_str().is_empty() || !seen.insert(dir.clone()) {
        return;
    }
    dirs.push(dir);
}

#[cfg(test)]
mod tests {
    use super::*;
    use tempfile::tempdir;

    #[test]
    fn resolves_binary_from_supplied_dirs() {
        let dir = tempdir().unwrap();
        let tool = dir.path().join("tool");
        std::fs::write(&tool, b"").unwrap();

        assert_eq!(resolve_binary_in_paths("tool", [dir.path()]), Some(tool));
    }

    #[test]
    fn resolves_explicit_path() {
        let dir = tempdir().unwrap();
        let tool = dir.path().join("tool");
        std::fs::write(&tool, b"").unwrap();

        assert_eq!(resolve_binary(tool.to_string_lossy().as_ref()), Some(tool));
    }
}
