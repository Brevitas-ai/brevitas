# Homebrew formula for the Brevitas installer CLI (command: bvx).
#
# The command is named `bvx` to avoid colliding with the brevitas-systems
# Python package's own `brevitas` CLI.
#
# This installs a PREBUILT binary per platform — no Go toolchain or Xcode
# Command Line Tools are required. `brew install --HEAD` builds from source.
#
# The optimization logic (brevitas-systems) is a separate Python package and is
# NOT bundled here — `bvx install` / `bvx update` manage it via pip.
class Bvx < Formula
  desc "Middleware installer that routes AI coding assistants through Brevitas"
  homepage "https://github.com/Brevitas-ai/brevitas"
  version "0.1.29"
  license "MIT"

  # Bleeding-edge source build (requires Go, only used with --HEAD).
  head do
    url "https://github.com/Brevitas-ai/brevitas.git", branch: "main"
    depends_on "go" => :build
  end

  depends_on "python@3.13"

  on_macos do
    on_arm do
      url "https://github.com/Brevitas-ai/brevitas/releases/download/v0.1.29/bvx-0.1.29-darwin-arm64.tar.gz"
      sha256 "9849ec61dca6986aae121003d99a9e2c9538ec4f286b18b4b96c108d3be99bf8"
    end
    on_intel do
      url "https://github.com/Brevitas-ai/brevitas/releases/download/v0.1.29/bvx-0.1.29-darwin-amd64.tar.gz"
      sha256 "5656f7f23200f32d8a7a936f1cf8a67747efcbb922ee19dcdbde4594d06c2e1d"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/Brevitas-ai/brevitas/releases/download/v0.1.29/bvx-0.1.29-linux-arm64.tar.gz"
      sha256 "bddb46c7fa12e9e2a416259de7f5fe2cc340185b5447d4c709001e9ede52b9e1"
    end
    on_intel do
      url "https://github.com/Brevitas-ai/brevitas/releases/download/v0.1.29/bvx-0.1.29-linux-amd64.tar.gz"
      sha256 "a860216295b61b9b0e6a0f1ce5df011967aec9f361847e3279fa2d822b88284e"
    end
  end

  def install
    if build.head?
      ldflags = %W[
        -s -w
        -X github.com/Brevitas-ai/brevitas/internal/version.Version=HEAD
        -X github.com/Brevitas-ai/brevitas/internal/version.Date=#{time.iso8601}
      ]
      system "go", "build", *std_go_args(ldflags: ldflags, output: bin/"bvx"), "./cmd/bvx"
    else
      bin.install "bvx"
    end
  end

  def caveats
    <<~EOS
      Brevitas configures your AI coding tools to route through a local proxy.

      Next steps:
        bvx install     # sign in, detect tools, configure, start

      bvx installs and pins the brevitas-systems optimization engine automatically.
      Run `bvx update` later to keep it current.
    EOS
  end

  service do
    run [opt_bin/"bvx", "serve"]
    keep_alive true
    log_path var/"log/brevitas/proxy.out.log"
    error_log_path var/"log/brevitas/proxy.err.log"
  end

  test do
    assert_match "brevitas", shell_output("#{bin}/bvx version")
    assert_match "bvx onboard", shell_output("#{bin}/bvx help")
  end
end
