# Rust integrator example

This crate starts the embedded SDK, uses the operating system keystore, and creates or loads the local identity.

```sh
YEOKCHAM_STATE_DIRECTORY=/absolute/path/to/yeokcham-state \
  cargo run -p yeokcham-rust-integrator-example
```

The state directory must be absolute. The first successful run creates client state in that directory and an identity in the platform keystore: macOS Keychain, Linux Secret Service, or Windows Credential Manager. The example deliberately does not send a message because a recipient identity and encrypted envelope must be supplied by the integrator.
