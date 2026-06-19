pub const DEFAULT_CHUNK_TARGET_TOKENS: usize = 512;
pub const DEFAULT_CHUNK_OVERLAP_TOKENS: usize = 64;

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ContentChunk {
    pub path: String,
    pub heading_path: Vec<String>,
    pub start_byte: usize,
    pub end_byte: usize,
    pub chunk_hash: String,
    pub chunk_id: String,
    pub content: String,
}

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

pub fn chunk_markdown(path: &str, markdown: &str, config: ChunkingConfig) -> Vec<ContentChunk> {
    let sections = markdown_sections(markdown);
    let mut chunks = Vec::new();

    for section in sections {
        let token_spans = token_spans(&markdown[section.start..section.end], section.start);
        chunks.extend(chunk_token_spans(
            path,
            markdown,
            &section.heading_path,
            &token_spans,
            config,
        ));
    }

    chunks
}

struct MarkdownSection {
    start: usize,
    end: usize,
    heading_path: Vec<String>,
}

#[derive(Debug, Clone, Copy)]
struct TokenSpan {
    start: usize,
    end: usize,
}

fn markdown_sections(markdown: &str) -> Vec<MarkdownSection> {
    let mut sections = Vec::new();
    let mut heading_path = Vec::new();
    let mut current_path = Vec::new();
    let mut current_start = 0;

    for (line_start, line_end) in line_ranges(markdown) {
        let line = &markdown[line_start..line_end];
        if let Some((level, title)) = markdown_heading(line) {
            if line_start > current_start {
                sections.push(MarkdownSection {
                    start: current_start,
                    end: line_start,
                    heading_path: current_path.clone(),
                });
            }
            heading_path.truncate(level.saturating_sub(1));
            heading_path.push(title);
            current_path = heading_path.clone();
            current_start = line_start;
        }
    }

    if current_start < markdown.len() {
        sections.push(MarkdownSection {
            start: current_start,
            end: markdown.len(),
            heading_path: current_path,
        });
    }

    sections
}

fn line_ranges(text: &str) -> Vec<(usize, usize)> {
    let mut ranges = Vec::new();
    let mut start = 0;
    for line in text.split_inclusive('\n') {
        let end = start + line.len();
        ranges.push((start, end));
        start = end;
    }
    if start < text.len() {
        ranges.push((start, text.len()));
    }
    ranges
}

fn markdown_heading(line: &str) -> Option<(usize, String)> {
    let trimmed = line.trim_start();
    let level = trimmed
        .chars()
        .take_while(|character| *character == '#')
        .count();
    if !(1..=6).contains(&level) {
        return None;
    }
    let rest = trimmed.get(level..)?;
    if !rest.starts_with(char::is_whitespace) {
        return None;
    }
    let title = rest.trim().trim_end_matches('#').trim();
    if title.is_empty() {
        return None;
    }
    Some((level, title.to_string()))
}

fn token_spans(text: &str, base_offset: usize) -> Vec<TokenSpan> {
    let mut spans = Vec::new();
    let mut token_start = None;

    for (index, character) in text.char_indices() {
        if character.is_whitespace() {
            if let Some(start) = token_start.take() {
                spans.push(TokenSpan {
                    start: base_offset + start,
                    end: base_offset + index,
                });
            }
        } else if token_start.is_none() {
            token_start = Some(index);
        }
    }

    if let Some(start) = token_start {
        spans.push(TokenSpan {
            start: base_offset + start,
            end: base_offset + text.len(),
        });
    }

    spans
}

fn chunk_token_spans(
    path: &str,
    source: &str,
    heading_path: &[String],
    token_spans: &[TokenSpan],
    config: ChunkingConfig,
) -> Vec<ContentChunk> {
    if token_spans.is_empty() {
        return Vec::new();
    }

    let target = config.target_tokens.max(1);
    let overlap = config.overlap_tokens.min(target.saturating_sub(1));
    let step = target - overlap;
    let mut chunks = Vec::new();
    let mut start = 0;

    loop {
        let end = (start + target).min(token_spans.len());
        let window = &token_spans[start..end];
        chunks.push(content_chunk(path, source, heading_path, window));
        if end == token_spans.len() {
            break;
        }
        start += step;
    }

    chunks
}

fn content_chunk(
    path: &str,
    source: &str,
    heading_path: &[String],
    token_spans: &[TokenSpan],
) -> ContentChunk {
    let content = token_spans
        .iter()
        .map(|span| &source[span.start..span.end])
        .collect::<Vec<_>>()
        .join(" ");
    let chunk_hash = blake3::hash(content.as_bytes()).to_hex().to_string();
    let chunk_id_seed = format!("{path}:{chunk_hash}");
    let chunk_id = blake3::hash(chunk_id_seed.as_bytes()).to_hex()[..16].to_string();

    ContentChunk {
        path: path.to_string(),
        heading_path: heading_path.to_vec(),
        start_byte: token_spans.first().expect("non-empty token window").start,
        end_byte: token_spans.last().expect("non-empty token window").end,
        chunk_hash,
        chunk_id,
        content,
    }
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

    #[test]
    fn markdown_chunks_include_heading_path_and_stable_hashes() {
        let markdown = "# Root\nalpha beta gamma delta epsilon zeta eta theta\n## Child\none two three four five six seven eight nine ten\n# Next\nred blue green\n";
        let config = ChunkingConfig {
            target_tokens: 6,
            overlap_tokens: 2,
            heading_aware: true,
        };

        let first = chunk_markdown("docs/example.md", markdown, config);
        let second = chunk_markdown("docs/example.md", markdown, config);

        assert_eq!(first, second);
        assert!(first.iter().any(|chunk| chunk.heading_path == ["Root"]));
        assert!(first
            .iter()
            .any(|chunk| chunk.heading_path == ["Root", "Child"]));
        assert!(first.iter().all(|chunk| chunk.path == "docs/example.md"));
        assert!(first
            .iter()
            .all(|chunk| chunk.start_byte < chunk.end_byte && !chunk.content.is_empty()));

        let child = first
            .iter()
            .find(|chunk| chunk.heading_path == ["Root", "Child"])
            .expect("child chunk");
        assert_eq!(
            child.chunk_hash,
            blake3::hash(child.content.as_bytes()).to_hex().to_string()
        );
        assert_eq!(child.chunk_id.len(), 16);
    }
}
