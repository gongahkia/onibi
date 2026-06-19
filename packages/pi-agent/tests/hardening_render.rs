use std::fs;
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

    fs::remove_dir_all(root).ok();
}

fn temp_root(name: &str) -> PathBuf {
    let nonce = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .expect("time after epoch")
        .as_nanos();
    std::env::temp_dir().join(format!("kelp-pi-{name}-{}-{nonce}", std::process::id()))
}
