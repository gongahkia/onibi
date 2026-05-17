function handle(input, role) {
  const value = input;
  if (typeof value === "string") {
    if (value.length > 5) {
      if (value.includes("admin")) {
        switch (role) {
          case "admin":
            sink(value);
            break;
          default:
            sinkFallback(value);
        }
      }
    }
  }

  if (checkAccess(value, role)) {
    sink(value);
  }

  value === "expected" ? sink(value) : sinkFallback(value);
}
