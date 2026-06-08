use base64::{engine::general_purpose::STANDARD, Engine as _};
use serde_json::{json, Value};

use crate::pty::PtyOutputSnapshot;

pub(super) fn snapshot_json(snapshot: PtyOutputSnapshot) -> Value {
    json!({
        "data": STANDARD.encode(snapshot.data.as_ref()),
        "startOffset": snapshot.start_offset,
        "endOffset": snapshot.end_offset,
    })
}

pub(super) fn unwrap_recent_lines(text: &str, cols: usize) -> String {
    let cols = cols.max(1);
    let mut out = String::new();
    let mut previous_soft_wrapped = false;
    for line in text.lines() {
        if previous_soft_wrapped {
            out.push_str(line);
        } else {
            if !out.is_empty() {
                out.push('\n');
            }
            out.push_str(line);
        }
        previous_soft_wrapped = line.chars().count() >= cols && !line.trim().is_empty();
    }
    out
}

pub(super) fn tail_lines(text: &str, rows: usize) -> String {
    if rows == 0 {
        return String::new();
    }
    let mut lines = text.lines().rev().take(rows).collect::<Vec<_>>();
    lines.reverse();
    lines.join("\n")
}

#[derive(Debug, Clone)]
pub(super) struct TerminalScreen {
    rows: usize,
    cols: usize,
    cursor_row: usize,
    cursor_col: usize,
    cells: Vec<Vec<char>>,
    parser: ScreenParser,
}

#[derive(Debug, Clone)]
enum ScreenParser {
    Normal,
    Esc,
    Csi(String),
    Osc,
    OscEsc,
}

impl TerminalScreen {
    pub(super) fn new(rows: usize, cols: usize) -> Self {
        let rows = rows.max(1);
        let cols = cols.max(1);
        Self {
            rows,
            cols,
            cursor_row: 0,
            cursor_col: 0,
            cells: vec![vec![' '; cols]; rows],
            parser: ScreenParser::Normal,
        }
    }

    pub(super) fn resize(&mut self, rows: usize, cols: usize) {
        let rows = rows.max(1);
        let cols = cols.max(1);
        self.cells.resize_with(rows, || vec![' '; cols]);
        for row in &mut self.cells {
            row.resize(cols, ' ');
        }
        self.rows = rows;
        self.cols = cols;
        self.cursor_row = self.cursor_row.min(rows - 1);
        self.cursor_col = self.cursor_col.min(cols - 1);
    }

    pub(super) fn feed(&mut self, text: &str) {
        for ch in text.chars() {
            match std::mem::replace(&mut self.parser, ScreenParser::Normal) {
                ScreenParser::Normal => self.feed_normal(ch),
                ScreenParser::Esc => match ch {
                    '[' => self.parser = ScreenParser::Csi(String::new()),
                    ']' => self.parser = ScreenParser::Osc,
                    _ => self.parser = ScreenParser::Normal,
                },
                ScreenParser::Csi(mut raw) => {
                    if ch.is_ascii_alphabetic() || matches!(ch, '@' | '`' | '~') {
                        self.apply_csi(&raw, ch);
                        self.parser = ScreenParser::Normal;
                    } else {
                        raw.push(ch);
                        self.parser = ScreenParser::Csi(raw);
                    }
                }
                ScreenParser::Osc => {
                    self.parser = match ch {
                        '\u{7}' => ScreenParser::Normal,
                        '\u{1b}' => ScreenParser::OscEsc,
                        _ => ScreenParser::Osc,
                    };
                }
                ScreenParser::OscEsc => {
                    self.parser = if ch == '\\' {
                        ScreenParser::Normal
                    } else {
                        ScreenParser::Osc
                    };
                }
            }
        }
    }

    fn feed_normal(&mut self, ch: char) {
        match ch {
            '\u{1b}' => self.parser = ScreenParser::Esc,
            '\r' => self.cursor_col = 0,
            '\n' => {
                self.linefeed();
                self.cursor_col = 0;
            }
            '\u{8}' => self.cursor_col = self.cursor_col.saturating_sub(1),
            '\t' => {
                let next = ((self.cursor_col / 8) + 1) * 8;
                self.cursor_col = next.min(self.cols - 1);
            }
            ch if ch.is_control() => {}
            ch => self.put_char(ch),
        }
    }

    fn apply_csi(&mut self, raw: &str, command: char) {
        let params = csi_params(raw);
        match command {
            'A' => self.cursor_row = self.cursor_row.saturating_sub(param_or(&params, 0, 1)),
            'B' => self.cursor_row = (self.cursor_row + param_or(&params, 0, 1)).min(self.rows - 1),
            'C' => self.cursor_col = (self.cursor_col + param_or(&params, 0, 1)).min(self.cols - 1),
            'D' => self.cursor_col = self.cursor_col.saturating_sub(param_or(&params, 0, 1)),
            'G' | '`' => {
                self.cursor_col = param_or(&params, 0, 1).saturating_sub(1).min(self.cols - 1)
            }
            'H' | 'f' => {
                self.cursor_row = param_or(&params, 0, 1).saturating_sub(1).min(self.rows - 1);
                self.cursor_col = param_or(&params, 1, 1).saturating_sub(1).min(self.cols - 1);
            }
            'J' => {
                if param_or(&params, 0, 0) == 2 {
                    self.clear_all();
                }
            }
            'K' => self.clear_line(param_or(&params, 0, 0)),
            'm' | 'h' | 'l' | '?' => {}
            _ => {}
        }
    }

    fn put_char(&mut self, ch: char) {
        if self.cursor_row >= self.rows {
            self.cursor_row = self.rows - 1;
        }
        if self.cursor_col >= self.cols {
            self.linefeed();
            self.cursor_col = 0;
        }
        self.cells[self.cursor_row][self.cursor_col] = ch;
        self.cursor_col += 1;
        if self.cursor_col >= self.cols {
            self.linefeed();
            self.cursor_col = 0;
        }
    }

    fn linefeed(&mut self) {
        if self.cursor_row + 1 >= self.rows {
            self.cells.remove(0);
            self.cells.push(vec![' '; self.cols]);
        } else {
            self.cursor_row += 1;
        }
    }

    fn clear_all(&mut self) {
        for row in &mut self.cells {
            row.fill(' ');
        }
        self.cursor_row = 0;
        self.cursor_col = 0;
    }

    fn clear_line(&mut self, mode: usize) {
        let row = &mut self.cells[self.cursor_row];
        match mode {
            1 => {
                for cell in row.iter_mut().take(self.cursor_col + 1) {
                    *cell = ' ';
                }
            }
            2 => row.fill(' '),
            _ => {
                for cell in row.iter_mut().skip(self.cursor_col) {
                    *cell = ' ';
                }
            }
        }
    }

    pub(super) fn visible_text(&self) -> String {
        let mut lines = self
            .cells
            .iter()
            .map(|row| row.iter().collect::<String>().trim_end().to_string())
            .collect::<Vec<_>>();
        while lines.last().is_some_and(|line| line.is_empty()) {
            lines.pop();
        }
        lines.join("\n")
    }
}

fn csi_params(raw: &str) -> Vec<usize> {
    raw.trim_start_matches('?')
        .split(';')
        .map(|part| part.parse::<usize>().unwrap_or(0))
        .collect()
}

fn param_or(params: &[usize], index: usize, default: usize) -> usize {
    params
        .get(index)
        .copied()
        .filter(|value| *value > 0)
        .unwrap_or(default)
}
