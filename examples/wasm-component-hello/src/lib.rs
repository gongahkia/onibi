wit_bindgen::generate!({
    world: "extension",
    path: "wit",
});

mod zenbu_sdk;

use exports::zenbu::plugin::control::Guest;
use zenbu::plugin::types::{Invocation, Registration, Value};

struct Hello;

impl Guest for Hello {
    fn register() -> Result<Vec<Registration>, String> {
        Ok(vec![
            zenbu_sdk::registration(
                "commands",
                "com.example.hello.insert",
                "insert",
                "Insert Component hello",
                "Insert a declarative marker.",
            ),
            {
                let mut binding = zenbu_sdk::registration(
                    "bindings",
                    "com.example.hello.insert",
                    "binding",
                    "Bind Component hello",
                    "Bind Ctrl-K to the command.",
                );
                binding.input = "Ctrl-K".to_owned();
                binding.scope = "global".to_owned();
                binding
            },
        ])
    }

    fn invoke(invocation: Invocation) -> Result<Value, String> {
        if invocation.callback == "insert" {
            Ok(zenbu_sdk::insert("[hello]"))
        } else {
            Err("unknown callback".to_owned())
        }
    }
}

export!(Hello);
