use super::session::{
    PtyError, PtyEvent, PtyExitStatus, PtyId, PtyOutputSnapshot, PtySession, PtySpawnRequest,
    PtyStore, ShellMode,
};
use bytes::Bytes;
use portable_pty::{native_pty_system, CommandBuilder, PtySize};
use std::{
    collections::HashMap,
    env, fs,
    io::{Read, Write},
    path::{Path, PathBuf},
    sync::Arc,
    time::Duration,
};
use tokio::{
    sync::{broadcast, mpsc},
    task, time,
};
use tracing::{debug, info, warn};
use uuid::Uuid;

type Result<T> = std::result::Result<T, PtyError>;

pub struct PtyManager {
    sessions: PtyStore,
}

impl PtyManager {
    pub fn new() -> Arc<Self> {
        Arc::new(Self {
            sessions: Arc::new(parking_lot::RwLock::new(HashMap::new())),
        })
    }

    pub async fn spawn(&self, req: PtySpawnRequest) -> Result<PtyId> {
        let id = Uuid::new_v4();
        let rows = req.rows.max(1);
        let cols = req.cols.max(1);
        let pty_system = native_pty_system();
        let pair = pty_system.openpty(PtySize {
            rows,
            cols,
            pixel_width: 0,
            pixel_height: 0,
        })?;
        let apply_shell_mode = req.agent.as_deref() == Some("shell") && req.args.is_empty();
        let shell_mode = req.shell_mode;
        let command = if req.command.trim().is_empty() {
            crate::util::shell::default_shell()
        } else {
            req.command
        };
        let mut args = req.args;
        let mut env_values = req.env;
        let shell_integration_dir =
            configure_shell_integration(id, &command, &mut args, &mut env_values)?;
        if apply_shell_mode && shell_mode == ShellMode::Login {
            apply_login_shell_mode(&command, &mut args);
        }
        let mut cmd = CommandBuilder::new(&command);
        cmd.args(args.iter());
        cmd.env("TERM", "xterm-256color");
        cmd.env("COLORTERM", "truecolor");
        if let Some(cwd) = req.cwd {
            cmd.cwd(cwd.as_os_str());
        }
        for (key, value) in env_values {
            if key == "ONIBI_SHELL_INTEGRATION" {
                continue;
            }
            cmd.env(key, value);
        }
        let mut reader = pair.master.try_clone_reader()?;
        let writer = pair.master.take_writer()?;
        let child = pair.slave.spawn_command(cmd)?;
        let (tx, _) = broadcast::channel(1024);
        let session = PtySession::new(id, pair.master, child, writer, tx);
        self.sessions.write().insert(id, session.clone());
        self.spawn_reader(session.clone(), &mut reader);
        self.spawn_waiter(session.clone(), shell_integration_dir);
        info!(%id, command, rows, cols, "spawned pty");
        Ok(id)
    }

    pub async fn write(&self, id: PtyId, data: &[u8]) -> Result<()> {
        let session = self.session(id)?;
        if session.is_terminated() {
            return Err(PtyError::Terminated(id));
        }
        debug!(%id, bytes = data.len(), "pty write");
        let writer = session.writer();
        let payload = Bytes::copy_from_slice(data);
        task::spawn_blocking(move || {
            let handle = tokio::runtime::Handle::current();
            handle.block_on(async {
                let mut writer = writer.lock().await;
                writer.write_all(&payload)?;
                writer.flush()
            })
        })
        .await??;
        Ok(())
    }

    pub async fn resize(&self, id: PtyId, rows: u16, cols: u16) -> Result<()> {
        let session = self.session(id)?;
        if session.is_terminated() {
            return Err(PtyError::Terminated(id));
        }
        let rows = rows.max(1);
        let cols = cols.max(1);
        debug!(%id, rows, cols, "pty resize");
        let master = session.master();
        task::spawn_blocking(move || {
            master.lock().resize(PtySize {
                rows,
                cols,
                pixel_width: 0,
                pixel_height: 0,
            })
        })
        .await??;
        Ok(())
    }

    pub async fn kill(&self, id: PtyId) -> Result<()> {
        let session = self.session(id)?;
        if session.is_terminated() {
            return Ok(());
        }
        debug!(%id, "pty kill");
        let child = session.child();
        task::spawn_blocking(move || child.lock().kill()).await??;
        Ok(())
    }

    pub fn subscribe(&self, id: PtyId) -> Result<broadcast::Receiver<PtyEvent>> {
        Ok(self.session(id)?.subscribe())
    }

    pub fn output_snapshot(&self, id: PtyId) -> Result<PtyOutputSnapshot> {
        Ok(self.session(id)?.output_snapshot())
    }

    pub fn process_id(&self, id: PtyId) -> Result<Option<u32>> {
        Ok(self.session(id)?.child_process_id())
    }

    pub async fn list(&self) -> Vec<PtyId> {
        self.sessions
            .read()
            .iter()
            .filter_map(|(id, session)| (!session.is_terminated()).then_some(*id))
            .collect()
    }

    fn session(&self, id: PtyId) -> Result<PtySession> {
        self.sessions
            .read()
            .get(&id)
            .cloned()
            .ok_or(PtyError::NotFound(id))
    }

    fn spawn_reader(&self, session: PtySession, reader: &mut Box<dyn Read + Send>) {
        let id = session.id();
        let tx = session.sender();
        let mut reader = std::mem::replace(reader, Box::new(std::io::empty()));
        let (chunk_tx, mut chunk_rx) = mpsc::unbounded_channel::<Bytes>();
        task::spawn_blocking(move || {
            let mut buf = [0_u8; 8192];
            loop {
                match reader.read(&mut buf) {
                    Ok(0) => break,
                    Ok(n) => {
                        if chunk_tx.send(Bytes::copy_from_slice(&buf[..n])).is_err() {
                            break;
                        }
                    }
                    Err(err) => {
                        debug!(%id, %err, "pty reader stopped");
                        break;
                    }
                }
            }
        });
        tokio::spawn(async move {
            let mut pending = Vec::with_capacity(64 * 1024);
            let mut flush = time::interval(Duration::from_millis(16));
            flush.set_missed_tick_behavior(time::MissedTickBehavior::Skip);
            let mut osc_parser = super::notifications::OscNotificationParser::new();

            loop {
                tokio::select! {
                    chunk = chunk_rx.recv() => match chunk {
                        Some(bytes) => {
                            for event in osc_parser.feed(&bytes) {
                                let _ = tx.send(PtyEvent::Notification(event));
                            }
                            pending.extend_from_slice(&bytes);
                            if pending.len() >= 64 * 1024 {
                                broadcast_pending(&session, &tx, &mut pending);
                            }
                        }
                        None => {
                            broadcast_pending(&session, &tx, &mut pending);
                            break;
                        }
                    },
                    _ = flush.tick() => broadcast_pending(&session, &tx, &mut pending),
                }
            }
        });
    }

    fn spawn_waiter(&self, session: PtySession, shell_integration_dir: Option<PathBuf>) {
        let id = session.id();
        let child = session.child();
        let tx = session.sender();
        tokio::spawn(async move {
            loop {
                let child = child.clone();
                match task::spawn_blocking(move || child.lock().try_wait()).await {
                    Ok(Ok(Some(status))) => {
                        let exit = PtyExitStatus::from(status);
                        session.set_terminated(exit.clone());
                        info!(%id, code = exit.code, signal = ?exit.signal, "pty exited");
                        let _ = tx.send(PtyEvent::Exit(exit));
                        break;
                    }
                    Ok(Ok(None)) => time::sleep(Duration::from_millis(20)).await,
                    Ok(Err(err)) => {
                        warn!(%id, %err, "pty wait failed");
                        let exit = PtyExitStatus {
                            code: 1,
                            signal: Some(format!("wait error: {err}")),
                        };
                        session.set_terminated(exit.clone());
                        let _ = tx.send(PtyEvent::Exit(exit));
                        break;
                    }
                    Err(err) => {
                        warn!(%id, %err, "pty wait task failed");
                        let exit = PtyExitStatus {
                            code: 1,
                            signal: Some(format!("wait task error: {err}")),
                        };
                        session.set_terminated(exit.clone());
                        let _ = tx.send(PtyEvent::Exit(exit));
                        break;
                    }
                }
            }
            if let Some(path) = shell_integration_dir {
                let _ = fs::remove_dir_all(path);
            }
        });
    }
}

fn env_flag(env_values: &[(String, String)], key: &str) -> bool {
    env_values
        .iter()
        .any(|(env_key, value)| env_key == key && value != "0" && value != "false")
}

fn shell_name(command: &str) -> Option<String> {
    Path::new(command)
        .file_name()
        .and_then(|name| name.to_str())
        .map(ToOwned::to_owned)
}

fn login_shell_flag(command: &str) -> Option<&'static str> {
    match shell_name(command)?.as_str() {
        "bash" | "fish" => Some("--login"),
        "zsh" => Some("-l"),
        _ => None,
    }
}

fn apply_login_shell_mode(command: &str, args: &mut Vec<String>) -> bool {
    let Some(flag) = login_shell_flag(command) else {
        warn!(command, "login shell mode requested for unsupported shell");
        return false;
    };
    if !args.iter().any(|arg| arg == flag) {
        args.insert(0, flag.to_string());
    }
    true
}

fn shell_integration_root(id: PtyId) -> PathBuf {
    env::temp_dir().join(format!("onibi-shell-{id}"))
}

fn write_zsh_integration(root: &Path) -> std::io::Result<()> {
    fs::create_dir_all(root)?;
    fs::write(
        root.join(".zshenv"),
        r#"if [ -r "$HOME/.zshenv" ]; then
  source "$HOME/.zshenv"
fi
"#,
    )?;
    fs::write(
        root.join(".zshrc"),
        r#"if [ -r "$HOME/.zshrc" ]; then
  source "$HOME/.zshrc"
fi

autoload -Uz compinit
zstyle ':completion:*' menu select
mkdir -p "${XDG_CACHE_HOME:-$HOME/.cache}/zsh" 2>/dev/null
compinit -d "${XDG_CACHE_HOME:-$HOME/.cache}/zsh/zcompdump-onibi-${ZSH_VERSION}" 2>/dev/null

for plugin in \
  "${HOMEBREW_PREFIX:-}/share/zsh-autosuggestions/zsh-autosuggestions.zsh" \
  /opt/homebrew/share/zsh-autosuggestions/zsh-autosuggestions.zsh \
  /usr/local/share/zsh-autosuggestions/zsh-autosuggestions.zsh \
  "$HOME/.zsh/zsh-autosuggestions/zsh-autosuggestions.zsh" \
  "$HOME/.oh-my-zsh/custom/plugins/zsh-autosuggestions/zsh-autosuggestions.zsh"; do
  if [ -r "$plugin" ]; then
    source "$plugin"
    break
  fi
done

function _onibi_precmd() {
  local status="$?"
  printf '\033]133;D;%s\007\033]7;file://%s%s\007\033]133;A\007' "$status" "${HOST:-localhost}" "$PWD"
}

function _onibi_preexec() {
  printf '\033]133;C;%s\007' "$1"
}

precmd_functions+=(_onibi_precmd)
preexec_functions+=(_onibi_preexec)
"#,
    )
}

fn write_bash_integration(root: &Path) -> std::io::Result<PathBuf> {
    fs::create_dir_all(root)?;
    let rcfile = root.join("bashrc");
    fs::write(
        &rcfile,
        r#"if [ -r "$HOME/.bashrc" ]; then
  source "$HOME/.bashrc"
fi

for completion in \
  "${HOMEBREW_PREFIX:-}/etc/profile.d/bash_completion.sh" \
  /opt/homebrew/etc/profile.d/bash_completion.sh \
  /usr/local/etc/profile.d/bash_completion.sh \
  /etc/bash_completion; do
  if [ -r "$completion" ]; then
    source "$completion"
    break
  fi
done

bind 'set show-all-if-ambiguous on'
bind 'TAB:menu-complete'

__onibi_prompt_command() {
  local status="$?"
  printf '\033]133;D;%s\a\033]7;file://%s%s\a\033]133;A\a' "$status" "${HOSTNAME:-localhost}" "$PWD"
}

if [ -n "${PROMPT_COMMAND:-}" ]; then
  PROMPT_COMMAND="__onibi_prompt_command; $PROMPT_COMMAND"
else
  PROMPT_COMMAND="__onibi_prompt_command"
fi

trap 'printf "\033]133;C;%s\a" "$BASH_COMMAND"' DEBUG
"#,
    )?;
    Ok(rcfile)
}

fn write_fish_integration(root: &Path) -> std::io::Result<()> {
    let fish_dir = root.join("fish");
    fs::create_dir_all(&fish_dir)?;
    fs::write(
        fish_dir.join("config.fish"),
        r#"if test -r "$HOME/.config/fish/config.fish"
  source "$HOME/.config/fish/config.fish"
end

function __onibi_prompt --on-event fish_prompt
  set -l code $status
  printf '\033]133;D;%s\a\033]7;file://%s%s\a\033]133;A\a' "$code" (hostname) (pwd)
end

function __onibi_preexec --on-event fish_preexec
  printf '\033]133;C;%s\a' "$argv"
end
"#,
    )
}

fn configure_shell_integration(
    id: PtyId,
    command: &str,
    args: &mut Vec<String>,
    env_values: &mut Vec<(String, String)>,
) -> std::io::Result<Option<PathBuf>> {
    if !env_flag(env_values, "ONIBI_SHELL_INTEGRATION") || !args.is_empty() {
        return Ok(None);
    }
    let Some(shell) = shell_name(command) else {
        return Ok(None);
    };
    let root = shell_integration_root(id);
    match shell.as_str() {
        "zsh" => {
            write_zsh_integration(&root)?;
            env_values.push(("ZDOTDIR".to_string(), root.display().to_string()));
            Ok(Some(root))
        }
        "bash" => {
            let rcfile = write_bash_integration(&root)?;
            args.push("--rcfile".to_string());
            args.push(rcfile.display().to_string());
            args.push("-i".to_string());
            Ok(Some(root))
        }
        "fish" => {
            write_fish_integration(&root)?;
            env_values.push(("XDG_CONFIG_HOME".to_string(), root.display().to_string()));
            Ok(Some(root))
        }
        _ => Ok(None),
    }
}

fn broadcast_pending(
    session: &PtySession,
    tx: &broadcast::Sender<PtyEvent>,
    pending: &mut Vec<u8>,
) {
    if pending.is_empty() {
        return;
    }
    let bytes = Bytes::copy_from_slice(pending);
    let offset = session.append_output(&bytes);
    let _ = tx.send(PtyEvent::Data { bytes, offset });
    pending.clear();
}

#[cfg(test)]
mod tests {
    use super::*;
    use tokio::time::{timeout, Instant};

    async fn collect_until_exit(
        rx: &mut broadcast::Receiver<PtyEvent>,
        max_wait: Duration,
    ) -> (Vec<u8>, PtyExitStatus) {
        let deadline = Instant::now() + max_wait;
        let mut output = Vec::new();
        loop {
            let now = Instant::now();
            assert!(now < deadline, "timed out waiting for pty exit");
            let remaining = deadline - now;
            match timeout(remaining, rx.recv()).await.unwrap().unwrap() {
                PtyEvent::Data { bytes, .. } => output.extend_from_slice(&bytes),
                PtyEvent::Exit(status) => return (output, status),
                PtyEvent::Notification(_) => {}
            }
        }
    }

    async fn recv_until_contains(
        rx: &mut broadcast::Receiver<PtyEvent>,
        needle: &[u8],
        max_wait: Duration,
    ) -> Vec<u8> {
        let deadline = Instant::now() + max_wait;
        let mut output = Vec::new();
        loop {
            let now = Instant::now();
            assert!(now < deadline, "timed out waiting for pty output");
            let remaining = deadline - now;
            if let PtyEvent::Data { bytes, .. } =
                timeout(remaining, rx.recv()).await.unwrap().unwrap()
            {
                output.extend_from_slice(&bytes);
                if output.windows(needle.len()).any(|window| window == needle) {
                    return output;
                }
            }
        }
    }

    fn sh_req(script: &str) -> PtySpawnRequest {
        PtySpawnRequest {
            command: "/bin/sh".to_string(),
            args: vec!["-c".to_string(), script.to_string()],
            cwd: None,
            env: vec![],
            shell_mode: ShellMode::Auto,
            rows: 24,
            cols: 80,
            name: None,
            agent: None,
            workspace_id: None,
            title: None,
            remote: None,
        }
    }

    #[test]
    fn login_shell_mode_prepends_known_shell_flags() {
        let mut bash_args = vec!["--rcfile".to_string(), "/tmp/onibi-bashrc".to_string()];
        assert!(apply_login_shell_mode("/bin/bash", &mut bash_args));
        assert_eq!(bash_args[0], "--login");
        assert_eq!(bash_args[1], "--rcfile");

        let mut zsh_args = Vec::new();
        assert!(apply_login_shell_mode("/bin/zsh", &mut zsh_args));
        assert_eq!(zsh_args, vec!["-l"]);

        let mut fish_args = Vec::new();
        assert!(apply_login_shell_mode(
            "/usr/local/bin/fish",
            &mut fish_args
        ));
        assert_eq!(fish_args, vec!["--login"]);
    }

    #[test]
    fn login_shell_mode_ignores_unsupported_shells() {
        let mut args = Vec::new();

        assert!(!apply_login_shell_mode("/bin/sh", &mut args));
        assert!(args.is_empty());
    }

    #[tokio::test]
    async fn spawn_echo_collects_output_and_exit() {
        let manager = PtyManager::new();
        let id = manager.spawn(sh_req("echo hello")).await.unwrap();
        let mut rx = manager.subscribe(id).unwrap();
        let (output, status) = collect_until_exit(&mut rx, Duration::from_secs(2)).await;
        assert!(String::from_utf8_lossy(&output).contains("hello"));
        assert_eq!(status.code, 0);
    }

    #[tokio::test]
    async fn cat_echoes_written_input() {
        let manager = PtyManager::new();
        let id = manager
            .spawn(PtySpawnRequest {
                command: "/bin/cat".to_string(),
                args: vec![],
                cwd: None,
                env: vec![],
                shell_mode: ShellMode::Auto,
                rows: 24,
                cols: 80,
                name: None,
                agent: None,
                workspace_id: None,
                title: None,
                remote: None,
            })
            .await
            .unwrap();
        let mut rx = manager.subscribe(id).unwrap();
        manager.write(id, b"ping\n").await.unwrap();
        let output = recv_until_contains(&mut rx, b"ping", Duration::from_secs(2)).await;
        assert!(String::from_utf8_lossy(&output).contains("ping"));
        manager.kill(id).await.unwrap();
    }

    #[tokio::test]
    async fn sleep_can_be_killed() {
        let manager = PtyManager::new();
        let id = manager
            .spawn(PtySpawnRequest {
                command: "/bin/sleep".to_string(),
                args: vec!["10".to_string()],
                cwd: None,
                env: vec![],
                shell_mode: ShellMode::Auto,
                rows: 24,
                cols: 80,
                name: None,
                agent: None,
                workspace_id: None,
                title: None,
                remote: None,
            })
            .await
            .unwrap();
        let mut rx = manager.subscribe(id).unwrap();
        manager.kill(id).await.unwrap();
        let (_, status) = collect_until_exit(&mut rx, Duration::from_secs(1)).await;
        assert_ne!(status.code, 0);
    }

    #[tokio::test]
    async fn resize_active_pty() {
        let manager = PtyManager::new();
        let id = manager.spawn(sh_req("sleep 1")).await.unwrap();
        manager.resize(id, 40, 120).await.unwrap();
        manager.kill(id).await.unwrap();
    }

    #[tokio::test]
    async fn resize_is_visible_to_child_process() {
        let manager = PtyManager::new();
        let id = manager.spawn(sh_req("read line; stty size")).await.unwrap();
        let mut rx = manager.subscribe(id).unwrap();
        manager.resize(id, 41, 123).await.unwrap();
        manager.write(id, b"\n").await.unwrap();

        let (output, status) = collect_until_exit(&mut rx, Duration::from_secs(2)).await;

        assert_eq!(status.code, 0);
        assert!(
            String::from_utf8_lossy(&output).contains("41 123"),
            "expected resized pty dimensions in output, got {:?}",
            String::from_utf8_lossy(&output),
        );
    }

    #[tokio::test]
    async fn integration_receives_output_and_exit_code() {
        let manager = PtyManager::new();
        let id = manager
            .spawn(sh_req("echo hi; sleep 0.1; exit 7"))
            .await
            .unwrap();
        let mut rx = manager.subscribe(id).unwrap();
        let (output, status) = collect_until_exit(&mut rx, Duration::from_secs(2)).await;
        assert!(String::from_utf8_lossy(&output).contains("hi"));
        assert_eq!(status.code, 7);
    }

    #[tokio::test]
    async fn output_snapshot_replays_data_emitted_before_subscribe() {
        let manager = PtyManager::new();
        let id = manager
            .spawn(sh_req("printf early; sleep 0.2"))
            .await
            .unwrap();
        let deadline = Instant::now() + Duration::from_secs(2);
        loop {
            let snapshot = manager.output_snapshot(id).unwrap();
            if String::from_utf8_lossy(&snapshot.data).contains("early") {
                manager.kill(id).await.unwrap();
                return;
            }
            assert!(Instant::now() < deadline, "timed out waiting for replay");
            time::sleep(Duration::from_millis(10)).await;
        }
    }

    #[tokio::test]
    async fn subscribe_before_snapshot_preserves_live_and_replay_offsets() {
        let manager = PtyManager::new();
        let id = manager
            .spawn(sh_req("printf early; sleep 0.05; printf late"))
            .await
            .unwrap();
        let mut rx = manager.subscribe(id).unwrap();
        let deadline = Instant::now() + Duration::from_secs(2);
        let snapshot = loop {
            let snapshot = manager.output_snapshot(id).unwrap();
            if String::from_utf8_lossy(&snapshot.data).contains("early") {
                break snapshot;
            }
            assert!(Instant::now() < deadline, "timed out waiting for replay");
            time::sleep(Duration::from_millis(10)).await;
        };
        let replay_end = snapshot.end_offset;
        let mut live_offsets = Vec::new();
        let mut live_output = Vec::new();

        loop {
            match timeout(Duration::from_secs(2), rx.recv())
                .await
                .unwrap()
                .unwrap()
            {
                PtyEvent::Data { bytes, offset } => {
                    live_offsets.push(offset);
                    live_output.extend_from_slice(&bytes);
                }
                PtyEvent::Exit(status) => {
                    assert_eq!(status.code, 0);
                    break;
                }
                PtyEvent::Notification(_) => {}
            }
        }

        assert!(
            String::from_utf8_lossy(&live_output).contains("late"),
            "expected live output after snapshot, got {:?}",
            String::from_utf8_lossy(&live_output),
        );
        assert!(
            live_offsets.windows(2).all(|pair| pair[0] <= pair[1]),
            "live offsets should be monotonic: {live_offsets:?}",
        );
        assert!(
            live_offsets.iter().any(|offset| *offset <= replay_end),
            "subscription should be allowed to overlap replay end for frontend dedupe",
        );
    }
}
