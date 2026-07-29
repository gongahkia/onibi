use minicbor::{Decoder, Encoder};

pub const MAX_EXTENSION_DATA_BYTES: usize = 65_536;
const EXTENSION_FRAME_FIELDS: u64 = 2;

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ExtensionFrame {
    id: u16,
    data: Vec<u8>,
}

impl ExtensionFrame {
    pub fn new(id: u16, data: Vec<u8>) -> Result<Self, ExtensionFrameError> {
        validate(id, &data)?;
        Ok(Self { id, data })
    }

    #[must_use]
    pub const fn id(&self) -> u16 {
        self.id
    }

    #[must_use]
    pub fn data(&self) -> &[u8] {
        &self.data
    }

    pub fn encode(&self) -> Result<Vec<u8>, ExtensionFrameError> {
        validate(self.id, &self.data)?;
        let mut encoder = Encoder::new(Vec::new());
        encoder
            .array(EXTENSION_FRAME_FIELDS)
            .map_err(|_| ExtensionFrameError::Encode)?
            .u16(self.id)
            .map_err(|_| ExtensionFrameError::Encode)?
            .bytes(&self.data)
            .map_err(|_| ExtensionFrameError::Encode)?;
        Ok(encoder.into_writer())
    }

    pub fn decode(encoded: &[u8]) -> Result<Self, ExtensionFrameError> {
        let mut decoder = Decoder::new(encoded);
        if decoder.array().map_err(|_| ExtensionFrameError::Decode)? != Some(EXTENSION_FRAME_FIELDS)
        {
            return Err(ExtensionFrameError::InvalidShape);
        }
        let id = decoder.u16().map_err(|_| ExtensionFrameError::Decode)?;
        let data = decoder.bytes().map_err(|_| ExtensionFrameError::Decode)?;
        if decoder.position() != encoded.len() {
            return Err(ExtensionFrameError::TrailingBytes);
        }
        Self::new(id, data.to_vec())
    }
}

#[derive(Debug, Eq, PartialEq, thiserror::Error)]
pub enum ExtensionFrameError {
    #[error("extension identifier must be nonzero")]
    InvalidIdentifier,
    #[error("extension data exceeds the configured limit")]
    DataTooLarge,
    #[error("extension frame must be a two-element definite-length CBOR array")]
    InvalidShape,
    #[error("CBOR decoding failed")]
    Decode,
    #[error("CBOR encoding failed")]
    Encode,
    #[error("trailing bytes after extension frame")]
    TrailingBytes,
}

fn validate(id: u16, data: &[u8]) -> Result<(), ExtensionFrameError> {
    if id == 0 {
        return Err(ExtensionFrameError::InvalidIdentifier);
    }
    if data.len() > MAX_EXTENSION_DATA_BYTES {
        return Err(ExtensionFrameError::DataTooLarge);
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::{ExtensionFrame, ExtensionFrameError, MAX_EXTENSION_DATA_BYTES};

    #[test]
    fn canonical_frame_round_trip() {
        let frame = ExtensionFrame::new(42, vec![1, 2]).unwrap();
        assert_eq!(frame.encode().unwrap(), [0x82, 0x18, 42, 0x42, 1, 2]);
        assert_eq!(
            ExtensionFrame::decode(&frame.encode().unwrap()).unwrap(),
            frame
        );
    }

    #[test]
    fn rejects_invalid_and_malformed_frames() {
        assert_eq!(
            ExtensionFrame::new(0, Vec::new()).unwrap_err(),
            ExtensionFrameError::InvalidIdentifier
        );
        assert_eq!(
            ExtensionFrame::new(1, vec![0; MAX_EXTENSION_DATA_BYTES + 1]).unwrap_err(),
            ExtensionFrameError::DataTooLarge
        );
        assert_eq!(
            ExtensionFrame::decode(&[0x9f, 1, 0x40, 0xff]).unwrap_err(),
            ExtensionFrameError::InvalidShape
        );
        assert_eq!(
            ExtensionFrame::decode(&[0x82, 1, 0x40, 0]).unwrap_err(),
            ExtensionFrameError::TrailingBytes
        );
    }
}
