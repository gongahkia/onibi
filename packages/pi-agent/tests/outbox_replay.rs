use std::fs;
use std::path::{Path, PathBuf};
use std::process::Command;
use std::time::{SystemTime, UNIX_EPOCH};

use kelp_pi_agent::{PiWireEnvelope, REQUIRED_DATA_DIRS};

#[test]
fn outbox_replay_sends_queued_envelopes_in_order() {
    let root = temp_root("outbox-replay");
    create_layout(&root);
    run_agent(&root, &["keygen"]);

    for seq in 1..=3 {
        run_agent(
            &root,
            &[
                "outbox",
                "enqueue",
                "--kind",
                "evidence.append",
                "--msg-id",
                &format!("replay-{seq:03}"),
                "--payload-json",
                &format!(r#"{{"seq":{seq}}}"#),
            ],
        );
    }

    let replay = run_agent(&root, &["outbox", "replay"]);
    let envelopes = replay
        .stdout
        .lines()
        .map(|line| serde_json::from_str::<PiWireEnvelope>(line).expect("replay envelope"))
        .collect::<Vec<_>>();
    assert_eq!(envelopes.len(), 3);
    assert_eq!(
        envelopes
            .iter()
            .map(|envelope| envelope.msg_id.as_str())
            .collect::<Vec<_>>(),
        vec!["replay-001", "replay-002", "replay-003"]
    );
    assert_eq!(envelopes[0].payload["seq"], 1);
    assert_eq!(envelopes[1].payload["seq"], 2);
    assert_eq!(envelopes[2].payload["seq"], 3);

    let second_replay = run_agent(&root, &["outbox", "replay"]);
    assert!(second_replay.stdout.trim().is_empty());
    let sent = fs::read_dir(root.join("outbox").join("sent"))
        .expect("sent dir")
        .count();
    assert_eq!(sent, 3);

    fs::remove_dir_all(root).ok();
}

struct AgentOutput {
    stdout: String,
}

fn run_agent(root: &Path, args: &[&str]) -> AgentOutput {
    let mut command_args = Vec::from(args);
    command_args.extend(["--data-dir", root.to_str().expect("temp path utf8")]);
    let output = Command::new(env!("CARGO_BIN_EXE_kelp-pi-agent"))
        .args(command_args)
        .output()
        .expect("run agent");
    assert!(
        output.status.success(),
        "agent failed\nstdout:\n{}\nstderr:\n{}",
        String::from_utf8_lossy(&output.stdout),
        String::from_utf8_lossy(&output.stderr)
    );
    AgentOutput {
        stdout: String::from_utf8(output.stdout).expect("stdout utf8"),
    }
}

fn create_layout(root: &Path) {
    fs::create_dir_all(root).expect("create root");
    for name in REQUIRED_DATA_DIRS {
        fs::create_dir_all(root.join(name)).expect("create child");
    }
}

fn temp_root(name: &str) -> PathBuf {
    let nonce = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .expect("time after epoch")
        .as_nanos();
    std::env::temp_dir().join(format!("kelp-pi-{name}-{}-{nonce}", std::process::id()))
}
