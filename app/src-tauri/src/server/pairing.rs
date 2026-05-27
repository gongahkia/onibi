use anyhow::{Context, Result};
use qrcodegen::{QrCode, QrCodeEcc};
use serde_json::json;

pub fn pairing_payload(port: u16, token: &str) -> String {
    json!({
        "host": "127.0.0.1",
        "port": port,
        "token": token,
        "cert_fingerprint": null,
        "transports": ["loopback"]
    })
    .to_string()
}

pub fn qr_png(payload: &str) -> Result<Vec<u8>> {
    let qr = QrCode::encode_text(payload, QrCodeEcc::Medium).context("encode QR payload")?;
    let border = 4;
    let scale = 6;
    let modules = qr.size();
    let size = ((modules + border * 2) * scale) as usize;
    let mut pixels = vec![255u8; size * size];

    for y in 0..modules {
        for x in 0..modules {
            if qr.get_module(x, y) {
                let start_x = ((x + border) * scale) as usize;
                let start_y = ((y + border) * scale) as usize;
                for dy in 0..scale as usize {
                    for dx in 0..scale as usize {
                        pixels[(start_y + dy) * size + start_x + dx] = 0;
                    }
                }
            }
        }
    }

    let mut out = Vec::new();
    {
        let mut encoder = png::Encoder::new(&mut out, size as u32, size as u32);
        encoder.set_color(png::ColorType::Grayscale);
        encoder.set_depth(png::BitDepth::Eight);
        let mut writer = encoder.write_header().context("write PNG header")?;
        writer
            .write_image_data(&pixels)
            .context("write PNG payload")?;
    }
    Ok(out)
}
