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
  version "0.1.8"
  license "MIT"

  on_macos do
    on_arm do
      url "https://github.com/Brevitas-ai/brevitas/releases/download/v0.1.8/bvx-0.1.8-darwin-arm64.tar.gz"
      sha256 "6d19176454e9c6c28a82f866ba677732174bbb94edae576827dc3b04ee10f236"
    end
    on_intel do
      url "https://github.com/Brevitas-ai/brevitas/releases/download/v0.1.8/bvx-0.1.8-darwin-amd64.tar.gz"
      sha256 "1a8d68d702fb5f82b7278212706de0c73e49042388dd71dc6e572f039a9324f4"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/Brevitas-ai/brevitas/releases/download/v0.1.8/bvx-0.1.8-linux-arm64.tar.gz"
      sha256 "abb98f1c145bea97df87f60de8bd5675eb3c04cce9f694fe2adebcb510195a28"
    end
    on_intel do
      url "https://github.com/Brevitas-ai/brevitas/releases/download/v0.1.8/bvx-0.1.8-linux-amd64.tar.gz"
      sha256 "a25dd028bfed4866507bd9f23ffd0896ebb8fa0c206897f507bf2f5ef43d04e8"
    end
  end

  # Bleeding-edge source build (requires Go, only used with --HEAD).
  head do
    url "https://github.com/Brevitas-ai/brevitas.git", branch: "main"
    depends_on "go" => :build
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
        bvx install     # detect tools, store your API key, configure, start

      The optimization engine (brevitas-systems) is a Python package:
        pip install brevitas-systems
        bvx update      # keep it current
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
    assert_match "Commands:", shell_output("#{bin}/bvx help")
  end
end
