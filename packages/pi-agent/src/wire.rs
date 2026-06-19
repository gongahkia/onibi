use std::fmt::{Display, Formatter};

use base64ct::{Base64UrlUnpadded, Encoding};
use ed25519_dalek::{Signature, Signer, SigningKey, VerifyingKey};
use serde::{Deserialize, Serialize};
use serde_json::{Map, Value};

#[derive(Debug)]
pub enum EnvelopeError {
    Json(serde_json::Error),
    Base64(base64ct::Error),
    Signature(String),
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
#[serde(rename_all = "lowercase")]
pub enum PiEnvelopeSender {
    Pi,
    Cp,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub enum PiEnvelopeKind {
    #[serde(rename = "hello")]
    Hello,
    #[serde(rename = "welcome")]
    Welcome,
    #[serde(rename = "policy.pull")]
    PolicyPull,
    #[serde(rename = "policy.push")]
    PolicyPush,
    #[serde(rename = "scope.set")]
    ScopeSet,
    #[serde(rename = "scan.request")]
    ScanRequest,
    #[serde(rename = "scan.event")]
    ScanEvent,
    #[serde(rename = "scan.complete")]
    ScanComplete,
    #[serde(rename = "evidence.append")]
    EvidenceAppend,
    #[serde(rename = "bundle.export")]
    BundleExport,
    #[serde(rename = "bundle.fetch")]
    BundleFetch,
    #[serde(rename = "ask.query")]
    AskQuery,
    #[serde(rename = "ask.result")]
    AskResult,
    #[serde(rename = "selfcheck.run")]
    SelfcheckRun,
    #[serde(rename = "selfcheck.report")]
    SelfcheckReport,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq)]
pub struct UnsignedPiWireEnvelope {
    pub msg_id: String,
    pub ts: String,
    pub sender: PiEnvelopeSender,
    pub kind: PiEnvelopeKind,
    pub payload: Map<String, Value>,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq)]
pub struct PiWireEnvelope {
    pub msg_id: String,
    pub ts: String,
    pub sender: PiEnvelopeSender,
    pub kind: PiEnvelopeKind,
    pub payload: Map<String, Value>,
    pub sig: String,
}

pub fn sign_envelope(
    unsigned: UnsignedPiWireEnvelope,
    signing_key: &SigningKey,
) -> Result<PiWireEnvelope, EnvelopeError> {
    let canonical = canonical_unsigned_envelope_bytes(&unsigned)?;
    let signature: Signature = signing_key.sign(&canonical);

    Ok(PiWireEnvelope {
        msg_id: unsigned.msg_id,
        ts: unsigned.ts,
        sender: unsigned.sender,
        kind: unsigned.kind,
        payload: unsigned.payload,
        sig: Base64UrlUnpadded::encode_string(&signature.to_bytes()),
    })
}

pub fn verify_envelope(
    envelope: &PiWireEnvelope,
    verifying_key: &VerifyingKey,
) -> Result<(), EnvelopeError> {
    let unsigned = envelope.unsigned();
    let canonical = canonical_unsigned_envelope_bytes(&unsigned)?;
    let signature_bytes = Base64UrlUnpadded::decode_vec(&envelope.sig)?;
    let signature = Signature::try_from(signature_bytes.as_slice())
        .map_err(|error| EnvelopeError::Signature(error.to_string()))?;

    verifying_key
        .verify_strict(&canonical, &signature)
        .map_err(|error| EnvelopeError::Signature(error.to_string()))
}

pub fn canonical_unsigned_envelope_bytes(
    unsigned: &UnsignedPiWireEnvelope,
) -> Result<Vec<u8>, EnvelopeError> {
    let value = serde_json::to_value(unsigned)?;
    let canonical = canonicalize_json(value);
    serde_json::to_vec(&canonical).map_err(EnvelopeError::Json)
}

impl PiWireEnvelope {
    pub fn unsigned(&self) -> UnsignedPiWireEnvelope {
        UnsignedPiWireEnvelope {
            msg_id: self.msg_id.clone(),
            ts: self.ts.clone(),
            sender: self.sender.clone(),
            kind: self.kind.clone(),
            payload: self.payload.clone(),
        }
    }
}

fn canonicalize_json(value: Value) -> Value {
    match value {
        Value::Array(values) => Value::Array(values.into_iter().map(canonicalize_json).collect()),
        Value::Object(map) => {
            let mut entries: Vec<_> = map.into_iter().collect();
            entries.sort_by(|left, right| left.0.cmp(&right.0));
            let mut sorted = Map::new();
            for (key, value) in entries {
                sorted.insert(key, canonicalize_json(value));
            }
            Value::Object(sorted)
        }
        other => other,
    }
}

impl Display for EnvelopeError {
    fn fmt(&self, formatter: &mut Formatter<'_>) -> std::fmt::Result {
        match self {
            EnvelopeError::Json(error) => write!(formatter, "{error}"),
            EnvelopeError::Base64(error) => write!(formatter, "{error}"),
            EnvelopeError::Signature(message) => write!(formatter, "{message}"),
        }
    }
}

impl std::error::Error for EnvelopeError {}

impl From<serde_json::Error> for EnvelopeError {
    fn from(error: serde_json::Error) -> Self {
        EnvelopeError::Json(error)
    }
}

impl From<base64ct::Error> for EnvelopeError {
    fn from(error: base64ct::Error) -> Self {
        EnvelopeError::Base64(error)
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use rand_core::OsRng;
    use serde_json::json;

    #[test]
    fn signed_envelope_round_trips_and_corruption_fails() {
        let mut rng = OsRng;
        let signing_key = SigningKey::generate(&mut rng);
        let payload = json!({
            "protocol_version": "kelp-pi.v1",
            "device_id": "pi-a",
            "key_id": "sha256:0000000000000000000000000000000000000000000000000000000000000000",
            "agent_version": "0.1.0",
            "capabilities": ["selfcheck.report"]
        })
        .as_object()
        .expect("payload object")
        .clone();
        let unsigned = UnsignedPiWireEnvelope {
            msg_id: "msg-1".to_string(),
            ts: "2026-06-19T00:00:00Z".to_string(),
            sender: PiEnvelopeSender::Pi,
            kind: PiEnvelopeKind::Hello,
            payload,
        };

        let envelope = sign_envelope(unsigned, &signing_key).expect("sign envelope");

        assert_eq!(envelope.sig.len(), 86);
        verify_envelope(&envelope, &signing_key.verifying_key()).expect("verify envelope");

        let mut corrupted = envelope.clone();
        corrupted.payload.insert(
            "agent_version".to_string(),
            Value::String("9.9.9".to_string()),
        );

        assert!(verify_envelope(&corrupted, &signing_key.verifying_key()).is_err());
    }
}
