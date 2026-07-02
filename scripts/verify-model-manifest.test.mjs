import assert from "node:assert/strict";
import { describe, it } from "node:test";
import { verifyReleaseModelManifestText } from "./verify-model-manifest.mjs";

const validSha = "a".repeat(64);

describe("verifyReleaseModelManifestText", () => {
  it("accepts blank sha256 for non-release fallback models", () => {
    const models = verifyReleaseModelManifestText(
      `
[[model]]
id = "primary"
sha256 = "${validSha}"
primary = true

[[model]]
id = "fallback"
sha256 = ""
primary = false
`,
      "models/manifest.toml"
    );

    assert.deepEqual(
      models.map((model) => model.id),
      ["primary"]
    );
  });

  it("rejects blank sha256 for primary models with model id and path", () => {
    assert.throws(
      () =>
        verifyReleaseModelManifestText(
          `
[[model]]
id = "blank-primary"
sha256 = ""
primary = true
`,
          "tmp/manifest.toml"
        ),
      /tmp\/manifest\.toml: release-required model blank-primary has blank sha256/u
    );
  });

  it("rejects invalid sha256 for release_required models", () => {
    assert.throws(
      () =>
        verifyReleaseModelManifestText(
          `
[[model]]
id = "required-fallback"
sha256 = "abc"
primary = false
release_required = true
`,
          "models/manifest.toml"
        ),
      /models\/manifest\.toml: release-required model required-fallback has invalid sha256/u
    );
  });
});
