use std::io::{self, Write};

fn main() {
    let output = kelp::run();

    if !output.stdout.is_empty() {
        write_stream(&mut io::stdout(), &output.stdout);
    }
    if !output.stderr.is_empty() {
        write_stream(&mut io::stderr(), &output.stderr);
    }

    std::process::exit(output.exit_code);
}

fn write_stream(stream: &mut impl Write, contents: &str) {
    if contents.ends_with('\n') {
        let _ = write!(stream, "{contents}");
    } else {
        let _ = writeln!(stream, "{contents}");
    }
}
