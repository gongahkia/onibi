use std::fmt;
use zeroize::Zeroize;

pub struct Secret<T: Zeroize>(T);

impl<T: Zeroize> Secret<T> {
    #[must_use]
    pub const fn new(value: T) -> Self {
        Self(value)
    }

    pub const fn expose_mut(&mut self) -> &mut T {
        &mut self.0
    }

    #[must_use]
    pub fn redacted(&self) -> impl tracing::field::Value + '_ {
        tracing::field::debug(self)
    }
}

impl<T: Zeroize> Drop for Secret<T> {
    fn drop(&mut self) {
        self.0.zeroize();
    }
}

impl<T: Zeroize> fmt::Debug for Secret<T> {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter.write_str("Secret(REDACTED)")
    }
}

#[cfg(test)]
mod tests {
    use super::Secret;
    use std::{
        io::{self, Write},
        sync::{Arc, Mutex},
    };
    use tracing::Dispatch;
    use tracing_subscriber::fmt::MakeWriter;

    #[derive(Clone, Default)]
    struct Buffer(Arc<Mutex<Vec<u8>>>);

    impl Write for Buffer {
        fn write(&mut self, bytes: &[u8]) -> io::Result<usize> {
            self.0.lock().expect("test log buffer lock").extend(bytes);
            Ok(bytes.len())
        }

        fn flush(&mut self) -> io::Result<()> {
            Ok(())
        }
    }

    impl<'writer> MakeWriter<'writer> for Buffer {
        type Writer = Self;

        fn make_writer(&'writer self) -> Self::Writer {
            self.clone()
        }
    }

    #[test]
    fn structured_events_redact_secret_fields() {
        let writer = Buffer::default();
        let subscriber = tracing_subscriber::fmt()
            .without_time()
            .with_writer(writer.clone())
            .finish();
        let dispatch = Dispatch::new(subscriber);
        let mut binary_credential = Secret::new(vec![0xA5; 32]);
        let binary_marker = format!("{:?}", binary_credential.expose_mut());

        tracing::dispatcher::with_default(&dispatch, || {
            let credential = Secret::new(String::from("not-for-logs"));
            tracing::info!(
                credential = credential.redacted(),
                binary_credential = binary_credential.redacted(),
                "stored"
            );
        });

        let output = String::from_utf8(writer.0.lock().expect("test log buffer lock").clone())
            .expect("test log output must be utf-8");
        assert!(output.contains("credential=Secret(REDACTED)"));
        assert!(output.contains("binary_credential=Secret(REDACTED)"));
        assert!(!output.contains("not-for-logs"));
        assert!(!output.contains(&binary_marker));
    }
}
