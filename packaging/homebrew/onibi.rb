# typed: strict
# frozen_string_literal: true

# Homebrew formula for the Onibi desktop app and CLI.
class Onibi < Formula
  desc "Cross-platform cockpit for local AI coding agents"
  homepage "https://github.com/gongahkia/onibi"
  version "1.5.0"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/gongahkia/onibi/releases/download/v1.5.0/Onibi-1.5.0-aarch64.dmg"
      sha256 :no_check
    end

    on_intel do
      url "https://github.com/gongahkia/onibi/releases/download/v1.5.0/Onibi-1.5.0-x86_64.dmg"
      sha256 :no_check
    end
  end

  def install
    mountpoint = buildpath/"mnt"
    mountpoint.mkpath
    system "hdiutil", "attach", cached_download, "-nobrowse", "-readonly", "-mountpoint", mountpoint
    begin
      app = Dir[mountpoint/"*.app"].first
      odie "Onibi.app not found in dmg" unless app
      prefix.install app
      bin.write_exec_script prefix/"Onibi.app/Contents/MacOS/onibi"
    ensure
      system "hdiutil", "detach", mountpoint, "-quiet"
    end
  end

  def caveats
    <<~EOS
      Run `onibi setup` for first-time pairing.
      The in-repo formula is mirrored to gongahkia/homebrew-onibi on each release.
    EOS
  end

  test do
    assert_match "onibi", shell_output("#{bin}/onibi --version")
  end
end
