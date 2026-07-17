use std::{fmt, mem};

pub const BLE_FRAGMENT_PROTOCOL_VERSION: u8 = 1;
pub const BLE_FRAGMENT_HEADER_BYTES: usize = 9;
pub const MIN_BLE_GATT_CHARACTERISTIC_VALUE_BYTES: usize = 20;
pub const MAX_BLE_WIRE_FRAME_BYTES: usize = 65_536;
pub const MAX_BLE_FRAGMENT_COUNT: usize = 8_192;

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct BleFragmentLimits {
    maximum_fragment_bytes: usize,
}

impl BleFragmentLimits {
    pub fn new(maximum_fragment_bytes: usize) -> Result<Self, BleFrameError> {
        if !(MIN_BLE_GATT_CHARACTERISTIC_VALUE_BYTES..=MAX_BLE_WIRE_FRAME_BYTES)
            .contains(&maximum_fragment_bytes)
        {
            return Err(BleFrameError::InvalidMaximumFragmentBytes);
        }
        Ok(Self {
            maximum_fragment_bytes,
        })
    }

    #[must_use]
    pub const fn maximum_fragment_bytes(self) -> usize {
        self.maximum_fragment_bytes
    }

    const fn maximum_payload_bytes(self) -> usize {
        self.maximum_fragment_bytes - BLE_FRAGMENT_HEADER_BYTES
    }
}

pub struct BleFragment {
    frame_id: u32,
    index: u16,
    count: u16,
    payload: Vec<u8>,
}

impl fmt::Debug for BleFragment {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter.write_str("BleFragment(REDACTED)")
    }
}

impl BleFragment {
    pub fn fragment_wire_frame(
        frame: &[u8],
        frame_id: u32,
        limits: BleFragmentLimits,
    ) -> Result<Vec<Self>, BleFrameError> {
        if frame.is_empty() {
            return Err(BleFrameError::EmptyFrame);
        }
        if frame.len() > MAX_BLE_WIRE_FRAME_BYTES {
            return Err(BleFrameError::FrameTooLarge);
        }
        let count = frame.len().div_ceil(limits.maximum_payload_bytes());
        if count > MAX_BLE_FRAGMENT_COUNT || count > usize::from(u16::MAX) {
            return Err(BleFrameError::TooManyFragments);
        }
        let count = u16::try_from(count).map_err(|_| BleFrameError::TooManyFragments)?;
        frame
            .chunks(limits.maximum_payload_bytes())
            .enumerate()
            .map(|(index, payload)| {
                Ok(Self {
                    frame_id,
                    index: u16::try_from(index).map_err(|_| BleFrameError::TooManyFragments)?,
                    count,
                    payload: payload.to_vec(),
                })
            })
            .collect()
    }

    pub fn encode(&self, limits: BleFragmentLimits) -> Result<Vec<u8>, BleFrameError> {
        self.validate()?;
        let encoded_len = BLE_FRAGMENT_HEADER_BYTES
            .checked_add(self.payload.len())
            .ok_or(BleFrameError::FragmentTooLarge)?;
        if encoded_len > limits.maximum_fragment_bytes() {
            return Err(BleFrameError::FragmentTooLarge);
        }
        let mut encoded = Vec::with_capacity(encoded_len);
        encoded.push(BLE_FRAGMENT_PROTOCOL_VERSION);
        encoded.extend_from_slice(&self.frame_id.to_be_bytes());
        encoded.extend_from_slice(&self.index.to_be_bytes());
        encoded.extend_from_slice(&self.count.to_be_bytes());
        encoded.extend_from_slice(&self.payload);
        Ok(encoded)
    }

    pub fn decode(encoded: &[u8], limits: BleFragmentLimits) -> Result<Self, BleFrameError> {
        if encoded.len() < BLE_FRAGMENT_HEADER_BYTES + 1 {
            return Err(BleFrameError::FragmentTooSmall);
        }
        if encoded.len() > limits.maximum_fragment_bytes() {
            return Err(BleFrameError::FragmentTooLarge);
        }
        if encoded[0] != BLE_FRAGMENT_PROTOCOL_VERSION {
            return Err(BleFrameError::UnsupportedVersion(encoded[0]));
        }
        let fragment = Self {
            frame_id: u32::from_be_bytes([encoded[1], encoded[2], encoded[3], encoded[4]]),
            index: u16::from_be_bytes([encoded[5], encoded[6]]),
            count: u16::from_be_bytes([encoded[7], encoded[8]]),
            payload: encoded[BLE_FRAGMENT_HEADER_BYTES..].to_vec(),
        };
        fragment.validate()?;
        Ok(fragment)
    }

    #[must_use]
    pub const fn frame_id(&self) -> u32 {
        self.frame_id
    }

    #[must_use]
    pub const fn index(&self) -> u16 {
        self.index
    }

    #[must_use]
    pub const fn count(&self) -> u16 {
        self.count
    }

    #[must_use]
    pub fn payload(&self) -> &[u8] {
        &self.payload
    }

    fn validate(&self) -> Result<(), BleFrameError> {
        if self.count == 0 || usize::from(self.count) > MAX_BLE_FRAGMENT_COUNT {
            return Err(BleFrameError::InvalidFragmentCount);
        }
        if self.index >= self.count {
            return Err(BleFrameError::InvalidFragmentIndex);
        }
        if self.payload.is_empty() {
            return Err(BleFrameError::EmptyFragmentPayload);
        }
        Ok(())
    }
}

pub struct BleFrameReassembler {
    frame_id: Option<u32>,
    count: u16,
    next_index: u16,
    bytes: Vec<u8>,
}

impl fmt::Debug for BleFrameReassembler {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter.write_str("BleFrameReassembler(REDACTED)")
    }
}

impl Default for BleFrameReassembler {
    fn default() -> Self {
        Self::new()
    }
}

impl BleFrameReassembler {
    #[must_use]
    pub const fn new() -> Self {
        Self {
            frame_id: None,
            count: 0,
            next_index: 0,
            bytes: Vec::new(),
        }
    }

    pub fn push(&mut self, fragment: &BleFragment) -> Result<Option<Vec<u8>>, BleFrameError> {
        if let Err(error) = fragment.validate() {
            self.reset();
            return Err(error);
        }
        if let Some(frame_id) = self.frame_id {
            if frame_id != fragment.frame_id
                || self.count != fragment.count
                || self.next_index != fragment.index
            {
                self.reset();
                return Err(BleFrameError::UnexpectedFragment);
            }
        } else if fragment.index != 0 {
            return Err(BleFrameError::UnexpectedFragment);
        } else {
            self.frame_id = Some(fragment.frame_id);
            self.count = fragment.count;
        }
        let Some(new_len) = self.bytes.len().checked_add(fragment.payload.len()) else {
            self.reset();
            return Err(BleFrameError::ReassemblyTooLarge);
        };
        if new_len > MAX_BLE_WIRE_FRAME_BYTES {
            self.reset();
            return Err(BleFrameError::ReassemblyTooLarge);
        }
        self.bytes.extend_from_slice(&fragment.payload);
        self.next_index = self.next_index.saturating_add(1);
        if self.next_index == self.count {
            let frame = mem::take(&mut self.bytes);
            self.reset();
            return Ok(Some(frame));
        }
        Ok(None)
    }

    pub fn reset(&mut self) {
        self.frame_id = None;
        self.count = 0;
        self.next_index = 0;
        self.bytes.clear();
    }
}

#[derive(Clone, Debug, Eq, PartialEq, thiserror::Error)]
pub enum BleFrameError {
    #[error("maximum BLE fragment size is outside the supported range")]
    InvalidMaximumFragmentBytes,
    #[error("BLE wire frame is empty")]
    EmptyFrame,
    #[error("BLE wire frame exceeds the resource limit")]
    FrameTooLarge,
    #[error("BLE wire frame needs too many fragments")]
    TooManyFragments,
    #[error("BLE fragment is smaller than its header and payload")]
    FragmentTooSmall,
    #[error("BLE fragment exceeds the configured limit")]
    FragmentTooLarge,
    #[error("unsupported BLE fragment protocol version: {0}")]
    UnsupportedVersion(u8),
    #[error("BLE fragment count is invalid")]
    InvalidFragmentCount,
    #[error("BLE fragment index is invalid")]
    InvalidFragmentIndex,
    #[error("BLE fragment payload is empty")]
    EmptyFragmentPayload,
    #[error("BLE fragment is not the expected frame segment")]
    UnexpectedFragment,
    #[error("BLE frame reassembly exceeds the resource limit")]
    ReassemblyTooLarge,
}

#[cfg(test)]
mod tests {
    use super::{
        BLE_FRAGMENT_HEADER_BYTES, BLE_FRAGMENT_PROTOCOL_VERSION, BleFragment, BleFragmentLimits,
        BleFrameError, BleFrameReassembler, MAX_BLE_WIRE_FRAME_BYTES,
    };

    fn minimum_limits() -> BleFragmentLimits {
        BleFragmentLimits::new(20).unwrap()
    }

    #[test]
    fn round_trips_a_wire_frame_at_the_minimum_characteristic_value_size() {
        let frame = vec![0xa5; 100];
        let fragments = BleFragment::fragment_wire_frame(&frame, 7, minimum_limits()).unwrap();
        assert_eq!(fragments.len(), 10);
        let mut reassembler = BleFrameReassembler::new();
        let mut completed_frame = None;
        for fragment in fragments {
            let encoded = fragment.encode(minimum_limits()).unwrap();
            assert!(encoded.len() <= 20);
            completed_frame = reassembler
                .push(&BleFragment::decode(&encoded, minimum_limits()).unwrap())
                .unwrap()
                .or(completed_frame);
        }
        assert_eq!(completed_frame.unwrap(), frame);
    }

    #[test]
    fn rejects_malformed_and_out_of_order_fragments_then_recovers() {
        let limits = minimum_limits();
        let mut invalid = vec![BLE_FRAGMENT_PROTOCOL_VERSION + 1; BLE_FRAGMENT_HEADER_BYTES + 1];
        assert_eq!(
            BleFragment::decode(&invalid, limits).unwrap_err(),
            BleFrameError::UnsupportedVersion(BLE_FRAGMENT_PROTOCOL_VERSION + 1)
        );
        invalid[0] = BLE_FRAGMENT_PROTOCOL_VERSION;
        invalid[5..7].copy_from_slice(&1_u16.to_be_bytes());
        invalid[7..9].copy_from_slice(&1_u16.to_be_bytes());
        assert_eq!(
            BleFragment::decode(&invalid, limits).unwrap_err(),
            BleFrameError::InvalidFragmentIndex
        );

        let fragments = BleFragment::fragment_wire_frame(&[1; 12], 3, limits).unwrap();
        let mut reassembler = BleFrameReassembler::new();
        assert_eq!(
            reassembler
                .push(&BleFragment::decode(&fragments[1].encode(limits).unwrap(), limits).unwrap()),
            Err(BleFrameError::UnexpectedFragment)
        );
        assert_eq!(
            reassembler
                .push(&BleFragment::decode(&fragments[0].encode(limits).unwrap(), limits).unwrap()),
            Ok(None)
        );
        assert_eq!(
            reassembler
                .push(&BleFragment::decode(&fragments[1].encode(limits).unwrap(), limits).unwrap()),
            Ok(Some(vec![1; 12]))
        );
    }

    #[test]
    fn enforces_wire_and_fragment_bounds() {
        assert!(BleFragmentLimits::new(19).is_err());
        assert!(BleFragmentLimits::new(MAX_BLE_WIRE_FRAME_BYTES + 1).is_err());
        assert_eq!(
            BleFragment::fragment_wire_frame(&[], 1, minimum_limits()).unwrap_err(),
            BleFrameError::EmptyFrame
        );
        assert_eq!(
            BleFragment::fragment_wire_frame(
                &vec![0; MAX_BLE_WIRE_FRAME_BYTES + 1],
                1,
                minimum_limits(),
            )
            .unwrap_err(),
            BleFrameError::FrameTooLarge
        );
        assert_eq!(
            BleFragment::decode(
                &[BLE_FRAGMENT_PROTOCOL_VERSION; BLE_FRAGMENT_HEADER_BYTES],
                minimum_limits()
            )
            .unwrap_err(),
            BleFrameError::FragmentTooSmall
        );
    }

    #[test]
    fn debug_output_redacts_fragment_payloads() {
        let fragment = BleFragment::fragment_wire_frame(b"secret", 1, minimum_limits())
            .unwrap()
            .pop()
            .unwrap();
        assert_eq!(format!("{fragment:?}"), "BleFragment(REDACTED)");
        assert_eq!(
            format!("{:?}", BleFrameReassembler::new()),
            "BleFrameReassembler(REDACTED)"
        );
    }
}
