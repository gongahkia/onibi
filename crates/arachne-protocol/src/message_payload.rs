use minicbor::{Decoder, Encoder};

use crate::wire::MAX_FRAME_BYTES;

pub const MAX_MESSAGE_PAYLOAD_BYTES: usize = MAX_FRAME_BYTES - 32;
const MESSAGE_PAYLOAD_FIELDS: u64 = 2;

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
#[repr(u8)]
pub enum MessageContentType {
    TextUtf8 = 1,
    Binary = 2,
}

impl TryFrom<u8> for MessageContentType {
    type Error = MessagePayloadError;

    fn try_from(value: u8) -> Result<Self, Self::Error> {
        match value {
            1 => Ok(Self::TextUtf8),
            2 => Ok(Self::Binary),
            _ => Err(MessagePayloadError::UnknownContentType(value)),
        }
    }
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct MessagePayload {
    content_type: MessageContentType,
    body: Vec<u8>,
}

impl MessagePayload {
    pub fn new(
        content_type: MessageContentType,
        body: Vec<u8>,
    ) -> Result<Self, MessagePayloadError> {
        validate_body(content_type, &body)?;
        Ok(Self { content_type, body })
    }

    #[must_use]
    pub const fn content_type(&self) -> MessageContentType {
        self.content_type
    }

    #[must_use]
    pub fn body(&self) -> &[u8] {
        &self.body
    }

    pub fn encode(&self) -> Result<Vec<u8>, MessagePayloadError> {
        validate_body(self.content_type, &self.body)?;
        let mut encoder = Encoder::new(Vec::new());
        encoder
            .array(MESSAGE_PAYLOAD_FIELDS)
            .map_err(|_| MessagePayloadError::Encode)?
            .u8(self.content_type as u8)
            .map_err(|_| MessagePayloadError::Encode)?
            .bytes(&self.body)
            .map_err(|_| MessagePayloadError::Encode)?;
        let output = encoder.into_writer();
        if output.len() > MAX_MESSAGE_PAYLOAD_BYTES {
            return Err(MessagePayloadError::PayloadTooLarge);
        }
        Ok(output)
    }

    pub fn decode(encoded: &[u8]) -> Result<Self, MessagePayloadError> {
        if encoded.len() > MAX_MESSAGE_PAYLOAD_BYTES {
            return Err(MessagePayloadError::PayloadTooLarge);
        }
        let mut decoder = Decoder::new(encoded);
        if decoder.array().map_err(|_| MessagePayloadError::Decode)? != Some(MESSAGE_PAYLOAD_FIELDS)
        {
            return Err(MessagePayloadError::InvalidShape);
        }
        let content_type =
            MessageContentType::try_from(decoder.u8().map_err(|_| MessagePayloadError::Decode)?)?;
        let body = decoder.bytes().map_err(|_| MessagePayloadError::Decode)?;
        if decoder.position() != encoded.len() {
            return Err(MessagePayloadError::TrailingBytes);
        }
        validate_body(content_type, body)?;
        Ok(Self {
            content_type,
            body: body.to_vec(),
        })
    }
}

#[derive(Debug, Eq, PartialEq, thiserror::Error)]
pub enum MessagePayloadError {
    #[error("message payload exceeds the configured limit")]
    PayloadTooLarge,
    #[error("message payload body is required")]
    MissingBody,
    #[error("text payload is not valid UTF-8")]
    InvalidUtf8Text,
    #[error("unknown message content type: {0}")]
    UnknownContentType(u8),
    #[error("message payload must be a two-element definite-length CBOR array")]
    InvalidShape,
    #[error("CBOR decoding failed")]
    Decode,
    #[error("CBOR encoding failed")]
    Encode,
    #[error("trailing bytes after message payload")]
    TrailingBytes,
}

fn validate_body(content_type: MessageContentType, body: &[u8]) -> Result<(), MessagePayloadError> {
    if body.is_empty() {
        return Err(MessagePayloadError::MissingBody);
    }
    if body.len() > MAX_MESSAGE_PAYLOAD_BYTES {
        return Err(MessagePayloadError::PayloadTooLarge);
    }
    if content_type == MessageContentType::TextUtf8 && std::str::from_utf8(body).is_err() {
        return Err(MessagePayloadError::InvalidUtf8Text);
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::{MessageContentType, MessagePayload, MessagePayloadError};

    #[test]
    fn canonical_text_payload_round_trip() {
        let payload = MessagePayload::new(MessageContentType::TextUtf8, b"hi".to_vec()).unwrap();
        let encoded = payload.encode().unwrap();
        assert_eq!(encoded, [0x82, 0x01, 0x42, b'h', b'i']);
        assert_eq!(MessagePayload::decode(&encoded).unwrap(), payload);
    }

    #[test]
    fn rejects_empty_invalid_and_unknown_content() {
        assert_eq!(
            MessagePayload::new(MessageContentType::Binary, Vec::new()).unwrap_err(),
            MessagePayloadError::MissingBody
        );
        assert_eq!(
            MessagePayload::decode(&[0x82, 0x01, 0x41, 0xff]).unwrap_err(),
            MessagePayloadError::InvalidUtf8Text
        );
        assert_eq!(
            MessagePayload::decode(&[0x82, 0x03, 0x41, 0x01]).unwrap_err(),
            MessagePayloadError::UnknownContentType(3)
        );
    }

    #[test]
    fn rejects_noncanonical_shapes_and_trailing_data() {
        assert_eq!(
            MessagePayload::decode(&[0x9f, 0x01, 0x41, 0x01, 0xff]).unwrap_err(),
            MessagePayloadError::InvalidShape
        );
        assert_eq!(
            MessagePayload::decode(&[0x82, 0x02, 0x41, 0x01, 0]).unwrap_err(),
            MessagePayloadError::TrailingBytes
        );
    }
}
