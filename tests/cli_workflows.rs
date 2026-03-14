use chrono::NaiveDate;
use kelp::{run_with_args, FixedClock, JsonFileStorage};
use serde_json::Value;
use std::env;
use std::fs;
use std::path::PathBuf;
use std::time::{SystemTime, UNIX_EPOCH};

fn date(value: &str) -> NaiveDate {
    NaiveDate::parse_from_str(value, "%Y-%m-%d").expect("date fixture should be valid")
}

fn temp_root(prefix: &str) -> PathBuf {
    let nanos = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .expect("system time should be after the unix epoch")
        .as_nanos();
    env::temp_dir().join(format!("kelp-{prefix}-{}-{nanos}", std::process::id()))
}

fn run(args: &[&str], storage: &JsonFileStorage, clock: &FixedClock) -> String {
    run_with_args(args, storage, clock).expect("command should succeed")
}

#[test]
fn legacy_import_command_migrates_old_project_files() {
    let storage_root = temp_root("import-storage");
    let legacy_root = temp_root("import-legacy");
    let projects_dir = legacy_root.join(".kelpProjects").join("Launch");
    fs::create_dir_all(&projects_dir).expect("legacy project tree should be created");
    fs::write(
        legacy_root.join(".kelpStorage"),
        "Inbox task, Capture notes, 14/03/26/, Medium, inbox\n",
    )
    .expect("legacy inbox file should be written");
    fs::write(
        projects_dir.join(".kelpStorage"),
        "Project task, Ship docs, 20/03/26/, High, launch&docs\n",
    )
    .expect("legacy project file should be written");

    let storage = JsonFileStorage::at(storage_root.clone());
    let clock = FixedClock::new(date("2026-03-14"));
    let imported = run(
        &[
            "kelp",
            "import",
            "legacy",
            "--source",
            legacy_root.to_str().expect("legacy path should be UTF-8"),
            "--json",
        ],
        &storage,
        &clock,
    );
    let imported: Value =
        serde_json::from_str(&imported).expect("import output should be valid JSON");

    assert_eq!(imported["imported_tasks"], 2);
    assert_eq!(imported["imported_projects"], 1);

    let list = run(&["kelp", "task", "list", "--json"], &storage, &clock);
    let list: Value = serde_json::from_str(&list).expect("list output should be valid JSON");
    assert_eq!(list["tasks"].as_array().expect("tasks should be an array").len(), 2);

    fs::remove_dir_all(storage_root).expect("storage cleanup should succeed");
    fs::remove_dir_all(legacy_root).expect("legacy cleanup should succeed");
}

#[test]
fn storage_backup_and_export_commands_write_files() {
    let storage_root = temp_root("storage-tools");
    let export_root = temp_root("storage-export");
    let storage = JsonFileStorage::at(storage_root.clone());
    let clock = FixedClock::new(date("2026-03-14"));
    run(
        &[
            "kelp",
            "task",
            "add",
            "--title",
            "Persist me",
            "--due",
            "2026-03-20",
        ],
        &storage,
        &clock,
    );

    let backup = run(&["kelp", "storage", "backup", "--json"], &storage, &clock);
    let backup: Value = serde_json::from_str(&backup).expect("backup output should be valid JSON");
    assert!(PathBuf::from(backup["path"].as_str().expect("backup path should be a string")).exists());

    let export_path = export_root.join("kelp-export.json");
    let exported = run(
        &[
            "kelp",
            "storage",
            "export",
            "--output",
            export_path.to_str().expect("export path should be UTF-8"),
            "--json",
        ],
        &storage,
        &clock,
    );
    let exported: Value =
        serde_json::from_str(&exported).expect("export output should be valid JSON");
    assert_eq!(
        exported["path"].as_str().expect("export path should be a string"),
        export_path.to_str().expect("export path should be UTF-8")
    );
    assert!(export_path.exists());

    fs::remove_dir_all(storage_root).expect("storage cleanup should succeed");
    fs::remove_dir_all(export_root).expect("export cleanup should succeed");
}

#[test]
fn review_actions_update_task_state_before_rendering() {
    let storage_root = temp_root("review-actions");
    let storage = JsonFileStorage::at(storage_root.clone());
    let clock = FixedClock::new(date("2026-03-14"));

    run(
        &[
            "kelp",
            "task",
            "add",
            "--title",
            "Start me",
            "--due",
            "2026-03-14",
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
            "Defer me",
            "--due",
            "2026-03-14",
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
            "Complete me",
            "--due",
            "2026-03-14",
            "--repeat",
            "weekly",
        ],
        &storage,
        &clock,
    );

    let review = run(
        &[
            "kelp",
            "review",
            "daily",
            "--start",
            "1",
            "--defer",
            "2:2026-03-20",
            "--complete",
            "3",
            "--json",
        ],
        &storage,
        &clock,
    );
    let review: Value = serde_json::from_str(&review).expect("review output should be valid JSON");

    assert_eq!(
        review["applied_actions"]
            .as_array()
            .expect("actions should be an array")
            .len(),
        3
    );

    let task_one = run(&["kelp", "task", "show", "1", "--json"], &storage, &clock);
    let task_one: Value =
        serde_json::from_str(&task_one).expect("task one output should be valid JSON");
    assert_eq!(task_one["status"], "in_progress");

    let task_two = run(&["kelp", "task", "show", "2", "--json"], &storage, &clock);
    let task_two: Value =
        serde_json::from_str(&task_two).expect("task two output should be valid JSON");
    assert_eq!(task_two["due_date"], "2026-03-20");

    let task_three = run(&["kelp", "task", "show", "3", "--json"], &storage, &clock);
    let task_three: Value =
        serde_json::from_str(&task_three).expect("task three output should be valid JSON");
    assert_eq!(task_three["status"], "done");

    let task_four = run(&["kelp", "task", "show", "4", "--json"], &storage, &clock);
    let task_four: Value =
        serde_json::from_str(&task_four).expect("task four output should be valid JSON");
    assert_eq!(task_four["status"], "todo");

    fs::remove_dir_all(storage_root).expect("storage cleanup should succeed");
}

#[test]
fn archive_unarchive_and_bulk_edit_commands_work_together() {
    let storage_root = temp_root("task-actions");
    let storage = JsonFileStorage::at(storage_root.clone());
    let clock = FixedClock::new(date("2026-03-14"));

    run(&["kelp", "project", "add", "--name", "Launch"], &storage, &clock);
    run(
        &[
            "kelp",
            "task",
            "add",
            "--title",
            "Checklist",
            "--project",
            "Launch",
        ],
        &storage,
        &clock,
    );
    run(
        &[
            "kelp",
            "task",
            "archive",
            "1",
        ],
        &storage,
        &clock,
    );
    run(
        &[
            "kelp",
            "task",
            "unarchive",
            "1",
        ],
        &storage,
        &clock,
    );
    run(
        &[
            "kelp",
            "task",
            "bulk-edit",
            "1",
            "--priority",
            "high",
            "--due",
            "2026-03-18",
            "--tag",
            "launch",
        ],
        &storage,
        &clock,
    );
    run(
        &[
            "kelp",
            "project",
            "archive",
            "Launch",
        ],
        &storage,
        &clock,
    );
    run(
        &[
            "kelp",
            "project",
            "unarchive",
            "Launch",
        ],
        &storage,
        &clock,
    );

    let task = run(&["kelp", "task", "show", "1", "--json"], &storage, &clock);
    let task: Value = serde_json::from_str(&task).expect("task output should be valid JSON");
    assert_eq!(task["priority"], "high");
    assert_eq!(task["due_date"], "2026-03-18");
    assert_eq!(
        task["tags"].as_array().expect("tags should be an array")[0],
        "launch"
    );

    let projects = run(&["kelp", "project", "list", "--json"], &storage, &clock);
    let projects: Value =
        serde_json::from_str(&projects).expect("project list output should be valid JSON");
    assert_eq!(
        projects["projects"]
            .as_array()
            .expect("projects should be an array")
            .len(),
        1
    );

    fs::remove_dir_all(storage_root).expect("storage cleanup should succeed");
}

#[test]
fn config_defaults_drive_json_output_and_upcoming_windows() {
    let storage_root = temp_root("config-defaults");
    let storage = JsonFileStorage::at(storage_root.clone());
    let clock = FixedClock::new(date("2026-03-14"));

    run(
        &[
            "kelp",
            "config",
            "set",
            "--upcoming-days",
            "10",
            "--task-sort",
            "priority",
            "--json-output",
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
            "Ship release notes",
            "--due",
            "2026-03-22",
            "--priority",
            "high",
        ],
        &storage,
        &clock,
    );

    let config = run(&["kelp", "config", "show", "--json"], &storage, &clock);
    let config: Value = serde_json::from_str(&config).expect("config output should be valid JSON");
    assert_eq!(config["default_upcoming_days"], 10);
    assert_eq!(config["default_task_sort"], "priority");
    assert_eq!(config["default_json_output"], true);

    let upcoming = run(&["kelp", "upcoming"], &storage, &clock);
    let upcoming: Value =
        serde_json::from_str(&upcoming).expect("upcoming output should follow the JSON default");
    assert_eq!(
        upcoming["sections"][0]["tasks"][0]["title"],
        "Ship release notes"
    );

    fs::remove_dir_all(storage_root).expect("storage cleanup should succeed");
}

#[test]
fn review_planning_actions_and_date_shortcuts_work_together() {
    let storage_root = temp_root("review-plan");
    let storage = JsonFileStorage::at(storage_root.clone());
    let clock = FixedClock::new(date("2026-03-14"));

    run(&["kelp", "project", "add", "--name", "Launch"], &storage, &clock);
    run(
        &[
            "kelp",
            "task",
            "add",
            "--title",
            "Prep launch brief",
            "--due",
            "next-monday",
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
            "Confirm venue",
            "--due",
            "+3d",
        ],
        &storage,
        &clock,
    );

    let review = run(
        &[
            "kelp",
            "review",
            "weekly",
            "--defer",
            "1:tomorrow",
            "--plan",
            "Launch:Ship launch checklist",
            "--json",
        ],
        &storage,
        &clock,
    );
    let review: Value = serde_json::from_str(&review).expect("review output should be valid JSON");
    assert_eq!(
        review["applied_actions"]
            .as_array()
            .expect("actions should be an array")
            .len(),
        2
    );

    let first_task = run(&["kelp", "task", "show", "1", "--json"], &storage, &clock);
    let first_task: Value =
        serde_json::from_str(&first_task).expect("task output should be valid JSON");
    assert_eq!(first_task["due_date"], "2026-03-15");

    let second_task = run(&["kelp", "task", "show", "2", "--json"], &storage, &clock);
    let second_task: Value =
        serde_json::from_str(&second_task).expect("task output should be valid JSON");
    assert_eq!(second_task["due_date"], "2026-03-17");

    let planned_task = run(&["kelp", "task", "show", "3", "--json"], &storage, &clock);
    let planned_task: Value =
        serde_json::from_str(&planned_task).expect("planned task output should be valid JSON");
    assert_eq!(planned_task["project"], "Launch");
    assert_eq!(planned_task["title"], "Ship launch checklist");
    assert_eq!(planned_task["tags"][0], "next-action");

    fs::remove_dir_all(storage_root).expect("storage cleanup should succeed");
}

#[test]
fn aliases_and_completion_generation_are_available() {
    let storage_root = temp_root("aliases");
    let storage = JsonFileStorage::at(storage_root.clone());
    let clock = FixedClock::new(date("2026-03-14"));

    run(
        &[
            "kelp",
            "task",
            "create",
            "--title",
            "Alias task",
            "--due",
            "today",
        ],
        &storage,
        &clock,
    );

    let list = run(&["kelp", "task", "ls", "--json"], &storage, &clock);
    let list: Value = serde_json::from_str(&list).expect("list output should be valid JSON");
    assert_eq!(list["tasks"][0]["title"], "Alias task");

    let bash = run(&["kelp", "completions", "bash"], &storage, &clock);
    assert!(bash.contains("complete -F _kelp kelp"));
    assert!(bash.contains("review"));

    fs::remove_dir_all(storage_root).expect("storage cleanup should succeed");
}
