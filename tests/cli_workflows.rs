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

fn run_json(args: &[&str], storage: &JsonFileStorage, clock: &FixedClock) -> Value {
    let output = run(args, storage, clock);
    let envelope: Value = serde_json::from_str(&output).expect("output should be valid JSON");
    envelope
        .get("data")
        .cloned()
        .expect("JSON output should include a data envelope")
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
    let imported = run_json(
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

    assert_eq!(imported["imported_tasks"], 2);
    assert_eq!(imported["imported_projects"], 1);
    assert_eq!(imported["skipped_duplicates"], 0);

    let reimported = run_json(
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
    assert_eq!(reimported["imported_tasks"], 0);
    assert_eq!(reimported["skipped_duplicates"], 2);

    let list = run_json(&["kelp", "task", "list", "--json"], &storage, &clock);
    assert_eq!(
        list["tasks"]
            .as_array()
            .expect("tasks should be an array")
            .len(),
        2
    );

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

    let backup = run_json(&["kelp", "storage", "backup", "--json"], &storage, &clock);
    assert!(PathBuf::from(
        backup["path"]
            .as_str()
            .expect("backup path should be a string")
    )
    .exists());

    let export_path = export_root.join("kelp-export.json");
    let exported = run_json(
        &[
            "kelp",
            "storage",
            "export",
            "--file",
            export_path.to_str().expect("export path should be UTF-8"),
            "--json",
        ],
        &storage,
        &clock,
    );
    assert_eq!(
        exported["path"]
            .as_str()
            .expect("export path should be a string"),
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

    let review = run_json(
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

    assert_eq!(
        review["applied_actions"]
            .as_array()
            .expect("actions should be an array")
            .len(),
        3
    );

    let task_one = run_json(&["kelp", "task", "show", "1", "--json"], &storage, &clock);
    assert_eq!(task_one["status"], "in_progress");

    let task_two = run_json(&["kelp", "task", "show", "2", "--json"], &storage, &clock);
    assert_eq!(task_two["due_date"], "2026-03-20");

    let task_three = run_json(&["kelp", "task", "show", "3", "--json"], &storage, &clock);
    assert_eq!(task_three["status"], "done");

    let task_four = run_json(&["kelp", "task", "show", "4", "--json"], &storage, &clock);
    assert_eq!(task_four["status"], "todo");

    fs::remove_dir_all(storage_root).expect("storage cleanup should succeed");
}

#[test]
fn archive_unarchive_and_bulk_edit_commands_work_together() {
    let storage_root = temp_root("task-actions");
    let storage = JsonFileStorage::at(storage_root.clone());
    let clock = FixedClock::new(date("2026-03-14"));

    run(
        &["kelp", "project", "add", "--name", "Launch"],
        &storage,
        &clock,
    );
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
    run(&["kelp", "task", "archive", "1"], &storage, &clock);
    run(&["kelp", "task", "unarchive", "1"], &storage, &clock);
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
    run(&["kelp", "project", "archive", "Launch"], &storage, &clock);
    run(
        &["kelp", "project", "unarchive", "Launch"],
        &storage,
        &clock,
    );

    let task = run_json(&["kelp", "task", "show", "1", "--json"], &storage, &clock);
    assert_eq!(task["priority"], "high");
    assert_eq!(task["due_date"], "2026-03-18");
    assert_eq!(
        task["tags"].as_array().expect("tags should be an array")[0],
        "launch"
    );

    let projects = run_json(&["kelp", "project", "list", "--json"], &storage, &clock);
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

    let config = run_json(&["kelp", "config", "show", "--json"], &storage, &clock);
    assert_eq!(config["default_upcoming_days"], 10);
    assert_eq!(config["default_task_sort"], "priority");
    assert_eq!(config["default_json_output"], true);

    let upcoming = run_json(&["kelp", "upcoming"], &storage, &clock);
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

    run(
        &["kelp", "project", "add", "--name", "Launch"],
        &storage,
        &clock,
    );
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

    let review = run_json(
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
    assert_eq!(
        review["applied_actions"]
            .as_array()
            .expect("actions should be an array")
            .len(),
        2
    );

    let first_task = run_json(&["kelp", "task", "show", "1", "--json"], &storage, &clock);
    assert_eq!(first_task["due_date"], "2026-03-15");

    let second_task = run_json(&["kelp", "task", "show", "2", "--json"], &storage, &clock);
    assert_eq!(second_task["due_date"], "2026-03-17");

    let planned_task = run_json(&["kelp", "task", "show", "3", "--json"], &storage, &clock);
    assert_eq!(planned_task["project"], "Launch");
    assert_eq!(planned_task["title"], "Ship launch checklist");
    assert_eq!(planned_task["status"], "next_action");
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

    let list = run_json(&["kelp", "task", "ls", "--json"], &storage, &clock);
    assert_eq!(list["tasks"][0]["title"], "Alias task");

    let bash = run(&["kelp", "completions", "bash"], &storage, &clock);
    assert!(bash.contains("_kelp()"));
    assert!(bash.contains("review"));
    assert!(bash.contains("next"));
    assert!(bash.contains("blocked"));

    fs::remove_dir_all(storage_root).expect("storage cleanup should succeed");
}

#[test]
fn explicit_next_wait_and_block_flows_are_available() {
    let storage_root = temp_root("status-flows");
    let storage = JsonFileStorage::at(storage_root.clone());
    let clock = FixedClock::new(date("2026-03-14"));

    run(
        &[
            "kelp",
            "task",
            "add",
            "--title",
            "Unclear task",
            "--due",
            "2026-03-20",
        ],
        &storage,
        &clock,
    );
    run(&["kelp", "task", "next", "1"], &storage, &clock);
    run(&["kelp", "task", "wait", "1"], &storage, &clock);
    run(&["kelp", "task", "block", "1"], &storage, &clock);

    let task = run_json(&["kelp", "task", "show", "1", "--json"], &storage, &clock);
    assert_eq!(task["status"], "blocked");

    let review = run_json(
        &["kelp", "review", "weekly", "--next-action", "1", "--json"],
        &storage,
        &clock,
    );
    assert!(review["sections"]
        .as_array()
        .expect("sections should be an array")
        .iter()
        .any(|section| section["name"] == "Next actions"));

    let task = run_json(&["kelp", "task", "show", "1", "--json"], &storage, &clock);
    assert_eq!(task["status"], "next_action");

    fs::remove_dir_all(storage_root).expect("storage cleanup should succeed");
}

#[test]
fn deadlines_and_blocker_metadata_roundtrip_through_the_cli() {
    let storage_root = temp_root("planner-metadata");
    let storage = JsonFileStorage::at(storage_root.clone());
    let clock = FixedClock::new(date("2026-03-14"));

    run(
        &[
            "kelp",
            "project",
            "add",
            "--name",
            "Launch",
            "--description",
            "Ship the next release",
            "--deadline",
            "2026-03-18",
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
            "Wait on design",
            "--project",
            "Launch",
            "--wait-until",
            "2026-03-16",
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
            "Blocked release",
            "--project",
            "Launch",
            "--blocked-reason",
            "Vendor API outage",
        ],
        &storage,
        &clock,
    );
    run(
        &["kelp", "task", "wait", "1", "--until", "2026-03-17"],
        &storage,
        &clock,
    );
    run(
        &[
            "kelp",
            "task",
            "block",
            "2",
            "--reason",
            "Waiting on legal sign-off",
        ],
        &storage,
        &clock,
    );
    run(
        &[
            "kelp",
            "project",
            "edit",
            "Launch",
            "--deadline",
            "2026-03-20",
        ],
        &storage,
        &clock,
    );

    let waiting_task = run_json(&["kelp", "task", "show", "1", "--json"], &storage, &clock);
    assert_eq!(waiting_task["status"], "waiting");
    assert_eq!(waiting_task["waiting_until"], "2026-03-17");

    let blocked_task = run_json(&["kelp", "task", "show", "2", "--json"], &storage, &clock);
    assert_eq!(blocked_task["status"], "blocked");
    assert_eq!(blocked_task["blocked_reason"], "Waiting on legal sign-off");

    let project = run_json(
        &["kelp", "project", "show", "Launch", "--json"],
        &storage,
        &clock,
    );
    assert_eq!(project["project"]["deadline"], "2026-03-20");

    let review = run_json(&["kelp", "review", "weekly", "--json"], &storage, &clock);
    assert!(review["deadline_projects"]
        .as_array()
        .expect("deadline projects should be an array")
        .iter()
        .any(|project| project["name"] == "Launch"));

    let bash = run(&["kelp", "completions", "bash"], &storage, &clock);
    assert!(bash.contains("--wait-until"));
    assert!(bash.contains("--blocked-reason"));
    assert!(bash.contains("edit"));

    fs::remove_dir_all(storage_root).expect("storage cleanup should succeed");
}

#[test]
fn task_dependencies_drive_review_risk_and_clear_after_completion() {
    let storage_root = temp_root("task-dependencies");
    let storage = JsonFileStorage::at(storage_root.clone());
    let clock = FixedClock::new(date("2026-03-14"));

    run(
        &[
            "kelp",
            "project",
            "add",
            "--name",
            "Launch",
            "--deadline",
            "2026-03-18",
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
            "Prepare assets",
            "--project",
            "Launch",
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
            "Publish assets",
            "--project",
            "Launch",
            "--due",
            "2026-03-16",
            "--depends-on",
            "1",
        ],
        &storage,
        &clock,
    );

    let dependent = run_json(&["kelp", "task", "show", "2", "--json"], &storage, &clock);
    assert_eq!(
        dependent["depends_on"]
            .as_array()
            .expect("depends_on should be an array")[0],
        1
    );
    assert_eq!(
        dependent["unresolved_dependencies"]
            .as_array()
            .expect("unresolved deps should be an array")[0],
        1
    );

    let review = run_json(&["kelp", "review", "weekly", "--json"], &storage, &clock);
    assert!(review["sections"]
        .as_array()
        .expect("sections should be an array")
        .iter()
        .any(|section| {
            section["name"] == "Blocked by dependencies"
                && section["tasks"]
                    .as_array()
                    .expect("tasks should be an array")
                    .iter()
                    .any(|task| task["title"] == "Publish assets")
        }));
    assert!(review["at_risk_projects"]
        .as_array()
        .expect("at risk projects should be an array")
        .iter()
        .any(|project| project["name"] == "Launch"));

    run(&["kelp", "task", "done", "1"], &storage, &clock);

    let dependent = run_json(&["kelp", "task", "show", "2", "--json"], &storage, &clock);
    assert!(dependent["unresolved_dependencies"]
        .as_array()
        .expect("unresolved deps should be an array")
        .is_empty());

    fs::remove_dir_all(storage_root).expect("storage cleanup should succeed");
}

#[test]
fn project_next_actions_are_canonicalized_and_weekly_review_splits_project_signals() {
    let storage_root = temp_root("canonical-next-actions");
    let storage = JsonFileStorage::at(storage_root.clone());
    let clock = FixedClock::new(date("2026-03-14"));

    run(
        &[
            "kelp",
            "project",
            "add",
            "--name",
            "Launch",
            "--deadline",
            "2026-03-18",
        ],
        &storage,
        &clock,
    );
    run(
        &[
            "kelp",
            "project",
            "add",
            "--name",
            "Docs",
            "--deadline",
            "2026-03-20",
        ],
        &storage,
        &clock,
    );
    run(
        &[
            "kelp",
            "project",
            "add",
            "--name",
            "Ops",
            "--deadline",
            "2026-03-19",
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
            "Draft copy",
            "--project",
            "Launch",
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
            "Publish copy",
            "--project",
            "Launch",
            "--due",
            "2026-03-16",
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
            "Write runbook",
            "--project",
            "Docs",
            "--due",
            "2026-03-17",
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
            "Lead incident cleanup",
            "--project",
            "Ops",
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
            "Wait for vendor RCA",
            "--project",
            "Ops",
            "--due",
            "2026-03-16",
        ],
        &storage,
        &clock,
    );

    run(&["kelp", "task", "next", "1"], &storage, &clock);
    run(&["kelp", "task", "next", "2"], &storage, &clock);
    run(&["kelp", "task", "next", "4"], &storage, &clock);
    run(
        &[
            "kelp",
            "task",
            "block",
            "5",
            "--reason",
            "Vendor RCA still pending",
        ],
        &storage,
        &clock,
    );

    let first_launch_task = run_json(&["kelp", "task", "show", "1", "--json"], &storage, &clock);
    assert_eq!(first_launch_task["status"], "todo");

    let second_launch_task = run_json(&["kelp", "task", "show", "2", "--json"], &storage, &clock);
    assert_eq!(second_launch_task["status"], "next_action");

    let review = run_json(&["kelp", "review", "weekly", "--json"], &storage, &clock);
    assert!(review["projects_without_next_actions"]
        .as_array()
        .expect("projects without next actions should be an array")
        .iter()
        .any(|project| project["name"] == "Docs"));
    assert!(!review["projects_without_next_actions"]
        .as_array()
        .expect("projects without next actions should be an array")
        .iter()
        .any(|project| project["name"] == "Launch"));
    assert!(review["stalled_projects"]
        .as_array()
        .expect("stalled projects should be an array")
        .iter()
        .any(|project| project["name"] == "Ops"));

    fs::remove_dir_all(storage_root).expect("storage cleanup should succeed");
}
