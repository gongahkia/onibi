class Kelp < Formula
  desc "Strict, local-first planner CLI for tasks, projects, and reviews"
  homepage "https://github.com/gongahkia/kelp"
  url "https://github.com/gongahkia/kelp/releases/download/v1.0.0/kelp-v1.0.0-source.tar.gz"
  sha256 "97c99d06e920e5ce08a9e0dcb91ad6b03552996bb0ccbf2b49bdbc8113ee76da"
  license "MIT"

  depends_on "rust" => :build

  def install
    system "cargo", "install", *std_cargo_args(path: ".")
    generate_completions_from_executable(bin/"kelp", "completions")
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/kelp --version")
  end
end
