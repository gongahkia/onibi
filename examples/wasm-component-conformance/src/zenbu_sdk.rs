#![allow(dead_code)]

//! Zenbu Component guest SDK 1.0.0.
//!
//! This module is copied into a guest package and deliberately refers to the
//! bindings generated in that guest crate.  Component exports must be emitted
//! by the final `cdylib`; putting them in a reusable Rust library would create
//! a second, incorrect ABI ownership boundary.

use crate::zenbu::plugin::types::{
    PathSegment, PathSegmentKind, Registration, Value, ValueKind, ValueNode,
};

pub const SDK_VERSION: &str = "1.0.0";
pub const WIT_PACKAGE: &str = "zenbu:plugin@1.0.0";
pub const WIT_BINDGEN_VERSION: &str = "0.41.0";
pub const CARGO_COMPONENT_VERSION: &str = "0.21.1";

pub const CAPABILITIES: &[&str] = &[
    "document.read",
    "document.edit",
    "selection.read",
    "selection.write",
    "syntax.read",
    "command.invoke",
    "ui.message",
    "event.subscribe",
];

pub fn field(name: &str) -> PathSegment {
    PathSegment {
        kind: PathSegmentKind::Field,
        name: name.to_owned(),
        index: 0,
    }
}

pub fn item(index: u32) -> PathSegment {
    PathSegment {
        kind: PathSegmentKind::Item,
        name: String::new(),
        index,
    }
}

pub fn extend(path: &[PathSegment], segment: PathSegment) -> Vec<PathSegment> {
    let mut result = path.to_vec();
    result.push(segment);
    result
}

pub fn node(path: Vec<PathSegment>, kind: ValueKind) -> ValueNode {
    ValueNode {
        path,
        kind,
        boolean: false,
        integer: 0,
        floating: 0.0,
        text: String::new(),
    }
}

pub fn nil() -> Value {
    vec![node(vec![], ValueKind::Nil)]
}

pub fn boolean(value: bool) -> Value {
    let mut node = node(vec![], ValueKind::Boolean);
    node.boolean = value;
    vec![node]
}

pub fn integer(value: i64) -> Value {
    let mut node = node(vec![], ValueKind::Integer);
    node.integer = value;
    vec![node]
}

pub fn floating(value: f64) -> Value {
    let mut node = node(vec![], ValueKind::Floating);
    node.floating = value;
    vec![node]
}

pub fn text(value: &str) -> Value {
    let mut node = node(vec![], ValueKind::Text);
    node.text = value.to_owned();
    vec![node]
}

fn rebase(prefix: &[PathSegment], value: Value) -> Vec<ValueNode> {
    value
        .into_iter()
        .map(|mut node| {
            let mut path = prefix.to_vec();
            path.append(&mut node.path);
            node.path = path;
            node
        })
        .collect()
}

pub fn list(values: impl IntoIterator<Item = Value>) -> Value {
    let mut result = vec![node(vec![], ValueKind::Items)];
    for (index, value) in values.into_iter().enumerate() {
        result.extend(rebase(&[item(index as u32)], value));
    }
    result
}

pub fn record(values: impl IntoIterator<Item = (&'static str, Value)>) -> Value {
    let mut result = vec![node(vec![], ValueKind::Fields)];
    for (name, value) in values {
        result.extend(rebase(&[field(name)], value));
    }
    result
}

pub fn action(kind: &str) -> Value {
    record([("kind", text(kind))])
}

pub fn insert(value: &str) -> Value {
    record([("kind", text("insert")), ("text", text(value))])
}

pub fn replace(value: &str) -> Value {
    record([("kind", text("replace")), ("text", text(value))])
}

pub fn message(value: &str) -> Value {
    record([("kind", text("message")), ("text", text(value))])
}

pub fn invoke_command(id: &str) -> Value {
    record([("kind", text("command")), ("id", text(id))])
}

pub fn set_selections(selections: impl IntoIterator<Item = (i64, i64)>, primary: i64) -> Value {
    let selections = selections
        .into_iter()
        .map(|(anchor, head)| record([("anchor", integer(anchor)), ("head", integer(head))]));
    record([
        ("kind", text("set-selections")),
        ("selections", list(selections)),
        ("primary", integer(primary)),
    ])
}

pub fn actions(values: impl IntoIterator<Item = Value>) -> Value {
    list(values)
}

pub fn registration(
    contribution: &str,
    id: &str,
    callback: &str,
    title: &str,
    description: &str,
) -> Registration {
    Registration {
        contribution: contribution.to_owned(),
        id: id.to_owned(),
        callback: callback.to_owned(),
        title: title.to_owned(),
        description: description.to_owned(),
        requires_syntax: false,
        input: String::new(),
        scope: String::new(),
        event: String::new(),
    }
}

fn same_path(left: &[PathSegment], right: &[PathSegment]) -> bool {
    left.len() == right.len()
        && left.iter().zip(right).all(|(left, right)| {
            left.kind == right.kind && left.name == right.name && left.index == right.index
        })
}

pub fn integer_at(value: &Value, path: &[PathSegment]) -> Option<i64> {
    value
        .iter()
        .find(|node| node.kind == ValueKind::Integer && same_path(&node.path, path))
        .map(|node| node.integer)
}

pub fn text_at<'a>(value: &'a Value, path: &[PathSegment]) -> Option<&'a str> {
    value
        .iter()
        .find(|node| node.kind == ValueKind::Text && same_path(&node.path, path))
        .map(|node| node.text.as_str())
}
