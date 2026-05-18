class Kelp < Formula
  desc "Strict, local-first planner CLI and Lazygit-style TUI"
  homepage "https://github.com/gongahkia/kelp"
  url "https://github.com/gongahkia/kelp/releases/download/v1.0.0/kelp-v1.0.0-source.tar.gz"
  sha256 "646fef1ab7b6a569a83eeb8a66473ba89f8a16b23648fc62ef1a73d9928a04ce"
  license "MIT"

  depends_on "zig" => :build

  def install
    system "zig", "build", "-Doptimize=ReleaseSafe", "--prefix", prefix
    generate_completions_from_executable(bin/"kelp", "completions")
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/kelp --version")
  end
end
