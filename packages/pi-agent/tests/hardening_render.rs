use std::fs;
#[cfg(unix)]
use std::os::unix::fs::PermissionsExt;
use std::path::PathBuf;
use std::process::Command;
use std::time::{SystemTime, UNIX_EPOCH};

#[test]
fn hardening_render_network_writes_reference_artifacts() {
    let root = temp_root("hardening-render");

    let output = Command::new(env!("CARGO_BIN_EXE_kelp-pi-agent"))
        .args([
            "hardening",
            "render-network",
            "--output",
            root.to_str().expect("temp path utf8"),
            "--wpa3-passphrase",
            "correct-horse-battery",
            "--allow-outbound",
            "cp.example.test:443",
        ])
        .output()
        .expect("run hardening renderer");

    assert!(
        output.status.success(),
        "stderr={}",
        String::from_utf8_lossy(&output.stderr)
    );
    let nm = fs::read_to_string(
        root.join("etc/NetworkManager/system-connections/kelp-pi-ap.nmconnection"),
    )
    .expect("read nmconnection");
    assert!(nm.contains("key-mgmt=sae"));
    assert!(nm.contains("ap-isolation=1"));
    let dnsmasq =
        fs::read_to_string(root.join("etc/dnsmasq.d/kelp-pi-captive.conf")).expect("dnsmasq");
    assert!(dnsmasq.contains("no-resolv"));
    assert!(dnsmasq.contains("address=/clients3.google.com/10.42.0.1"));
    let nft = fs::read_to_string(root.join("etc/nftables.d/kelp-pi.nft")).expect("nft");
    assert!(nft.contains("policy drop;"));
    assert!(nft.contains("ip daddr cp.example.test tcp dport 443 accept"));
    let config = fs::read_to_string(root.join("etc/kelp-pi/network-hardening.json"))
        .expect("read runtime config");
    assert!(config.contains("\"cp.example.test:443\""));

    fs::remove_dir_all(root).ok();
}

#[cfg(unix)]
#[test]
fn hardening_apply_network_loads_config_and_feeds_nft_stdin() {
    let root = temp_root("hardening-apply");
    fs::create_dir_all(&root).expect("create temp");
    let config = root.join("network-hardening.json");
    fs::write(
        &config,
        r#"{"ap_interface":"wlan0","allow_outbound":["cp.example.test:443"]}"#,
    )
    .expect("write config");
    let nft_stdin = root.join("nft.stdin");
    let fake_nft = root.join("fake-nft.sh");
    fs::write(
        &fake_nft,
        "#!/usr/bin/env sh\ntest \"$1\" = \"-f\"\ntest \"$2\" = \"-\"\ncat > \"$KELP_PI_FAKE_NFT_STDIN\"\n",
    )
    .expect("write fake nft");
    let mut permissions = fs::metadata(&fake_nft)
        .expect("fake nft metadata")
        .permissions();
    permissions.set_mode(0o755);
    fs::set_permissions(&fake_nft, permissions).expect("chmod fake nft");

    let output = Command::new(env!("CARGO_BIN_EXE_kelp-pi-agent"))
        .args([
            "hardening",
            "apply-network",
            "--config",
            config.to_str().expect("config path utf8"),
            "--nft-bin",
            fake_nft.to_str().expect("fake nft path utf8"),
        ])
        .env("KELP_PI_FAKE_NFT_STDIN", &nft_stdin)
        .output()
        .expect("run hardening apply");

    assert!(
        output.status.success(),
        "stderr={}",
        String::from_utf8_lossy(&output.stderr)
    );
    let applied = fs::read_to_string(nft_stdin).expect("read nft stdin");
    assert!(applied.contains("flush table inet kelp_pi_filter"));
    assert!(applied.contains("ct state established,related accept"));
    assert!(applied.contains("ip daddr cp.example.test tcp dport 443 accept"));

    fs::remove_dir_all(root).ok();
}

#[cfg(unix)]
#[test]
fn hardening_apply_scanner_targets_feeds_nft_stdin() {
    let root = temp_root("hardening-scanner-targets");
    fs::create_dir_all(&root).expect("create temp");
    let nft_stdin = root.join("nft.stdin");
    let fake_nft = root.join("fake-nft.sh");
    fs::write(
        &fake_nft,
        "#!/usr/bin/env sh\ntest \"$1\" = \"-f\"\ntest \"$2\" = \"-\"\ncat > \"$KELP_PI_FAKE_NFT_STDIN\"\n",
    )
    .expect("write fake nft");
    let mut permissions = fs::metadata(&fake_nft)
        .expect("fake nft metadata")
        .permissions();
    permissions.set_mode(0o755);
    fs::set_permissions(&fake_nft, permissions).expect("chmod fake nft");

    let output = Command::new(env!("CARGO_BIN_EXE_kelp-pi-agent"))
        .args([
            "hardening",
            "apply-scanner-targets",
            "--target-ip",
            "10.42.0.21",
            "--target-ip",
            "10.42.0.20",
            "--target-ip",
            "10.42.0.20",
            "--nft-bin",
            fake_nft.to_str().expect("fake nft path utf8"),
        ])
        .env("KELP_PI_FAKE_NFT_STDIN", &nft_stdin)
        .output()
        .expect("run hardening apply scanner targets");

    assert!(
        output.status.success(),
        "stderr={}",
        String::from_utf8_lossy(&output.stderr)
    );
    let applied = fs::read_to_string(nft_stdin).expect("read nft stdin");
    assert!(applied.contains("flush set inet kelp_pi_filter scanner_ipv4_targets"));
    assert!(applied.contains(
        "add element inet kelp_pi_filter scanner_ipv4_targets { 10.42.0.20, 10.42.0.21 }"
    ));

    fs::remove_dir_all(root).ok();
}

fn temp_root(name: &str) -> PathBuf {
    let nonce = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .expect("time after epoch")
        .as_nanos();
    std::env::temp_dir().join(format!("kelp-pi-{name}-{}-{nonce}", std::process::id()))
}
