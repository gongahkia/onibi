use crate::{Error, Result};

pub const MAX_FIXTURE_BYTES: usize = 1_048_576;

pub fn bytes(seed: u64, length: usize) -> Result<Vec<u8>> {
    if length > MAX_FIXTURE_BYTES {
        return Err(Error::ResourceLimit("test fixture exceeds maximum length"));
    }

    let mut state = seed;
    let mut output = Vec::with_capacity(length);
    while output.len() < length {
        let word = next_word(&mut state).to_le_bytes();
        let remaining = length - output.len();
        output.extend_from_slice(&word[..remaining.min(word.len())]);
    }
    Ok(output)
}

fn next_word(state: &mut u64) -> u64 {
    *state = state.wrapping_add(0x9e37_79b9_7f4a_7c15);
    let mut value = *state;
    value = (value ^ (value >> 30)).wrapping_mul(0xbf58_476d_1ce4_e5b9);
    value = (value ^ (value >> 27)).wrapping_mul(0x94d0_49bb_1331_11eb);
    value ^ (value >> 31)
}

#[cfg(test)]
mod tests {
    use super::{MAX_FIXTURE_BYTES, bytes};
    use crate::Error;

    #[test]
    fn emits_stable_bytes_for_a_seed() {
        assert_eq!(
            bytes(0, 16).expect("fixture must be generated"),
            [
                175, 205, 29, 123, 57, 168, 32, 226, 244, 101, 185, 161, 106, 158, 120, 110,
            ]
        );
        assert_ne!(
            bytes(1, 16).expect("fixture must be generated"),
            bytes(2, 16).expect("fixture must be generated")
        );
    }

    #[test]
    fn rejects_oversized_fixtures() {
        assert!(matches!(
            bytes(0, MAX_FIXTURE_BYTES + 1),
            Err(Error::ResourceLimit(_))
        ));
    }
}
