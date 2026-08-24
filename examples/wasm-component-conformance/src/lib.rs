wit_bindgen::generate!({
    world: "extension",
    path: "wit",
});

mod zenbu_sdk;

use exports::zenbu::plugin::control::Guest;
use zenbu::plugin::types::{Invocation, Registration, Value};

struct Conformance;

fn apply(selector: &str, transformation: &str) -> Value {
    zenbu_sdk::record([
        ("kind", zenbu_sdk::text("apply")),
        ("selector", zenbu_sdk::text(selector)),
        ("transformation", zenbu_sdk::text(transformation)),
    ])
}

fn document_selector(request: &Value) -> Value {
    let length = zenbu_sdk::integer_at(
        request,
        &[
            zenbu_sdk::field("context"),
            zenbu_sdk::field("document"),
            zenbu_sdk::field("length"),
        ],
    )
    .unwrap_or(0);
    zenbu_sdk::record([
        (
            "selections",
            zenbu_sdk::list([zenbu_sdk::record([
                ("anchor", zenbu_sdk::integer(0)),
                ("head", zenbu_sdk::integer(length)),
            ])]),
        ),
        ("primary", zenbu_sdk::integer(1)),
    ])
}

fn syntax_selector(request: &Value) -> Value {
    let start = zenbu_sdk::integer_at(
        request,
        &[
            zenbu_sdk::field("context"),
            zenbu_sdk::field("syntax"),
            zenbu_sdk::field("node"),
            zenbu_sdk::field("start"),
        ],
    )
    .unwrap_or(0);
    let stop = zenbu_sdk::integer_at(
        request,
        &[
            zenbu_sdk::field("context"),
            zenbu_sdk::field("syntax"),
            zenbu_sdk::field("node"),
            zenbu_sdk::field("stop"),
        ],
    )
    .unwrap_or(start);
    zenbu_sdk::record([
        (
            "selections",
            zenbu_sdk::list([zenbu_sdk::record([
                ("anchor", zenbu_sdk::integer(start)),
                ("head", zenbu_sdk::integer(stop)),
            ])]),
        ),
        ("primary", zenbu_sdk::integer(1)),
    ])
}

fn done_transformation(request: &Value) -> Value {
    let selection = [
        zenbu_sdk::field("arguments"),
        zenbu_sdk::field("selection_set"),
        zenbu_sdk::item(0),
    ];
    let anchor = zenbu_sdk::integer_at(
        request,
        &zenbu_sdk::extend(&selection, zenbu_sdk::field("anchor")),
    )
    .unwrap_or(0);
    let head = zenbu_sdk::integer_at(
        request,
        &zenbu_sdk::extend(&selection, zenbu_sdk::field("head")),
    )
    .unwrap_or(0);
    zenbu_sdk::record([(
        "edits",
        zenbu_sdk::list([zenbu_sdk::record([
            ("start", zenbu_sdk::integer(anchor.min(head))),
            ("stop", zenbu_sdk::integer(anchor.max(head))),
            ("text", zenbu_sdk::text("done")),
        ])]),
    )])
}

fn invalid_action_list() -> Value {
    zenbu_sdk::actions([
        zenbu_sdk::insert("must-not-commit"),
        zenbu_sdk::action("not-an-action"),
    ])
}

fn oversized_value() -> Value {
    zenbu_sdk::list((0..4_096).map(|_| zenbu_sdk::text("x")))
}

fn registration(contribution: &str, id: &str, callback: &str) -> Registration {
    zenbu_sdk::registration(
        contribution,
        id,
        callback,
        id,
        "deterministic Component conformance fixture",
    )
}

fn registrations() -> Vec<Registration> {
    let mut syntax = registration("selectors", "com.example.conformance.syntax", "syntax");
    syntax.requires_syntax = true;
    let mut insert_binding = registration(
        "bindings",
        "com.example.conformance.insert",
        "insert-binding",
    );
    insert_binding.input = "Ctrl-K".to_owned();
    insert_binding.scope = "global".to_owned();
    let mut apply_binding =
        registration("bindings", "com.example.conformance.apply", "apply-binding");
    apply_binding.input = "Ctrl-A".to_owned();
    apply_binding.scope = "global".to_owned();
    let mut command_binding = registration(
        "bindings",
        "com.example.conformance.invoke-command",
        "command-binding",
    );
    command_binding.input = "Ctrl-J".to_owned();
    command_binding.scope = "global".to_owned();
    let mut syntax_binding = registration(
        "bindings",
        "com.example.conformance.syntax-action",
        "syntax-binding",
    );
    syntax_binding.input = "Ctrl-Y".to_owned();
    syntax_binding.scope = "global".to_owned();
    let mut changed = registration("events", "com.example.conformance.changed", "changed");
    changed.event = "document-changed".to_owned();

    vec![
        registration("commands", "com.example.conformance.insert", "insert"),
        registration("commands", "com.example.conformance.apply", "apply"),
        registration("commands", "com.example.conformance.invalid", "invalid"),
        registration(
            "commands",
            "com.example.conformance.bad-selection",
            "bad-selection",
        ),
        registration("commands", "com.example.conformance.trap", "trap"),
        registration("commands", "com.example.conformance.loop", "loop"),
        registration("commands", "com.example.conformance.memory", "memory"),
        registration("commands", "com.example.conformance.large", "large"),
        registration(
            "commands",
            "com.example.conformance.invoke-command",
            "command",
        ),
        registration(
            "commands",
            "com.example.conformance.syntax-action",
            "syntax-action",
        ),
        registration("selectors", "com.example.conformance.document", "document"),
        registration("transformations", "com.example.conformance.done", "done"),
        syntax,
        insert_binding,
        apply_binding,
        command_binding,
        syntax_binding,
        changed,
    ]
}

impl Guest for Conformance {
    fn register() -> Result<Vec<Registration>, String> {
        Ok(registrations())
    }

    fn invoke(invocation: Invocation) -> Result<Value, String> {
        match invocation.callback.as_str() {
            "insert" => Ok(zenbu_sdk::insert("!")),
            "apply" => Ok(apply(
                "com.example.conformance.document",
                "com.example.conformance.done",
            )),
            "invalid" => Ok(invalid_action_list()),
            "bad-selection" => Ok(zenbu_sdk::set_selections([(-1, -1)], 1)),
            "document" => Ok(document_selector(&invocation.request)),
            "syntax" => Ok(syntax_selector(&invocation.request)),
            "syntax-action" => Ok(apply("com.example.conformance.syntax", "select")),
            "done" => Ok(done_transformation(&invocation.request)),
            "changed" => Ok(zenbu_sdk::message("component event delivered")),
            "command" => Ok(zenbu_sdk::invoke_command("com.example.conformance.insert")),
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
