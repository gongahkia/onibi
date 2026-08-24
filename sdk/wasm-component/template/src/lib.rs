wit_bindgen::generate!({
    world: "extension",
    path: "wit",
});

mod zenbu_sdk;

use exports::zenbu::plugin::control::Guest;
use zenbu::plugin::types::{Invocation, Registration, Value};

struct Component;

impl Guest for Component {
    fn register() -> Result<Vec<Registration>, String> {
        Ok(vec![zenbu_sdk::registration(
            "commands",
            "com.example.component.hello",
            "hello",
            "Insert Component hello",
            "Insert a declarative marker through the Zenbu SDK.",
        )])
    }

    fn invoke(invocation: Invocation) -> Result<Value, String> {
        match invocation.callback.as_str() {
            "hello" => Ok(zenbu_sdk::insert("[hello]")),
            _ => Err("unknown callback".to_owned()),
        }
    }
}

export!(Component);
