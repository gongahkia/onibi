pub const DEFAULT_CHUNK_TARGET_TOKENS: usize = 512;
pub const DEFAULT_CHUNK_OVERLAP_TOKENS: usize = 64;

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct ChunkingConfig {
    pub target_tokens: usize,
    pub overlap_tokens: usize,
    pub heading_aware: bool,
}

pub fn default_chunking_config() -> ChunkingConfig {
    ChunkingConfig {
        target_tokens: DEFAULT_CHUNK_TARGET_TOKENS,
        overlap_tokens: DEFAULT_CHUNK_OVERLAP_TOKENS,
        heading_aware: true,
    }
}

pub fn deterministic_token_windows(text: &str, config: ChunkingConfig) -> Vec<String> {
    let tokens: Vec<&str> = text.split_whitespace().collect();
    if tokens.is_empty() {
        return Vec::new();
    }

    let target = config.target_tokens.max(1);
    let overlap = config.overlap_tokens.min(target.saturating_sub(1));
    let step = target - overlap;
    let mut chunks = Vec::new();
    let mut start = 0;

    loop {
        let end = (start + target).min(tokens.len());
        chunks.push(tokens[start..end].join(" "));
        if end == tokens.len() {
            break;
        }
        start += step;
    }

    chunks
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn token_windows_are_deterministic_with_default_parameters() {
        let input = (0..1_100)
            .map(|index| format!("token-{index}"))
            .collect::<Vec<_>>()
            .join(" ");
        let config = default_chunking_config();

        let first = deterministic_token_windows(&input, config);
        let second = deterministic_token_windows(&input, config);

        assert_eq!(first, second);
        assert_eq!(config.target_tokens, 512);
        assert_eq!(config.overlap_tokens, 64);
        assert!(config.heading_aware);
        assert_eq!(first[0].split_whitespace().count(), 512);
        assert_eq!(first[1].split_whitespace().next(), Some("token-448"));
    }
}
