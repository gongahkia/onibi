// minimal OSC 9 / 99 / 777 notification extractor.
// not a full terminal parser: only scans for the OSC sequences we care about
// and returns structured events. it does not strip the bytes from the stream;
// the caller still forwards the original data to the frontend renderer.

use serde::Serialize;

const ESC: u8 = 0x1b;
const BEL: u8 = 0x07;
const OSC_INTRODUCER: u8 = b']';
const MAX_PAYLOAD: usize = 8 * 1024;

#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
#[serde(rename_all = "camelCase")]
pub struct OscNotification {
    pub source: NotificationSource,
    pub title: String,
    pub body: Option<String>,
    pub urgency: Option<String>,
}

#[derive(Debug, Clone, Copy, Serialize, PartialEq, Eq)]
#[serde(rename_all = "kebab-case")]
pub enum NotificationSource {
    Osc9,   // iTerm2 / VSCode
    Osc99,  // Kitty
    Osc777, // urxvt
}

#[derive(Debug)]
enum State {
    Normal,
    Esc,
    Osc(Vec<u8>),
    OscEsc(Vec<u8>),
}

pub struct OscNotificationParser {
    state: State,
}

impl Default for OscNotificationParser {
    fn default() -> Self {
        Self::new()
    }
}

impl OscNotificationParser {
    pub fn new() -> Self {
        Self {
            state: State::Normal,
        }
    }

    pub fn feed(&mut self, bytes: &[u8]) -> Vec<OscNotification> {
        let mut out = Vec::new();
        for &b in bytes {
            self.step(b, &mut out);
        }
        out
    }

    fn step(&mut self, b: u8, out: &mut Vec<OscNotification>) {
        let next = match std::mem::replace(&mut self.state, State::Normal) {
            State::Normal => {
                if b == ESC {
                    State::Esc
                } else {
                    State::Normal
                }
            }
            State::Esc => match b {
                OSC_INTRODUCER => State::Osc(Vec::with_capacity(64)),
                ESC => State::Esc,
                _ => State::Normal,
            },
            State::Osc(mut buf) => {
                if b == BEL {
                    if let Some(event) = parse_payload(&buf) {
                        out.push(event);
                    }
                    State::Normal
                } else if b == ESC {
                    State::OscEsc(buf)
                } else if buf.len() < MAX_PAYLOAD {
                    buf.push(b);
                    State::Osc(buf)
                } else {
                    // overflow; abandon this sequence
                    State::Normal
                }
            }
            State::OscEsc(buf) => {
                if b == b'\\' {
                    // ESC \ is the ST terminator
                    if let Some(event) = parse_payload(&buf) {
                        out.push(event);
                    }
                    State::Normal
                } else {
                    // not ST; re-enter Osc state but lose the buffer
                    // (this is a rare edge case where ESC appears mid-OSC)
                    State::Osc(buf)
                }
            }
        };
        self.state = next;
    }
}

fn parse_payload(buf: &[u8]) -> Option<OscNotification> {
    let text = std::str::from_utf8(buf).ok()?;
    let (code_str, rest) = text.split_once(';')?;
    let code: u32 = code_str.parse().ok()?;
    match code {
        9 => Some(OscNotification {
            source: NotificationSource::Osc9,
            title: rest.to_string(),
            body: None,
            urgency: None,
        }),
        99 => {
            // OSC 99 ; <metadata> ; <body>
            // metadata is colon-separated key=value pairs (Kitty spec).
            // commonly used keys: i=<id>, d=<title>, u=<urgency>, p=<payload-type>
            let (meta_part, body_part) = match rest.split_once(';') {
                Some((m, b)) => (m, Some(b.to_string())),
                None => (rest, None),
            };
            let mut title = String::new();
            let mut urgency = None;
            for pair in meta_part.split(':').filter(|s| !s.is_empty()) {
                if let Some((k, v)) = pair.split_once('=') {
                    match k {
                        "d" => title = v.to_string(),
                        "u" => urgency = Some(v.to_string()),
                        _ => {}
                    }
                }
            }
            if title.is_empty() {
                title = body_part.clone().unwrap_or_default();
            }
            Some(OscNotification {
                source: NotificationSource::Osc99,
                title,
                body: body_part,
                urgency,
            })
        }
        777 => {
            // OSC 777 ; notify ; <title> [ ; <body> ]
            let after = rest.strip_prefix("notify;")?;
            let (title, body) = match after.split_once(';') {
                Some((t, b)) => (t.to_string(), Some(b.to_string())),
                None => (after.to_string(), None),
            };
            Some(OscNotification {
                source: NotificationSource::Osc777,
                title,
                body,
                urgency: None,
            })
        }
        _ => None,
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parses_osc_9_bel_terminated() {
        let mut parser = OscNotificationParser::new();
        let events = parser.feed(b"\x1b]9;Build complete\x07rest");
        assert_eq!(events.len(), 1);
        assert_eq!(events[0].source, NotificationSource::Osc9);
        assert_eq!(events[0].title, "Build complete");
    }

    #[test]
    fn parses_osc_777_st_terminated() {
        let mut parser = OscNotificationParser::new();
        let events = parser.feed(b"\x1b]777;notify;Title;Body text\x1b\\");
        assert_eq!(events.len(), 1);
        assert_eq!(events[0].source, NotificationSource::Osc777);
        assert_eq!(events[0].title, "Title");
        assert_eq!(events[0].body.as_deref(), Some("Body text"));
    }

    #[test]
    fn parses_osc_99_with_metadata() {
        let mut parser = OscNotificationParser::new();
        let events = parser.feed(b"\x1b]99;d=Test Title:u=critical;The body\x07");
        assert_eq!(events.len(), 1);
        assert_eq!(events[0].source, NotificationSource::Osc99);
        assert_eq!(events[0].title, "Test Title");
        assert_eq!(events[0].urgency.as_deref(), Some("critical"));
        assert_eq!(events[0].body.as_deref(), Some("The body"));
    }

    #[test]
    fn handles_chunked_input() {
        let mut parser = OscNotificationParser::new();
        let mut events = Vec::new();
        events.extend(parser.feed(b"\x1b]9;Build "));
        events.extend(parser.feed(b"complete\x07"));
        assert_eq!(events.len(), 1);
        assert_eq!(events[0].title, "Build complete");
    }

    #[test]
    fn ignores_unrelated_osc() {
        let mut parser = OscNotificationParser::new();
        let events = parser.feed(b"\x1b]0;Window Title\x07");
        assert!(events.is_empty());
    }
}
