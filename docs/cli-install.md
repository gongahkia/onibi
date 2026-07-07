# KelpClaw CLI Install

## npm

Install:

```sh
npm install -g @kelpclaw/cli
kelp-claw doctor
```

Upgrade:

```sh
npm update -g @kelpclaw/cli
kelp-claw version
```

Local release dry run:

```sh
pnpm release:cli:dry-run
pnpm test:cli-package
```

`release:cli:dry-run` and `test:cli-package` pack from
`.kelpclaw/release/cli/package`, not from `packages/cli` directly. The smoke
script builds a deployable CLI package, bundles the production dependency
closure, packs it, installs the tarball into a temporary npm prefix, and runs
`kelp-claw doctor`.

## Homebrew Tap Plan

Draft formula for a future `gongahkia/tap`:

```ruby
class KelpClaw < Formula
  desc "Local AppSec and agent-governance CLI"
  homepage "https://github.com/gongahkia/kelp"
  url "https://registry.npmjs.org/@kelpclaw/cli/-/cli-0.1.0.tgz"
  sha256 "<npm-tarball-sha256>"
  license "MIT"

  depends_on "node@20"

  def install
    system "npm", "install", "--global", "--prefix", libexec, cached_download
    bin.install_symlink Dir["#{libexec}/bin/kelp-claw"].first
  end

  test do
    assert_match "\"ok\": true", shell_output("#{bin}/kelp-claw doctor")
  end
end
```
