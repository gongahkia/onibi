use chrono::NaiveDate;
use kelp::{run_with_args, run_with_args_capture, FixedClock, JsonFileStorage};
use serde_json::Value;
use std::env;
use std::fs;
use std::path::PathBuf;
use std::process::Command;
use std::time::{SystemTime, UNIX_EPOCH};

fn date(value: &str) -> NaiveDate {
    NaiveDate::parse_from_str(value, "%Y-%m-%d").expect("date fixture should be valid")
}

fn temp_root(prefix: &str) -> PathBuf {
    let nanos = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .expect("system time should be after the unix epoch")
        .as_nanos();
    env::temp_dir().join(format!(
        "kelp-contract-{prefix}-{}-{nanos}",
        std::process::id()
    ))
}

fn run(args: &[&str], storage: &JsonFileStorage, clock: &FixedClock) -> String {
    run_with_args(args, storage, clock).expect("command should succeed")
}

fn parse_json_data(output: &str) -> Value {
    let envelope: Value = serde_json::from_str(output).expect("output should be valid JSON");
    envelope
        .get("data")
        .cloned()
        .expect("JSON output should include a data envelope")
}

fn parse_json_error(output: &str) -> Value {
    serde_json::from_str(output).expect("error output should be valid JSON")
}

#[test]
fn json_errors_and_exit_codes_are_stable() {
    let storage_root = temp_root("errors");
    let storage = JsonFileStorage::at(storage_root.clone());
    let clock = FixedClock::new(date("2026-03-14"));

    let not_found = run_with_args_capture(
        ["kelp", "--output", "json", "task", "show", "999"],
        &storage,
        &clock,
    );
    assert_eq!(not_found.exit_code, 3);
    let not_found_error = parse_json_error(&not_found.stderr);
    assert_eq!(not_found_error["error"]["code"], "task_not_found");

    let usage = run_with_args_capture(
        ["kelp", "--output", "json", "task", "add", "--title", ""],
        &storage,
        &clock,
    );
    assert_eq!(usage.exit_code, 2);
    let usage_error = parse_json_error(&usage.stderr);
    assert_eq!(usage_error["error"]["code"], "empty_field");

    run(
        &["kelp", "project", "add", "--name", "Launch"],
        &storage,
        &clock,
    );
    let conflict = run_with_args_capture(
        [
            "kelp", "--output", "json", "project", "add", "--name", "Launch",
        ],
        &storage,
        &clock,
    );
    assert_eq!(conflict.exit_code, 4);
    let conflict_error = parse_json_error(&conflict.stderr);
    assert_eq!(conflict_error["error"]["code"], "duplicate_project");

    fs::remove_dir_all(storage_root).expect("storage cleanup should succeed");
}

#[test]
fn ready_view_and_file_inputs_work_for_scriptable_flows() {
    let storage_root = temp_root("ready");
    let storage = JsonFileStorage::at(storage_root.clone());
    let clock = FixedClock::new(date("2026-03-14"));
    let notes_path = storage_root.join("notes.md");
    let description_path = storage_root.join("brief.md");

    fs::create_dir_all(&storage_root).expect("storage root should exist");
    fs::write(&notes_path, "Capture release checklist\n").expect("notes fixture should be written");
    fs::write(&description_path, "Launch plan\n").expect("description fixture should be written");

    run(
        &[
            "kelp",
            "project",
            "add",
            "--name",
            "Launch",
            "--description-file",
            description_path
                .to_str()
                .expect("description path should be UTF-8"),
        ],
        &storage,
        &clock,
    );
    run(
        &[
            "kelp",
            "task",
            "add",
            "--title",
            "Ready task",
            "--project",
            "Launch",
            "--notes-file",
            notes_path.to_str().expect("notes path should be UTF-8"),
            "--due",
            "2026-03-15",
        ],
        &storage,
        &clock,
    );
    run(
        &[
            "kelp",
            "task",
            "add",
            "--title",
            "Waiting task",
            "--project",
            "Launch",
            "--wait-until",
            "2026-03-20",
        ],
        &storage,
        &clock,
    );
    run(
        &[
            "kelp",
            "task",
            "add",
            "--title",
            "Blocked task",
            "--project",
            "Launch",
            "--blocked-reason",
            "Vendor outage",
        ],
        &storage,
        &clock,
    );
    run(
        &[
            "kelp",
            "task",
            "add",
            "--title",
            "Dependency task",
            "--project",
            "Launch",
            "--depends-on",
            "1",
        ],
        &storage,
        &clock,
    );
    run(&["kelp", "task", "next", "1"], &storage, &clock);

    let ready = parse_json_data(&run(
        &["kelp", "--output", "json", "task", "ready"],
        &storage,
        &clock,
    ));
    let ready_titles = ready["tasks"]
        .as_array()
        .expect("tasks should be an array")
        .iter()
        .map(|task| task["title"].as_str().expect("title should be a string"))
        .collect::<Vec<_>>();
    assert_eq!(ready_titles, vec!["Ready task"]);

    let task = parse_json_data(&run(
        &["kelp", "--output", "json", "task", "show", "1"],
        &storage,
        &clock,
    ));
    assert_eq!(task["notes"], "Capture release checklist");

    let project = parse_json_data(&run(
        &["kelp", "--output", "json", "project", "show", "Launch"],
        &storage,
        &clock,
    ));
    assert_eq!(project["project"]["description"], "Launch plan");

    fs::remove_dir_all(storage_root).expect("storage cleanup should succeed");
}

#[test]
fn help_output_surfaces_global_contract_flags() {
    let storage = JsonFileStorage::at(temp_root("help"));
    let clock = FixedClock::new(date("2026-03-14"));

    let top_level_help = run_with_args_capture(["kelp", "--help"], &storage, &clock);
    assert_eq!(top_level_help.exit_code, 0);
    assert!(top_level_help
        .stdout
        .contains("kelp storage export --file ./kelp-export.json"));
    assert!(!top_level_help
        .stdout
        .contains("kelp storage export --output ./kelp-export.json"));

    let help = run_with_args_capture(["kelp", "task", "add", "--help"], &storage, &clock);
    assert_eq!(help.exit_code, 0);
    assert!(help.stdout.contains("--output <OUTPUT>"));
    assert!(help.stdout.contains("--color <COLOR>"));
    assert!(help.stdout.contains("--data-dir <DATA_DIR>"));
    assert!(help.stdout.contains("--notes-file <NOTES_FILE>"));

    let review_daily_help =
        run_with_args_capture(["kelp", "review", "daily", "--help"], &storage, &clock);
    assert_eq!(review_daily_help.exit_code, 0);
    assert!(review_daily_help
        .stdout
        .contains("Defer a task with ID:DATE."));
    assert!(review_daily_help.stdout.contains("DATE accepts YYYY-MM-DD"));

    let review_weekly_help =
        run_with_args_capture(["kelp", "review", "weekly", "--help"], &storage, &clock);
    assert_eq!(review_weekly_help.exit_code, 0);
    assert!(review_weekly_help
        .stdout
        .contains("Defer a task with ID:DATE."));
}

#[test]
fn default_paths_split_config_and_data_but_explicit_data_dir_keeps_config_colocated() {
    let xdg_data_home = temp_root("xdg-data");
    let xdg_config_home = temp_root("xdg-config");
    let explicit_data_dir = temp_root("explicit-data");
    let binary = env!("CARGO_BIN_EXE_kelp");

    let init = Command::new(binary)
        .env("XDG_DATA_HOME", &xdg_data_home)
        .env("XDG_CONFIG_HOME", &xdg_config_home)
        .arg("init")
        .output()
        .expect("kelp init should run");
    assert!(init.status.success());

    let config_show = Command::new(binary)
        .env("XDG_DATA_HOME", &xdg_data_home)
        .env("XDG_CONFIG_HOME", &xdg_config_home)
        .args(["--output", "json", "config", "show"])
        .output()
        .expect("kelp config show should run");
    assert!(config_show.status.success());
    let config_data =
        parse_json_data(std::str::from_utf8(&config_show.stdout).expect("stdout should be UTF-8"));
    assert_eq!(
        config_data["path"],
        xdg_config_home
            .join("kelp")
            .join("config.json")
            .display()
            .to_string()
    );

    let storage_show = Command::new(binary)
        .env("XDG_DATA_HOME", &xdg_data_home)
        .env("XDG_CONFIG_HOME", &xdg_config_home)
        .args(["--output", "json", "storage", "path"])
        .output()
        .expect("kelp storage path should run");
    assert!(storage_show.status.success());
    let storage_data =
        parse_json_data(std::str::from_utf8(&storage_show.stdout).expect("stdout should be UTF-8"));
    assert_eq!(
        storage_data["data_file"],
        xdg_data_home
            .join("kelp")
            .join("data.json")
            .display()
            .to_string()
    );

    let explicit_init = Command::new(binary)
        .args([
            "--data-dir",
            explicit_data_dir
                .to_str()
                .expect("data dir should be UTF-8"),
            "init",
        ])
        .output()
        .expect("kelp init with explicit data dir should run");
    assert!(explicit_init.status.success());

    let explicit_config = Command::new(binary)
        .args([
            "--data-dir",
            explicit_data_dir
                .to_str()
                .expect("data dir should be UTF-8"),
            "--output",
            "json",
            "config",
            "show",
        ])
        .output()
        .expect("kelp config show should run");
    assert!(explicit_config.status.success());
    let explicit_config_data = parse_json_data(
        std::str::from_utf8(&explicit_config.stdout).expect("stdout should be UTF-8"),
    );
    assert_eq!(
        explicit_config_data["path"],
        explicit_data_dir.join("config.json").display().to_string()
    );
}
