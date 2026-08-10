wit_bindgen::generate!({
    world: "extension",
    path: "../../docs/wit",
});

use exports::zenbu::plugin::control::Guest;
use zenbu::plugin::types::{
    Invocation, PathSegment, PathSegmentKind, Registration, Value, ValueKind, ValueNode,
};

struct Conformance;

fn field(name: &str) -> PathSegment {
    PathSegment {
        kind: PathSegmentKind::Field,
        name: name.to_string(),
        index: 0,
    }
}

fn item(index: u32) -> PathSegment {
    PathSegment {
        kind: PathSegmentKind::Item,
        name: String::new(),
        index,
    }
}

fn extend(path: &[PathSegment], segment: PathSegment) -> Vec<PathSegment> {
    let mut result = path.to_vec();
    result.push(segment);
    result
}

fn node(path: Vec<PathSegment>, kind: ValueKind) -> ValueNode {
    ValueNode {
        path,
        kind,
        boolean: false,
        integer: 0,
        floating: 0.0,
        text: String::new(),
    }
}

fn text(path: Vec<PathSegment>, value: &str) -> ValueNode {
    let mut node = node(path, ValueKind::Text);
    node.text = value.to_string();
    node
}

fn integer(path: Vec<PathSegment>, value: i64) -> ValueNode {
    let mut node = node(path, ValueKind::Integer);
    node.integer = value;
    node
}

fn root_record() -> Value {
    vec![node(vec![], ValueKind::Fields)]
}

fn action(kind: &str) -> Value {
    let mut result = root_record();
    result.push(text(vec![field("kind")], kind));
    result
}

fn insert(text_value: &str) -> Value {
    let mut result = action("insert");
    result.push(text(vec![field("text")], text_value));
    result
}

fn message(text_value: &str) -> Value {
    let mut result = action("message");
    result.push(text(vec![field("text")], text_value));
    result
}

fn apply() -> Value {
    let mut result = action("apply");
    result.push(text(
        vec![field("selector")],
        "com.example.conformance.document",
    ));
    result.push(text(
        vec![field("transformation")],
        "com.example.conformance.done",
    ));
    result
}

fn same_segment(left: &PathSegment, right: &PathSegment) -> bool {
    left.kind == right.kind && left.name == right.name && left.index == right.index
}

fn same_path(left: &[PathSegment], right: &[PathSegment]) -> bool {
    left.len() == right.len()
        && left
            .iter()
            .zip(right.iter())
            .all(|(left, right)| same_segment(left, right))
}

fn integer_at(value: &Value, path: &[PathSegment]) -> Option<i64> {
    value
        .iter()
        .find(|node| node.kind == ValueKind::Integer && same_path(&node.path, path))
        .map(|node| node.integer)
}

fn document_selector(request: &Value) -> Value {
    let length_path = vec![
        field("context"),
        field("document"),
        field("length"),
    ];
    let length = integer_at(request, &length_path).unwrap_or(0);
    let mut result = root_record();
    let selections = vec![field("selections")];
    let selection = extend(&selections, item(0));
    result.push(node(selections.clone(), ValueKind::Items));
    result.push(node(selection.clone(), ValueKind::Fields));
    result.push(integer(extend(&selection, field("anchor")), 0));
    result.push(integer(extend(&selection, field("head")), length));
    result.push(integer(vec![field("primary")], 1));
    result
}

fn done_transformation(request: &Value) -> Value {
    let selection = vec![field("arguments"), field("selection_set"), item(0)];
    let anchor = integer_at(request, &extend(&selection, field("anchor"))).unwrap_or(0);
    let head = integer_at(request, &extend(&selection, field("head"))).unwrap_or(0);
    let mut result = root_record();
    let edits = vec![field("edits")];
    let edit = extend(&edits, item(0));
    result.push(node(edits.clone(), ValueKind::Items));
    result.push(node(edit.clone(), ValueKind::Fields));
    result.push(integer(extend(&edit, field("start")), anchor.min(head)));
    result.push(integer(extend(&edit, field("stop")), anchor.max(head)));
    result.push(text(extend(&edit, field("text")), "done"));
    result
}

fn invalid_action_list() -> Value {
    let mut result = vec![node(vec![], ValueKind::Items)];
    let first = vec![item(0)];
    let second = vec![item(1)];
    result.push(node(first.clone(), ValueKind::Fields));
    result.push(text(extend(&first, field("kind")), "insert"));
    result.push(text(extend(&first, field("text")), "must-not-commit"));
    result.push(node(second.clone(), ValueKind::Fields));
    result.push(text(extend(&second, field("kind")), "not-an-action"));
    result
}

fn invalid_selection() -> Value {
    let mut result = action("set-selections");
    let selections = vec![field("selections")];
    let selection = extend(&selections, item(0));
    result.push(node(selections.clone(), ValueKind::Items));
    result.push(node(selection.clone(), ValueKind::Fields));
    result.push(integer(extend(&selection, field("anchor")), -1));
    result.push(integer(extend(&selection, field("head")), -1));
    result.push(integer(vec![field("primary")], 1));
    result
}

fn oversized_value() -> Value {
    let mut result = vec![node(vec![], ValueKind::Items)];
    for index in 0..4_096 {
        result.push(text(vec![item(index)], "x"));
    }
    result
}

fn registration(contribution: &str, id: &str, callback: &str, input: &str, event: &str) -> Registration {
    Registration {
        contribution: contribution.to_string(),
        id: id.to_string(),
        callback: callback.to_string(),
        title: id.to_string(),
        description: "deterministic Component conformance fixture".to_string(),
        requires_syntax: false,
        input: input.to_string(),
        scope: if input.is_empty() { String::new() } else { "global".to_string() },
        event: event.to_string(),
    }
}

fn registrations() -> Vec<Registration> {
    vec![
        registration("commands", "com.example.conformance.insert", "insert", "", ""),
        registration("commands", "com.example.conformance.apply", "apply", "", ""),
        registration("commands", "com.example.conformance.invalid", "invalid", "", ""),
        registration("commands", "com.example.conformance.bad-selection", "bad-selection", "", ""),
        registration("commands", "com.example.conformance.trap", "trap", "", ""),
        registration("commands", "com.example.conformance.loop", "loop", "", ""),
        registration("commands", "com.example.conformance.memory", "memory", "", ""),
        registration("commands", "com.example.conformance.large", "large", "", ""),
        registration("selectors", "com.example.conformance.document", "document", "", ""),
        registration("transformations", "com.example.conformance.done", "done", "", ""),
        registration("bindings", "com.example.conformance.insert", "insert-binding", "Ctrl-K", ""),
        registration("bindings", "com.example.conformance.apply", "apply-binding", "Ctrl-A", ""),
        registration("bindings", "com.example.conformance.loop", "loop-binding", "Ctrl-L", ""),
        registration("events", "com.example.conformance.changed", "changed", "", "document-changed"),
    ]
}

impl Guest for Conformance {
    fn register() -> Result<Vec<Registration>, String> {
        Ok(registrations())
    }

    fn invoke(invocation: Invocation) -> Result<Value, String> {
        match invocation.callback.as_str() {
            "insert" => Ok(insert("!")),
            "apply" => Ok(apply()),
            "invalid" => Ok(invalid_action_list()),
            "bad-selection" => Ok(invalid_selection()),
            "document" => Ok(document_selector(&invocation.request)),
            "done" => Ok(done_transformation(&invocation.request)),
            "changed" => Ok(message("component event delivered")),
            "large" => Ok(oversized_value()),
            "trap" => panic!("intentional Component trap"),
            "loop" => {
                let mut value = 0_u64;
                loop {
                    value = value.wrapping_add(1);
                    core::hint::black_box(value);
                }
            }
            "memory" => loop {
                let allocation = vec![0_u8; 1024 * 1024];
                core::mem::forget(allocation);
            },
            callback => Err(format!("unknown callback {callback}")),
        }
    }
}

export!(Conformance);
