wit_bindgen::generate!({
    world: "extension",
    path: "wit",
});

use exports::zenbu::plugin::control::Guest;
use zenbu::plugin::types::{Invocation, Registration, Value};

struct UnauthorizedImport;

impl Guest for UnauthorizedImport {
    fn register() -> Result<Vec<Registration>, String> {
        let _ = zenbu::plugin::document_read::slice(0, 0);
        Ok(vec![])
    }

    fn invoke(_: Invocation) -> Result<Value, String> {
        Err("this Component must not activate because its import is unavailable".to_string())
    }
}

export!(UnauthorizedImport);
