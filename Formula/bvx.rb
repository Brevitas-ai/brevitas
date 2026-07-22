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
  version "0.1.27"
  license "MIT"

  # Bleeding-edge source build (requires Go, only used with --HEAD).
  head do
    url "https://github.com/Brevitas-ai/brevitas.git", branch: "main"
    depends_on "go" => :build
  end

  depends_on "python@3.13"

  on_macos do
    on_arm do
      url "https://github.com/Brevitas-ai/brevitas/releases/download/v0.1.27/bvx-0.1.27-darwin-arm64.tar.gz"
      sha256 "796709fdcb334ab1b98e824491d5f06289b5a27451d07a3ad9130a8e1757a2ed"
    end
    on_intel do
      url "https://github.com/Brevitas-ai/brevitas/releases/download/v0.1.27/bvx-0.1.27-darwin-amd64.tar.gz"
      sha256 "02366fcbd52ad8d6be857db73535764f2ffae9c72a88066efe5a249c33c7e9ce"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/Brevitas-ai/brevitas/releases/download/v0.1.27/bvx-0.1.27-linux-arm64.tar.gz"
      sha256 "73592ab534188fcebe6ae3213689d7ece08946fcc6879b860bbdb34bc94561f2"
    end
    on_intel do
      url "https://github.com/Brevitas-ai/brevitas/releases/download/v0.1.27/bvx-0.1.27-linux-amd64.tar.gz"
      sha256 "5a6424ed9b426abb6b4be6b11fef551ed549742a89c3687b696c85c36e66fa95"
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
