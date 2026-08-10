wit_bindgen::generate!({
    world: "extension",
    path: "../../docs/wit",
});

use exports::zenbu::plugin::control::Guest;
use zenbu::plugin::types::{
    Invocation, PathSegment, PathSegmentKind, Registration, Value, ValueKind, ValueNode,
};

struct Hello;

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

fn node(path: Vec<PathSegment>, kind: ValueKind, text: &str) -> ValueNode {
    ValueNode {
        path,
        kind,
        boolean: false,
        integer: 0,
        floating: 0.0,
        text: text.to_string(),
    }
}

fn insert_marker() -> Value {
    vec![
        node(vec![], ValueKind::Items, ""),
        node(vec![item(0)], ValueKind::Fields, ""),
        node(vec![item(0), field("kind")], ValueKind::Text, "insert"),
        node(vec![item(0), field("text")], ValueKind::Text, "[hello]"),
    ]
}

impl Guest for Hello {
    fn register() -> Result<Vec<Registration>, String> {
        Ok(vec![
            Registration {
                contribution: "commands".to_string(),
                id: "com.example.hello.insert".to_string(),
                callback: "insert".to_string(),
                title: "Insert Component hello".to_string(),
                description: "Insert a declarative marker.".to_string(),
                requires_syntax: false,
                input: String::new(),
                scope: String::new(),
                event: String::new(),
            },
            Registration {
                contribution: "bindings".to_string(),
                id: "com.example.hello.insert".to_string(),
                callback: "binding".to_string(),
                title: "Bind Component hello".to_string(),
                description: "Bind Ctrl-K to the command.".to_string(),
                requires_syntax: false,
                input: "Ctrl-K".to_string(),
                scope: "global".to_string(),
                event: String::new(),
            },
        ])
    }

    fn invoke(invocation: Invocation) -> Result<Value, String> {
        if invocation.callback == "insert" {
            Ok(insert_marker())
        } else {
            Err("unknown callback".to_string())
        }
    }
}

export!(Hello);
